package workers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"telegram-archive-bot/app/extraction/convert"
	"telegram-archive-bot/models"
	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
)

type ConversionWorker struct {
	config             *utils.Config
	logger             *utils.Logger
	taskStore          *storage.TaskStore
	timeout            time.Duration
	extractionDir      string
	circuitBreaker     *utils.SubprocessCircuitBreaker
	retryService       *utils.EnhancedRetryService
	degradationManager *utils.GracefulDegradationManager
}

func NewConversionWorker(config *utils.Config, logger *utils.Logger, taskStore *storage.TaskStore) *ConversionWorker {
	degradationManager := utils.NewGracefulDegradationManager(logger)
	
	// Register convert.go and dependencies
	degradationManager.RegisterDependency("convert", "executable", 2*time.Minute, utils.FallbackQueue)
	degradationManager.RegisterDependency("go", "executable", 5*time.Minute, utils.FallbackManual)
	degradationManager.RegisterDependency("app/extraction/files/pass", "directory", 1*time.Minute, utils.FallbackManual)
	
	return &ConversionWorker{
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
func (cw *ConversionWorker) StartMonitoring(ctx context.Context) {
	cw.degradationManager.StartMonitoring(ctx)
}

// StopMonitoring stops dependency monitoring
func (cw *ConversionWorker) StopMonitoring() {
	cw.degradationManager.StopMonitoring()
}

// GetDependencyHealth returns health status of conversion dependencies
func (cw *ConversionWorker) GetDependencyHealth() (bool, []string) {
	return cw.degradationManager.GetSystemHealth()
}

func (cw *ConversionWorker) Process(ctx context.Context, job Job) error {
	task := job.GetTask()

	cw.logger.WithField("task_id", task.ID).
		WithField("file_name", task.FileName).
		Info("Starting file conversion")

	// Create context with timeout
	conversionCtx, cancel := context.WithTimeout(ctx, cw.timeout)
	defer cancel()

	// Generate output filename with task ID for uniqueness
	outputFileName := fmt.Sprintf("output_%s_%s.txt", task.ID, time.Now().Format("20060102_150405"))

	// Execute convert.go subprocess
	if err := cw.runConversion(conversionCtx, task, outputFileName); err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	// Verify conversion output and move files to appropriate directories
	if err := cw.processConversionResults(task, outputFileName); err != nil {
		return fmt.Errorf("failed to process conversion results: %w", err)
	}

	cw.logger.WithField("task_id", task.ID).
		WithField("output_file", outputFileName).
		Info("File conversion completed successfully")

	return nil
}

func (cw *ConversionWorker) runConversion(ctx context.Context, task *models.Task, outputFileName string) error {
	cw.logger.WithField("task_id", task.ID).
		WithField("output_file", outputFileName).
		Info("Running convert.go subprocess with graceful degradation")

	// Check if convert.go is available
	if !cw.degradationManager.IsAvailable("convert") {
		cw.logger.WithField("task_id", task.ID).
			Warn("convert.go is unavailable, using graceful degradation")
		
		parameters := map[string]interface{}{
			"task_id":     task.ID,
			"output_file": outputFileName,
			"file_name":   task.FileName,
		}
		
		degradationErr := cw.degradationManager.HandleUnavailableDependency("convert", "file_conversion", parameters)
		if degradationErr != nil {
			return fmt.Errorf("graceful degradation for convert.go: %w", degradationErr)
		}
	}

	// If we reach here, convert.go should be available or fallback was handled
	// Use direct function call instead of subprocess execution
	if cw.degradationManager.IsAvailable("convert") {
		// Change to the extraction directory for relative path operations
		originalDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		
		if err := os.Chdir(cw.extractionDir); err != nil {
			return fmt.Errorf("failed to change to extraction directory: %w", err)
		}
		
		// Ensure we change back to original directory
		defer func() {
			if err := os.Chdir(originalDir); err != nil {
				cw.logger.WithError(err).Error("Failed to change back to original directory")
			}
		}()

		// Use a goroutine with context cancellation for timeout handling
		done := make(chan error, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Errorf("conversion panicked: %v", r)
				}
			}()
			
			// Set environment variables for the conversion function
			os.Setenv("CONVERT_INPUT_DIR", "files/pass")
			os.Setenv("CONVERT_OUTPUT_FILE", filepath.Join("files/txt", outputFileName))
			
			// Call the conversion function directly
			err := convert.ConvertTextFiles()
			done <- err
		}()

		select {
		case err := <-done:
			if err != nil {
				cw.logger.WithField("task_id", task.ID).
					WithField("output_file", outputFileName).
					WithError(err).
					Error("conversion function execution failed")
				
				// Mark dependency as potentially unavailable for degradation handling
				cw.degradationManager.HandleUnavailableDependency("convert", "file_conversion", map[string]interface{}{
					"task_id": task.ID,
					"error":   err.Error(),
				})
				
				return fmt.Errorf("conversion failed: %w", err)
			}
		case <-ctx.Done():
			return fmt.Errorf("conversion timed out: %w", ctx.Err())
		}

		cw.logger.WithField("task_id", task.ID).
			WithField("output_file", outputFileName).
			Info("conversion completed successfully")
	} else {
		// Fallback was already handled above
		return fmt.Errorf("conversion unavailable and fallback executed")
	}

	return nil
}

func (cw *ConversionWorker) processConversionResults(task *models.Task, outputFileName string) error {
	// Check if main output file was created
	outputFilePath := filepath.Join(cw.extractionDir, outputFileName)
	if _, err := os.Stat(outputFilePath); err != nil {
		cw.logger.WithField("task_id", task.ID).
			WithField("output_file", outputFilePath).
			Warn("Main output file not found, checking special directories")
	} else {
		cw.logger.WithField("task_id", task.ID).
			WithField("output_file", outputFilePath).
			Info("Main output file created successfully")
	}

	// Check for special output directories as per PRD
	specialDirs := map[string]string{
		"done":    "files/done",     // Context for found search strings
		"errors":  "files/errors",   // Quarantined problematic files
		"etbanks": "files/etbanks",  // Files with search strings but no credentials
	}

	results := make(map[string]int)
	for dirName, dirPath := range specialDirs {
		fullPath := filepath.Join(cw.extractionDir, dirPath)
		fileCount, err := cw.countFilesInDirectory(fullPath)
		if err != nil {
			cw.logger.WithField("task_id", task.ID).
				WithField("directory", fullPath).
				WithError(err).
				Warn("Failed to check special directory")
			continue
		}
		results[dirName] = fileCount
		
		if fileCount > 0 {
			cw.logger.WithField("task_id", task.ID).
				WithField("directory", dirName).
				WithField("file_count", fileCount).
				Info("Files found in special directory")
		}
	}

	// Log conversion results summary
	cw.logger.WithField("task_id", task.ID).
		WithField("results", results).
		Info("Conversion results processed")

	// Clean up processed files from files/pass directory
	if err := cw.cleanupProcessedFiles(task); err != nil {
		cw.logger.WithField("task_id", task.ID).
			WithError(err).
			Warn("Failed to cleanup processed files")
	}

	return nil
}

func (cw *ConversionWorker) countFilesInDirectory(dirPath string) (int, error) {
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return 0, nil
	}

	files, err := filepath.Glob(filepath.Join(dirPath, "*"))
	if err != nil {
		return 0, err
	}

	count := 0
	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			count++
		}
	}

	return count, nil
}

func (cw *ConversionWorker) cleanupProcessedFiles(task *models.Task) error {
	passDir := filepath.Join(cw.extractionDir, "files", "pass")
	
	// Find files related to this task
	files, err := filepath.Glob(filepath.Join(passDir, "*"))
	if err != nil {
		return fmt.Errorf("failed to list files in pass directory: %w", err)
	}

	cleanedCount := 0
	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			// Remove the file since it has been processed
			if err := os.Remove(file); err != nil {
				cw.logger.WithField("file", file).
					WithError(err).
					Warn("Failed to remove processed file")
			} else {
				cleanedCount++
			}
		}
	}

	if cleanedCount > 0 {
		cw.logger.WithField("task_id", task.ID).
			WithField("cleaned_files", cleanedCount).
			Info("Cleaned up processed files from pass directory")
	}

	return nil
}

func (cw *ConversionWorker) GetProcessingQueue() []string {
	// Return list of files currently in files/pass directory waiting for conversion
	passDir := filepath.Join(cw.extractionDir, "files", "pass")
	files, err := filepath.Glob(filepath.Join(passDir, "*"))
	if err != nil {
		cw.logger.WithError(err).Error("Failed to get processing queue")
		return []string{}
	}

	var queue []string
	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			queue = append(queue, filepath.Base(file))
		}
	}

	return queue
}

func (cw *ConversionWorker) GetStats() ConversionStats {
	return ConversionStats{
		TotalConversions:   0, // TODO: Implement stats collection
		ActiveConversions:  0,
		QueueSize:         len(cw.GetProcessingQueue()),
		FailedConversions: 0,
	}
}

type ConversionStats struct {
	TotalConversions   int
	ActiveConversions  int
	QueueSize         int
	FailedConversions int
}