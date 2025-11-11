package utils

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileManager struct {
	logger *Logger
}

func NewFileManager(logger *Logger) *FileManager {
	return &FileManager{
		logger: logger,
	}
}

func (fm *FileManager) MoveFile(src, dst string) error {
	fm.logger.WithField("source", src).
		WithField("destination", dst).
		Debug("Moving file")

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Try rename first (fastest if on same filesystem)
	if err := os.Rename(src, dst); err != nil {
		// If rename fails, copy and delete
		if err := fm.CopyFile(src, dst); err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}
		if err := os.Remove(src); err != nil {
			fm.logger.WithError(err).WithField("file", src).Warn("Failed to remove source file after copy")
		}
	}

	fm.logger.WithField("source", src).
		WithField("destination", dst).
		Info("File moved successfully")

	return nil
}

func (fm *FileManager) CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	bytesWritten, err := io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	fm.logger.WithField("source", src).
		WithField("destination", dst).
		WithField("bytes", bytesWritten).
		Debug("File copied successfully")

	return nil
}

func (fm *FileManager) CalculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to calculate hash: %w", err)
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func (fm *FileManager) ValidateFileType(fileName string) (string, error) {
	supportedTypes := map[string]string{
		".zip": "zip",
		".rar": "rar",
		".txt": "txt",
	}

	fileName = strings.ToLower(fileName)
	for ext, fileType := range supportedTypes {
		if strings.HasSuffix(fileName, ext) {
			return fileType, nil
		}
	}

	return "", fmt.Errorf("unsupported file type: %s", fileName)
}

func (fm *FileManager) MoveFilesToExtraction(tempDir, extractionDir string) (int, error) {
	fm.logger.WithField("temp_dir", tempDir).
		WithField("extraction_dir", extractionDir).
		Info("Starting file movement from temp to extraction")

	files, err := filepath.Glob(filepath.Join(tempDir, "*"))
	if err != nil {
		return 0, fmt.Errorf("failed to list temp files: %w", err)
	}

	movedCount := 0
	allDir := filepath.Join(extractionDir, "files", "all")

	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			fileName := filepath.Base(file)
			destPath := filepath.Join(allDir, fileName)

			if err := fm.MoveFile(file, destPath); err != nil {
				fm.logger.WithError(err).
					WithField("file", file).
					Error("Failed to move file to extraction directory")
				continue
			}
			movedCount++
		}
	}

	fm.logger.WithField("moved_files", movedCount).
		Info("File movement to extraction completed")

	return movedCount, nil
}

func (fm *FileManager) CleanupOldFiles(directory string, maxAge time.Duration) (int, error) {
	fm.logger.WithField("directory", directory).
		WithField("max_age", maxAge).
		Debug("Starting cleanup of old files")

	files, err := filepath.Glob(filepath.Join(directory, "*"))
	if err != nil {
		return 0, fmt.Errorf("failed to list files: %w", err)
	}

	cleanedCount := 0
	cutoffTime := time.Now().Add(-maxAge)

	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			if info.ModTime().Before(cutoffTime) {
				if err := os.Remove(file); err != nil {
					fm.logger.WithError(err).
						WithField("file", file).
						Warn("Failed to remove old file")
				} else {
					cleanedCount++
					fm.logger.WithField("file", file).
						Debug("Removed old file")
				}
			}
		}
	}

	if cleanedCount > 0 {
		fm.logger.WithField("directory", directory).
			WithField("cleaned_files", cleanedCount).
			Info("Cleanup completed")
	}

	return cleanedCount, nil
}

func (fm *FileManager) GetDirectoryStats(directory string) (DirectoryStats, error) {
	stats := DirectoryStats{
		Directory: directory,
	}

	if _, err := os.Stat(directory); os.IsNotExist(err) {
		return stats, nil
	}

	files, err := filepath.Glob(filepath.Join(directory, "*"))
	if err != nil {
		return stats, fmt.Errorf("failed to list files: %w", err)
	}

	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			stats.FileCount++
			stats.TotalSize += info.Size()
			
			if stats.OldestFile.IsZero() || info.ModTime().Before(stats.OldestFile) {
				stats.OldestFile = info.ModTime()
			}
			if info.ModTime().After(stats.NewestFile) {
				stats.NewestFile = info.ModTime()
			}
		}
	}

	return stats, nil
}

func (fm *FileManager) SafeFileOperation(operation func() error, description string) error {
	fm.logger.WithField("operation", description).Debug("Starting safe file operation")
	
	if err := operation(); err != nil {
		fm.logger.WithError(err).
			WithField("operation", description).
			Error("File operation failed")
		return fmt.Errorf("file operation '%s' failed: %w", description, err)
	}
	
	fm.logger.WithField("operation", description).Debug("File operation completed successfully")
	return nil
}

type DirectoryStats struct {
	Directory  string
	FileCount  int
	TotalSize  int64
	OldestFile time.Time
	NewestFile time.Time
}