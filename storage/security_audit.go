package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"telegram-archive-bot/utils"
)

// SecurityEvent represents a security-related event
type SecurityEvent struct {
	ID            int64                  `json:"id"`
	TaskID        string                 `json:"task_id"`
	EventType     SecurityEventType      `json:"event_type"`
	ThreatLevel   utils.ThreatLevel      `json:"threat_level"`
	Description   string                 `json:"description"`
	FileName      string                 `json:"file_name"`
	FileHash      string                 `json:"file_hash,omitempty"`
	UserID        int64                  `json:"user_id"`
	Warnings      []string               `json:"warnings,omitempty"`
	ActionTaken   SecurityAction         `json:"action_taken"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Timestamp     time.Time              `json:"timestamp"`
}

// SecurityEventType represents different types of security events
type SecurityEventType string

const (
	SecurityEventFileValidation    SecurityEventType = "file_validation"
	SecurityEventFileSanitization  SecurityEventType = "file_sanitization"
	SecurityEventFileQuarantine    SecurityEventType = "file_quarantine"
	SecurityEventSuspiciousContent SecurityEventType = "suspicious_content"
	SecurityEventRateLimitHit      SecurityEventType = "rate_limit_hit"
	SecurityEventUnauthorizedAccess SecurityEventType = "unauthorized_access"
	SecurityEventMalwareDetection  SecurityEventType = "malware_detection"
	SecurityEventPolicyViolation   SecurityEventType = "policy_violation"
)

// SecurityAction represents the action taken in response to a security event
type SecurityAction string

const (
	SecurityActionAllow      SecurityAction = "allow"
	SecurityActionSanitize   SecurityAction = "sanitize"
	SecurityActionQuarantine SecurityAction = "quarantine"
	SecurityActionReject     SecurityAction = "reject"
	SecurityActionMonitor    SecurityAction = "monitor"
	SecurityActionAlert      SecurityAction = "alert"
)

// SecurityAuditLogger handles security event logging
type SecurityAuditLogger struct {
	db     *sql.DB
	logger *utils.Logger
}

// NewSecurityAuditLogger creates a new security audit logger
func NewSecurityAuditLogger(db *sql.DB, logger *utils.Logger) *SecurityAuditLogger {
	return &SecurityAuditLogger{
		db:     db,
		logger: logger,
	}
}

// LogSecurityEvent logs a security event to the database
func (sal *SecurityAuditLogger) LogSecurityEvent(event *SecurityEvent) error {
	event.Timestamp = time.Now()
	
	// Serialize warnings and metadata as JSON
	warningsJSON, err := json.Marshal(event.Warnings)
	if err != nil {
		warningsJSON = []byte("[]")
	}
	
	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}
	
	query := `
		INSERT INTO security_audit (
			task_id, event_type, threat_level, description, file_name, file_hash,
			user_id, warnings, action_taken, metadata, timestamp
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	
	result, err := sal.db.Exec(query,
		event.TaskID,
		string(event.EventType),
		int(event.ThreatLevel),
		event.Description,
		event.FileName,
		event.FileHash,
		event.UserID,
		string(warningsJSON),
		string(event.ActionTaken),
		string(metadataJSON),
		event.Timestamp,
	)
	
	if err != nil {
		sal.logger.WithError(err).Error("Failed to log security event")
		return fmt.Errorf("failed to log security event: %w", err)
	}
	
	id, err := result.LastInsertId()
	if err == nil {
		event.ID = id
	}
	
	sal.logger.WithField("event_id", event.ID).
		WithField("event_type", event.EventType).
		WithField("threat_level", event.ThreatLevel).
		WithField("task_id", event.TaskID).
		Info("Security event logged")
	
	return nil
}

// LogFileValidationEvent logs a file validation security event
func (sal *SecurityAuditLogger) LogFileValidationEvent(taskID, fileName, fileHash string, userID int64, result *utils.ValidationResult, action SecurityAction) error {
	event := &SecurityEvent{
		TaskID:      taskID,
		EventType:   SecurityEventFileValidation,
		ThreatLevel: result.ThreatLevel,
		Description: fmt.Sprintf("File validation completed with threat level %s", result.ThreatLevel.String()),
		FileName:    fileName,
		FileHash:    fileHash,
		UserID:      userID,
		Warnings:    result.SecurityWarnings,
		ActionTaken: action,
		Metadata: map[string]interface{}{
			"file_size":       result.FileSize,
			"actual_mimetype": result.ActualMimeType,
			"file_type":       result.FileType,
			"valid":           result.Valid,
			"sanitized":       len(result.SanitizationLog) > 0,
			"sanitization_log": result.SanitizationLog,
		},
	}
	
	return sal.LogSecurityEvent(event)
}

// LogSuspiciousActivity logs suspicious activity events
func (sal *SecurityAuditLogger) LogSuspiciousActivity(taskID, description string, userID int64, metadata map[string]interface{}) error {
	event := &SecurityEvent{
		TaskID:      taskID,
		EventType:   SecurityEventSuspiciousContent,
		ThreatLevel: utils.ThreatLevelMedium,
		Description: description,
		UserID:      userID,
		ActionTaken: SecurityActionMonitor,
		Metadata:    metadata,
	}
	
	return sal.LogSecurityEvent(event)
}

// LogQuarantineEvent logs file quarantine events
func (sal *SecurityAuditLogger) LogQuarantineEvent(taskID, fileName, fileHash, reason string, userID int64) error {
	event := &SecurityEvent{
		TaskID:      taskID,
		EventType:   SecurityEventFileQuarantine,
		ThreatLevel: utils.ThreatLevelHigh,
		Description: fmt.Sprintf("File quarantined: %s", reason),
		FileName:    fileName,
		FileHash:    fileHash,
		UserID:      userID,
		ActionTaken: SecurityActionQuarantine,
		Metadata: map[string]interface{}{
			"quarantine_reason": reason,
		},
	}
	
	return sal.LogSecurityEvent(event)
}

// GetSecurityEvents retrieves security events with pagination
func (sal *SecurityAuditLogger) GetSecurityEvents(limit, offset int) ([]*SecurityEvent, error) {
	query := `
		SELECT id, task_id, event_type, threat_level, description, file_name, file_hash,
			   user_id, warnings, action_taken, metadata, timestamp
		FROM security_audit
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`
	
	rows, err := sal.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query security events: %w", err)
	}
	defer rows.Close()
	
	events := make([]*SecurityEvent, 0)
	
	for rows.Next() {
		event := &SecurityEvent{}
		var warningsJSON, metadataJSON string
		var threatLevelInt int
		
		err := rows.Scan(
			&event.ID,
			&event.TaskID,
			&event.EventType,
			&threatLevelInt,
			&event.Description,
			&event.FileName,
			&event.FileHash,
			&event.UserID,
			&warningsJSON,
			&event.ActionTaken,
			&metadataJSON,
			&event.Timestamp,
		)
		
		if err != nil {
			sal.logger.WithError(err).Warn("Failed to scan security event row")
			continue
		}
		
		event.ThreatLevel = utils.ThreatLevel(threatLevelInt)
		
		// Deserialize JSON fields
		if err := json.Unmarshal([]byte(warningsJSON), &event.Warnings); err != nil {
			event.Warnings = []string{}
		}
		
		if err := json.Unmarshal([]byte(metadataJSON), &event.Metadata); err != nil {
			event.Metadata = make(map[string]interface{})
		}
		
		events = append(events, event)
	}
	
	return events, nil
}

// GetSecurityEventsByTaskID retrieves security events for a specific task
func (sal *SecurityAuditLogger) GetSecurityEventsByTaskID(taskID string) ([]*SecurityEvent, error) {
	query := `
		SELECT id, task_id, event_type, threat_level, description, file_name, file_hash,
			   user_id, warnings, action_taken, metadata, timestamp
		FROM security_audit
		WHERE task_id = ?
		ORDER BY timestamp ASC
	`
	
	rows, err := sal.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to query security events for task: %w", err)
	}
	defer rows.Close()
	
	events := make([]*SecurityEvent, 0)
	
	for rows.Next() {
		event := &SecurityEvent{}
		var warningsJSON, metadataJSON string
		var threatLevelInt int
		
		err := rows.Scan(
			&event.ID,
			&event.TaskID,
			&event.EventType,
			&threatLevelInt,
			&event.Description,
			&event.FileName,
			&event.FileHash,
			&event.UserID,
			&warningsJSON,
			&event.ActionTaken,
			&metadataJSON,
			&event.Timestamp,
		)
		
		if err != nil {
			sal.logger.WithError(err).Warn("Failed to scan security event row")
			continue
		}
		
		event.ThreatLevel = utils.ThreatLevel(threatLevelInt)
		
		// Deserialize JSON fields
		if err := json.Unmarshal([]byte(warningsJSON), &event.Warnings); err != nil {
			event.Warnings = []string{}
		}
		
		if err := json.Unmarshal([]byte(metadataJSON), &event.Metadata); err != nil {
			event.Metadata = make(map[string]interface{})
		}
		
		events = append(events, event)
	}
	
	return events, nil
}

// GetSecurityStats returns security statistics
func (sal *SecurityAuditLogger) GetSecurityStats(since time.Time) (*SecurityStats, error) {
	stats := &SecurityStats{
		Period: since,
	}
	
	// Count events by type
	typeQuery := `
		SELECT event_type, COUNT(*) as count
		FROM security_audit
		WHERE timestamp >= ?
		GROUP BY event_type
	`
	
	rows, err := sal.db.Query(typeQuery, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query event type stats: %w", err)
	}
	defer rows.Close()
	
	stats.EventsByType = make(map[SecurityEventType]int)
	for rows.Next() {
		var eventType SecurityEventType
		var count int
		if err := rows.Scan(&eventType, &count); err == nil {
			stats.EventsByType[eventType] = count
		}
	}
	
	// Count events by threat level
	threatQuery := `
		SELECT threat_level, COUNT(*) as count
		FROM security_audit
		WHERE timestamp >= ?
		GROUP BY threat_level
	`
	
	rows, err = sal.db.Query(threatQuery, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query threat level stats: %w", err)
	}
	defer rows.Close()
	
	stats.EventsByThreatLevel = make(map[utils.ThreatLevel]int)
	for rows.Next() {
		var threatLevelInt int
		var count int
		if err := rows.Scan(&threatLevelInt, &count); err == nil {
			stats.EventsByThreatLevel[utils.ThreatLevel(threatLevelInt)] = count
		}
	}
	
	// Count actions taken
	actionQuery := `
		SELECT action_taken, COUNT(*) as count
		FROM security_audit
		WHERE timestamp >= ?
		GROUP BY action_taken
	`
	
	rows, err = sal.db.Query(actionQuery, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query action stats: %w", err)
	}
	defer rows.Close()
	
	stats.ActionsTaken = make(map[SecurityAction]int)
	for rows.Next() {
		var action SecurityAction
		var count int
		if err := rows.Scan(&action, &count); err == nil {
			stats.ActionsTaken[action] = count
		}
	}
	
	return stats, nil
}

// SecurityStats represents security statistics
type SecurityStats struct {
	Period               time.Time                         `json:"period"`
	EventsByType         map[SecurityEventType]int         `json:"events_by_type"`
	EventsByThreatLevel  map[utils.ThreatLevel]int         `json:"events_by_threat_level"`
	ActionsTaken         map[SecurityAction]int            `json:"actions_taken"`
}