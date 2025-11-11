package pipeline

import (
	"context"
	"sync"
	"time"

	"telegram-archive-bot/models"
	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
)

type Pipeline struct {
	config         *utils.Config
	logger         *utils.Logger
	taskStore      *storage.TaskStore
	downloadPool   *WorkerPool
	extractionPool *WorkerPool
	conversionPool *WorkerPool
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

type PipelineConfig struct {
	DownloadWorkers   int
	ExtractionWorkers int // Always 1 for single-threaded bottleneck
	ConversionWorkers int
	WorkerTimeout     time.Duration
	QueueBufferSize   int
}

func NewPipeline(config *utils.Config, logger *utils.Logger, taskStore *storage.TaskStore) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())

	// Pipeline configuration based on PRD requirements
	pipelineConfig := &PipelineConfig{
		DownloadWorkers:   3,  // Concurrent downloads
		ExtractionWorkers: 1,  // Single-threaded bottleneck as per PRD
		ConversionWorkers: 2,  // Concurrent conversions
		WorkerTimeout:     30 * time.Minute,
		QueueBufferSize:   100,
	}

	p := &Pipeline{
		config:    config,
		logger:    logger,
		taskStore: taskStore,
		ctx:       ctx,
		cancel:    cancel,
	}

	// Initialize worker pools
	p.downloadPool = NewWorkerPool("download", pipelineConfig.DownloadWorkers, pipelineConfig.QueueBufferSize, logger)
	p.extractionPool = NewWorkerPool("extraction", pipelineConfig.ExtractionWorkers, pipelineConfig.QueueBufferSize, logger)
	p.conversionPool = NewWorkerPool("conversion", pipelineConfig.ConversionWorkers, pipelineConfig.QueueBufferSize, logger)

	return p
}

func (p *Pipeline) Start() error {
	p.logger.Info("Starting processing pipeline...")

	// Start all worker pools
	if err := p.downloadPool.Start(p.ctx); err != nil {
		return err
	}
	if err := p.extractionPool.Start(p.ctx); err != nil {
		return err
	}
	if err := p.conversionPool.Start(p.ctx); err != nil {
		return err
	}

	// Start pipeline coordinator
	p.wg.Add(1)
	go p.coordinator()

	p.logger.Info("Processing pipeline started successfully")
	return nil
}

func (p *Pipeline) Stop() {
	p.logger.Info("Stopping processing pipeline...")
	
	p.cancel()
	
	// Stop worker pools
	p.downloadPool.Stop()
	p.extractionPool.Stop()
	p.conversionPool.Stop()
	
	// Wait for coordinator to finish
	p.wg.Wait()
	
	p.logger.Info("Processing pipeline stopped")
}

func (p *Pipeline) SubmitTask(task *models.Task) error {
	p.logger.WithField("task_id", task.ID).
		WithField("file_name", task.FileName).
		Info("Submitting task to pipeline")

	// Create download job with normal priority
	job := &Job{
		ID:       task.ID,
		Type:     JobTypeDownload,
		Task:     task,
		Status:   JobStatusPending,
		Priority: PriorityNormal,
	}

	return p.downloadPool.Submit(job)
}

func (p *Pipeline) coordinator() {
	defer p.wg.Done()
	
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	// Auto-move ticker for file management
	autoMoveTicker := time.NewTicker(10 * time.Second)
	defer autoMoveTicker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.processCompletedJobs()
		case <-autoMoveTicker.C:
			p.autoMoveDownloadedFiles()
		}
	}
}

func (p *Pipeline) processCompletedJobs() {
	// Check for completed download jobs and submit to extraction
	for job := range p.downloadPool.GetCompletedJobs() {
		if job.Status == JobStatusCompleted {
			p.logger.WithField("task_id", job.Task.ID).Info("Download completed, submitting to extraction")
			
			extractionJob := &Job{
				ID:     job.ID,
				Type:   JobTypeExtraction,
				Task:   job.Task,
				Status: JobStatusPending,
			}
			
			if err := p.extractionPool.Submit(extractionJob); err != nil {
				p.logger.WithError(err).WithField("task_id", job.Task.ID).Error("Failed to submit extraction job")
			}
		}
	}

	// Check for completed extraction jobs and submit to conversion
	for job := range p.extractionPool.GetCompletedJobs() {
		if job.Status == JobStatusCompleted {
			p.logger.WithField("task_id", job.Task.ID).Info("Extraction completed, submitting to conversion")
			
			conversionJob := &Job{
				ID:     job.ID,
				Type:   JobTypeConversion,
				Task:   job.Task,
				Status: JobStatusPending,
			}
			
			if err := p.conversionPool.Submit(conversionJob); err != nil {
				p.logger.WithError(err).WithField("task_id", job.Task.ID).Error("Failed to submit conversion job")
			}
		}
	}

	// Process completed conversion jobs (final stage)
	for job := range p.conversionPool.GetCompletedJobs() {
		p.logger.WithField("task_id", job.Task.ID).
			WithField("status", job.Status).
			Info("Conversion completed - task finished")
		
		// Update final task status
		if job.Status == JobStatusCompleted {
			p.taskStore.UpdateStatus(job.Task.ID, models.TaskStatusCompleted, "")
		} else if job.Status == JobStatusFailed {
			p.taskStore.UpdateStatus(job.Task.ID, models.TaskStatusFailed, job.Error)
		}
	}
}

// autoMoveDownloadedFiles automatically moves downloaded files to extraction directories
func (p *Pipeline) autoMoveDownloadedFiles() {
	// This runs periodically to move files from Local Bot API temp to extraction directories
	// This ensures files are moved even if the download worker doesn't trigger the move immediately
	p.logger.Debug("Running auto-move for downloaded files")
	
	// Note: The actual auto-move logic is implemented in the download worker
	// This is just a monitoring function to ensure files don't get stuck
	
	// Get downloaded tasks count for monitoring
	downloadedTasks, err := p.taskStore.GetByStatus(models.TaskStatusDownloaded)
	if err != nil {
		p.logger.WithError(err).Debug("Failed to get downloaded tasks count for auto-move monitoring")
		return
	}
	
	if len(downloadedTasks) > 0 {
		p.logger.WithField("downloaded_tasks", len(downloadedTasks)).
			Debug("Found downloaded tasks that may need to be moved")
		// The actual movement will be handled by the download worker's auto-move functionality
		// or when extraction is triggered
	}
}

func (p *Pipeline) GetStats() PipelineStats {
	return PipelineStats{
		DownloadQueue:   p.downloadPool.GetQueueSize(),
		ExtractionQueue: p.extractionPool.GetQueueSize(),
		ConversionQueue: p.conversionPool.GetQueueSize(),
		ActiveWorkers: WorkerStats{
			Download:   p.downloadPool.GetActiveWorkers(),
			Extraction: p.extractionPool.GetActiveWorkers(),
			Conversion: p.conversionPool.GetActiveWorkers(),
		},
	}
}

type PipelineStats struct {
	DownloadQueue   int
	ExtractionQueue int
	ConversionQueue int
	ActiveWorkers   WorkerStats
}

type WorkerStats struct {
	Download   int
	Extraction int
	Conversion int
}