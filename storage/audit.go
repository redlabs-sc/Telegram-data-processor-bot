package storage

import (
	"fmt"
	"time"

	"telegram-archive-bot/models"
	"telegram-archive-bot/utils"
)

type AuditLogger struct {
	db     *Database
	logger *utils.Logger
}

type AuditEvent struct {
	ID        int64     `db:"id" json:"id"`
	TaskID    string    `db:"task_id" json:"task_id"`
	UserID    int64     `db:"user_id" json:"user_id"`
	Action    string    `db:"action" json:"action"`
	Details   string    `db:"details" json:"details"`
	OldStatus string    `db:"old_status" json:"old_status"`
	NewStatus string    `db:"new_status" json:"new_status"`
	Timestamp time.Time `db:"timestamp" json:"timestamp"`
}

func NewAuditLogger(db *Database, logger *utils.Logger) *AuditLogger {
	return &AuditLogger{
		db:     db,
		logger: logger,
	}
}

func (al *AuditLogger) LogTaskCreated(task *models.Task, userID int64) error {
	return al.logEvent(&AuditEvent{
		TaskID:    task.ID,
		UserID:    userID,
		Action:    "TASK_CREATED",
		Details:   fmt.Sprintf("File: %s (%.2fMB)", task.FileName, float64(task.FileSize)/(1024*1024)),
		OldStatus: "",
		NewStatus: string(task.Status),
		Timestamp: time.Now(),
	})
}

func (al *AuditLogger) LogTaskStatusChanged(taskID string, oldStatus, newStatus models.TaskStatus, details string) error {
	return al.logEvent(&AuditEvent{
		TaskID:    taskID,
		UserID:    0, // System action
		Action:    "STATUS_CHANGED",
		Details:   details,
		OldStatus: string(oldStatus),
		NewStatus: string(newStatus),
		Timestamp: time.Now(),
	})
}

func (al *AuditLogger) LogAdminAction(userID int64, action, details string) error {
	return al.logEvent(&AuditEvent{
		TaskID:    "",
		UserID:    userID,
		Action:    action,
		Details:   details,
		OldStatus: "",
		NewStatus: "",
		Timestamp: time.Now(),
	})
}

func (al *AuditLogger) LogFileProcessing(taskID, stage, details string) error {
	return al.logEvent(&AuditEvent{
		TaskID:    taskID,
		UserID:    0, // System action
		Action:    fmt.Sprintf("PROCESSING_%s", stage),
		Details:   details,
		OldStatus: "",
		NewStatus: "",
		Timestamp: time.Now(),
	})
}

func (al *AuditLogger) LogError(taskID, errorType, errorDetails string) error {
	return al.logEvent(&AuditEvent{
		TaskID:    taskID,
		UserID:    0, // System action
		Action:    fmt.Sprintf("ERROR_%s", errorType),
		Details:   errorDetails,
		OldStatus: "",
		NewStatus: "",
		Timestamp: time.Now(),
	})
}

func (al *AuditLogger) logEvent(event *AuditEvent) error {
	query := `
		INSERT INTO audit_log (task_id, user_id, action, details, old_status, new_status, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	
	_, err := al.db.DB().Exec(query, event.TaskID, event.UserID, event.Action, 
		event.Details, event.OldStatus, event.NewStatus, event.Timestamp)
	
	if err != nil {
		al.logger.WithError(err).Error("Failed to log audit event")
		return fmt.Errorf("failed to log audit event: %w", err)
	}

	// Also log to application logger for real-time monitoring
	al.logger.WithField("task_id", event.TaskID).
		WithField("user_id", event.UserID).
		WithField("action", event.Action).
		WithField("details", event.Details).
		WithField("old_status", event.OldStatus).
		WithField("new_status", event.NewStatus).
		Info("Audit event logged")

	return nil
}

func (al *AuditLogger) GetTaskHistory(taskID string) ([]*AuditEvent, error) {
	query := `
		SELECT id, task_id, user_id, action, details, old_status, new_status, timestamp
		FROM audit_log 
		WHERE task_id = ? 
		ORDER BY timestamp ASC
	`
	
	rows, err := al.db.DB().Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to query task history: %w", err)
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		event := &AuditEvent{}
		err := rows.Scan(&event.ID, &event.TaskID, &event.UserID, &event.Action,
			&event.Details, &event.OldStatus, &event.NewStatus, &event.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return events, nil
}

func (al *AuditLogger) GetUserActions(userID int64, limit int) ([]*AuditEvent, error) {
	query := `
		SELECT id, task_id, user_id, action, details, old_status, new_status, timestamp
		FROM audit_log 
		WHERE user_id = ? 
		ORDER BY timestamp DESC
		LIMIT ?
	`
	
	rows, err := al.db.DB().Query(query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query user actions: %w", err)
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		event := &AuditEvent{}
		err := rows.Scan(&event.ID, &event.TaskID, &event.UserID, &event.Action,
			&event.Details, &event.OldStatus, &event.NewStatus, &event.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return events, nil
}

func (al *AuditLogger) GetRecentEvents(hours int, limit int) ([]*AuditEvent, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	
	query := `
		SELECT id, task_id, user_id, action, details, old_status, new_status, timestamp
		FROM audit_log 
		WHERE timestamp >= ? 
		ORDER BY timestamp DESC
		LIMIT ?
	`
	
	rows, err := al.db.DB().Query(query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent events: %w", err)
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		event := &AuditEvent{}
		err := rows.Scan(&event.ID, &event.TaskID, &event.UserID, &event.Action,
			&event.Details, &event.OldStatus, &event.NewStatus, &event.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return events, nil
}

func (al *AuditLogger) GetActionStats(since time.Time) (map[string]int, error) {
	query := `
		SELECT action, COUNT(*) as count
		FROM audit_log 
		WHERE timestamp >= ?
		GROUP BY action
		ORDER BY count DESC
	`
	
	rows, err := al.db.DB().Query(query, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query action stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var action string
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			return nil, fmt.Errorf("failed to scan action stats: %w", err)
		}
		stats[action] = count
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return stats, nil
}

func (al *AuditLogger) CleanupOldEvents(days int) error {
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	
	query := `DELETE FROM audit_log WHERE timestamp < ?`
	result, err := al.db.DB().Exec(query, cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup old audit events: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	al.logger.WithField("rows_deleted", rowsAffected).
		WithField("cutoff_date", cutoff).
		Info("Cleaned up old audit events")

	return nil
}