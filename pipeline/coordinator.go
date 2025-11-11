package pipeline

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-archive-bot/models"
	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
	"telegram-archive-bot/workers"
)

// WorkerAdapter adapts workers.Worker to pipeline.Worker interface
type WorkerAdapter struct {
	worker workers.Worker
}

func (wa *WorkerAdapter) Process(ctx context.Context, job *Job) error {
	return wa.worker.Process(ctx, job)
}

type PipelineCoordinator struct {
	pipeline       *Pipeline
	downloadWorker *workers.DownloadWorker
	extractWorker  *workers.ExtractionWorker
	convertWorker  *workers.ConversionWorker
	logger         *utils.Logger
}

func NewPipelineCoordinator(
	bot *tgbotapi.BotAPI,
	config *utils.Config,
	logger *utils.Logger,
	taskStore *storage.TaskStore,
) *PipelineCoordinator {
	// Create workers
	downloadWorker := workers.NewDownloadWorker(bot, config, logger, taskStore)
	extractWorker := workers.NewExtractionWorker(config, logger, taskStore)
	convertWorker := workers.NewConversionWorker(config, logger, taskStore)

	// Create pipeline
	pipeline := NewPipeline(config, logger, taskStore)

	// Set workers for each pool (convert to pipeline Workers)
	pipeline.downloadPool.SetWorkers([]Worker{&WorkerAdapter{worker: downloadWorker}})
	pipeline.extractionPool.SetWorkers([]Worker{&WorkerAdapter{worker: extractWorker}})
	pipeline.conversionPool.SetWorkers([]Worker{&WorkerAdapter{worker: convertWorker}})

	return &PipelineCoordinator{
		pipeline:       pipeline,
		downloadWorker: downloadWorker,
		extractWorker:  extractWorker,
		convertWorker:  convertWorker,
		logger:         logger,
	}
}

func (pc *PipelineCoordinator) Start(ctx context.Context) error {
	pc.logger.Info("Starting pipeline coordinator")
	
	// Start graceful degradation monitoring for workers
	pc.extractWorker.StartMonitoring(ctx)
	pc.convertWorker.StartMonitoring(ctx)
	
	return pc.pipeline.Start()
}

func (pc *PipelineCoordinator) Stop() {
	pc.logger.Info("Stopping pipeline coordinator")
	
	// Stop graceful degradation monitoring
	pc.extractWorker.StopMonitoring()
	pc.convertWorker.StopMonitoring()
	
	pc.pipeline.Stop()
}

func (pc *PipelineCoordinator) SubmitFileTask(task *models.Task) error {
	// Validate file before submitting to pipeline
	if err := pc.downloadWorker.ValidateFile(task); err != nil {
		pc.logger.WithField("task_id", task.ID).
			WithError(err).
			Error("File validation failed")
		return err
	}

	return pc.pipeline.SubmitTask(task)
}

func (pc *PipelineCoordinator) TriggerManualExtraction() error {
	pc.logger.Info("Manual extraction triggered")
	
	if pc.extractWorker.IsRunning() {
		return fmt.Errorf("extraction already in progress")
	}

	// Use download worker's auto-move functionality
	return pc.downloadWorker.MoveDownloadedFilesToExtraction()
}

func (pc *PipelineCoordinator) TriggerManualConversion(outputFile string) error {
	pc.logger.WithField("output_file", outputFile).Info("Manual conversion triggered")
	
	// Check conversion queue
	queue := pc.convertWorker.GetProcessingQueue()
	if len(queue) == 0 {
		return fmt.Errorf("no files available for conversion in files/pass directory")
	}

	pc.logger.WithField("queue_size", len(queue)).Info("Files ready for conversion")
	return nil
}

// moveFilesToExtraction is deprecated - use download worker's auto-move functionality
func (pc *PipelineCoordinator) moveFilesToExtraction() error {
	pc.logger.Info("Moving files using download worker auto-move functionality")
	return pc.downloadWorker.MoveDownloadedFilesToExtraction()
}

// moveFileBasedOnType is deprecated - file movement is now handled by download worker auto-move
func (pc *PipelineCoordinator) moveFileBasedOnType(task *models.Task) error {
	pc.logger.WithField("task_id", task.ID).
		Warn("moveFileBasedOnType is deprecated, using download worker auto-move instead")
	return pc.downloadWorker.MoveDownloadedFilesToExtraction()
}

func (pc *PipelineCoordinator) GetPipelineStats() PipelineStats {
	return pc.pipeline.GetStats()
}

func (pc *PipelineCoordinator) GetWorkerStats() WorkerStatsDetailed {
	return WorkerStatsDetailed{
		Download:   pc.downloadWorker.GetStats(),
		Extraction: pc.extractWorker.GetStats(),
		Conversion: pc.convertWorker.GetStats(),
	}
}

func (pc *PipelineCoordinator) IsExtractionRunning() bool {
	return pc.extractWorker.IsRunning()
}

func (pc *PipelineCoordinator) GetExtractionQueue() []string {
	return pc.extractWorker.GetQueue()
}

func (pc *PipelineCoordinator) GetConversionQueue() []string {
	return pc.convertWorker.GetProcessingQueue()
}

// GetExtractionWorker returns the extraction worker for external access
func (pc *PipelineCoordinator) GetExtractionWorker() *workers.ExtractionWorker {
	return pc.extractWorker
}

// GetConversionWorker returns the conversion worker for external access
func (pc *PipelineCoordinator) GetConversionWorker() *workers.ConversionWorker {
	return pc.convertWorker
}

type WorkerStatsDetailed struct {
	Download   workers.DownloadStats
	Extraction workers.ExtractionStats
	Conversion workers.ConversionStats
}