package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"telegram-archive-bot/models"
	"telegram-archive-bot/utils"
)

type JobType string

const (
	JobTypeDownload   JobType = "download"
	JobTypeExtraction JobType = "extraction"
	JobTypeConversion JobType = "conversion"
)

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

type JobPriority int

const (
	PriorityLow    JobPriority = 1
	PriorityNormal JobPriority = 2
	PriorityHigh   JobPriority = 3
	PriorityUrgent JobPriority = 4
)

type Job struct {
	ID        string
	Type      JobType
	Task      *models.Task
	Status    JobStatus
	Priority  JobPriority
	Error     string
	CreatedAt time.Time
	StartedAt *time.Time
	EndedAt   *time.Time
}

// Implement Job interface for workers
func (j *Job) GetID() string {
	return j.ID
}

func (j *Job) GetTask() *models.Task {
	return j.Task
}

func (j *Job) GetType() string {
	return string(j.Type)
}

func (j *Job) SetStatus(status string) {
	j.Status = JobStatus(status)
}

func (j *Job) SetError(error string) {
	j.Error = error
}

func (j *Job) GetCreatedAt() time.Time {
	return j.CreatedAt
}

func (j *Job) SetStartedAt(t time.Time) {
	j.StartedAt = &t
}

func (j *Job) SetEndedAt(t time.Time) {
	j.EndedAt = &t
}

type Worker interface {
	Process(ctx context.Context, job *Job) error
}

type WorkerPool struct {
	name           string
	workers        []Worker
	workerCount    int
	logger         *utils.Logger
	jobQueue       chan *Job
	completedJobs  chan *Job
	activeJobs     map[string]*Job
	activeWorkers  int
	timeout        time.Duration
	mutex          sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

func NewWorkerPool(name string, workerCount int, bufferSize int, logger *utils.Logger) *WorkerPool {
	return &WorkerPool{
		name:          name,
		workerCount:   workerCount,
		logger:        logger,
		jobQueue:      make(chan *Job, bufferSize),
		completedJobs: make(chan *Job, bufferSize),
		activeJobs:    make(map[string]*Job),
		timeout:       30 * time.Minute, // Default timeout
	}
}

func (wp *WorkerPool) SetTimeout(timeout time.Duration) {
	wp.timeout = timeout
}

func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.ctx, wp.cancel = context.WithCancel(ctx)
	
	wp.logger.WithField("pool", wp.name).
		WithField("workers", wp.workerCount).
		Info("Starting worker pool")

	// Start worker goroutines
	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		go wp.workerLoop(i)
	}

	return nil
}

func (wp *WorkerPool) Stop() {
	wp.logger.WithField("pool", wp.name).Info("Stopping worker pool")
	
	wp.cancel()
	close(wp.jobQueue)
	wp.wg.Wait()
	close(wp.completedJobs)
	
	wp.logger.WithField("pool", wp.name).Info("Worker pool stopped")
}

func (wp *WorkerPool) StopGracefully(timeout time.Duration) error {
	wp.logger.WithField("pool", wp.name).
		WithField("timeout", timeout).
		Info("Starting graceful shutdown of worker pool")
	
	// Stop accepting new jobs
	close(wp.jobQueue)
	
	// Wait for active jobs to complete or timeout
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		wp.logger.WithField("pool", wp.name).Info("All workers completed gracefully")
	case <-time.After(timeout):
		wp.logger.WithField("pool", wp.name).Warn("Graceful shutdown timeout, forcing shutdown")
		wp.cancel() // Force cancel remaining jobs
		wp.wg.Wait()
	}
	
	close(wp.completedJobs)
	wp.logger.WithField("pool", wp.name).Info("Worker pool stopped gracefully")
	return nil
}

func (wp *WorkerPool) Submit(job *Job) error {
	if job == nil {
		return fmt.Errorf("job cannot be nil")
	}

	job.CreatedAt = time.Now()
	job.Status = JobStatusPending

	select {
	case wp.jobQueue <- job:
		wp.logger.WithField("pool", wp.name).
			WithField("job_id", job.ID).
			WithField("job_type", job.Type).
			Debug("Job submitted to queue")
		return nil
	default:
		return fmt.Errorf("worker pool queue is full")
	}
}

func (wp *WorkerPool) workerLoop(workerID int) {
	defer wp.wg.Done()
	
	wp.logger.WithField("pool", wp.name).
		WithField("worker_id", workerID).
		Info("Worker started")

	for {
		select {
		case <-wp.ctx.Done():
			wp.logger.WithField("pool", wp.name).
				WithField("worker_id", workerID).
				Info("Worker stopping due to context cancellation")
			return
			
		case job, ok := <-wp.jobQueue:
			if !ok {
				wp.logger.WithField("pool", wp.name).
					WithField("worker_id", workerID).
					Info("Worker stopping due to closed job queue")
				return
			}
			
			wp.processJob(workerID, job)
		}
	}
}

func (wp *WorkerPool) processJob(workerID int, job *Job) {
	wp.mutex.Lock()
	wp.activeJobs[job.ID] = job
	wp.activeWorkers++
	wp.mutex.Unlock()

	defer func() {
		wp.mutex.Lock()
		delete(wp.activeJobs, job.ID)
		wp.activeWorkers--
		wp.mutex.Unlock()
	}()

	now := time.Now()
	job.StartedAt = &now
	job.Status = JobStatusRunning

	wp.logger.WithField("pool", wp.name).
		WithField("worker_id", workerID).
		WithField("job_id", job.ID).
		WithField("job_type", job.Type).
		WithField("timeout", wp.timeout).
		Info("Processing job")

	// Get appropriate worker for job type
	worker := wp.getWorker(job.Type)
	if worker == nil {
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("no worker available for job type: %s", job.Type)
		wp.logger.WithField("job_id", job.ID).Error(job.Error)
	} else {
		// Create timeout context for the job
		jobCtx, jobCancel := context.WithTimeout(wp.ctx, wp.timeout)
		defer jobCancel()

		// Process the job with timeout
		done := make(chan error, 1)
		go func() {
			done <- worker.Process(jobCtx, job)
		}()

		select {
		case err := <-done:
			if err != nil {
				job.Status = JobStatusFailed
				job.Error = err.Error()
				wp.logger.WithField("pool", wp.name).
					WithField("job_id", job.ID).
					WithError(err).
					Error("Job processing failed")
			} else {
				job.Status = JobStatusCompleted
				wp.logger.WithField("pool", wp.name).
					WithField("job_id", job.ID).
					Info("Job completed successfully")
			}
		case <-jobCtx.Done():
			job.Status = JobStatusFailed
			if jobCtx.Err() == context.DeadlineExceeded {
				job.Error = fmt.Sprintf("job timed out after %v", wp.timeout)
				wp.logger.WithField("pool", wp.name).
					WithField("job_id", job.ID).
					WithField("timeout", wp.timeout).
					Error("Job timed out")
			} else {
				job.Error = "job cancelled"
				wp.logger.WithField("pool", wp.name).
					WithField("job_id", job.ID).
					Info("Job cancelled")
			}
		}
	}

	endTime := time.Now()
	job.EndedAt = &endTime
	duration := endTime.Sub(*job.StartedAt)

	wp.logger.WithField("pool", wp.name).
		WithField("job_id", job.ID).
		WithField("duration", duration).
		WithField("status", job.Status).
		Info("Job processing completed")

	// Send completed job to completion channel
	select {
	case wp.completedJobs <- job:
	default:
		wp.logger.WithField("job_id", job.ID).Warn("Completed jobs channel is full, dropping job")
	}
}

func (wp *WorkerPool) SetWorkers(workers []Worker) {
	wp.workers = workers
}

func (wp *WorkerPool) getWorker(jobType JobType) Worker {
	// Return the first available worker - in a real implementation,
	// you might want to have different worker types
	if len(wp.workers) > 0 {
		return wp.workers[0]
	}
	return nil
}

func (wp *WorkerPool) GetQueueSize() int {
	return len(wp.jobQueue)
}

func (wp *WorkerPool) GetActiveWorkers() int {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()
	return wp.activeWorkers
}

func (wp *WorkerPool) GetCompletedJobs() <-chan *Job {
	return wp.completedJobs
}

func (wp *WorkerPool) GetActiveJobs() []*Job {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()
	
	jobs := make([]*Job, 0, len(wp.activeJobs))
	for _, job := range wp.activeJobs {
		jobs = append(jobs, job)
	}
	return jobs
}