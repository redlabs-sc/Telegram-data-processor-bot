package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"telegram-archive-bot/app/extraction"
	"telegram-archive-bot/app/extraction/convert"
	"telegram-archive-bot/app/extraction/extract"
	"telegram-archive-bot/bot"
	"telegram-archive-bot/models"
	"telegram-archive-bot/storage"
)

// SequentialOrchestrator manages the sequential processing pipeline
// Extract → Convert → Store (one stage at a time)
type SequentialOrchestrator struct {
	logger       *logrus.Logger
	taskStore    *storage.TaskStore
	telegramBot  *bot.TelegramBot
	pollInterval time.Duration
}

// NewSequentialOrchestrator creates a new sequential processing orchestrator
func NewSequentialOrchestrator(
	logger *logrus.Logger,
	taskStore *storage.TaskStore,
	telegramBot *bot.TelegramBot,
) *SequentialOrchestrator {
	return &SequentialOrchestrator{
		logger:       logger,
		taskStore:    taskStore,
		telegramBot:  telegramBot,
		pollInterval: 10 * time.Second, // Check every 10 seconds
	}
}

// Start begins the sequential processing loop
func (so *SequentialOrchestrator) Start(ctx context.Context) error {
	so.logger.Info("Sequential orchestrator started")

	ticker := time.NewTicker(so.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			so.logger.Info("Sequential orchestrator stopped (context cancelled)")
			return ctx.Err()

		case <-ticker.C:
			// Run the processing stages sequentially
			if err := so.runProcessingCycle(ctx); err != nil {
				so.logger.WithError(err).Error("Processing cycle failed")
				// Continue to next cycle even if this one failed
			}

			// Send notifications for completed tasks
			if err := so.sendNotifications(); err != nil {
				so.logger.WithError(err).Error("Failed to send notifications")
			}
		}
	}
}

// runProcessingCycle executes all three stages in sequence
func (so *SequentialOrchestrator) runProcessingCycle(ctx context.Context) error {
	// Stage 1: Extract archives (files/all/ → files/pass/)
	if err := so.runExtractionStage(ctx); err != nil {
		so.logger.WithError(err).Error("Extraction stage failed")
		// Continue to next stage even if extraction failed
	}

	// Stage 2: Convert extracted files (files/pass/ → files/txt/)
	if err := so.runConversionStage(ctx); err != nil {
		so.logger.WithError(err).Error("Conversion stage failed")
		// Continue to next stage even if conversion failed
	}

	// Stage 3: Store text files (files/txt/ → database)
	if err := so.runStoreStage(ctx); err != nil {
		so.logger.WithError(err).Error("Store stage failed")
	}

	return nil
}

// runExtractionStage processes archive files in files/all/
func (so *SequentialOrchestrator) runExtractionStage(ctx context.Context) error {
	extractDir := "app/extraction/files/all"

	// Check if there are files to extract
	fileCount, err := so.countFilesInDirectory(extractDir)
	if err != nil {
		return fmt.Errorf("failed to count files in %s: %w", extractDir, err)
	}

	if fileCount == 0 {
		// No files to extract
		return nil
	}

	so.logger.WithField("file_count", fileCount).
		Info("Starting extraction stage")

	startTime := time.Now()

	// Run extract.go's main function (BLOCKS until complete)
	// This processes all files in app/extraction/files/all/
	extract.ExtractArchives()

	duration := time.Since(startTime)

	so.logger.WithFields(logrus.Fields{
		"duration_seconds": duration.Seconds(),
		"files_processed":  fileCount,
	}).Info("Extraction stage completed")

	// Update task statuses for extracted files
	// Note: We can't easily track which specific files were extracted
	// since extract.go doesn't return that info. Tasks will be marked
	// as COMPLETED when the store stage finishes.

	return nil
}

// runConversionStage converts extracted files in files/pass/
func (so *SequentialOrchestrator) runConversionStage(ctx context.Context) error {
	passDir := "app/extraction/files/pass"

	// Check if there are files to convert
	fileCount, err := so.countFilesInDirectory(passDir)
	if err != nil {
		return fmt.Errorf("failed to count files in %s: %w", passDir, err)
	}

	if fileCount == 0 {
		// No files to convert
		return nil
	}

	so.logger.WithField("file_count", fileCount).
		Info("Starting conversion stage")

	startTime := time.Now()

	// Run convert.go's main function (BLOCKS until complete)
	// This processes all files in app/extraction/files/pass/
	err = convert.ConvertTextFiles()

	duration := time.Since(startTime)

	if err != nil {
		so.logger.WithFields(logrus.Fields{
			"duration_seconds": duration.Seconds(),
			"error":            err.Error(),
		}).Error("Conversion stage failed")
		return fmt.Errorf("conversion failed: %w", err)
	}

	so.logger.WithFields(logrus.Fields{
		"duration_seconds": duration.Seconds(),
		"files_processed":  fileCount,
	}).Info("Conversion stage completed")

	return nil
}

// runStoreStage processes text files in files/txt/
func (so *SequentialOrchestrator) runStoreStage(ctx context.Context) error {
	txtDir := "app/extraction/files/txt"

	// Check if there are files to store
	fileCount, err := so.countFilesInDirectory(txtDir)
	if err != nil {
		return fmt.Errorf("failed to count files in %s: %w", txtDir, err)
	}

	if fileCount == 0 {
		// No files to store
		return nil
	}

	so.logger.WithField("file_count", fileCount).
		Info("Starting store stage")

	startTime := time.Now()

	// Create store service with logger function
	logFunc := func(format string, args ...interface{}) {
		so.logger.Infof(format, args...)
	}
	storeService := extraction.NewStoreService(logFunc)
	defer storeService.Close()

	// Create context with timeout (2 hours for large files)
	storeCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()

	// Run store pipeline (BLOCKS until complete)
	// This runs 4 stages: Move → Merge → Valuable → Backup
	err = storeService.RunPipeline(storeCtx)

	duration := time.Since(startTime)

	if err != nil {
		so.logger.WithFields(logrus.Fields{
			"duration_seconds": duration.Seconds(),
			"error":            err.Error(),
		}).Error("Store stage failed")
		return fmt.Errorf("store pipeline failed: %w", err)
	}

	so.logger.WithFields(logrus.Fields{
		"duration_seconds": duration.Seconds(),
		"files_processed":  fileCount,
	}).Info("Store stage completed")

	// Mark tasks as COMPLETED
	// All tasks that reached this stage are considered successful
	if err := so.markTasksCompleted(); err != nil {
		so.logger.WithError(err).Error("Failed to mark tasks as completed")
	}

	return nil
}

// markTasksCompleted marks all DOWNLOADED tasks as COMPLETED
// This is called after the store stage successfully completes
func (so *SequentialOrchestrator) markTasksCompleted() error {
	tasks, err := so.taskStore.GetByStatus(models.TaskStatusDownloaded)
	if err != nil {
		return fmt.Errorf("failed to get downloaded tasks: %w", err)
	}

	for _, task := range tasks {
		task.Status = models.TaskStatusCompleted
		now := time.Now()
		task.CompletedAt = &now
		task.UpdatedAt = now

		if err := so.taskStore.UpdateTask(task); err != nil {
			so.logger.WithField("task_id", task.ID).
				WithError(err).
				Error("Failed to update task to COMPLETED")
			continue
		}

		so.logger.WithFields(logrus.Fields{
			"task_id":   task.ID,
			"file_name": task.FileName,
		}).Info("Task marked as COMPLETED")
	}

	return nil
}

// sendNotifications sends completion notifications to users
func (so *SequentialOrchestrator) sendNotifications() error {
	if so.telegramBot == nil {
		return nil // Bot not initialized, skip notifications
	}

	return so.telegramBot.SendCompletionNotifications()
}

// countFilesInDirectory counts regular files in a directory (non-recursive)
func (so *SequentialOrchestrator) countFilesInDirectory(dir string) (int, error) {
	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// Directory doesn't exist, create it
		if err := os.MkdirAll(dir, 0755); err != nil {
			return 0, fmt.Errorf("failed to create directory: %w", err)
		}
		return 0, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			// Skip hidden files and .gitkeep files
			name := entry.Name()
			if !filepath.HasPrefix(name, ".") && name != ".gitkeep" {
				count++
			}
		}
	}

	return count, nil
}

// GetStats returns current orchestrator statistics
func (so *SequentialOrchestrator) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// Count files in each directory
	allCount, _ := so.countFilesInDirectory("app/extraction/files/all")
	passCount, _ := so.countFilesInDirectory("app/extraction/files/pass")
	txtCount, _ := so.countFilesInDirectory("app/extraction/files/txt")

	stats["files_awaiting_extraction"] = allCount
	stats["files_awaiting_conversion"] = passCount
	stats["files_awaiting_store"] = txtCount

	// Get task counts by status
	pending, _ := so.taskStore.GetTaskCountByStatus(models.TaskStatusPending)
	downloading, _ := so.taskStore.GetTaskCountByStatus(models.TaskStatusDownloading)
	downloaded, _ := so.taskStore.GetTaskCountByStatus(models.TaskStatusDownloaded)
	completed, _ := so.taskStore.GetTaskCountByStatus(models.TaskStatusCompleted)
	failed, _ := so.taskStore.GetTaskCountByStatus(models.TaskStatusFailed)

	stats["tasks_pending"] = pending
	stats["tasks_downloading"] = downloading
	stats["tasks_downloaded"] = downloaded
	stats["tasks_completed"] = completed
	stats["tasks_failed"] = failed

	return stats
}
