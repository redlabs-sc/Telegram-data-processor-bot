package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"telegram-archive-bot/models"
	"telegram-archive-bot/utils"
)

type RecoveryService struct {
	taskStore         *TaskStore
	logger            *utils.Logger
	botAPIPathManager *utils.BotAPIPathManager
}

func NewRecoveryService(taskStore *TaskStore, logger *utils.Logger, botAPIPathManager *utils.BotAPIPathManager) *RecoveryService {
	return &RecoveryService{
		taskStore:         taskStore,
		logger:            logger,
		botAPIPathManager: botAPIPathManager,
	}
}

func (rs *RecoveryService) RecoverIncompleteTasks(ctx context.Context) error {
	rs.logger.Info("Starting crash recovery - checking for incomplete tasks")

	// Get tasks that are not completed or failed
	pendingTasks, err := rs.taskStore.GetByStatus(models.TaskStatusPending)
	if err != nil {
		return fmt.Errorf("failed to get pending tasks: %w", err)
	}

	downloadedTasks, err := rs.taskStore.GetByStatus(models.TaskStatusDownloaded)
	if err != nil {
		return fmt.Errorf("failed to get downloaded tasks: %w", err)
	}

	allIncompleteTasks := append(pendingTasks, downloadedTasks...)

	if len(allIncompleteTasks) == 0 {
		rs.logger.Info("No incomplete tasks found - system is clean")
		return nil
	}

	rs.logger.WithField("incomplete_tasks", len(allIncompleteTasks)).
		Info("Found incomplete tasks, starting recovery process")

	recoveredCount := 0
	failedCount := 0

	for _, task := range allIncompleteTasks {
		rs.logger.WithField("task_id", task.ID).
			WithField("status", task.Status).
			WithField("file_name", task.FileName).
			Info("Recovering task")

		if err := rs.recoverTask(ctx, task); err != nil {
			rs.logger.WithField("task_id", task.ID).
				WithError(err).
				Error("Failed to recover task")
			
			// Mark task as failed
			if updateErr := rs.taskStore.UpdateStatus(task.ID, models.TaskStatusFailed, 
				fmt.Sprintf("Recovery failed: %v", err)); updateErr != nil {
				rs.logger.WithField("task_id", task.ID).
					WithError(updateErr).
					Error("Failed to update task status during recovery")
			}
			failedCount++
		} else {
			recoveredCount++
		}
	}

	rs.logger.WithField("recovered", recoveredCount).
		WithField("failed", failedCount).
		WithField("total", len(allIncompleteTasks)).
		Info("Crash recovery completed")

	return nil
}

func (rs *RecoveryService) recoverTask(ctx context.Context, task *models.Task) error {
	switch task.Status {
	case models.TaskStatusPending:
		return rs.recoverPendingTask(ctx, task)
	case models.TaskStatusDownloaded:
		return rs.recoverDownloadedTask(ctx, task)
	default:
		return fmt.Errorf("unknown task status for recovery: %s", task.Status)
	}
}

func (rs *RecoveryService) recoverPendingTask(ctx context.Context, task *models.Task) error {
	rs.logger.WithField("task_id", task.ID).Info("Recovering pending task")

	// Get Local Bot API paths
	tempPath, err := rs.botAPIPathManager.GetTempPath()
	if err != nil {
		rs.logger.WithError(err).Error("Failed to get Local Bot API temp path")
		return nil // Continue with recovery
	}
	
	documentsPath, err := rs.botAPIPathManager.GetDocumentsPath()
	if err != nil {
		rs.logger.WithError(err).Error("Failed to get Local Bot API documents path")
		return nil // Continue with recovery
	}

	// Check if file was already downloaded to Local Bot API temp directory
	tempFilePath := filepath.Join(tempPath, fmt.Sprintf("%s_%s", task.ID, task.FileName))
	if _, err := os.Stat(tempFilePath); err == nil {
		rs.logger.WithField("task_id", task.ID).
			WithField("temp_file", tempFilePath).
			Info("Found downloaded file in Local Bot API temp directory, updating status")
		
		// Update task with temp file path and mark as downloaded
		task.LocalAPIPath = tempFilePath
		if updateErr := rs.taskStore.UpdateTask(task); updateErr != nil {
			rs.logger.WithError(updateErr).Error("Failed to update task with temp path")
		}
		
		// File exists in temp, update status to DOWNLOADED
		return rs.taskStore.UpdateStatus(task.ID, models.TaskStatusDownloaded, "")
	}
	
	// Check if file is still in documents directory (not yet moved to temp)
	// Look for files with pattern file_*.ext in documents
	documentFiles, err := filepath.Glob(filepath.Join(documentsPath, "file_*"))
	if err == nil && len(documentFiles) > 0 {
		rs.logger.WithField("task_id", task.ID).
			WithField("documents_files", len(documentFiles)).
			Info("Found files in documents directory, may need to match with task")
		// Note: Actual file matching would require more complex logic
		// For now, we'll let the download worker handle these files
	}

	// File not found in temp or documents - will need to be re-downloaded
	rs.logger.WithField("task_id", task.ID).
		Info("File not found in Local Bot API directories, task will be re-queued for download")
	
	return nil // Task remains PENDING and will be picked up by pipeline
}

func (rs *RecoveryService) recoverDownloadedTask(ctx context.Context, task *models.Task) error {
	rs.logger.WithField("task_id", task.ID).Info("Recovering downloaded task")

	// Get Local Bot API paths
	tempPath, err := rs.botAPIPathManager.GetTempPath()
	if err != nil {
		rs.logger.WithError(err).Error("Failed to get Local Bot API temp path")
		return nil
	}

	// Check if file is in Local Bot API temp directory
	tempFilePath := filepath.Join(tempPath, fmt.Sprintf("%s_%s", task.ID, task.FileName))
	if _, err := os.Stat(tempFilePath); err == nil {
		rs.logger.WithField("task_id", task.ID).
			Info("Downloaded file found in Local Bot API temp directory, ready for extraction")
		
		// Update task with temp file path if not already set
		if task.LocalAPIPath == "" {
			task.LocalAPIPath = tempFilePath
			if updateErr := rs.taskStore.UpdateTask(task); updateErr != nil {
				rs.logger.WithError(updateErr).Error("Failed to update task with temp path")
			}
		}
		
		return nil // Task will be picked up by extraction pipeline
	}

	// Check if file was moved to extraction directory
	extractionFilePath := filepath.Join("app", "extraction", "files", "all", task.FileName)
	if _, err := os.Stat(extractionFilePath); err == nil {
		rs.logger.WithField("task_id", task.ID).
			Info("File found in extraction directory, ready for processing")
		return nil // File is ready for extraction
	}
	
	// Check txt files directory
	txtFilePath := filepath.Join("app", "extraction", "files", "txt", task.FileName)
	if _, err := os.Stat(txtFilePath); err == nil {
		rs.logger.WithField("task_id", task.ID).
			Info("File found in txt extraction directory, ready for processing")
		return nil // File is ready for extraction
	}

	// Check if file was already extracted to pass directory
	passFiles, err := filepath.Glob(filepath.Join("app", "extraction", "files", "pass", "*"))
	if err == nil && len(passFiles) > 0 {
		// Check if any files are related to this task (by timestamp or naming)
		for _, passFile := range passFiles {
			if info, err := os.Stat(passFile); err == nil {
				// If file was created after task creation, it might be related
				if info.ModTime().After(task.CreatedAt) {
					rs.logger.WithField("task_id", task.ID).
						WithField("pass_file", passFile).
						Info("Found extracted files in pass directory, task may have been processed")
					// We could mark as completed, but let pipeline handle it
					return nil
				}
			}
		}
	}

	// File not found anywhere - check if it's still being downloaded
	documentsPath, docErr := rs.botAPIPathManager.GetDocumentsPath()
	if docErr == nil {
		// Look for potential matches in documents directory
		documentFiles, globErr := filepath.Glob(filepath.Join(documentsPath, "file_*"))
		if globErr == nil && len(documentFiles) > 0 {
			rs.logger.WithField("task_id", task.ID).
				WithField("documents_files", len(documentFiles)).
				Info("Found files in documents directory, task may need to be re-processed")
			// Reset task to PENDING so it can be re-downloaded and moved properly
			return rs.taskStore.UpdateStatus(task.ID, models.TaskStatusPending, "File found in documents, re-processing")
		}
	}

	// File not found anywhere - mark as failed
	return fmt.Errorf("downloaded file not found in any expected location")
}

func (rs *RecoveryService) CleanupOrphanedFiles() error {
	rs.logger.Info("Starting cleanup of orphaned files")

	// Clean up Local Bot API temp directory of files older than 24 hours
	tempPath, err := rs.botAPIPathManager.GetTempPath()
	if err != nil {
		rs.logger.WithError(err).Warn("Failed to get Local Bot API temp path for cleanup")
	} else {
		if err := rs.cleanupDirectory(tempPath, 24*time.Hour); err != nil {
			rs.logger.WithError(err).Warn("Failed to cleanup Local Bot API temp directory")
		}
	}

	// Clean up extraction directories of very old files
	extractionDirs := []string{
		"app/extraction/files/all",
		"app/extraction/files/txt",
		"app/extraction/files/pass",
		"app/extraction/files/errors",
		"app/extraction/files/nopass",
	}

	for _, dir := range extractionDirs {
		if err := rs.cleanupDirectory(dir, 7*24*time.Hour); err != nil {
			rs.logger.WithError(err).
				WithField("directory", dir).
				Warn("Failed to cleanup extraction directory")
		}
	}

	// Also cleanup old files in Local Bot API documents directory (files that may have been stuck)
	documentsPath, docErr := rs.botAPIPathManager.GetDocumentsPath()
	if docErr != nil {
		rs.logger.WithError(docErr).Warn("Failed to get Local Bot API documents path for cleanup")
	} else {
		if err := rs.cleanupDirectory(documentsPath, 48*time.Hour); err != nil {
			rs.logger.WithError(err).Warn("Failed to cleanup Local Bot API documents directory")
		}
	}

	rs.logger.Info("Orphaned file cleanup completed")
	return nil
}

func (rs *RecoveryService) cleanupDirectory(dir string, maxAge time.Duration) error {
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}

	cleanedCount := 0
	cutoffTime := time.Now().Add(-maxAge)

	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			if info.ModTime().Before(cutoffTime) {
				if err := os.Remove(file); err != nil {
					rs.logger.WithError(err).
						WithField("file", file).
						Warn("Failed to remove old file")
				} else {
					cleanedCount++
				}
			}
		}
	}

	if cleanedCount > 0 {
		rs.logger.WithField("directory", dir).
			WithField("cleaned_files", cleanedCount).
			Info("Cleaned up old files")
	}

	return nil
}

func (rs *RecoveryService) GetRecoveryStats() RecoveryStats {
	stats := RecoveryStats{}

	// Count tasks by status
	if pendingTasks, err := rs.taskStore.GetByStatus(models.TaskStatusPending); err == nil {
		stats.PendingTasks = len(pendingTasks)
	}

	if downloadedTasks, err := rs.taskStore.GetByStatus(models.TaskStatusDownloaded); err == nil {
		stats.DownloadedTasks = len(downloadedTasks)
	}

	if completedTasks, err := rs.taskStore.GetByStatus(models.TaskStatusCompleted); err == nil {
		stats.CompletedTasks = len(completedTasks)
	}

	if failedTasks, err := rs.taskStore.GetByStatus(models.TaskStatusFailed); err == nil {
		stats.FailedTasks = len(failedTasks)
	}

	return stats
}

type RecoveryStats struct {
	PendingTasks    int
	DownloadedTasks int
	CompletedTasks  int
	FailedTasks     int
}