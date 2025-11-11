package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"telegram-archive-bot/models"
)

// DeadLetterReason represents why a task was moved to dead letter queue
type DeadLetterReason string

const (
	DeadLetterReasonMaxRetriesExceeded DeadLetterReason = "max_retries_exceeded"
	DeadLetterReasonNonRetryableError  DeadLetterReason = "non_retryable_error"
	DeadLetterReasonCriticalError      DeadLetterReason = "critical_error"
	DeadLetterReasonManualMove         DeadLetterReason = "manual_move"
	DeadLetterReasonSystemFailure      DeadLetterReason = "system_failure"
	DeadLetterReasonTimeout            DeadLetterReason = "timeout"
	DeadLetterReasonCorruption         DeadLetterReason = "corruption"
)

// DeadLetterEntry represents a task in the dead letter queue
type DeadLetterEntry struct {
	ID                string            `db:"id" json:"id"`
	OriginalTaskID    string            `db:"original_task_id" json:"original_task_id"`
	UserID            int64             `db:"user_id" json:"user_id"`
	ChatID            int64             `db:"chat_id" json:"chat_id"`
	FileName          string            `db:"file_name" json:"file_name"`
	FileSize          int64             `db:"file_size" json:"file_size"`
	FileType          string            `db:"file_type" json:"file_type"`
	FileHash          string            `db:"file_hash" json:"file_hash"`
	TelegramFileID    string            `db:"telegram_file_id" json:"telegram_file_id"`
	Reason            DeadLetterReason  `db:"reason" json:"reason"`
	FinalError        string            `db:"final_error" json:"final_error"`
	ErrorCategory     string            `db:"error_category" json:"error_category"`
	ErrorSeverity     string            `db:"error_severity" json:"error_severity"`
	RetryCount        int               `db:"retry_count" json:"retry_count"`
	TaskContext       string            `db:"task_context" json:"task_context"` // JSON serialized context
	CreatedAt         time.Time         `db:"created_at" json:"created_at"`
	DeadLetterAt      time.Time         `db:"dead_letter_at" json:"dead_letter_at"`
	LastAttemptAt     *time.Time        `db:"last_attempt_at" json:"last_attempt_at,omitempty"`
	CanRetry          bool              `db:"can_retry" json:"can_retry"`
	RequiresManual    bool              `db:"requires_manual" json:"requires_manual"`
}

// DeadLetterQueue manages permanently failed tasks
type DeadLetterQueue struct {
	db *Database
}

func NewDeadLetterQueue(db *Database) *DeadLetterQueue {
	return &DeadLetterQueue{db: db}
}

// Add moves a task to the dead letter queue
func (dlq *DeadLetterQueue) Add(task *models.Task, reason DeadLetterReason, finalError string, context map[string]interface{}) error {
	entry := &DeadLetterEntry{
		ID:                generateDeadLetterID(),
		OriginalTaskID:    task.ID,
		UserID:            task.UserID,
		ChatID:            task.ChatID,
		FileName:          task.FileName,
		FileSize:          task.FileSize,
		FileType:          task.FileType,
		FileHash:          task.FileHash,
		TelegramFileID:    task.TelegramFileID,
		Reason:            reason,
		FinalError:        finalError,
		ErrorCategory:     task.ErrorCategory,
		ErrorSeverity:     task.ErrorSeverity,
		RetryCount:        task.RetryCount,
		CreatedAt:         task.CreatedAt,
		DeadLetterAt:      time.Now(),
		LastAttemptAt:     task.CompletedAt,
		CanRetry:          dlq.determineRetryability(reason),
		RequiresManual:    dlq.requiresManualIntervention(reason),
	}

	// Serialize context if provided
	if context != nil {
		contextJSON, err := json.Marshal(context)
		if err != nil {
			return fmt.Errorf("failed to serialize task context: %w", err)
		}
		entry.TaskContext = string(contextJSON)
	}

	query := `
		INSERT INTO dead_letter_queue (id, original_task_id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, reason, final_error, error_category, error_severity, retry_count, task_context, created_at, dead_letter_at, last_attempt_at, can_retry, requires_manual)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := dlq.db.DB().Exec(query,
		entry.ID, entry.OriginalTaskID, entry.UserID, entry.ChatID, entry.FileName,
		entry.FileSize, entry.FileType, entry.FileHash, entry.TelegramFileID,
		entry.Reason, entry.FinalError, entry.ErrorCategory, entry.ErrorSeverity,
		entry.RetryCount, entry.TaskContext, entry.CreatedAt, entry.DeadLetterAt,
		entry.LastAttemptAt, entry.CanRetry, entry.RequiresManual)

	if err != nil {
		return fmt.Errorf("failed to add task to dead letter queue: %w", err)
	}

	return nil
}

// GetByID retrieves a dead letter entry by ID
func (dlq *DeadLetterQueue) GetByID(id string) (*DeadLetterEntry, error) {
	query := `
		SELECT id, original_task_id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, reason, final_error, error_category, error_severity, retry_count, task_context, created_at, dead_letter_at, last_attempt_at, can_retry, requires_manual
		FROM dead_letter_queue WHERE id = ?
	`

	row := dlq.db.DB().QueryRow(query, id)

	entry := &DeadLetterEntry{}
	err := row.Scan(&entry.ID, &entry.OriginalTaskID, &entry.UserID, &entry.ChatID,
		&entry.FileName, &entry.FileSize, &entry.FileType, &entry.FileHash,
		&entry.TelegramFileID, &entry.Reason, &entry.FinalError, &entry.ErrorCategory,
		&entry.ErrorSeverity, &entry.RetryCount, &entry.TaskContext, &entry.CreatedAt,
		&entry.DeadLetterAt, &entry.LastAttemptAt, &entry.CanRetry, &entry.RequiresManual)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("dead letter entry not found")
		}
		return nil, fmt.Errorf("failed to get dead letter entry: %w", err)
	}

	return entry, nil
}

// GetByReason retrieves dead letter entries by reason
func (dlq *DeadLetterQueue) GetByReason(reason DeadLetterReason) ([]*DeadLetterEntry, error) {
	query := `
		SELECT id, original_task_id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, reason, final_error, error_category, error_severity, retry_count, task_context, created_at, dead_letter_at, last_attempt_at, can_retry, requires_manual
		FROM dead_letter_queue WHERE reason = ? ORDER BY dead_letter_at DESC
	`

	rows, err := dlq.db.DB().Query(query, reason)
	if err != nil {
		return nil, fmt.Errorf("failed to query dead letter entries by reason: %w", err)
	}
	defer rows.Close()

	var entries []*DeadLetterEntry
	for rows.Next() {
		entry := &DeadLetterEntry{}
		err := rows.Scan(&entry.ID, &entry.OriginalTaskID, &entry.UserID, &entry.ChatID,
			&entry.FileName, &entry.FileSize, &entry.FileType, &entry.FileHash,
			&entry.TelegramFileID, &entry.Reason, &entry.FinalError, &entry.ErrorCategory,
			&entry.ErrorSeverity, &entry.RetryCount, &entry.TaskContext, &entry.CreatedAt,
			&entry.DeadLetterAt, &entry.LastAttemptAt, &entry.CanRetry, &entry.RequiresManual)
		if err != nil {
			return nil, fmt.Errorf("failed to scan dead letter entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return entries, nil
}

// GetRetryable retrieves entries that can potentially be retried
func (dlq *DeadLetterQueue) GetRetryable() ([]*DeadLetterEntry, error) {
	query := `
		SELECT id, original_task_id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, reason, final_error, error_category, error_severity, retry_count, task_context, created_at, dead_letter_at, last_attempt_at, can_retry, requires_manual
		FROM dead_letter_queue WHERE can_retry = true ORDER BY dead_letter_at ASC
	`

	rows, err := dlq.db.DB().Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query retryable dead letter entries: %w", err)
	}
	defer rows.Close()

	var entries []*DeadLetterEntry
	for rows.Next() {
		entry := &DeadLetterEntry{}
		err := rows.Scan(&entry.ID, &entry.OriginalTaskID, &entry.UserID, &entry.ChatID,
			&entry.FileName, &entry.FileSize, &entry.FileType, &entry.FileHash,
			&entry.TelegramFileID, &entry.Reason, &entry.FinalError, &entry.ErrorCategory,
			&entry.ErrorSeverity, &entry.RetryCount, &entry.TaskContext, &entry.CreatedAt,
			&entry.DeadLetterAt, &entry.LastAttemptAt, &entry.CanRetry, &entry.RequiresManual)
		if err != nil {
			return nil, fmt.Errorf("failed to scan retryable dead letter entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// GetManualIntervention retrieves entries requiring manual intervention
func (dlq *DeadLetterQueue) GetManualIntervention() ([]*DeadLetterEntry, error) {
	query := `
		SELECT id, original_task_id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, reason, final_error, error_category, error_severity, retry_count, task_context, created_at, dead_letter_at, last_attempt_at, can_retry, requires_manual
		FROM dead_letter_queue WHERE requires_manual = true ORDER BY dead_letter_at DESC
	`

	rows, err := dlq.db.DB().Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query manual intervention entries: %w", err)
	}
	defer rows.Close()

	var entries []*DeadLetterEntry
	for rows.Next() {
		entry := &DeadLetterEntry{}
		err := rows.Scan(&entry.ID, &entry.OriginalTaskID, &entry.UserID, &entry.ChatID,
			&entry.FileName, &entry.FileSize, &entry.FileType, &entry.FileHash,
			&entry.TelegramFileID, &entry.Reason, &entry.FinalError, &entry.ErrorCategory,
			&entry.ErrorSeverity, &entry.RetryCount, &entry.TaskContext, &entry.CreatedAt,
			&entry.DeadLetterAt, &entry.LastAttemptAt, &entry.CanRetry, &entry.RequiresManual)
		if err != nil {
			return nil, fmt.Errorf("failed to scan manual intervention entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// Remove deletes a dead letter entry (typically after successful retry or manual resolution)
func (dlq *DeadLetterQueue) Remove(id string) error {
	query := `DELETE FROM dead_letter_queue WHERE id = ?`
	result, err := dlq.db.DB().Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to remove dead letter entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("dead letter entry not found")
	}

	return nil
}

// GetStats returns statistics about the dead letter queue
func (dlq *DeadLetterQueue) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count by reason
	reasonQuery := `
		SELECT reason, COUNT(*) as count
		FROM dead_letter_queue
		GROUP BY reason
	`
	rows, err := dlq.db.DB().Query(reasonQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to get reason stats: %w", err)
	}
	defer rows.Close()

	reasonStats := make(map[string]int)
	totalCount := 0
	for rows.Next() {
		var reason string
		var count int
		if err := rows.Scan(&reason, &count); err != nil {
			return nil, fmt.Errorf("failed to scan reason stats: %w", err)
		}
		reasonStats[reason] = count
		totalCount += count
	}
	stats["by_reason"] = reasonStats
	stats["total_count"] = totalCount

	// Count retryable vs non-retryable
	retryableQuery := `
		SELECT 
			SUM(CASE WHEN can_retry = true THEN 1 ELSE 0 END) as retryable,
			SUM(CASE WHEN requires_manual = true THEN 1 ELSE 0 END) as manual_intervention
		FROM dead_letter_queue
	`
	row := dlq.db.DB().QueryRow(retryableQuery)
	var retryable, manualIntervention int
	if err := row.Scan(&retryable, &manualIntervention); err != nil {
		return nil, fmt.Errorf("failed to get retryable stats: %w", err)
	}
	stats["retryable_count"] = retryable
	stats["manual_intervention_count"] = manualIntervention

	// Oldest entry age
	ageQuery := `SELECT MIN(dead_letter_at) FROM dead_letter_queue`
	var oldestTime sql.NullTime
	if err := dlq.db.DB().QueryRow(ageQuery).Scan(&oldestTime); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get oldest entry: %w", err)
	}
	if oldestTime.Valid {
		stats["oldest_entry_age"] = time.Since(oldestTime.Time).String()
	} else {
		stats["oldest_entry_age"] = "N/A"
	}

	return stats, nil
}

// PurgeOld removes old dead letter entries beyond a certain age
func (dlq *DeadLetterQueue) PurgeOld(olderThan time.Duration) (int, error) {
	cutoffTime := time.Now().Add(-olderThan)
	
	query := `DELETE FROM dead_letter_queue WHERE dead_letter_at < ? AND can_retry = false`
	result, err := dlq.db.DB().Exec(query, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old dead letter entries: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// Helper functions
func (dlq *DeadLetterQueue) determineRetryability(reason DeadLetterReason) bool {
	switch reason {
	case DeadLetterReasonMaxRetriesExceeded:
		return true // Can potentially be retried with manual intervention
	case DeadLetterReasonNonRetryableError:
		return false // Permanent failure
	case DeadLetterReasonCriticalError:
		return false // System-level issue
	case DeadLetterReasonManualMove:
		return true // Admin moved, can be retried
	case DeadLetterReasonSystemFailure:
		return true // System issues can be resolved
	case DeadLetterReasonTimeout:
		return true // Timeouts can be retried
	case DeadLetterReasonCorruption:
		return false // Corrupted data cannot be retried
	default:
		return false // Conservative default
	}
}

func (dlq *DeadLetterQueue) requiresManualIntervention(reason DeadLetterReason) bool {
	switch reason {
	case DeadLetterReasonMaxRetriesExceeded:
		return true // Need to investigate why it kept failing
	case DeadLetterReasonNonRetryableError:
		return false // Nothing can be done
	case DeadLetterReasonCriticalError:
		return true // System admin needs to investigate
	case DeadLetterReasonManualMove:
		return false // Already handled manually
	case DeadLetterReasonSystemFailure:
		return true // System admin intervention required
	case DeadLetterReasonTimeout:
		return false // Can be automatically retried
	case DeadLetterReasonCorruption:
		return true // Need manual data recovery
	default:
		return true // Conservative default - ask for help
	}
}

// generateDeadLetterID creates a unique ID for dead letter entries
func generateDeadLetterID() string {
	bytes := make([]byte, 8)
	// Simple timestamp-based ID for dead letter entries
	timestamp := time.Now().UnixNano()
	for i := 0; i < 8; i++ {
		bytes[i] = byte(timestamp >> (i * 8))
	}
	return fmt.Sprintf("dl_%x", bytes)
}

// ConvertToTask converts a dead letter entry back to a task for retry
func (dlq *DeadLetterQueue) ConvertToTask(entry *DeadLetterEntry) *models.Task {
	task := &models.Task{
		ID:             entry.OriginalTaskID,
		UserID:         entry.UserID,
		ChatID:         entry.ChatID,
		FileName:       entry.FileName,
		FileSize:       entry.FileSize,
		FileType:       entry.FileType,
		FileHash:       entry.FileHash,
		TelegramFileID: entry.TelegramFileID,
		Status:         models.TaskStatusPending, // Reset to pending for retry
		ErrorMessage:   "",                       // Clear error message
		ErrorCategory:  "",                       // Clear error category
		ErrorSeverity:  "",                       // Clear error severity
		RetryCount:     0,                        // Reset retry count
		CreatedAt:      entry.CreatedAt,
		UpdatedAt:      time.Now(),
		CompletedAt:    nil, // Clear completion time
	}

	return task
}