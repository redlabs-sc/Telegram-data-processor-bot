package storage

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupService provides database backup and restore functionality
type BackupService struct {
	db         *Database
	backupDir  string
	retention  time.Duration
}

// BackupOptions configures backup behavior
type BackupOptions struct {
	BackupDir       string        // Directory to store backups
	RetentionPeriod time.Duration // How long to keep backups
	Compress        bool          // Whether to compress backups
	VerifyBackup    bool          // Whether to verify backup integrity
}

// RestoreOptions configures restore behavior
type RestoreOptions struct {
	BackupFile      string // Path to backup file to restore from
	VerifyRestore   bool   // Whether to verify restore integrity
	CreateBackup    bool   // Whether to backup current DB before restore
}

// NewBackupService creates a new backup service instance
func NewBackupService(db *Database, opts BackupOptions) (*BackupService, error) {
	if opts.BackupDir == "" {
		opts.BackupDir = "backups"
	}
	
	if opts.RetentionPeriod == 0 {
		opts.RetentionPeriod = 30 * 24 * time.Hour // 30 days default
	}

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(opts.BackupDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	return &BackupService{
		db:        db,
		backupDir: opts.BackupDir,
		retention: opts.RetentionPeriod,
	}, nil
}

// CreateBackup creates a new database backup
func (bs *BackupService) CreateBackup(opts BackupOptions) (string, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("bot_backup_%s.sql", timestamp)
	
	// Add .gz extension if compression is requested
	if opts.Compress {
		backupName += ".gz"
	}
	
	backupPath := filepath.Join(bs.backupDir, backupName)

	// Create the backup file
	backupFile, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer backupFile.Close()

	var writer io.Writer = backupFile
	var gzipWriter *gzip.Writer

	// Use compression if requested
	if opts.Compress {
		gzipWriter = gzip.NewWriter(backupFile)
		writer = gzipWriter
	}

	// Perform the backup
	if err := bs.dumpDatabase(writer); err != nil {
		os.Remove(backupPath) // Clean up partial backup
		return "", fmt.Errorf("failed to dump database: %w", err)
	}

	// Close gzip writer if used
	if gzipWriter != nil {
		if err := gzipWriter.Close(); err != nil {
			return "", fmt.Errorf("failed to close gzip writer: %w", err)
		}
	}

	// Verify backup if requested
	if opts.VerifyBackup {
		if err := bs.verifyBackup(backupPath, opts.Compress); err != nil {
			return "", fmt.Errorf("backup verification failed: %w", err)
		}
	}

	return backupPath, nil
}

// RestoreFromBackup restores database from a backup file
func (bs *BackupService) RestoreFromBackup(opts RestoreOptions) error {
	// Verify backup file exists
	if _, err := os.Stat(opts.BackupFile); os.IsNotExist(err) {
		return fmt.Errorf("backup file does not exist: %s", opts.BackupFile)
	}

	// Create backup of current database if requested
	if opts.CreateBackup {
		backupOpts := BackupOptions{
			BackupDir:    bs.backupDir,
			Compress:     true,
			VerifyBackup: false,
		}
		currentBackup, err := bs.CreateBackup(backupOpts)
		if err != nil {
			return fmt.Errorf("failed to backup current database: %w", err)
		}
		fmt.Printf("Created backup of current database: %s\n", currentBackup)
	}

	// Open backup file
	backupFile, err := os.Open(opts.BackupFile)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer backupFile.Close()

	var reader io.Reader = backupFile

	// Handle compressed backups
	if strings.HasSuffix(opts.BackupFile, ".gz") {
		gzipReader, err := gzip.NewReader(backupFile)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	// Restore the database
	if err := bs.restoreDatabase(reader); err != nil {
		return fmt.Errorf("failed to restore database: %w", err)
	}

	// Verify restore if requested
	if opts.VerifyRestore {
		if err := bs.verifyDatabaseIntegrity(); err != nil {
			return fmt.Errorf("restore verification failed: %w", err)
		}
	}

	return nil
}

// CleanupOldBackups removes backup files older than retention period
func (bs *BackupService) CleanupOldBackups() error {
	cutoffTime := time.Now().Add(-bs.retention)
	
	entries, err := os.ReadDir(bs.backupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	var removed int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		
		// Check if it's a backup file
		name := entry.Name()
		if !strings.HasPrefix(name, "bot_backup_") {
			continue
		}
		
		info, err := entry.Info()
		if err != nil {
			continue
		}
		
		if info.ModTime().Before(cutoffTime) {
			backupPath := filepath.Join(bs.backupDir, name)
			if err := os.Remove(backupPath); err != nil {
				fmt.Printf("Warning: failed to remove old backup %s: %v\n", backupPath, err)
			} else {
				removed++
			}
		}
	}
	
	if removed > 0 {
		fmt.Printf("Cleaned up %d old backup files\n", removed)
	}
	
	return nil
}

// ListBackups returns a list of available backup files
func (bs *BackupService) ListBackups() ([]BackupInfo, error) {
	entries, err := os.ReadDir(bs.backupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		
		name := entry.Name()
		if !strings.HasPrefix(name, "bot_backup_") {
			continue
		}
		
		info, err := entry.Info()
		if err != nil {
			continue
		}
		
		backup := BackupInfo{
			Name:       name,
			Path:       filepath.Join(bs.backupDir, name),
			Size:       info.Size(),
			Created:    info.ModTime(),
			Compressed: strings.HasSuffix(name, ".gz"),
		}
		
		backups = append(backups, backup)
	}
	
	return backups, nil
}

// BackupInfo contains information about a backup file
type BackupInfo struct {
	Name       string
	Path       string
	Size       int64
	Created    time.Time
	Compressed bool
}

// dumpDatabase performs the actual database dump
func (bs *BackupService) dumpDatabase(writer io.Writer) error {
	// Write backup header
	fmt.Fprintf(writer, "-- Telegram Archive Bot Database Backup\n")
	fmt.Fprintf(writer, "-- Created: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(writer, "-- Database: %s\n\n", "bot.db")
	
	// Begin transaction for consistent backup
	fmt.Fprintf(writer, "BEGIN TRANSACTION;\n\n")
	
	// Get all table names
	tables, err := bs.getTables()
	if err != nil {
		return fmt.Errorf("failed to get table list: %w", err)
	}
	
	// Dump each table
	for _, table := range tables {
		if err := bs.dumpTable(writer, table); err != nil {
			return fmt.Errorf("failed to dump table %s: %w", table, err)
		}
	}
	
	// Commit transaction
	fmt.Fprintf(writer, "COMMIT;\n")
	
	return nil
}

// getTables returns list of all tables in the database
func (bs *BackupService) getTables() ([]string, error) {
	query := `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
	rows, err := bs.db.DB().Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}
	
	return tables, rows.Err()
}

// dumpTable dumps a single table's schema and data
func (bs *BackupService) dumpTable(writer io.Writer, tableName string) error {
	// Dump table schema
	schemaQuery := fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='table' AND name='%s'", tableName)
	var schema string
	if err := bs.db.DB().QueryRow(schemaQuery).Scan(&schema); err != nil {
		return err
	}
	
	fmt.Fprintf(writer, "-- Table: %s\n", tableName)
	fmt.Fprintf(writer, "DROP TABLE IF EXISTS %s;\n", tableName)
	fmt.Fprintf(writer, "%s;\n\n", schema)
	
	// Dump table data
	dataQuery := fmt.Sprintf("SELECT * FROM %s", tableName)
	rows, err := bs.db.DB().Query(dataQuery)
	if err != nil {
		return err
	}
	defer rows.Close()
	
	// Get column information
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	
	// Create placeholders for row data
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}
	
	// Dump each row
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return err
		}
		
		// Build INSERT statement
		fmt.Fprintf(writer, "INSERT INTO %s VALUES (", tableName)
		for i, value := range values {
			if i > 0 {
				fmt.Fprintf(writer, ", ")
			}
			
			if value == nil {
				fmt.Fprintf(writer, "NULL")
			} else {
				switch v := value.(type) {
				case string:
					fmt.Fprintf(writer, "'%s'", strings.ReplaceAll(v, "'", "''"))
				case []byte:
					fmt.Fprintf(writer, "'%s'", strings.ReplaceAll(string(v), "'", "''"))
				case int, int64, int32, float64, float32:
					fmt.Fprintf(writer, "%v", v)
				default:
					// For any other type (including time.Time), quote it as a string
					valueStr := fmt.Sprintf("%v", v)
					fmt.Fprintf(writer, "'%s'", strings.ReplaceAll(valueStr, "'", "''"))
				}
			}
		}
		fmt.Fprintf(writer, ");\n")
	}
	
	fmt.Fprintf(writer, "\n")
	return rows.Err()
}

// restoreDatabase restores database from SQL dump
func (bs *BackupService) restoreDatabase(reader io.Reader) error {
	// Read the entire SQL dump
	sqlBytes, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}
	
	sqlContent := string(sqlBytes)
	
	// Split into individual statements
	statements := strings.Split(sqlContent, ";\n")
	
	// Execute each statement
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		
		// Skip statements that might cause conflicts during restore
		if strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS") {
			continue // Skip IF NOT EXISTS statements as they may conflict
		}
		
		// Execute the statement, ignoring some expected errors
		if _, err := bs.db.DB().Exec(stmt); err != nil {
			// Ignore some common restore errors
			errStr := err.Error()
			if strings.Contains(errStr, "already exists") ||
			   strings.Contains(errStr, "duplicate column") ||
			   strings.Contains(errStr, "no such table") ||
			   strings.Contains(errStr, "UNIQUE constraint failed") ||
			   strings.Contains(errStr, "no transaction is active") ||
			   strings.Contains(errStr, "cannot commit") ||
			   strings.Contains(errStr, "cannot rollback") {
				continue // Ignore these errors during restore
			}
			return fmt.Errorf("failed to execute statement: %s, error: %w", stmt, err)
		}
	}
	
	return nil
}

// verifyBackup verifies the integrity of a backup file
func (bs *BackupService) verifyBackup(backupPath string, compressed bool) error {
	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup file for verification: %w", err)
	}
	defer file.Close()
	
	var reader io.Reader = file
	
	if compressed {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader for verification: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}
	
	// Read the backup file to ensure it's not corrupted
	_, err = io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("backup file appears to be corrupted: %w", err)
	}
	
	return nil
}

// verifyDatabaseIntegrity checks database integrity after restore
func (bs *BackupService) verifyDatabaseIntegrity() error {
	var result string
	err := bs.db.DB().QueryRow("PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return fmt.Errorf("failed to run integrity check: %w", err)
	}
	
	if result != "ok" {
		return fmt.Errorf("database integrity check failed: %s", result)
	}
	
	return nil
}

// GetBackupStats returns statistics about backups
func (bs *BackupService) GetBackupStats() (BackupStats, error) {
	backups, err := bs.ListBackups()
	if err != nil {
		return BackupStats{}, err
	}
	
	stats := BackupStats{
		TotalBackups: len(backups),
		BackupDir:    bs.backupDir,
		Retention:    bs.retention,
	}
	
	var totalSize int64
	var oldestBackup, newestBackup time.Time
	
	for i, backup := range backups {
		totalSize += backup.Size
		
		if i == 0 {
			oldestBackup = backup.Created
			newestBackup = backup.Created
		} else {
			if backup.Created.Before(oldestBackup) {
				oldestBackup = backup.Created
			}
			if backup.Created.After(newestBackup) {
				newestBackup = backup.Created
			}
		}
	}
	
	stats.TotalSize = totalSize
	if len(backups) > 0 {
		stats.OldestBackup = &oldestBackup
		stats.NewestBackup = &newestBackup
	}
	
	return stats, nil
}

// BackupStats contains statistics about the backup system
type BackupStats struct {
	TotalBackups int
	TotalSize    int64
	BackupDir    string
	Retention    time.Duration
	OldestBackup *time.Time
	NewestBackup *time.Time
}