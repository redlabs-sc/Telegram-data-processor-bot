package models

import (
	"time"
)

type TaskStatus string

const (
	TaskStatusPending     TaskStatus = "PENDING"
	TaskStatusDownloading TaskStatus = "DOWNLOADING"
	TaskStatusDownloaded  TaskStatus = "DOWNLOADED"
	TaskStatusCompleted   TaskStatus = "COMPLETED"
	TaskStatusFailed      TaskStatus = "FAILED"
)

type Task struct {
	ID             string    `db:"id" json:"id"`
	UserID         int64     `db:"user_id" json:"user_id"`
	ChatID         int64     `db:"chat_id" json:"chat_id"`
	FileName       string    `db:"file_name" json:"file_name"`
	FileSize       int64     `db:"file_size" json:"file_size"`
	FileType       string    `db:"file_type" json:"file_type"`
	FileHash       string    `db:"file_hash" json:"file_hash"`
	TelegramFileID string    `db:"telegram_file_id" json:"telegram_file_id"`
	LocalAPIPath   string    `db:"local_api_path" json:"local_api_path,omitempty"`
	Status         TaskStatus `db:"status" json:"status"`
	ErrorMessage   string    `db:"error_message" json:"error_message,omitempty"`
	ErrorCategory  string    `db:"error_category" json:"error_category,omitempty"`
	ErrorSeverity  string    `db:"error_severity" json:"error_severity,omitempty"`
	RetryCount     int       `db:"retry_count" json:"retry_count"`
	Notified       bool      `db:"notified" json:"notified"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
	CompletedAt    *time.Time `db:"completed_at" json:"completed_at,omitempty"`
}

func (t *Task) IsCompleted() bool {
	return t.Status == TaskStatusCompleted || t.Status == TaskStatusFailed
}