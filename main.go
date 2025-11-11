package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"telegram-archive-bot/bot"
	"telegram-archive-bot/monitoring"
	"telegram-archive-bot/pipeline"
	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
	"telegram-archive-bot/workers"
)

func main() {
	config, err := utils.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger, err := utils.NewLogger(config)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	db, err := storage.NewDatabase(config.DatabasePath)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	taskStore := storage.NewTaskStore(db)
	
	// Initialize download worker first to get BotAPIPathManager
	downloadWorker := workers.NewDownloadWorker(nil, config, logger, taskStore) // Temporary, will set bot later
	
	// Initialize recovery service with BotAPIPathManager and perform crash recovery
	recoveryService := storage.NewRecoveryService(taskStore, logger, downloadWorker.GetBotAPIPathManager())
	if err := recoveryService.RecoverIncompleteTasks(context.Background()); err != nil {
		logger.WithError(err).Error("Crash recovery failed, continuing with startup")
	}
	
	// Cleanup orphaned files
	if err := recoveryService.CleanupOrphanedFiles(); err != nil {
		logger.WithError(err).Warn("Orphaned file cleanup failed")
	}
	
	// Initialize Telegram bot
	telegramBot, err := bot.NewTelegramBot(config, logger, db.DB())
	if err != nil {
		logger.Fatalf("Failed to initialize Telegram bot: %v", err)
	}
	
	// Set task store for handlers
	telegramBot.SetTaskStore(taskStore)
	
	// Update download worker with actual bot API and set it
	downloadWorker = workers.NewDownloadWorker(telegramBot.GetBotAPI(), config, logger, taskStore)
	telegramBot.SetDownloadWorker(downloadWorker)
	
	// Initialize pipeline coordinator with graceful degradation
	coordinator := pipeline.NewPipelineCoordinator(telegramBot.GetBotAPI(), config, logger, taskStore)
	
	// Set pipeline coordinator for handlers (critical for file processing)
	telegramBot.SetPipelineCoordinator(coordinator)
	
	// Set workers for handlers to access graceful degradation features
	telegramBot.SetExtractionWorker(coordinator.GetExtractionWorker())
	telegramBot.SetConversionWorker(coordinator.GetConversionWorker())
	
	// Initialize health monitor
	healthMonitor := monitoring.NewHealthMonitor(logger, taskStore)
	
	// Register Telegram alert notification callback
	alertManager := healthMonitor.GetAlertManager()
	alertManager.AddAlertCallback(func(alert *monitoring.Alert) {
		// Send alert notification to all admin users
		alertMessage := formatAlertMessage(alert)
		for _, adminID := range config.AdminIDs {
			if err := telegramBot.SendMessage(adminID, alertMessage); err != nil {
				logger.WithError(err).
					WithField("admin_id", adminID).
					WithField("alert_id", alert.ID).
					Error("Failed to send alert notification to admin")
			}
		}
	})
	
	healthMonitor.Start()
	defer healthMonitor.Stop()
	
	// Set health monitor for handlers (for health check command)
	telegramBot.SetHealthMonitor(healthMonitor)
	
	logger.Info("Telegram Archive Bot starting...")
	logger.WithField("admins", config.AdminIDs).Info("Authorized admin IDs loaded")
	logger.WithField("start_time", healthMonitor.GetStartTime()).Info("Health monitoring started")

	// Start coordinator monitoring with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start pipeline coordinator monitoring
	go func() {
		if err := coordinator.Start(ctx); err != nil {
			logger.WithError(err).Error("Pipeline coordinator stopped with error")
		}
	}()
	
	// Start auto-move monitoring for downloaded files
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Periodically move downloaded files to extraction directories
				if err := downloadWorker.MoveDownloadedFilesToExtraction(); err != nil {
					logger.WithError(err).Debug("Auto-move failed, will retry on next cycle")
				}
			}
		}
	}()
	
	// Start bot in goroutine
	go func() {
		if err := telegramBot.Start(); err != nil {
			logger.WithError(err).Error("Bot stopped with error")
		}
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	logger.Info("Shutdown signal received, shutting down gracefully...")
	
	// Stop pipeline coordinator first
	coordinator.Stop()
	
	// Shutdown download worker (including secure temp manager)
	if err := downloadWorker.Shutdown(); err != nil {
		logger.WithError(err).Error("Error shutting down download worker")
	}
	
	// Cancel context to stop monitoring
	cancel()
	
	telegramBot.Stop()
	
	logger.Info("Telegram Archive Bot stopped")
}

// formatAlertMessage formats an alert for Telegram notification
func formatAlertMessage(alert *monitoring.Alert) string {
	var levelEmoji string
	switch alert.Level {
	case monitoring.AlertLevelInfo:
		levelEmoji = "ℹ️"
	case monitoring.AlertLevelWarning:
		levelEmoji = "⚠️"
	case monitoring.AlertLevelCritical:
		levelEmoji = "🚨"
	default:
		levelEmoji = "📢"
	}
	
	var typeDescription string
	switch alert.Type {
	case monitoring.AlertTypeHighMemory:
		typeDescription = "High Memory Usage"
	case monitoring.AlertTypeHighCPU:
		typeDescription = "High CPU Usage"
	case monitoring.AlertTypeDiskSpace:
		typeDescription = "Low Disk Space"
	case monitoring.AlertTypeQueueBackup:
		typeDescription = "Queue Backup"
	case monitoring.AlertTypeProcessFailure:
		typeDescription = "Process Failure"
	case monitoring.AlertTypeSystemFailure:
		typeDescription = "System Failure"
	case monitoring.AlertTypeComponentDown:
		typeDescription = "Component Down"
	case monitoring.AlertTypeHighLoadAvg:
		typeDescription = "High Load Average"
	default:
		typeDescription = string(alert.Type)
	}
	
	message := fmt.Sprintf(`%s **ALERT** - %s

🔍 **Type:** %s
📊 **Level:** %s
🕐 **Time:** %s
📝 **Message:** %s`,
		levelEmoji,
		typeDescription,
		typeDescription,
		string(alert.Level),
		alert.Timestamp.Format("2006-01-02 15:04:05"),
		alert.Message)
	
	if alert.Component != "" {
		message += fmt.Sprintf("\n🔧 **Component:** %s", alert.Component)
	}
	
	if alert.Count > 1 {
		message += fmt.Sprintf("\n🔢 **Count:** %d (repeated)", alert.Count)
	}
	
	return message
}