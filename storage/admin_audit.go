package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"telegram-archive-bot/utils"
)

// AdminAuditAction represents different types of admin actions
type AdminAuditAction string

const (
	// Command actions
	AdminActionCommand         AdminAuditAction = "COMMAND"
	AdminActionFileUpload      AdminAuditAction = "FILE_UPLOAD"
	AdminActionFileDownload    AdminAuditAction = "FILE_DOWNLOAD"
	AdminActionFileDelete      AdminAuditAction = "FILE_DELETE"
	
	// System actions
	AdminActionExtract         AdminAuditAction = "EXTRACT"
	AdminActionConvert         AdminAuditAction = "CONVERT"
	AdminActionCleanup         AdminAuditAction = "CLEANUP"
	AdminActionStop            AdminAuditAction = "STOP"
	AdminActionExit            AdminAuditAction = "EXIT"
	
	// Security actions
	AdminActionQuarantine      AdminAuditAction = "QUARANTINE"
	AdminActionSecurityReset   AdminAuditAction = "SECURITY_RESET"
	AdminActionRateLimitReset  AdminAuditAction = "RATE_LIMIT_RESET"
	
	// System management
	AdminActionHealthCheck     AdminAuditAction = "HEALTH_CHECK"
	AdminActionMetricsView     AdminAuditAction = "METRICS_VIEW"
	AdminActionSystemDiag      AdminAuditAction = "SYSTEM_DIAGNOSTIC"
	AdminActionConfigChange    AdminAuditAction = "CONFIG_CHANGE"
	
	// Authentication events
	AdminActionLogin           AdminAuditAction = "LOGIN"
	AdminActionUnauthorized    AdminAuditAction = "UNAUTHORIZED_ATTEMPT"
	AdminActionRateLimit       AdminAuditAction = "RATE_LIMITED"
)

// AdminAuditEntry represents a single audit log entry
type AdminAuditEntry struct {
	ID          int64                  `db:"id" json:"id"`
	UserID      int64                  `db:"user_id" json:"user_id"`
	Username    string                 `db:"username" json:"username"`
	Action      AdminAuditAction       `db:"action" json:"action"`
	Resource    string                 `db:"resource" json:"resource"`
	Details     map[string]interface{} `db:"details" json:"details"`
	ClientInfo  map[string]interface{} `db:"client_info" json:"client_info"`
	Result      string                 `db:"result" json:"result"`
	ErrorMsg    string                 `db:"error_message" json:"error_message,omitempty"`
	IPAddress   string                 `db:"ip_address" json:"ip_address,omitempty"`
	UserAgent   string                 `db:"user_agent" json:"user_agent,omitempty"`
	SessionID   string                 `db:"session_id" json:"session_id,omitempty"`
	Timestamp   time.Time              `db:"timestamp" json:"timestamp"`
	Duration    int64                  `db:"duration_ms" json:"duration_ms"`
}

// AdminAuditLogger handles audit logging for admin actions
type AdminAuditLogger struct {
	db     *sql.DB
	logger *utils.Logger
}

// NewAdminAuditLogger creates a new admin audit logger
func NewAdminAuditLogger(db *sql.DB, logger *utils.Logger) *AdminAuditLogger {
	return &AdminAuditLogger{
		db:     db,
		logger: logger,
	}
}

// LogAdminAction logs an admin action to the audit trail
func (aal *AdminAuditLogger) LogAdminAction(entry AdminAuditEntry) error {
	// Ensure timestamp is set
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Serialize details and client info as JSON
	detailsJSON, err := json.Marshal(entry.Details)
	if err != nil {
		aal.logger.WithError(err).Error("Failed to marshal audit entry details")
		detailsJSON = []byte("{}")
	}

	clientInfoJSON, err := json.Marshal(entry.ClientInfo)
	if err != nil {
		aal.logger.WithError(err).Error("Failed to marshal audit entry client info")
		clientInfoJSON = []byte("{}")
	}

	query := `
		INSERT INTO admin_audit_log (
			user_id, username, action, resource, details, client_info, 
			result, error_message, ip_address, user_agent, session_id, 
			timestamp, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = aal.db.Exec(query,
		entry.UserID,
		entry.Username,
		string(entry.Action),
		entry.Resource,
		string(detailsJSON),
		string(clientInfoJSON),
		entry.Result,
		entry.ErrorMsg,
		entry.IPAddress,
		entry.UserAgent,
		entry.SessionID,
		entry.Timestamp,
		entry.Duration,
	)

	if err != nil {
		aal.logger.WithError(err).
			WithField("user_id", entry.UserID).
			WithField("action", entry.Action).
			WithField("resource", entry.Resource).
			Error("Failed to insert admin audit log entry")
		return fmt.Errorf("failed to log admin action: %w", err)
	}

	aal.logger.WithField("user_id", entry.UserID).
		WithField("username", entry.Username).
		WithField("action", entry.Action).
		WithField("resource", entry.Resource).
		WithField("result", entry.Result).
		Debug("Admin action logged to audit trail")

	return nil
}

// LogCommand logs a command execution
func (aal *AdminAuditLogger) LogCommand(userID int64, username, command, args string, result string, duration time.Duration, err error) {
	details := map[string]interface{}{
		"command":   command,
		"arguments": args,
	}

	errorMsg := ""
	if err != nil {
		errorMsg = err.Error()
		result = "FAILED"
	}

	entry := AdminAuditEntry{
		UserID:    userID,
		Username:  username,
		Action:    AdminActionCommand,
		Resource:  command,
		Details:   details,
		Result:    result,
		ErrorMsg:  errorMsg,
		Timestamp: time.Now(),
		Duration:  duration.Milliseconds(),
	}

	if logErr := aal.LogAdminAction(entry); logErr != nil {
		aal.logger.WithError(logErr).Error("Failed to log command audit entry")
	}
}

// LogFileOperation logs file-related operations
func (aal *AdminAuditLogger) LogFileOperation(userID int64, username string, action AdminAuditAction, fileName string, fileSize int64, details map[string]interface{}, result string, err error) {
	if details == nil {
		details = make(map[string]interface{})
	}

	details["file_name"] = fileName
	details["file_size"] = fileSize

	errorMsg := ""
	if err != nil {
		errorMsg = err.Error()
		result = "FAILED"
	}

	entry := AdminAuditEntry{
		UserID:    userID,
		Username:  username,
		Action:    action,
		Resource:  fileName,
		Details:   details,
		Result:    result,
		ErrorMsg:  errorMsg,
		Timestamp: time.Now(),
	}

	if logErr := aal.LogAdminAction(entry); logErr != nil {
		aal.logger.WithError(logErr).Error("Failed to log file operation audit entry")
	}
}

// LogSystemAction logs system-level actions
func (aal *AdminAuditLogger) LogSystemAction(userID int64, username string, action AdminAuditAction, resource string, details map[string]interface{}, result string, err error) {
	if details == nil {
		details = make(map[string]interface{})
	}

	errorMsg := ""
	if err != nil {
		errorMsg = err.Error()
		result = "FAILED"
	}

	entry := AdminAuditEntry{
		UserID:    userID,
		Username:  username,
		Action:    action,
		Resource:  resource,
		Details:   details,
		Result:    result,
		ErrorMsg:  errorMsg,
		Timestamp: time.Now(),
	}

	if logErr := aal.LogAdminAction(entry); logErr != nil {
		aal.logger.WithError(logErr).Error("Failed to log system action audit entry")
	}
}

// LogSecurityEvent logs security-related events
func (aal *AdminAuditLogger) LogSecurityEvent(userID int64, username string, action AdminAuditAction, resource string, details map[string]interface{}, severity string) {
	if details == nil {
		details = make(map[string]interface{})
	}

	details["severity"] = severity

	entry := AdminAuditEntry{
		UserID:    userID,
		Username:  username,
		Action:    action,
		Resource:  resource,
		Details:   details,
		Result:    "SECURITY_EVENT",
		Timestamp: time.Now(),
	}

	if logErr := aal.LogAdminAction(entry); logErr != nil {
		aal.logger.WithError(logErr).Error("Failed to log security event audit entry")
	}
}

// LogUnauthorizedAttempt logs unauthorized access attempts
func (aal *AdminAuditLogger) LogUnauthorizedAttempt(userID int64, username, attemptedAction, ipAddress string) {
	details := map[string]interface{}{
		"attempted_action": attemptedAction,
		"security_note":    "User not in authorized admin list",
	}

	entry := AdminAuditEntry{
		UserID:    userID,
		Username:  username,
		Action:    AdminActionUnauthorized,
		Resource:  attemptedAction,
		Details:   details,
		Result:    "BLOCKED",
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	if logErr := aal.LogAdminAction(entry); logErr != nil {
		aal.logger.WithError(logErr).Error("Failed to log unauthorized attempt audit entry")
	}
}

// LogRateLimitEvent logs rate limiting events
func (aal *AdminAuditLogger) LogRateLimitEvent(userID int64, username, resource string, limitType string, remainingTokens int) {
	details := map[string]interface{}{
		"limit_type":        limitType,
		"remaining_tokens":  remainingTokens,
		"enforcement_rule":  "Progressive blocking enabled",
	}

	entry := AdminAuditEntry{
		UserID:    userID,
		Username:  username,
		Action:    AdminActionRateLimit,
		Resource:  resource,
		Details:   details,
		Result:    "RATE_LIMITED",
		Timestamp: time.Now(),
	}

	if logErr := aal.LogAdminAction(entry); logErr != nil {
		aal.logger.WithError(logErr).Error("Failed to log rate limit event audit entry")
	}
}

// GetAuditEntries retrieves audit entries with filtering and pagination
func (aal *AdminAuditLogger) GetAuditEntries(filters AuditFilters) ([]AdminAuditEntry, error) {
	query := `
		SELECT id, user_id, username, action, resource, details, client_info, 
		       result, error_message, ip_address, user_agent, session_id, 
		       timestamp, duration_ms
		FROM admin_audit_log 
		WHERE 1=1
	`
	args := []interface{}{}
	argIndex := 1

	// Apply filters
	if filters.UserID != 0 {
		query += fmt.Sprintf(" AND user_id = ?%d", argIndex)
		args = append(args, filters.UserID)
		argIndex++
	}

	if filters.Action != "" {
		query += fmt.Sprintf(" AND action = ?%d", argIndex)
		args = append(args, filters.Action)
		argIndex++
	}

	if !filters.StartTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= ?%d", argIndex)
		args = append(args, filters.StartTime)
		argIndex++
	}

	if !filters.EndTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= ?%d", argIndex)
		args = append(args, filters.EndTime)
		argIndex++
	}

	if filters.Resource != "" {
		query += fmt.Sprintf(" AND resource LIKE ?%d", argIndex)
		args = append(args, "%"+filters.Resource+"%")
		argIndex++
	}

	if filters.Result != "" {
		query += fmt.Sprintf(" AND result = ?%d", argIndex)
		args = append(args, filters.Result)
		argIndex++
	}

	// Order by timestamp (newest first)
	query += " ORDER BY timestamp DESC"

	// Apply limit
	if filters.Limit > 0 {
		query += fmt.Sprintf(" LIMIT ?%d", argIndex)
		args = append(args, filters.Limit)
		argIndex++
	}

	if filters.Offset > 0 {
		query += fmt.Sprintf(" OFFSET ?%d", argIndex)
		args = append(args, filters.Offset)
	}

	rows, err := aal.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit entries: %w", err)
	}
	defer rows.Close()

	var entries []AdminAuditEntry
	for rows.Next() {
		var entry AdminAuditEntry
		var detailsJSON, clientInfoJSON string

		err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.Username,
			&entry.Action,
			&entry.Resource,
			&detailsJSON,
			&clientInfoJSON,
			&entry.Result,
			&entry.ErrorMsg,
			&entry.IPAddress,
			&entry.UserAgent,
			&entry.SessionID,
			&entry.Timestamp,
			&entry.Duration,
		)
		if err != nil {
			aal.logger.WithError(err).Error("Failed to scan audit entry")
			continue
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal([]byte(detailsJSON), &entry.Details); err != nil {
			aal.logger.WithError(err).Error("Failed to unmarshal details JSON")
			entry.Details = make(map[string]interface{})
		}

		if err := json.Unmarshal([]byte(clientInfoJSON), &entry.ClientInfo); err != nil {
			aal.logger.WithError(err).Error("Failed to unmarshal client info JSON")
			entry.ClientInfo = make(map[string]interface{})
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// GetAuditStats returns statistics about audit entries
func (aal *AdminAuditLogger) GetAuditStats(timeRange time.Duration) (*AdminAuditStats, error) {
	since := time.Now().Add(-timeRange)

	query := `
		SELECT 
			COUNT(*) as total_entries,
			COUNT(CASE WHEN result = 'SUCCESS' THEN 1 END) as successful_actions,
			COUNT(CASE WHEN result = 'FAILED' THEN 1 END) as failed_actions,
			COUNT(CASE WHEN result = 'BLOCKED' THEN 1 END) as blocked_actions,
			COUNT(CASE WHEN result = 'RATE_LIMITED' THEN 1 END) as rate_limited_actions,
			COUNT(DISTINCT user_id) as unique_users,
			COUNT(DISTINCT action) as unique_actions,
			AVG(duration_ms) as avg_duration_ms
		FROM admin_audit_log 
		WHERE timestamp >= ?
	`

	stats := &AdminAuditStats{}
	err := aal.db.QueryRow(query, since).Scan(
		&stats.TotalEntries,
		&stats.SuccessfulActions,
		&stats.FailedActions,
		&stats.BlockedActions,
		&stats.RateLimitedActions,
		&stats.UniqueUsers,
		&stats.UniqueActions,
		&stats.AvgDurationMs,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get audit stats: %w", err)
	}

	// Get action breakdown
	actionQuery := `
		SELECT action, COUNT(*) as count
		FROM admin_audit_log 
		WHERE timestamp >= ?
		GROUP BY action
		ORDER BY count DESC
	`

	rows, err := aal.db.Query(actionQuery, since)
	if err != nil {
		aal.logger.WithError(err).Error("Failed to get action breakdown")
	} else {
		defer rows.Close()
		stats.ActionBreakdown = make(map[string]int)

		for rows.Next() {
			var action string
			var count int
			if err := rows.Scan(&action, &count); err == nil {
				stats.ActionBreakdown[action] = count
			}
		}
	}

	// Get user breakdown
	userQuery := `
		SELECT user_id, username, COUNT(*) as count
		FROM admin_audit_log 
		WHERE timestamp >= ?
		GROUP BY user_id, username
		ORDER BY count DESC
	`

	rows, err = aal.db.Query(userQuery, since)
	if err != nil {
		aal.logger.WithError(err).Error("Failed to get user breakdown")
	} else {
		defer rows.Close()
		stats.UserBreakdown = make(map[string]int)

		for rows.Next() {
			var userID int64
			var username string
			var count int
			if err := rows.Scan(&userID, &username, &count); err == nil {
				userKey := fmt.Sprintf("%d (%s)", userID, username)
				stats.UserBreakdown[userKey] = count
			}
		}
	}

	stats.TimeRange = timeRange
	return stats, nil
}

// AuditFilters defines filters for querying audit entries
type AuditFilters struct {
	UserID    int64
	Action    string
	Resource  string
	Result    string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
}

// AdminAuditStats provides statistics about admin audit entries
type AdminAuditStats struct {
	TotalEntries        int
	SuccessfulActions   int
	FailedActions       int
	BlockedActions      int
	RateLimitedActions  int
	UniqueUsers         int
	UniqueActions       int
	AvgDurationMs       float64
	ActionBreakdown     map[string]int
	UserBreakdown       map[string]int
	TimeRange           time.Duration
}

// CleanupOldEntries removes audit entries older than the specified duration
func (aal *AdminAuditLogger) CleanupOldEntries(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	result, err := aal.db.Exec(
		"DELETE FROM admin_audit_log WHERE timestamp < ?",
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old audit entries: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	aal.logger.WithField("rows_deleted", rowsAffected).
		WithField("cutoff_date", cutoff).
		Info("Cleaned up old audit entries")

	return rowsAffected, nil
}