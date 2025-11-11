package utils

import (
	"fmt"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// BotAPIClient wraps the standard Bot API client with local server support
type BotAPIClient struct {
	*tgbotapi.BotAPI
	config *Config
	logger *Logger
}

// NewBotAPIClient creates a new Bot API client that can use either standard or local Bot API server
func NewBotAPIClient(config *Config, logger *Logger) (*BotAPIClient, error) {
	var bot *tgbotapi.BotAPI
	var err error

	if config.UseLocalBotAPI && config.LocalBotAPIEnabled {
		logger.WithField("local_api_url", config.LocalBotAPIURL).
			Info("Initializing Bot API client with Local Bot API Server")
		
		// Create custom HTTP client with longer timeout for large files
		httpClient := &http.Client{
			Timeout: 30 * time.Minute, // Increased timeout for large file downloads
		}
		
		// First try to create standard client to validate token, then override the base URL
		bot, err = tgbotapi.NewBotAPIWithClient(config.TelegramBotToken, tgbotapi.APIEndpoint, httpClient)
		if err != nil {
			return nil, fmt.Errorf("failed to create Bot API client: %w", err)
		}
		
		// Override the base URL for local Bot API server  
		// The API endpoint should be in format: http://localhost:8081/bot%s/%s
		bot.SetAPIEndpoint(config.LocalBotAPIURL + "/bot%s/%s")
		
		logger.Info("Successfully connected to Local Bot API Server")
		
	} else {
		logger.Info("Initializing standard Bot API client (20MB file limit)")
		
		// Use standard Bot API
		bot, err = tgbotapi.NewBotAPI(config.TelegramBotToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create standard Bot API client: %w", err)
		}
		
		logger.Info("Successfully connected to standard Telegram Bot API")
	}

	// Test the connection
	me, err := bot.GetMe()
	if err != nil {
		return nil, fmt.Errorf("failed to verify bot connection: %w", err)
	}

	logger.WithField("bot_username", me.UserName).
		WithField("bot_id", me.ID).
		Info("Bot API client initialized successfully")

	return &BotAPIClient{
		BotAPI: bot,
		config: config,
		logger: logger,
	}, nil
}

// SupportsLargeFiles returns true if the client supports downloading large files
func (c *BotAPIClient) SupportsLargeFiles() bool {
	return c.config.UseLocalBotAPI && c.config.LocalBotAPIEnabled
}

// GetMaxFileSize returns the maximum supported file size in bytes (4GB)
func (c *BotAPIClient) GetMaxFileSize() int64 {
	// Always return 4GB limit since we only use Local Bot API Server
	return 4 * 1024 * 1024 * 1024 // 4GB for local Bot API Server
}

// GetAPIType returns a string describing the API type in use
func (c *BotAPIClient) GetAPIType() string {
	if c.SupportsLargeFiles() {
		return "Local Bot API Server (4GB limit)"
	}
	return "Standard Bot API"
}

// GetAPIEndpoint returns the API endpoint URL
func (c *BotAPIClient) GetAPIEndpoint() string {
	if c.SupportsLargeFiles() {
		return c.config.LocalBotAPIURL
	}
	return "https://api.telegram.org"
}