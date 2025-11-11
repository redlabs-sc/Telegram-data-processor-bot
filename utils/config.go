package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken    string
	AdminIDs            []int64
	MaxFileSizeMB       int64
	DatabasePath        string
	LogLevel            string
	LogFilePath         string
	// Local Bot API Server configuration
	UseLocalBotAPI      bool
	LocalBotAPIURL      string
	LocalBotAPIEnabled  bool
}

func LoadConfig() (*Config, error) {
	err := godotenv.Load()
	if err != nil {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	config := &Config{}

	config.TelegramBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if config.TelegramBotToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	adminIDsStr := os.Getenv("ADMIN_IDS")
	if adminIDsStr == "" {
		return nil, fmt.Errorf("ADMIN_IDS is required")
	}

	adminIDStrs := strings.Split(adminIDsStr, ",")
	config.AdminIDs = make([]int64, 0, len(adminIDStrs))
	for _, idStr := range adminIDStrs {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid admin ID '%s': %w", idStr, err)
		}
		config.AdminIDs = append(config.AdminIDs, id)
	}

	if len(config.AdminIDs) == 0 {
		return nil, fmt.Errorf("at least one valid admin ID is required")
	}


	maxFileSizeStr := os.Getenv("MAX_FILE_SIZE_MB")
	if maxFileSizeStr == "" {
		config.MaxFileSizeMB = 4096 // Default 4GB
	} else {
		config.MaxFileSizeMB, err = strconv.ParseInt(maxFileSizeStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_FILE_SIZE_MB: %w", err)
		}
		if config.MaxFileSizeMB <= 0 {
			return nil, fmt.Errorf("MAX_FILE_SIZE_MB must be positive")
		}
	}

	config.DatabasePath = os.Getenv("DATABASE_PATH")
	if config.DatabasePath == "" {
		config.DatabasePath = "data/bot.db"
	}

	config.LogLevel = os.Getenv("LOG_LEVEL")
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}

	config.LogFilePath = os.Getenv("LOG_FILE_PATH")
	if config.LogFilePath == "" {
		config.LogFilePath = "logs/bot.log"
	}

	// Load Local Bot API Server configuration
	config.UseLocalBotAPI = os.Getenv("USE_LOCAL_BOT_API") == "true"
	config.LocalBotAPIEnabled = os.Getenv("LOCAL_BOT_API_ENABLED") == "true"
	
	config.LocalBotAPIURL = os.Getenv("LOCAL_BOT_API_URL")
	if config.LocalBotAPIURL == "" {
		config.LocalBotAPIURL = "http://localhost:8081"
	}

	return config, nil
}

func (c *Config) IsAdmin(userID int64) bool {
	for _, adminID := range c.AdminIDs {
		if adminID == userID {
			return true
		}
	}
	return false
}

func (c *Config) MaxFileSizeBytes() int64 {
	return c.MaxFileSizeMB * 1024 * 1024
}