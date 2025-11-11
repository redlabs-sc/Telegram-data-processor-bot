package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"telegram-archive-bot/models"
)

type TaskStore struct {
	db *Database
}

func NewTaskStore(db *Database) *TaskStore {
	return &TaskStore{db: db}
}

func (ts *TaskStore) Create(task *models.Task) error {
	// Generate ID if not provided
	if task.ID == "" {
		task.ID = generateTaskID()
	}
	
	query := `
		INSERT INTO tasks (id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, local_api_path, status, error_message, error_category, error_severity, retry_count, created_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := ts.db.DB().Exec(query, 
		task.ID, task.UserID, task.ChatID, task.FileName, task.FileSize, task.FileType, 
		task.FileHash, task.TelegramFileID, task.LocalAPIPath, task.Status, task.ErrorMessage, task.ErrorCategory, 
		task.ErrorSeverity, task.RetryCount, task.CreatedAt, task.UpdatedAt, task.CompletedAt)
	
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}
	return nil
}

func (ts *TaskStore) GetByID(id string) (*models.Task, error) {
	query := `
		SELECT id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, local_api_path, status, error_message, error_category, error_severity, retry_count, created_at, updated_at, completed_at
		FROM tasks WHERE id = ?
	`
	row := ts.db.DB().QueryRow(query, id)
	
	task := &models.Task{}
	err := row.Scan(&task.ID, &task.UserID, &task.ChatID, &task.FileName, &task.FileSize, 
		&task.FileType, &task.FileHash, &task.TelegramFileID, &task.LocalAPIPath, &task.Status, &task.ErrorMessage,
		&task.ErrorCategory, &task.ErrorSeverity, &task.RetryCount, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task not found")
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return task, nil
}

func (ts *TaskStore) UpdateStatus(id string, status models.TaskStatus, errorMessage string) error {
	now := time.Now()
	var completedAt *time.Time
	
	if status == models.TaskStatusCompleted || status == models.TaskStatusFailed {
		completedAt = &now
	}
	
	query := `
		UPDATE tasks 
		SET status = ?, error_message = ?, updated_at = ?, completed_at = ?
		WHERE id = ?
	`
	result, err := ts.db.DB().Exec(query, status, errorMessage, now, completedAt, id)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("task not found")
	}
	
	return nil
}

// UpdateWithErrorInfo updates task status with detailed error information
func (ts *TaskStore) UpdateWithErrorInfo(id string, status models.TaskStatus, errorMessage, errorCategory, errorSeverity string, retryCount int) error {
	now := time.Now()
	var completedAt *time.Time
	
	if status == models.TaskStatusCompleted || status == models.TaskStatusFailed {
		completedAt = &now
	}
	
	query := `
		UPDATE tasks 
		SET status = ?, error_message = ?, error_category = ?, error_severity = ?, retry_count = ?, updated_at = ?, completed_at = ?
		WHERE id = ?
	`
	result, err := ts.db.DB().Exec(query, status, errorMessage, errorCategory, errorSeverity, retryCount, now, completedAt, id)
	if err != nil {
		return fmt.Errorf("failed to update task with error info: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("task not found")
	}
	
	return nil
}

// IncrementRetryCount increments the retry count for a task
func (ts *TaskStore) IncrementRetryCount(id string) error {
	query := `
		UPDATE tasks 
		SET retry_count = retry_count + 1, updated_at = ?
		WHERE id = ?
	`
	result, err := ts.db.DB().Exec(query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to increment retry count: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("task not found")
	}
	
	return nil
}

func (ts *TaskStore) GetByStatus(status models.TaskStatus) ([]*models.Task, error) {
	query := `
		SELECT id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, local_api_path, status, error_message, error_category, error_severity, retry_count, created_at, updated_at, completed_at
		FROM tasks WHERE status = ? ORDER BY created_at ASC
	`
	rows, err := ts.db.DB().Query(query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks by status: %w", err)
	}
	defer rows.Close()
	
	var tasks []*models.Task
	for rows.Next() {
		task := &models.Task{}
		err := rows.Scan(&task.ID, &task.UserID, &task.ChatID, &task.FileName, &task.FileSize,
			&task.FileType, &task.FileHash, &task.TelegramFileID, &task.LocalAPIPath, &task.Status, &task.ErrorMessage,
			&task.ErrorCategory, &task.ErrorSeverity, &task.RetryCount, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	
	return tasks, nil
}

func (ts *TaskStore) GetTasksByStatus(status models.TaskStatus, limit int) ([]*models.Task, error) {
	query := `
		SELECT id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, local_api_path, status, error_message, error_category, error_severity, retry_count, created_at, updated_at, completed_at
		FROM tasks WHERE status = ? ORDER BY created_at DESC LIMIT ?
	`
	rows, err := ts.db.DB().Query(query, status, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks by status with limit: %w", err)
	}
	defer rows.Close()
	
	var tasks []*models.Task
	for rows.Next() {
		task := &models.Task{}
		err := rows.Scan(&task.ID, &task.UserID, &task.ChatID, &task.FileName, &task.FileSize,
			&task.FileType, &task.FileHash, &task.TelegramFileID, &task.LocalAPIPath, &task.Status, &task.ErrorMessage,
			&task.ErrorCategory, &task.ErrorSeverity, &task.RetryCount, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	
	return tasks, nil
}

func (ts *TaskStore) GetByFileHash(fileHash string) (*models.Task, error) {
	query := `
		SELECT id, user_id, chat_id, file_name, file_size, file_type, file_hash, telegram_file_id, local_api_path, status, error_message, error_category, error_severity, retry_count, created_at, updated_at, completed_at
		FROM tasks WHERE file_hash = ? LIMIT 1
	`
	row := ts.db.DB().QueryRow(query, fileHash)
	
	task := &models.Task{}
	err := row.Scan(&task.ID, &task.UserID, &task.ChatID, &task.FileName, &task.FileSize,
		&task.FileType, &task.FileHash, &task.TelegramFileID, &task.LocalAPIPath, &task.Status, &task.ErrorMessage,
		&task.ErrorCategory, &task.ErrorSeverity, &task.RetryCount, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No duplicate found
		}
		return nil, fmt.Errorf("failed to get task by hash: %w", err)
	}
	return task, nil
}

func (ts *TaskStore) GetStats() (map[string]int, error) {
	query := `
		SELECT status, COUNT(*) as count
		FROM tasks
		GROUP BY status
	`
	rows, err := ts.db.DB().Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	defer rows.Close()
	
	stats := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan stats: %w", err)
		}
		stats[status] = count
	}
	
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	
	return stats, nil
}

// UpdateTask updates the full task record
func (ts *TaskStore) UpdateTask(task *models.Task) error {
	task.UpdatedAt = time.Now()
	
	query := `
		UPDATE tasks 
		SET user_id=?, chat_id=?, file_name=?, file_size=?, file_type=?, file_hash=?, 
		    telegram_file_id=?, local_api_path=?, status=?, error_message=?, error_category=?, 
		    error_severity=?, retry_count=?, updated_at=?, completed_at=?
		WHERE id=?
	`
	_, err := ts.db.DB().Exec(query,
		task.UserID, task.ChatID, task.FileName, task.FileSize, task.FileType, task.FileHash,
		task.TelegramFileID, task.LocalAPIPath, task.Status, task.ErrorMessage, task.ErrorCategory,
		task.ErrorSeverity, task.RetryCount, task.UpdatedAt, task.CompletedAt, task.ID)
	
	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}
	return nil
}

// GetDB returns the underlying database connection for security auditing
func (ts *TaskStore) GetDB() *sql.DB {
	return ts.db.DB()
}

// generateTaskID creates a random task ID
func generateTaskID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}