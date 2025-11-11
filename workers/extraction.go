package workers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"telegram-archive-bot/app/extraction/extract"
	"telegram-archive-bot/models"
	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
)

type ExtractionWorker struct {
	config              *utils.Config
	logger              *utils.Logger
	taskStore           *storage.TaskStore
	timeout             time.Duration
	extractionDir       string
	isRunning           bool
	mutex               sync.Mutex
	circuitBreaker      *utils.SubprocessCircuitBreaker
	retryService        *utils.EnhancedRetryService
	degradationManager  *utils.GracefulDegradationManager
}

func NewExtractionWorker(config *utils.Config, logger *utils.Logger, taskStore *storage.TaskStore) *ExtractionWorker {
	degradationManager := utils.NewGracefulDegradationManager(logger)
	
	// Register extract.go and dependencies
	degradationManager.RegisterDependency("extract", "executable", 2*time.Minute, utils.FallbackQueue)
	degradationManager.RegisterDependency("go", "executable", 5*time.Minute, utils.FallbackManual)
	degradationManager.RegisterDependency("app/extraction", "directory", 1*time.Minute, utils.FallbackManual)
	
	return &ExtractionWorker{
		config:             config,
		logger:             logger,
		taskStore:          taskStore,
		timeout:            30 * time.Minute,
		extractionDir:      "app/extraction",
		circuitBreaker:     utils.NewSubprocessCircuitBreaker(logger),
		retryService:       utils.NewEnhancedRetryService(logger),
		degradationManager: degradationManager,
	}
}

// StartMonitoring begins dependency monitoring for graceful degradation
func (ew *ExtractionWorker) StartMonitoring(ctx context.Context) {
	ew.degradationManager.StartMonitoring(ctx)
}

// StopMonitoring stops dependency monitoring
func (ew *ExtractionWorker) StopMonitoring() {
	ew.degradationManager.StopMonitoring()
}

// GetDependencyHealth returns health status of extraction dependencies
func (ew *ExtractionWorker) GetDependencyHealth() (bool, []string) {
	return ew.degradationManager.GetSystemHealth()
}

func (ew *ExtractionWorker) Process(ctx context.Context, job Job) error {
	task := job.GetTask()

	// Ensure single-threaded execution as per PRD requirements
	ew.mutex.Lock()
	defer ew.mutex.Unlock()

	if ew.isRunning {
		return fmt.Errorf("extraction already in progress, queueing not allowed")
	}

	ew.isRunning = true
	defer func() { ew.isRunning = false }()

	ew.logger.WithField("task_id", task.ID).
		WithField("file_name", task.FileName).
		Info("Starting file extraction")

	// Verify file is already in the correct extraction directory (moved by download worker)
	if err := ew.verifyFileInExtractionDirectory(task); err != nil {
		return fmt.Errorf("file not found in extraction directory: %w", err)
	}

	// Handle different file types
	switch task.FileType {
	case "zip", "rar":
		return ew.extractArchive(ctx, task)
	case "txt":
		return ew.processTxtFile(ctx, task)
	default:
		return fmt.Errorf("unsupported file type for extraction: %s", task.FileType)
	}
}

func (ew *ExtractionWorker) verifyFileInExtractionDirectory(task *models.Task) error {
	var extractionFilePath string
	
	// Determine expected file location based on type (same routing as download worker)
	switch task.FileType {
	case "txt":
		// TXT files should be in files/txt/ directory
		extractionFilePath = filepath.Join(ew.extractionDir, "files", "txt", task.FileName)
	case "zip", "rar":
		// Archive files should be in files/all/ directory
		extractionFilePath = filepath.Join(ew.extractionDir, "files", "all", task.FileName)
	default:
		return fmt.Errorf("unsupported file type: %s", task.FileType)
	}

	// Verify the file exists in the expected location
	if _, err := os.Stat(extractionFilePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file %s not found in expected extraction directory %s (should have been placed by download worker)", task.FileName, extractionFilePath)
		}
		return fmt.Errorf("failed to access file in extraction directory: %w", err)
	}

	ew.logger.WithField("task_id", task.ID).
		WithField("file_path", extractionFilePath).
		WithField("file_type", task.FileType).
		Info("File verified in correct extraction directory")

	return nil
}

func (ew *ExtractionWorker) extractArchive(ctx context.Context, task *models.Task) error {
	ew.logger.WithField("task_id", task.ID).Info("Running extract.go subprocess with graceful degradation")

	// Check if extract.go is available
	if !ew.degradationManager.IsAvailable("extract") {
		ew.logger.WithField("task_id", task.ID).
			Warn("extract.go is unavailable, using graceful degradation")
		
		parameters := map[string]interface{}{
			"task_id":   task.ID,
			"file_name": task.FileName,
			"file_type": task.FileType,
		}
		
		degradationErr := ew.degradationManager.HandleUnavailableDependency("extract", "archive_extraction", parameters)
		if degradationErr != nil {
			return fmt.Errorf("graceful degradation for extract.go: %w", degradationErr)
		}
	}

	// If we reach here, extract.go should be available or fallback was handled
	// Create context with timeout
	extractCtx, cancel := context.WithTimeout(ctx, ew.timeout)
	defer cancel()

	// Use direct function call instead of subprocess execution
	if ew.degradationManager.IsAvailable("extract") {
		// Change to the extraction directory for relative path operations
		originalDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		
		if err := os.Chdir(ew.extractionDir); err != nil {
			return fmt.Errorf("failed to change to extraction directory: %w", err)
		}
		
		// Ensure we change back to original directory
		defer func() {
			if err := os.Chdir(originalDir); err != nil {
				ew.logger.WithError(err).Error("Failed to change back to original directory")
			}
		}()

		// Use a goroutine with context cancellation for timeout handling
		done := make(chan error, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Errorf("extraction panicked: %v", r)
				}
			}()
			
			// Call the extraction function directly
			extract.ExtractArchives()
			done <- nil
		}()

		select {
		case err := <-done:
			if err != nil {
				ew.logger.WithField("task_id", task.ID).
					WithError(err).
					Error("extraction function execution failed")
				
				// Mark dependency as potentially unavailable for degradation handling
				ew.degradationManager.HandleUnavailableDependency("extract", "archive_extraction", map[string]interface{}{
					"task_id": task.ID,
					"error":   err.Error(),
				})
				
				return fmt.Errorf("extraction failed: %w", err)
			}
		case <-extractCtx.Done():
			return fmt.Errorf("extraction timed out: %w", extractCtx.Err())
		}

		ew.logger.WithField("task_id", task.ID).
			Info("extraction completed successfully")
	} else {
		// Fallback was already handled above
		return fmt.Errorf("extraction unavailable and fallback executed")
	}

	// Check if files were extracted to files/pass directory
	passDir := filepath.Join(ew.extractionDir, "files", "pass")
	if err := ew.verifyExtractionOutput(passDir); err != nil {
		ew.logger.WithField("task_id", task.ID).
			WithError(err).
			Warn("No extracted files found in pass directory")
		
		// Check if file went to nopass directory (password-protected)
		nopassDir := filepath.Join(ew.extractionDir, "files", "nopass")
		if ew.hasFilesInDirectory(nopassDir) {
			return fmt.Errorf("archive is password-protected and could not be extracted")
		}
		
		return fmt.Errorf("extraction produced no output files")
	}

	return nil
}

func (ew *ExtractionWorker) processTxtFile(ctx context.Context, task *models.Task) error {
	ew.logger.WithField("task_id", task.ID).Info("Processing TXT file - already in txt directory")

	// TXT files are already moved directly to files/txt directory by moveFileToExtraction
	targetFile := filepath.Join(ew.extractionDir, "files", "txt", task.FileName)

	// Verify the file exists in the txt directory
	if _, err := os.Stat(targetFile); err != nil {
		return fmt.Errorf("TXT file not found in txt directory: %w", err)
	}

	ew.logger.WithField("task_id", task.ID).
		WithField("location", targetFile).
		Info("TXT file stored successfully in txt directory")

	return nil
}

func (ew *ExtractionWorker) verifyExtractionOutput(passDir string) error {
	files, err := filepath.Glob(filepath.Join(passDir, "*"))
	if err != nil {
		return fmt.Errorf("failed to check pass directory: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no files found in pass directory")
	}

	return nil
}

func (ew *ExtractionWorker) hasFilesInDirectory(dir string) bool {
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return false
	}
	return len(files) > 0
}

func (ew *ExtractionWorker) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	return err
}

func (ew *ExtractionWorker) IsRunning() bool {
	ew.mutex.Lock()
	defer ew.mutex.Unlock()
	return ew.isRunning
}

func (ew *ExtractionWorker) GetQueue() []string {
	// Since this is single-threaded, queue is managed by the pipeline
	// This method could be used to show pending extraction tasks
	return []string{} // TODO: Implement if needed
}

func (ew *ExtractionWorker) GetStats() ExtractionStats {
	return ExtractionStats{
		TotalExtractions: 0, // TODO: Implement stats collection
		IsRunning:       ew.IsRunning(),
		QueueSize:       len(ew.GetQueue()),
	}
}

type ExtractionStats struct {
	TotalExtractions int
	IsRunning       bool
	QueueSize       int
}