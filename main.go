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
	"telegram-archive-bot/orchestrator"
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
	telegramBot, err := bot.NewTelegramBot(config, logger.Logger, taskStore)
	if err != nil {
		logger.Fatalf("Failed to initialize Telegram bot: %v", err)
	}

	// Update download worker with actual bot API
	downloadWorker = workers.NewDownloadWorker(telegramBot.GetBotAPI(), config, logger, taskStore)

	// Initialize sequential orchestrator (Option 1 architecture)
	sequentialOrchestrator := orchestrator.NewSequentialOrchestrator(logger.Logger, config, taskStore, telegramBot)
	
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

	logger.Info("Telegram Archive Bot starting (Option 1: Sequential Pipeline)...")
	logger.WithField("admins", config.AdminIDs).Info("Authorized admin IDs loaded")
	logger.WithField("start_time", healthMonitor.GetStartTime()).Info("Health monitoring started")

	// Start workers and orchestrator with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start 3 download workers (Telegram API limit)
	logger.Info("Starting 3 download workers...")
	for i := 1; i <= 3; i++ {
		workerID := i
		go func() {
			if err := downloadWorker.StartPolling(ctx, workerID); err != nil && err != context.Canceled {
				logger.WithField("worker_id", workerID).
					WithError(err).
					Error("Download worker stopped with error")
			}
		}()
	}

	// Start sequential orchestrator
	logger.Info("Starting sequential processing orchestrator...")
	go func() {
		if err := sequentialOrchestrator.Start(ctx); err != nil && err != context.Canceled {
			logger.WithError(err).Error("Sequential orchestrator stopped with error")
		}
	}()

	// Start bot in goroutine
	logger.Info("Starting Telegram bot...")
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

	// Cancel context to stop all workers and orchestrator
	cancel()

	// Give workers time to finish current tasks (5 seconds)
	logger.Info("Waiting for workers to finish current tasks...")
	time.Sleep(5 * time.Second)

	// Shutdown download worker (including secure temp manager)
	if err := downloadWorker.Shutdown(); err != nil {
		logger.WithError(err).Error("Error shutting down download worker")
	}

	// Stop Telegram bot
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