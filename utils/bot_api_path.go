package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BotAPIPathManager handles dynamic detection of Local Bot API paths
type BotAPIPathManager struct {
	logger   *Logger
	config   *Config
	basePath string
}

// NewBotAPIPathManager creates a new path manager for Local Bot API
func NewBotAPIPathManager(config *Config, logger *Logger) *BotAPIPathManager {
	return &BotAPIPathManager{
		logger: logger,
		config: config,
	}
}

// DetectLocalBotAPIPath dynamically detects the Local Bot API directory based on bot token
func (pm *BotAPIPathManager) DetectLocalBotAPIPath() (string, error) {
	if pm.basePath != "" {
		return pm.basePath, nil
	}

	// Extract bot token from config
	botToken := pm.config.TelegramBotToken
	if botToken == "" {
		return "", fmt.Errorf("bot token not found in configuration")
	}

	// The Local Bot API creates directories based on bot token
	// Look for directories that match the bot token pattern
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if there's a directory matching the bot token
	botAPIDir := filepath.Join(currentDir, botToken)
	if _, err := os.Stat(botAPIDir); err == nil {
		pm.basePath = botAPIDir
		pm.logger.WithField("bot_api_path", botAPIDir).Info("Found Local Bot API directory")
		return botAPIDir, nil
	}

	// If exact match not found, look for directories with similar pattern (token:hash format)
	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return "", fmt.Errorf("failed to read current directory: %w", err)
	}

	tokenPrefix := strings.Split(botToken, ":")[0]
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), tokenPrefix+":") {
			candidatePath := filepath.Join(currentDir, entry.Name())
			
			// Verify it has the expected Local Bot API structure (documents and temp folders)
			documentsPath := filepath.Join(candidatePath, "documents")
			tempPath := filepath.Join(candidatePath, "temp")
			
			if _, err := os.Stat(documentsPath); err == nil {
				if _, err := os.Stat(tempPath); err == nil {
					pm.basePath = candidatePath
					pm.logger.WithField("bot_api_path", candidatePath).Info("Auto-detected Local Bot API directory")
					return candidatePath, nil
				}
			}
		}
	}

	return "", fmt.Errorf("Local Bot API directory not found for token %s", tokenPrefix+":***")
}

// GetDocumentsPath returns the path to the documents folder
func (pm *BotAPIPathManager) GetDocumentsPath() (string, error) {
	basePath, err := pm.DetectLocalBotAPIPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(basePath, "documents"), nil
}

// GetTempPath returns the path to the temp folder
func (pm *BotAPIPathManager) GetTempPath() (string, error) {
	basePath, err := pm.DetectLocalBotAPIPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(basePath, "temp"), nil
}

// EnsureDirectories creates the necessary directories if they don't exist
func (pm *BotAPIPathManager) EnsureDirectories() error {
	// Try to detect existing path first
	basePath, err := pm.DetectLocalBotAPIPath()
	if err != nil {
		// If detection failed, create the directory structure
		pm.logger.Warn("Local Bot API directory not found, creating it...")

		botToken := pm.config.TelegramBotToken
		if botToken == "" {
			return fmt.Errorf("bot token not found in configuration")
		}

		currentDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		basePath = filepath.Join(currentDir, botToken)
		pm.basePath = basePath

		pm.logger.WithField("base_path", basePath).Info("Creating Local Bot API directory structure")
	}

	// Ensure base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Ensure temp directory exists
	tempPath := filepath.Join(basePath, "temp")
	if err := os.MkdirAll(tempPath, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Ensure documents directory exists
	documentsPath := filepath.Join(basePath, "documents")
	if err := os.MkdirAll(documentsPath, 0755); err != nil {
		return fmt.Errorf("failed to create documents directory: %w", err)
	}

	pm.logger.WithField("base_path", basePath).Info("Local Bot API directories ensured")
	return nil
}

// GetBotAPIInfo returns information about the current Local Bot API setup
func (pm *BotAPIPathManager) GetBotAPIInfo() (BotAPIInfo, error) {
	basePath, err := pm.DetectLocalBotAPIPath()
	if err != nil {
		return BotAPIInfo{}, err
	}

	documentsPath := filepath.Join(basePath, "documents")
	tempPath := filepath.Join(basePath, "temp")

	// Count files in each directory
	documentsCount := pm.countFilesInDir(documentsPath)
	tempCount := pm.countFilesInDir(tempPath)

	return BotAPIInfo{
		BasePath:       basePath,
		DocumentsPath:  documentsPath,
		TempPath:       tempPath,
		DocumentsCount: documentsCount,
		TempCount:      tempCount,
	}, nil
}

// countFilesInDir counts files in a directory
func (pm *BotAPIPathManager) countFilesInDir(dirPath string) int {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}
	return count
}

// BotAPIInfo contains information about the Local Bot API setup
type BotAPIInfo struct {
	BasePath       string
	DocumentsPath  string
	TempPath       string
	DocumentsCount int
	TempCount      int
}