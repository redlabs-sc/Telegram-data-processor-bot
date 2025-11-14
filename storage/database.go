package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
}

func NewDatabase(dbPath string) (*Database, error) {
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)

	database := &Database{db: db}

	if err := database.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return database, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) DB() *sql.DB {
	return d.db
}

func (d *Database) migrate() error {
	// Create migration tracking table first
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	migrations := []struct {
		version int
		sql     string
	}{
		{1, `CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			file_name TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			file_type TEXT NOT NULL,
			file_hash TEXT NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			completed_at DATETIME
		)`},
		{2, `CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id)`},
		{3, `CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`},
		{4, `CREATE INDEX IF NOT EXISTS idx_tasks_file_hash ON tasks(file_hash)`},
		{5, `CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at)`},
		{6, `CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT,
			user_id INTEGER,
			action TEXT NOT NULL,
			details TEXT,
			old_status TEXT,
			new_status TEXT,
			timestamp DATETIME NOT NULL
		)`},
		{7, `CREATE INDEX IF NOT EXISTS idx_audit_task_id ON audit_log(task_id)`},
		{8, `CREATE INDEX IF NOT EXISTS idx_audit_user_id ON audit_log(user_id)`},
		{9, `CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp)`},
		{10, `CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action)`},
		{11, `ALTER TABLE tasks ADD COLUMN chat_id INTEGER DEFAULT 0`},
		{12, `ALTER TABLE tasks ADD COLUMN telegram_file_id TEXT DEFAULT ''`},
		{13, `ALTER TABLE tasks ADD COLUMN error_category TEXT DEFAULT ''`},
		{14, `ALTER TABLE tasks ADD COLUMN error_severity TEXT DEFAULT ''`},
		{15, `ALTER TABLE tasks ADD COLUMN retry_count INTEGER DEFAULT 0`},
		{16, `CREATE TABLE IF NOT EXISTS dead_letter_queue (
			id TEXT PRIMARY KEY,
			original_task_id TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			chat_id INTEGER NOT NULL,
			file_name TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			file_type TEXT NOT NULL,
			file_hash TEXT NOT NULL,
			telegram_file_id TEXT NOT NULL,
			reason TEXT NOT NULL,
			final_error TEXT NOT NULL,
			error_category TEXT DEFAULT '',
			error_severity TEXT DEFAULT '',
			retry_count INTEGER DEFAULT 0,
			task_context TEXT DEFAULT '',
			created_at DATETIME NOT NULL,
			dead_letter_at DATETIME NOT NULL,
			last_attempt_at DATETIME,
			can_retry BOOLEAN DEFAULT false,
			requires_manual BOOLEAN DEFAULT false
		)`},
		{17, `CREATE INDEX IF NOT EXISTS idx_dead_letter_reason ON dead_letter_queue(reason)`},
		{18, `CREATE INDEX IF NOT EXISTS idx_dead_letter_original_task ON dead_letter_queue(original_task_id)`},
		{19, `CREATE INDEX IF NOT EXISTS idx_dead_letter_user_id ON dead_letter_queue(user_id)`},
		{20, `CREATE INDEX IF NOT EXISTS idx_dead_letter_dead_letter_at ON dead_letter_queue(dead_letter_at)`},
		{21, `CREATE INDEX IF NOT EXISTS idx_dead_letter_can_retry ON dead_letter_queue(can_retry)`},
		{22, `CREATE INDEX IF NOT EXISTS idx_dead_letter_requires_manual ON dead_letter_queue(requires_manual)`},
		{23, `CREATE TABLE IF NOT EXISTS security_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT,
			event_type TEXT NOT NULL,
			threat_level INTEGER NOT NULL,
			description TEXT NOT NULL,
			file_name TEXT,
			file_hash TEXT,
			user_id INTEGER,
			warnings TEXT DEFAULT '[]',
			action_taken TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			timestamp DATETIME NOT NULL
		)`},
		{24, `CREATE INDEX IF NOT EXISTS idx_security_audit_task_id ON security_audit(task_id)`},
		{25, `CREATE INDEX IF NOT EXISTS idx_security_audit_event_type ON security_audit(event_type)`},
		{26, `CREATE INDEX IF NOT EXISTS idx_security_audit_threat_level ON security_audit(threat_level)`},
		{27, `CREATE INDEX IF NOT EXISTS idx_security_audit_user_id ON security_audit(user_id)`},
		{28, `CREATE INDEX IF NOT EXISTS idx_security_audit_timestamp ON security_audit(timestamp)`},
		{29, `CREATE INDEX IF NOT EXISTS idx_security_audit_action_taken ON security_audit(action_taken)`},
		{30, `CREATE TABLE IF NOT EXISTS admin_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			action TEXT NOT NULL,
			resource TEXT NOT NULL,
			details TEXT DEFAULT '{}',
			client_info TEXT DEFAULT '{}',
			result TEXT NOT NULL,
			error_message TEXT DEFAULT '',
			ip_address TEXT DEFAULT '',
			user_agent TEXT DEFAULT '',
			session_id TEXT DEFAULT '',
			timestamp DATETIME NOT NULL,
			duration_ms INTEGER DEFAULT 0
		)`},
		{31, `CREATE INDEX IF NOT EXISTS idx_admin_audit_user_id ON admin_audit_log(user_id)`},
		{32, `CREATE INDEX IF NOT EXISTS idx_admin_audit_action ON admin_audit_log(action)`},
		{33, `CREATE INDEX IF NOT EXISTS idx_admin_audit_resource ON admin_audit_log(resource)`},
		{34, `CREATE INDEX IF NOT EXISTS idx_admin_audit_result ON admin_audit_log(result)`},
		{35, `CREATE INDEX IF NOT EXISTS idx_admin_audit_timestamp ON admin_audit_log(timestamp)`},
		{36, `CREATE INDEX IF NOT EXISTS idx_admin_audit_username ON admin_audit_log(username)`},
		{37, `CREATE INDEX IF NOT EXISTS idx_admin_audit_session_id ON admin_audit_log(session_id)`},
		{38, `ALTER TABLE tasks ADD COLUMN local_api_path TEXT DEFAULT ''`},
		{39, `ALTER TABLE tasks ADD COLUMN notified INTEGER DEFAULT 0`},
	}

	// Apply migrations that haven't been applied yet
	for _, migration := range migrations {
		var count int
		err := d.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migration.version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if count == 0 {
			// Execute migration with enhanced error handling
			_, err := d.db.Exec(migration.sql)
			if err != nil {
				// Check if it's a duplicate column/table/index error (safe to ignore)
				errorStr := strings.ToLower(err.Error())
				isIgnorableError := strings.Contains(errorStr, "duplicate column name") ||
					strings.Contains(errorStr, "table") && strings.Contains(errorStr, "already exists") ||
					strings.Contains(errorStr, "index") && strings.Contains(errorStr, "already exists")
				
				if !isIgnorableError {
					return fmt.Errorf("migration %d failed: %w", migration.version, err)
				}
				// Log that we ignored a duplicate error (but don't fail)
				fmt.Printf("Migration %d: Ignored duplicate error: %v\n", migration.version, err)
			}

			// Record that this migration has been applied
			_, err = d.db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", migration.version)
			if err != nil {
				return fmt.Errorf("failed to record migration %d: %w", migration.version, err)
			}
		}
	}

	return nil
}