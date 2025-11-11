package workers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-archive-bot/models"
	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
)

type DownloadWorker struct {
	bot               *tgbotapi.BotAPI
	config            *utils.Config
	logger            *utils.Logger
	taskStore         *storage.TaskStore
	timeout           time.Duration
	maxRetries        int
	securityValidator *utils.SecurityValidator
	securityAudit     *storage.SecurityAuditLogger
	tempManager       *utils.SecureTempManager
	botAPIPathManager *utils.BotAPIPathManager
}

func NewDownloadWorker(bot *tgbotapi.BotAPI, config *utils.Config, logger *utils.Logger, taskStore *storage.TaskStore) *DownloadWorker {
	// Get database connection from TaskStore for security auditing
	db := taskStore.GetDB()
	
	// Initialize Bot API path manager for dynamic path detection
	botAPIPathManager := utils.NewBotAPIPathManager(config, logger)
	
	// Ensure Local Bot API directories exist
	if err := botAPIPathManager.EnsureDirectories(); err != nil {
		logger.WithError(err).Fatal("Failed to ensure Local Bot API directories")
	}
	
	// Initialize secure temporary file manager using Local Bot API temp path
	tempPath, err := botAPIPathManager.GetTempPath()
	if err != nil {
		logger.WithError(err).Fatal("Failed to get Local Bot API temp path")
	}
	
	tempManager, err := utils.NewSecureTempManager(logger, tempPath)
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize secure temp manager")
	}
	
	return &DownloadWorker{
		bot:               bot,
		config:            config,
		logger:            logger,
		taskStore:         taskStore,
		timeout:           10 * time.Minute,
		maxRetries:        3,
		securityValidator: utils.NewSecurityValidator(logger, config),
		securityAudit:     storage.NewSecurityAuditLogger(db, logger),
		tempManager:       tempManager,
		botAPIPathManager: botAPIPathManager,
	}
}

func (dw *DownloadWorker) Process(ctx context.Context, job Job) error {
	task := job.GetTask()
	
	dw.logger.WithField("task_id", task.ID).
		WithField("file_name", task.FileName).
		Info("Starting file download")

	// Create context with timeout
	downloadCtx, cancel := context.WithTimeout(ctx, dw.timeout)
	defer cancel()

	// Download file with retries
	var downloadErr error
	for attempt := 1; attempt <= dw.maxRetries; attempt++ {
		dw.logger.WithField("task_id", task.ID).
			WithField("attempt", attempt).
			Debug("Attempting file download")

		if err := dw.downloadFile(downloadCtx, task); err != nil {
			downloadErr = err
			dw.logger.WithField("task_id", task.ID).
				WithField("attempt", attempt).
				WithError(err).
				Warn("Download attempt failed")

			if attempt < dw.maxRetries {
				// Exponential backoff
				backoff := time.Duration(attempt) * time.Second * 2
				select {
				case <-downloadCtx.Done():
					return downloadCtx.Err()
				case <-time.After(backoff):
					continue
				}
			}
		} else {
			downloadErr = nil
			break
		}
	}

	if downloadErr != nil {
		dw.logger.WithField("task_id", task.ID).
			WithError(downloadErr).
			Error("All download attempts failed")
		return fmt.Errorf("download failed after %d attempts: %w", dw.maxRetries, downloadErr)
	}

	dw.logger.WithField("task_id", task.ID).
		WithField("file_name", task.FileName).
		Info("File download completed successfully")

	// Update task status to DOWNLOADED and store the full task data
	task.Status = models.TaskStatusDownloaded
	if err := dw.taskStore.UpdateTask(task); err != nil {
		dw.logger.WithField("task_id", task.ID).
			WithError(err).
			Error("Failed to update task to DOWNLOADED")
		return fmt.Errorf("failed to update task: %w", err)
	}

	return nil
}

func (dw *DownloadWorker) downloadFile(ctx context.Context, task *models.Task) error {
	
	// Always use Local Bot API server for all file downloads (0GB-4GB)
	isLocalAPI := dw.config.UseLocalBotAPI && dw.config.LocalBotAPIEnabled
	maxFileSize := int64(4 * 1024 * 1024 * 1024) // 4GB local API limit
	
	// If Local Bot API is not configured, fail with clear instructions
	if !isLocalAPI {
		dw.logger.WithField("task_id", task.ID).
			WithField("file_size", task.FileSize).
			Error("Local Bot API Server not configured - required for all file downloads")
		
		return fmt.Errorf("Local Bot API Server not configured. This bot requires Local Bot API Server for all file downloads (0GB-4GB). Please configure USE_LOCAL_BOT_API=true in .env")
	}
	
	dw.logger.WithField("task_id", task.ID).
		WithField("file_size", task.FileSize).
		WithField("max_file_size", maxFileSize).
		WithField("using_local_api", isLocalAPI).
		Info("Starting file download via Local Bot API Server")
	
	// Check if file exceeds 4GB limit
	if task.FileSize > maxFileSize {
		dw.logger.WithField("task_id", task.ID).
			WithField("file_size", task.FileSize).
			WithField("max_file_size", maxFileSize).
			Error("File exceeds 4GB limit")
		
		return fmt.Errorf("file size %.2fGB exceeds maximum limit of 4GB", 
			float64(task.FileSize)/(1024*1024*1024))
	}
	
	// Try to get file info using GetFile API
	fileConfig := tgbotapi.FileConfig{FileID: task.TelegramFileID}
	file, err := dw.bot.GetFile(fileConfig)
	
	if err != nil && (strings.Contains(err.Error(), "file is too big") || strings.Contains(err.Error(), "too big")) {
		dw.logger.WithField("task_id", task.ID).
			WithField("file_size", task.FileSize).
			Error("File reported as too big even with Local Bot API Server (4GB limit)")
		
		return fmt.Errorf("file size %.2fGB exceeds Local Bot API Server limit. Maximum supported size is 4GB", 
			float64(task.FileSize)/(1024*1024*1024))
	} else if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}
	
	// For Local Bot API Server, access file directly from filesystem
	// The Local Bot API Server downloads files to its own directory structure
	localFilePath := file.FilePath // This is the relative path from Local Bot API Server
	
	apiType := "Local Bot API Server"
	
	dw.logger.WithField("task_id", task.ID).
		WithField("file_path", file.FilePath).
		WithField("api_type", apiType).
		Info("File info retrieved successfully, starting direct file access")

	// Get Local Bot API documents path dynamically
	documentsPath, err := dw.botAPIPathManager.GetDocumentsPath()
	if err != nil {
		return fmt.Errorf("failed to get Local Bot API documents path: %w", err)
	}
	
	// Extract just the filename from the full path since Local Bot API stores files with simplified names
	sourceFileName := filepath.Base(localFilePath)
	sourceFilePath := filepath.Join(documentsPath, sourceFileName)
	
	// Check if file exists in Local Bot API documents directory
	if _, err := os.Stat(sourceFilePath); os.IsNotExist(err) {
		return fmt.Errorf("file not found in Local Bot API Server documents directory: %s", sourceFilePath)
	}
	
	// Get file info for size verification and hash calculation
	fileInfo, err := os.Stat(sourceFilePath)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Calculate file hash directly from Local Bot API file
	sourceFile, err := os.Open(sourceFilePath)
	if err != nil {
		return fmt.Errorf("failed to open source file for hashing: %w", err)
	}
	defer sourceFile.Close()

	hasher := sha256.New()
	bytesRead, err := io.Copy(hasher, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to calculate file hash: %w", err)
	}

	// Verify file size against the hash calculation
	actualFileSize := fileInfo.Size()
	if bytesRead != actualFileSize {
		return fmt.Errorf("file size mismatch during hash calculation: expected %d, got %d", actualFileSize, bytesRead)
	}

	// Update task with file hash and confirm download
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))
	
	// Check for duplicate files
	existingTask, err := dw.taskStore.GetByFileHash(fileHash)
	if err == nil && existingTask != nil && existingTask.ID != task.ID {
		return fmt.Errorf("duplicate file detected, already processed as task %s", existingTask.ID)
	}
	
	// Perform comprehensive security validation on the Local Bot API file
	validationResult, err := dw.securityValidator.ValidateFile(sourceFilePath, task.FileType)
	if err != nil {
		return fmt.Errorf("security validation failed: %w", err)
	}
	
	// Log security validation results
	dw.logger.WithField("task_id", task.ID).
		WithField("threat_level", validationResult.ThreatLevel.String()).
		WithField("warnings_count", len(validationResult.SecurityWarnings)).
		WithField("valid", validationResult.Valid).
		Info("Security validation completed")
	
	// Handle files that should be quarantined
	if dw.securityValidator.ShouldQuarantine(validationResult) {
		quarantinePath := filepath.Join("app/extraction/files/errors", fmt.Sprintf("quarantine_%s_%s", task.ID, task.FileName))
		if err := os.MkdirAll(filepath.Dir(quarantinePath), 0755); err == nil {
			// Move the Local Bot API file directly to quarantine
			if err := os.Rename(sourceFilePath, quarantinePath); err == nil {
				// Log quarantine event to security audit
				dw.securityAudit.LogQuarantineEvent(
					task.ID, 
					task.FileName, 
					fileHash, 
					fmt.Sprintf("Threat level %s with %d security warnings", validationResult.ThreatLevel.String(), len(validationResult.SecurityWarnings)),
					task.UserID,
				)
				
				dw.logger.WithField("task_id", task.ID).
					WithField("quarantine_path", quarantinePath).
					WithField("threat_level", validationResult.ThreatLevel.String()).
					Warn("File quarantined due to security threats")
				return fmt.Errorf("file quarantined due to security threats: %s", validationResult.ThreatLevel.String())
			}
		}
		
		// Log failed quarantine attempt - file will remain in Local Bot API directory
		dw.securityAudit.LogQuarantineEvent(
			task.ID, 
			task.FileName, 
			fileHash, 
			fmt.Sprintf("Failed to quarantine file, remains in Local Bot API directory. Threat level: %s", validationResult.ThreatLevel.String()),
			task.UserID,
		)
		
		return fmt.Errorf("file rejected due to security threats: %s", validationResult.ThreatLevel.String())
	}
	
	// Attempt sanitization for medium-threat files (skip for now since we're doing direct moves)
	var securityAction storage.SecurityAction = storage.SecurityActionAllow
	if validationResult.ThreatLevel >= utils.ThreatLevelLow && validationResult.ThreatLevel <= utils.ThreatLevelMedium {
		// Note: Sanitization would require a temp file copy, which we're avoiding for efficiency
		// For now, we'll monitor medium-threat files
		dw.logger.WithField("task_id", task.ID).
			WithField("threat_level", validationResult.ThreatLevel.String()).
			Info("Medium-threat file detected, will be monitored without sanitization")
		securityAction = storage.SecurityActionMonitor
	}

	// Log the security validation event to audit log
	if err := dw.securityAudit.LogFileValidationEvent(
		task.ID,
		task.FileName,
		fileHash,
		task.UserID,
		validationResult,
		securityAction,
	); err != nil {
		dw.logger.WithError(err).Warn("Failed to log security validation event")
	}

	// Store file hash and move to Local Bot API temp directory first
	task.FileHash = fileHash
	
	// Get Local Bot API temp path
	tempPath, err := dw.botAPIPathManager.GetTempPath()
	if err != nil {
		return fmt.Errorf("failed to get Local Bot API temp path: %w", err)
	}
	
	// Move file from documents to temp directory for processing
	// Use task ID prefix to track files properly
	tempFileName := fmt.Sprintf("%s_%s", task.ID, task.FileName)
	tempFilePath := filepath.Join(tempPath, tempFileName)
	
	// Handle filename conflicts in temp directory
	if _, err := os.Stat(tempFilePath); err == nil {
		baseName := strings.TrimSuffix(tempFileName, filepath.Ext(tempFileName))
		ext := filepath.Ext(tempFileName)
		tempFileName = fmt.Sprintf("%s_%d%s", baseName, time.Now().Unix(), ext)
		tempFilePath = filepath.Join(tempPath, tempFileName)
	}
	
	// Move file from documents to temp directory
	if err := os.Rename(sourceFilePath, tempFilePath); err != nil {
		dw.logger.WithError(err).Error("Failed to move file from documents to temp directory")
		return fmt.Errorf("failed to move file to temp directory: %w", err)
	}
	
	// Store the temp file path for later processing
	task.LocalAPIPath = tempFilePath

	dw.logger.WithField("task_id", task.ID).
		WithField("source_path", sourceFilePath).
		WithField("temp_path", tempFilePath).
		WithField("file_hash", fileHash).
		WithField("bytes_read", bytesRead).
		WithField("threat_level", validationResult.ThreatLevel.String()).
		WithField("security_warnings", len(validationResult.SecurityWarnings)).
		WithField("security_action", securityAction).
		Info("File downloaded, validated, and moved to Local Bot API temp directory")

	return nil
}

func (dw *DownloadWorker) ValidateFile(task *models.Task) error {
	// Enhanced file validation using security validator patterns
	supportedTypes := map[string]string{
		".zip": "zip",
		".rar": "rar", 
		".txt": "txt",
	}
	
	fileName := strings.ToLower(task.FileName)
	fileType := ""
	supported := false
	
	for ext, fType := range supportedTypes {
		if strings.HasSuffix(fileName, ext) {
			supported = true
			fileType = fType
			break
		}
	}

	if !supported {
		return fmt.Errorf("unsupported file type: %s", task.FileName)
	}
	
	task.FileType = fileType

	// Validate file size with enhanced logging
	if task.FileSize > dw.config.MaxFileSizeBytes() {
		dw.logger.WithField("task_id", task.ID).
			WithField("file_size", task.FileSize).
			WithField("max_allowed", dw.config.MaxFileSizeBytes()).
			WithField("file_name", task.FileName).
			Warn("File rejected due to size limit")
		return fmt.Errorf("file too large: %d bytes (max: %d)", task.FileSize, dw.config.MaxFileSizeBytes())
	}
	
	// Additional pre-download validation
	if task.FileSize == 0 {
		return fmt.Errorf("file has zero size, cannot process")
	}
	
	// Check filename for suspicious patterns
	if err := dw.validateFileName(task.FileName); err != nil {
		return fmt.Errorf("filename validation failed: %w", err)
	}

	dw.logger.WithField("task_id", task.ID).
		WithField("file_type", fileType).
		WithField("file_size", task.FileSize).
		Debug("Pre-download validation passed")

	return nil
}

// validateFileName checks filename for suspicious patterns
func (dw *DownloadWorker) validateFileName(fileName string) error {
	// Check for directory traversal patterns
	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		return fmt.Errorf("filename contains path traversal patterns")
	}
	
	// Check for excessively long filenames (potential buffer overflow)
	if len(fileName) > 255 {
		return fmt.Errorf("filename too long: %d characters (max: 255)", len(fileName))
	}
	
	// Check for null bytes or control characters
	for _, char := range fileName {
		if char == 0 || (char < 32 && char != 9) { // Allow tab character
			return fmt.Errorf("filename contains invalid control characters")
		}
	}
	
	// Check for Windows reserved names
	reservedNames := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
	baseName := strings.ToUpper(fileName)
	if dotIndex := strings.LastIndex(baseName, "."); dotIndex != -1 {
		baseName = baseName[:dotIndex]
	}
	
	for _, reserved := range reservedNames {
		if baseName == reserved {
			return fmt.Errorf("filename uses reserved system name: %s", reserved)
		}
	}
	
	return nil
}

func (dw *DownloadWorker) GetStats() DownloadStats {
	return DownloadStats{
		TotalDownloads: 0, // TODO: Implement stats collection
		ActiveDownloads: 0,
		FailedDownloads: 0,
		BytesDownloaded: 0,
	}
}

// Shutdown performs graceful shutdown of the download worker
func (dw *DownloadWorker) Shutdown() error {
	dw.logger.Info("Shutting down download worker")
	
	// Move any remaining downloaded files before shutdown
	if err := dw.MoveDownloadedFilesToExtraction(); err != nil {
		dw.logger.WithError(err).Warn("Failed to move remaining files during shutdown")
	}
	
	// Shutdown the secure temp manager
	if dw.tempManager != nil {
		if err := dw.tempManager.Shutdown(); err != nil {
			dw.logger.WithError(err).Warn("Error shutting down secure temp manager")
			return err
		}
	}
	
	dw.logger.Info("Download worker shutdown completed")
	return nil
}

// GetTempManagerStats returns statistics about temporary file usage
func (dw *DownloadWorker) GetTempManagerStats() interface{} {
	if dw.tempManager != nil {
		return dw.tempManager.GetStats()
	}
	return nil
}

// GetTaskStore returns the task store for accessing task data
func (dw *DownloadWorker) GetTaskStore() *storage.TaskStore {
	return dw.taskStore
}

// GetBotAPIPathManager returns the bot API path manager
func (dw *DownloadWorker) GetBotAPIPathManager() *utils.BotAPIPathManager {
	return dw.botAPIPathManager
}

// MoveDownloadedFilesToExtraction moves files from Local Bot API temp to extraction directories
func (dw *DownloadWorker) MoveDownloadedFilesToExtraction() error {
	dw.logger.Info("Starting auto-move of downloaded files to extraction directories")
	
	// Get downloaded tasks that need to be moved
	tasks, err := dw.taskStore.GetByStatus(models.TaskStatusDownloaded)
	if err != nil {
		return fmt.Errorf("failed to get downloaded tasks: %w", err)
	}
	
	if len(tasks) == 0 {
		dw.logger.Debug("No downloaded files to move")
		return nil
	}
	
	movedCount := 0
	for _, task := range tasks {
		if err := dw.moveTaskFileToExtraction(task); err != nil {
			dw.logger.WithField("task_id", task.ID).
				WithField("file_name", task.FileName).
				WithError(err).
				Error("Failed to move file to extraction directory")
			continue
		}
		movedCount++
	}
	
	dw.logger.WithField("moved_count", movedCount).
		WithField("total_count", len(tasks)).
		Info("Auto-move of downloaded files completed")
	
	return nil
}

// moveTaskFileToExtraction moves a single task file from temp to extraction directory
func (dw *DownloadWorker) moveTaskFileToExtraction(task *models.Task) error {
	// Check if file exists in temp directory
	if task.LocalAPIPath == "" {
		dw.logger.WithField("task_id", task.ID).Debug("Task has no temp file path, may have been moved already")
		return nil
	}
	
	if _, err := os.Stat(task.LocalAPIPath); os.IsNotExist(err) {
		dw.logger.WithField("task_id", task.ID).
			WithField("temp_path", task.LocalAPIPath).
			Debug("File not found in temp directory, may have been moved already")
		return nil
	}
	
	// Determine destination directory based on file type
	var destDir string
	fileExt := strings.ToLower(filepath.Ext(task.FileName))
	
	switch fileExt {
	case ".txt":
		destDir = "app/extraction/files/txt"
	case ".zip", ".rar":
		destDir = "app/extraction/files/all"
	default:
		// For unknown file types, treat as archives and put in 'all' directory
		destDir = "app/extraction/files/all"
		dw.logger.WithField("task_id", task.ID).
			WithField("file_extension", fileExt).
			Warn("Unknown file type, routing to all directory")
	}
	
	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory %s: %w", destDir, err)
	}
	
	// Use original filename (not task ID prefix) for final storage
	finalFileName := task.FileName
	finalPath := filepath.Join(destDir, finalFileName)
	
	// Handle filename conflicts by adding task ID if file already exists
	if _, err := os.Stat(finalPath); err == nil {
		baseName := strings.TrimSuffix(task.FileName, filepath.Ext(task.FileName))
		ext := filepath.Ext(task.FileName)
		finalFileName = fmt.Sprintf("%s_%s%s", baseName, task.ID, ext)
		finalPath = filepath.Join(destDir, finalFileName)
	}
	
	// Move file from temp to extraction directory
	if err := os.Rename(task.LocalAPIPath, finalPath); err != nil {
		return fmt.Errorf("failed to move file from %s to %s: %w", task.LocalAPIPath, finalPath, err)
	}
	
	// Clear temp path since file has been moved
	task.LocalAPIPath = ""
	if err := dw.taskStore.UpdateTask(task); err != nil {
		dw.logger.WithError(err).Warn("Failed to update task after moving file")
	}
	
	dw.logger.WithField("task_id", task.ID).
		WithField("file_name", task.FileName).
		WithField("file_type", fileExt).
		WithField("temp_path", task.LocalAPIPath).
		WithField("final_path", finalPath).
		WithField("dest_dir", destDir).
		Info("File moved from temp to extraction directory")
	
	return nil
}

type DownloadStats struct {
	TotalDownloads  int
	ActiveDownloads int
	FailedDownloads int
	BytesDownloaded int64
}