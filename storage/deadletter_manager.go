package storage

import (
	"context"
	"fmt"
	"time"

	"telegram-archive-bot/models"
	"telegram-archive-bot/utils"
)

// DeadLetterManager manages the movement of tasks to and from the dead letter queue
type DeadLetterManager struct {
	deadLetterQueue *DeadLetterQueue
	taskStore       *TaskStore
	errorHandler    *utils.ErrorHandler
	logger          *utils.Logger
}

func NewDeadLetterManager(dlq *DeadLetterQueue, taskStore *TaskStore, logger *utils.Logger) *DeadLetterManager {
	return &DeadLetterManager{
		deadLetterQueue: dlq,
		taskStore:       taskStore,
		errorHandler:    utils.NewErrorHandler(logger),
		logger:          logger,
	}
}

// MoveToDeadLetter moves a task to the dead letter queue based on error analysis
func (dlm *DeadLetterManager) MoveToDeadLetter(task *models.Task, finalError error, context map[string]interface{}) error {
	// Categorize the error to determine the reason for dead lettering
	categorizedError := dlm.errorHandler.Handle(finalError, context)
	
	reason := dlm.determineDeadLetterReason(task, categorizedError)
	
	// Add to dead letter queue
	err := dlm.deadLetterQueue.Add(task, reason, finalError.Error(), context)
	if err != nil {
		dlm.logger.WithField("task_id", task.ID).
			WithField("error", err.Error()).
			Error("Failed to move task to dead letter queue")
		return fmt.Errorf("failed to move task to dead letter queue: %w", err)
	}

	// Update task status to FAILED in the main task store
	err = dlm.taskStore.UpdateWithErrorInfo(
		task.ID,
		models.TaskStatusFailed,
		fmt.Sprintf("Moved to dead letter queue: %s", finalError.Error()),
		string(categorizedError.Category),
		string(categorizedError.Severity),
		task.RetryCount,
	)
	if err != nil {
		dlm.logger.WithField("task_id", task.ID).
			WithField("error", err.Error()).
			Warn("Failed to update task status after moving to dead letter queue")
		// Don't return error here as the task is already in dead letter queue
	}

	dlm.logger.WithField("task_id", task.ID).
		WithField("reason", reason).
		WithField("error_category", categorizedError.Category).
		WithField("retry_count", task.RetryCount).
		Info("Task moved to dead letter queue")

	return nil
}

// RetryFromDeadLetter attempts to retry a task from the dead letter queue
func (dlm *DeadLetterManager) RetryFromDeadLetter(deadLetterID string) (*models.Task, error) {
	// Get the dead letter entry
	entry, err := dlm.deadLetterQueue.GetByID(deadLetterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dead letter entry: %w", err)
	}

	if !entry.CanRetry {
		return nil, fmt.Errorf("dead letter entry %s is not retryable", deadLetterID)
	}

	// Convert back to task
	task := dlm.deadLetterQueue.ConvertToTask(entry)
	
	// Create new task in the task store
	err = dlm.taskStore.Create(task)
	if err != nil {
		return nil, fmt.Errorf("failed to recreate task from dead letter: %w", err)
	}

	// Remove from dead letter queue
	err = dlm.deadLetterQueue.Remove(deadLetterID)
	if err != nil {
		dlm.logger.WithField("dead_letter_id", deadLetterID).
			WithField("error", err.Error()).
			Warn("Failed to remove entry from dead letter queue after successful retry creation")
		// Don't return error as task was successfully created
	}

	dlm.logger.WithField("dead_letter_id", deadLetterID).
		WithField("task_id", task.ID).
		Info("Task successfully retried from dead letter queue")

	return task, nil
}

// BulkRetryByReason retries multiple tasks from dead letter queue by reason
func (dlm *DeadLetterManager) BulkRetryByReason(reason DeadLetterReason, maxRetries int) ([]string, []string, error) {
	entries, err := dlm.deadLetterQueue.GetByReason(reason)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get dead letter entries by reason: %w", err)
	}

	var successfulRetries []string
	var failedRetries []string
	retryCount := 0

	for _, entry := range entries {
		if retryCount >= maxRetries {
			break
		}

		if !entry.CanRetry {
			failedRetries = append(failedRetries, fmt.Sprintf("%s: not retryable", entry.ID))
			continue
		}

		_, err := dlm.RetryFromDeadLetter(entry.ID)
		if err != nil {
			failedRetries = append(failedRetries, fmt.Sprintf("%s: %s", entry.ID, err.Error()))
			dlm.logger.WithField("dead_letter_id", entry.ID).
				WithField("error", err.Error()).
				Error("Failed to retry task from dead letter queue")
		} else {
			successfulRetries = append(successfulRetries, entry.ID)
			retryCount++
		}
	}

	dlm.logger.WithField("reason", reason).
		WithField("successful_retries", len(successfulRetries)).
		WithField("failed_retries", len(failedRetries)).
		Info("Bulk retry operation completed")

	return successfulRetries, failedRetries, nil
}

// GetRetryableCount returns the number of tasks that can be retried
func (dlm *DeadLetterManager) GetRetryableCount() (int, error) {
	entries, err := dlm.deadLetterQueue.GetRetryable()
	if err != nil {
		return 0, fmt.Errorf("failed to get retryable entries: %w", err)
	}
	return len(entries), nil
}

// GetManualInterventionCount returns the number of tasks requiring manual intervention
func (dlm *DeadLetterManager) GetManualInterventionCount() (int, error) {
	entries, err := dlm.deadLetterQueue.GetManualIntervention()
	if err != nil {
		return 0, fmt.Errorf("failed to get manual intervention entries: %w", err)
	}
	return len(entries), nil
}

// CleanupOldEntries removes old dead letter entries that cannot be retried
func (dlm *DeadLetterManager) CleanupOldEntries(olderThan time.Duration) (int, error) {
	purgedCount, err := dlm.deadLetterQueue.PurgeOld(olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old dead letter entries: %w", err)
	}

	dlm.logger.WithField("purged_count", purgedCount).
		WithField("older_than", olderThan.String()).
		Info("Purged old dead letter entries")

	return purgedCount, nil
}

// GetDetailedStats returns comprehensive statistics about the dead letter queue
func (dlm *DeadLetterManager) GetDetailedStats() (map[string]interface{}, error) {
	stats, err := dlm.deadLetterQueue.GetStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get dead letter queue stats: %w", err)
	}

	// Add additional computed statistics
	retryableCount, _ := dlm.GetRetryableCount()
	manualCount, _ := dlm.GetManualInterventionCount()

	stats["retryable_count"] = retryableCount
	stats["manual_intervention_count"] = manualCount
	stats["last_updated"] = time.Now()

	return stats, nil
}

// ManualMove allows administrators to manually move a task to dead letter queue
func (dlm *DeadLetterManager) ManualMove(taskID string, reason string) error {
	task, err := dlm.taskStore.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	context := map[string]interface{}{
		"manual_reason": reason,
		"moved_by":      "admin",
		"moved_at":      time.Now(),
	}

	manualError := fmt.Errorf("manually moved to dead letter queue: %s", reason)
	return dlm.MoveToDeadLetter(task, manualError, context)
}

// determineDeadLetterReason analyzes the task and error to determine the appropriate dead letter reason
func (dlm *DeadLetterManager) determineDeadLetterReason(task *models.Task, categorizedError *utils.CategorizedError) DeadLetterReason {
	// Check retry count first
	if task.RetryCount >= 5 { // Configurable max retry threshold
		return DeadLetterReasonMaxRetriesExceeded
	}

	// Check error severity
	switch categorizedError.Severity {
	case utils.SeverityCritical:
		return DeadLetterReasonCriticalError
	}

	// Check retry strategy
	switch categorizedError.Retry {
	case utils.RetryNever:
		return DeadLetterReasonNonRetryableError
	case utils.RetryManual:
		return DeadLetterReasonSystemFailure
	}

	// Check error category
	switch categorizedError.Category {
	case utils.ErrorCategoryValidation:
		return DeadLetterReasonNonRetryableError
	case utils.ErrorCategoryAuth:
		return DeadLetterReasonNonRetryableError
	case utils.ErrorCategoryConfiguration:
		return DeadLetterReasonSystemFailure
	case utils.ErrorCategoryCritical:
		return DeadLetterReasonCriticalError
	}

	// Check for timeout patterns
	if containsIgnoreCase(categorizedError.Message, "timeout") {
		return DeadLetterReasonTimeout
	}

	// Check for corruption patterns
	if containsIgnoreCase(categorizedError.Message, "corrupt") || 
	   containsIgnoreCase(categorizedError.Message, "invalid format") ||
	   containsIgnoreCase(categorizedError.Message, "malformed") {
		return DeadLetterReasonCorruption
	}

	// Default to max retries exceeded if we can't determine a specific reason
	return DeadLetterReasonMaxRetriesExceeded
}

// containsIgnoreCase checks if a string contains a substring (case insensitive)
func containsIgnoreCase(text, substr string) bool {
	textLower := ""
	substrLower := ""
	
	// Simple case conversion
	for _, r := range text {
		if r >= 'A' && r <= 'Z' {
			textLower += string(r + 32)
		} else {
			textLower += string(r)
		}
	}
	
	for _, r := range substr {
		if r >= 'A' && r <= 'Z' {
			substrLower += string(r + 32)
		} else {
			substrLower += string(r)
		}
	}
	
	// Simple substring check
	for i := 0; i <= len(textLower)-len(substrLower); i++ {
		if textLower[i:i+len(substrLower)] == substrLower {
			return true
		}
	}
	return false
}

// DeadLetterRetryService wraps the enhanced retry service to automatically move failed tasks to dead letter queue
type DeadLetterRetryService struct {
	enhancedRetryService *utils.EnhancedRetryService
	deadLetterManager    *DeadLetterManager
	taskStore            *TaskStore
	logger               *utils.Logger
}

func NewDeadLetterRetryService(
	enhancedRetryService *utils.EnhancedRetryService,
	deadLetterManager *DeadLetterManager,
	taskStore *TaskStore,
	logger *utils.Logger,
) *DeadLetterRetryService {
	return &DeadLetterRetryService{
		enhancedRetryService: enhancedRetryService,
		deadLetterManager:    deadLetterManager,
		taskStore:            taskStore,
		logger:               logger,
	}
}

// ExecuteWithDeadLetter executes an operation with automatic dead letter queue handling
func (dlrs *DeadLetterRetryService) ExecuteWithDeadLetter(
	ctx context.Context,
	taskID string,
	operation func() error,
	description string,
	operationContext map[string]interface{},
) error {
	// Try the operation with enhanced retry
	err := dlrs.enhancedRetryService.ExecuteWithCategoryOptimization(ctx, operation, description, operationContext)
	
	if err != nil {
		// Operation failed after all retries, move to dead letter queue
		task, getErr := dlrs.taskStore.GetByID(taskID)
		if getErr != nil {
			dlrs.logger.WithField("task_id", taskID).
				WithField("error", getErr.Error()).
				Error("Failed to get task for dead letter queue movement")
			return fmt.Errorf("operation failed and could not move to dead letter queue: %w", err)
		}

		// Move to dead letter queue
		deadLetterErr := dlrs.deadLetterManager.MoveToDeadLetter(task, err, operationContext)
		if deadLetterErr != nil {
			dlrs.logger.WithField("task_id", taskID).
				WithField("error", deadLetterErr.Error()).
				Error("Failed to move failed task to dead letter queue")
			return fmt.Errorf("operation failed and could not move to dead letter queue: %w", err)
		}

		dlrs.logger.WithField("task_id", taskID).
			WithField("operation", description).
			WithField("final_error", err.Error()).
			Info("Task moved to dead letter queue after exhausting retries")

		return fmt.Errorf("operation failed and moved to dead letter queue: %w", err)
	}

	return nil
}