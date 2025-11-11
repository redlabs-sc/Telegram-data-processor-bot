package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// SecureTempManager handles secure temporary file operations
type SecureTempManager struct {
	logger           *Logger
	baseTempDir      string
	sessionID        string
	activeFiles      map[string]*TempFileInfo
	mutex            sync.RWMutex
	cleanupInterval  time.Duration
	maxFileAge       time.Duration
	secureDelete     bool
	stopCleanup      chan struct{}
	cleanupRunning   bool
}

// TempFileInfo tracks information about temporary files
type TempFileInfo struct {
	Path           string
	OriginalName   string
	TaskID         string
	CreatedAt      time.Time
	LastAccessed   time.Time
	Size           int64
	Permissions    os.FileMode
	IsSecure       bool
	CleanupMethod  CleanupMethod
	References     int
	Locked         bool
}

// CleanupMethod defines how temporary files should be cleaned up
type CleanupMethod int

const (
	CleanupStandard   CleanupMethod = iota // Standard file deletion
	CleanupSecure                          // Secure overwrite then delete
	CleanupImmediate                       // Delete immediately after use
	CleanupRetainLogs                      // Keep for audit but mark for cleanup
)

// SecureTempOptions configures secure temporary file behavior
type SecureTempOptions struct {
	TaskID         string
	OriginalName   string
	Permissions    os.FileMode
	SecureDelete   bool
	CleanupMethod  CleanupMethod
	MaxAge         time.Duration
}

// NewSecureTempManager creates a new secure temporary file manager
func NewSecureTempManager(logger *Logger, baseTempDir string) (*SecureTempManager, error) {
	// Generate unique session ID
	sessionBytes := make([]byte, 16)
	if _, err := rand.Read(sessionBytes); err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}
	sessionID := hex.EncodeToString(sessionBytes)

	// Create secure temp directory structure
	secureBaseDir := filepath.Join(baseTempDir, "secure_"+sessionID)
	if err := os.MkdirAll(secureBaseDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create secure temp directory: %w", err)
	}

	stm := &SecureTempManager{
		logger:          logger,
		baseTempDir:     secureBaseDir,
		sessionID:       sessionID,
		activeFiles:     make(map[string]*TempFileInfo),
		cleanupInterval: 5 * time.Minute,
		maxFileAge:      30 * time.Minute,
		secureDelete:    true,
		stopCleanup:     make(chan struct{}),
		cleanupRunning:  false,
	}

	// Start background cleanup routine
	go stm.startCleanupRoutine()

	logger.WithField("session_id", sessionID).
		WithField("temp_dir", secureBaseDir).
		Info("Secure temporary file manager initialized")

	return stm, nil
}

// CreateSecureTempFile creates a new secure temporary file
func (stm *SecureTempManager) CreateSecureTempFile(options SecureTempOptions) (*SecureTempFile, error) {
	stm.mutex.Lock()
	defer stm.mutex.Unlock()

	// Generate secure filename
	fileID := stm.generateSecureFilename(options.OriginalName)
	tempPath := filepath.Join(stm.baseTempDir, fileID)

	// Set default permissions if not specified
	if options.Permissions == 0 {
		options.Permissions = 0600 // Owner read/write only
	}

	// Create the file with secure permissions
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, options.Permissions)
	if err != nil {
		return nil, fmt.Errorf("failed to create secure temp file: %w", err)
	}

	// Track file information
	tempInfo := &TempFileInfo{
		Path:          tempPath,
		OriginalName:  options.OriginalName,
		TaskID:        options.TaskID,
		CreatedAt:     time.Now(),
		LastAccessed:  time.Now(),
		Size:          0,
		Permissions:   options.Permissions,
		IsSecure:      options.SecureDelete,
		CleanupMethod: options.CleanupMethod,
		References:    1,
		Locked:        false,
	}

	stm.activeFiles[fileID] = tempInfo

	// Create secure temp file wrapper
	secureTempFile := &SecureTempFile{
		file:     file,
		fileInfo: tempInfo,
		manager:  stm,
		fileID:   fileID,
		closed:   false,
	}

	stm.logger.WithField("file_id", fileID).
		WithField("task_id", options.TaskID).
		WithField("original_name", options.OriginalName).
		WithField("permissions", fmt.Sprintf("%o", options.Permissions)).
		Info("Secure temporary file created")

	return secureTempFile, nil
}

// generateSecureFilename creates a cryptographically secure filename
func (stm *SecureTempManager) generateSecureFilename(originalName string) string {
	// Generate random bytes for filename
	randBytes := make([]byte, 16)
	rand.Read(randBytes)
	
	// Create timestamp component
	timestamp := time.Now().Format("20060102_150405")
	
	// Extract safe extension from original name
	ext := filepath.Ext(originalName)
	if len(ext) > 10 { // Prevent excessively long extensions
		ext = ".tmp"
	}
	
	// Sanitize extension
	ext = strings.ToLower(ext)
	safeExt := ""
	for _, char := range ext {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '.' {
			safeExt += string(char)
		}
	}
	
	return fmt.Sprintf("secure_%s_%s_%s%s", 
		stm.sessionID[:8], 
		timestamp, 
		hex.EncodeToString(randBytes), 
		safeExt)
}

// GetTempFileInfo returns information about a temporary file
func (stm *SecureTempManager) GetTempFileInfo(fileID string) (*TempFileInfo, bool) {
	stm.mutex.RLock()
	defer stm.mutex.RUnlock()
	
	info, exists := stm.activeFiles[fileID]
	if exists {
		// Update last accessed time
		info.LastAccessed = time.Now()
	}
	return info, exists
}

// LockFile prevents a file from being cleaned up automatically
func (stm *SecureTempManager) LockFile(fileID string) error {
	stm.mutex.Lock()
	defer stm.mutex.Unlock()
	
	info, exists := stm.activeFiles[fileID]
	if !exists {
		return fmt.Errorf("file not found: %s", fileID)
	}
	
	info.Locked = true
	stm.logger.WithField("file_id", fileID).Debug("Temporary file locked")
	return nil
}

// UnlockFile allows a file to be cleaned up automatically
func (stm *SecureTempManager) UnlockFile(fileID string) error {
	stm.mutex.Lock()
	defer stm.mutex.Unlock()
	
	info, exists := stm.activeFiles[fileID]
	if !exists {
		return fmt.Errorf("file not found: %s", fileID)
	}
	
	info.Locked = false
	stm.logger.WithField("file_id", fileID).Debug("Temporary file unlocked")
	return nil
}

// AddReference increases the reference count for a file
func (stm *SecureTempManager) AddReference(fileID string) error {
	stm.mutex.Lock()
	defer stm.mutex.Unlock()
	
	info, exists := stm.activeFiles[fileID]
	if !exists {
		return fmt.Errorf("file not found: %s", fileID)
	}
	
	info.References++
	stm.logger.WithField("file_id", fileID).
		WithField("references", info.References).
		Debug("Added reference to temporary file")
	return nil
}

// RemoveReference decreases the reference count for a file
func (stm *SecureTempManager) RemoveReference(fileID string) error {
	stm.mutex.Lock()
	defer stm.mutex.Unlock()
	
	info, exists := stm.activeFiles[fileID]
	if !exists {
		return fmt.Errorf("file not found: %s", fileID)
	}
	
	info.References--
	if info.References < 0 {
		info.References = 0
	}
	
	stm.logger.WithField("file_id", fileID).
		WithField("references", info.References).
		Debug("Removed reference from temporary file")
	
	// Clean up immediately if no references and cleanup method is immediate
	if info.References == 0 && info.CleanupMethod == CleanupImmediate {
		stm.cleanupFileUnsafe(fileID, info)
	}
	
	return nil
}

// startCleanupRoutine runs the background cleanup process
func (stm *SecureTempManager) startCleanupRoutine() {
	stm.cleanupRunning = true
	defer func() { stm.cleanupRunning = false }()
	
	ticker := time.NewTicker(stm.cleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			stm.performCleanup()
		case <-stm.stopCleanup:
			stm.logger.Info("Stopping secure temp file cleanup routine")
			return
		}
	}
}

// performCleanup cleans up old and unreferenced temporary files
func (stm *SecureTempManager) performCleanup() {
	stm.mutex.Lock()
	defer stm.mutex.Unlock()
	
	now := time.Now()
	cleanedCount := 0
	
	for fileID, info := range stm.activeFiles {
		shouldCleanup := false
		reason := ""
		
		// Check various cleanup conditions
		if info.Locked {
			continue // Skip locked files
		}
		
		if info.References <= 0 && info.CleanupMethod == CleanupImmediate {
			shouldCleanup = true
			reason = "immediate cleanup requested"
		} else if now.Sub(info.CreatedAt) > stm.maxFileAge {
			shouldCleanup = true
			reason = "file exceeded maximum age"
		} else if now.Sub(info.LastAccessed) > stm.maxFileAge/2 {
			shouldCleanup = true
			reason = "file not accessed recently"
		}
		
		if shouldCleanup {
			stm.logger.WithField("file_id", fileID).
				WithField("reason", reason).
				Debug("Cleaning up temporary file")
			
			stm.cleanupFileUnsafe(fileID, info)
			cleanedCount++
		}
	}
	
	if cleanedCount > 0 {
		stm.logger.WithField("cleaned_count", cleanedCount).
			WithField("active_files", len(stm.activeFiles)).
			Info("Temporary file cleanup completed")
	}
}

// cleanupFileUnsafe performs actual file cleanup (must be called with mutex held)
func (stm *SecureTempManager) cleanupFileUnsafe(fileID string, info *TempFileInfo) {
	defer func() {
		delete(stm.activeFiles, fileID)
	}()
	
	// Check if file still exists
	if _, err := os.Stat(info.Path); os.IsNotExist(err) {
		return // File already removed
	}
	
	switch info.CleanupMethod {
	case CleanupSecure:
		stm.secureDeleteFile(info.Path)
	case CleanupRetainLogs:
		// Move to logs directory instead of deleting
		stm.moveToLogsDirectory(info)
	default:
		// Standard deletion
		if err := os.Remove(info.Path); err != nil {
			stm.logger.WithError(err).
				WithField("file_path", info.Path).
				Warn("Failed to remove temporary file")
		}
	}
}

// secureDeleteFile performs secure file deletion by overwriting content
func (stm *SecureTempManager) secureDeleteFile(filePath string) {
	// Open file for overwriting
	file, err := os.OpenFile(filePath, os.O_WRONLY, 0)
	if err != nil {
		stm.logger.WithError(err).
			WithField("file_path", filePath).
			Warn("Failed to open file for secure deletion")
		// Fall back to standard deletion
		os.Remove(filePath)
		return
	}
	defer file.Close()
	
	// Get file size
	stat, err := file.Stat()
	if err != nil {
		stm.logger.WithError(err).Warn("Failed to get file stats for secure deletion")
		os.Remove(filePath)
		return
	}
	
	fileSize := stat.Size()
	
	// Perform multiple pass overwrite (DoD 5220.22-M standard)
	passes := [][]byte{
		make([]byte, 1024), // Pass 1: zeros
		make([]byte, 1024), // Pass 2: ones  
		make([]byte, 1024), // Pass 3: random
	}
	
	// Fill pass patterns
	for i := range passes[1] {
		passes[1][i] = 0xFF // All ones
	}
	rand.Read(passes[2]) // Random data
	
	// Perform overwrite passes
	for passNum, pattern := range passes {
		file.Seek(0, 0)
		written := int64(0)
		
		for written < fileSize {
			toWrite := int64(len(pattern))
			if written+toWrite > fileSize {
				toWrite = fileSize - written
			}
			
			n, err := file.Write(pattern[:toWrite])
			if err != nil {
				stm.logger.WithError(err).
					WithField("pass", passNum+1).
					Warn("Error during secure deletion pass")
				break
			}
			written += int64(n)
		}
		
		// Force write to disk
		file.Sync()
	}
	
	// Finally remove the file
	file.Close()
	if err := os.Remove(filePath); err != nil {
		stm.logger.WithError(err).
			WithField("file_path", filePath).
			Warn("Failed to remove file after secure overwrite")
	}
	
	stm.logger.WithField("file_path", filePath).
		WithField("size", fileSize).
		Debug("Secure file deletion completed")
}

// moveToLogsDirectory moves file to logs for audit retention
func (stm *SecureTempManager) moveToLogsDirectory(info *TempFileInfo) {
	logsDir := "logs/temp_files"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		stm.logger.WithError(err).Warn("Failed to create logs directory")
		// Fall back to standard deletion
		os.Remove(info.Path)
		return
	}
	
	// Create log filename with timestamp
	logFileName := fmt.Sprintf("%s_%s_%s", 
		time.Now().Format("20060102_150405"),
		info.TaskID,
		filepath.Base(info.OriginalName))
	
	logPath := filepath.Join(logsDir, logFileName)
	
	if err := os.Rename(info.Path, logPath); err != nil {
		stm.logger.WithError(err).Warn("Failed to move file to logs directory")
		os.Remove(info.Path) // Fall back to deletion
	} else {
		stm.logger.WithField("log_path", logPath).
			Debug("Moved temporary file to logs directory")
	}
}

// CleanupAll performs immediate cleanup of all temporary files
func (stm *SecureTempManager) CleanupAll() error {
	stm.mutex.Lock()
	defer stm.mutex.Unlock()
	
	cleanedCount := 0
	errorCount := 0
	
	for fileID, info := range stm.activeFiles {
		// Skip locked files
		if info.Locked {
			continue
		}
		
		stm.cleanupFileUnsafe(fileID, info)
		cleanedCount++
	}
	
	// Remove the session directory if empty
	if err := os.Remove(stm.baseTempDir); err != nil {
		// Directory not empty is expected if some files are locked
		if !os.IsExist(err) {
			errorCount++
		}
	}
	
	stm.logger.WithField("cleaned_count", cleanedCount).
		WithField("error_count", errorCount).
		Info("Emergency cleanup completed")
	
	if errorCount > 0 {
		return fmt.Errorf("cleanup completed with %d errors", errorCount)
	}
	
	return nil
}

// GetStats returns statistics about temporary file usage
func (stm *SecureTempManager) GetStats() TempFileStats {
	stm.mutex.RLock()
	defer stm.mutex.RUnlock()
	
	stats := TempFileStats{
		TotalFiles:    len(stm.activeFiles),
		SessionID:     stm.sessionID,
		BaseDirectory: stm.baseTempDir,
		MaxAge:        stm.maxFileAge,
		SecureDelete:  stm.secureDelete,
	}
	
	var totalSize int64
	lockedCount := 0
	
	for _, info := range stm.activeFiles {
		totalSize += info.Size
		if info.Locked {
			lockedCount++
		}
		
		age := time.Since(info.CreatedAt)
		if age > stats.OldestFileAge {
			stats.OldestFileAge = age
		}
	}
	
	stats.TotalSize = totalSize
	stats.LockedFiles = lockedCount
	
	return stats
}

// Shutdown gracefully shuts down the secure temp manager
func (stm *SecureTempManager) Shutdown() error {
	stm.logger.Info("Shutting down secure temporary file manager")
	
	// Stop cleanup routine
	if stm.cleanupRunning {
		close(stm.stopCleanup)
		
		// Wait a bit for cleanup routine to stop
		time.Sleep(100 * time.Millisecond)
	}
	
	// Perform final cleanup
	return stm.CleanupAll()
}

// TempFileStats provides statistics about temporary file usage
type TempFileStats struct {
	TotalFiles    int
	LockedFiles   int
	TotalSize     int64
	OldestFileAge time.Duration
	SessionID     string
	BaseDirectory string
	MaxAge        time.Duration
	SecureDelete  bool
}

// SecureTempFile represents a secure temporary file
type SecureTempFile struct {
	file     *os.File
	fileInfo *TempFileInfo
	manager  *SecureTempManager
	fileID   string
	closed   bool
	mutex    sync.Mutex
}

// Write writes data to the secure temporary file
func (stf *SecureTempFile) Write(data []byte) (int, error) {
	stf.mutex.Lock()
	defer stf.mutex.Unlock()
	
	if stf.closed {
		return 0, fmt.Errorf("cannot write to closed secure temp file")
	}
	
	n, err := stf.file.Write(data)
	if err == nil {
		stf.fileInfo.Size += int64(n)
		stf.fileInfo.LastAccessed = time.Now()
	}
	
	return n, err
}

// Read reads data from the secure temporary file
func (stf *SecureTempFile) Read(buffer []byte) (int, error) {
	stf.mutex.Lock()
	defer stf.mutex.Unlock()
	
	if stf.closed {
		return 0, fmt.Errorf("cannot read from closed secure temp file")
	}
	
	n, err := stf.file.Read(buffer)
	if err == nil {
		stf.fileInfo.LastAccessed = time.Now()
	}
	
	return n, err
}

// Seek sets the offset for the next Read or Write
func (stf *SecureTempFile) Seek(offset int64, whence int) (int64, error) {
	stf.mutex.Lock()
	defer stf.mutex.Unlock()
	
	if stf.closed {
		return 0, fmt.Errorf("cannot seek in closed secure temp file")
	}
	
	return stf.file.Seek(offset, whence)
}

// Sync commits the current contents to stable storage
func (stf *SecureTempFile) Sync() error {
	stf.mutex.Lock()
	defer stf.mutex.Unlock()
	
	if stf.closed {
		return fmt.Errorf("cannot sync closed secure temp file")
	}
	
	return stf.file.Sync()
}

// Close closes the secure temporary file
func (stf *SecureTempFile) Close() error {
	stf.mutex.Lock()
	defer stf.mutex.Unlock()
	
	if stf.closed {
		return nil
	}
	
	err := stf.file.Close()
	stf.closed = true
	
	// Remove reference from manager
	stf.manager.RemoveReference(stf.fileID)
	
	return err
}

// GetPath returns the file path (use with caution)
func (stf *SecureTempFile) GetPath() string {
	return stf.fileInfo.Path
}

// GetFileID returns the unique file identifier
func (stf *SecureTempFile) GetFileID() string {
	return stf.fileID
}

// CopyFrom copies data from an io.Reader to the secure temp file
func (stf *SecureTempFile) CopyFrom(reader io.Reader) (int64, error) {
	stf.mutex.Lock()
	defer stf.mutex.Unlock()
	
	if stf.closed {
		return 0, fmt.Errorf("cannot copy to closed secure temp file")
	}
	
	written, err := io.Copy(stf.file, reader)
	if err == nil {
		stf.fileInfo.Size += written
		stf.fileInfo.LastAccessed = time.Now()
	}
	
	return written, err
}

// init ensures secure defaults on different platforms
func init() {
	// Set more restrictive umask on Unix-like systems
	if runtime.GOOS != "windows" {
		// This will be inherited by child processes
		// Note: umask is not available in Go's standard library,
		// but file permissions are enforced through OpenFile calls
	}
}