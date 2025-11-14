package bot

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"telegram-archive-bot/models"
)

func (tb *TelegramBot) handleUpdate(update tgbotapi.Update) {
	// Check if user is admin
	if !tb.isAdmin(update.Message.From.ID) {
		tb.logger.WithField("user_id", update.Message.From.ID).
			Warn("Unauthorized access attempt")
		// Silently ignore non-admin messages (don't respond)
		return
	}

	// Handle commands
	if update.Message.IsCommand() {
		tb.handleCommand(update.Message)
		return
	}

	// Handle file uploads
	if update.Message.Document != nil {
		tb.handleDocument(update.Message)
		return
	}
}

func (tb *TelegramBot) isAdmin(userID int64) bool {
	for _, adminID := range tb.config.AdminIDs {
		if adminID == userID {
			return true
		}
	}
	return false
}

func (tb *TelegramBot) handleCommand(message *tgbotapi.Message) {
	switch message.Command() {
	case "start":
		tb.handleStartCommand(message)
	case "help":
		tb.handleHelpCommand(message)
	case "queue":
		tb.handleQueueCommand(message)
	case "stats":
		tb.handleStatsCommand(message)
	default:
		tb.SendMessage(message.Chat.ID, "Unknown command. Send /help for available commands.")
	}
}

func (tb *TelegramBot) handleStartCommand(message *tgbotapi.Message) {
	text := `👋 Welcome to Telegram Archive Bot (Option 1)

📤 Send me files to process:
• Archives: ZIP, RAR (up to 4GB)
• Text files: TXT (up to 4GB)

📊 Available commands:
/help - Show this help message
/queue - View queue status
/stats - View processing statistics

🔄 Files are processed sequentially for maximum reliability!`

	tb.SendMessage(message.Chat.ID, text)
}

func (tb *TelegramBot) handleHelpCommand(message *tgbotapi.Message) {
	text := `📚 Available Commands:

/start - Welcome message
/help - This help message
/queue - Show queue statistics (pending, downloading, processing)
/stats - Overall system statistics

📤 File Upload:
Simply send a file (ZIP, RAR, or TXT) and it will be queued for processing.

⚡ Processing Pipeline (Sequential):
1. Download (3 concurrent workers)
2. Extract archives → Convert → Store
3. Notification on completion

Files are processed one stage at a time for stability and reliability.`

	tb.SendMessage(message.Chat.ID, text)
}

func (tb *TelegramBot) handleQueueCommand(message *tgbotapi.Message) {
	// Get queue statistics
	pending, _ := tb.taskStore.GetTaskCountByStatus(models.TaskStatusPending)
	downloading, _ := tb.taskStore.GetTaskCountByStatus(models.TaskStatusDownloading)
	downloaded, _ := tb.taskStore.GetTaskCountByStatus(models.TaskStatusDownloaded)

	text := fmt.Sprintf(`📊 *Queue Status*

• Pending: %d files
• Downloading: %d files
• Downloaded (waiting for processing): %d files

Processing is sequential - one stage at a time for reliability.`,
		pending, downloading, downloaded)

	tb.SendMessage(message.Chat.ID, text)
}

func (tb *TelegramBot) handleStatsCommand(message *tgbotapi.Message) {
	// Get overall statistics
	completed, _ := tb.taskStore.GetTaskCountByStatus(models.TaskStatusCompleted)
	failed, _ := tb.taskStore.GetTaskCountByStatus(models.TaskStatusFailed)

	text := fmt.Sprintf(`📈 *System Statistics*

• Completed: %d files
• Failed: %d files

Use /queue to see current queue status.`,
		completed, failed)

	tb.SendMessage(message.Chat.ID, text)
}

func (tb *TelegramBot) handleDocument(message *tgbotapi.Message) {
	doc := message.Document

	// Validate file size
	maxSize := tb.config.MaxFileSizeMB * 1024 * 1024
	if int64(doc.FileSize) > maxSize {
		tb.SendMessage(message.Chat.ID, fmt.Sprintf("❌ File too large. Max size: %d MB", tb.config.MaxFileSizeMB))
		return
	}

	// Detect file type
	fileType := tb.detectFileType(doc.FileName)
	if fileType == "" {
		tb.SendMessage(message.Chat.ID, "❌ Unsupported file type. Supported: ZIP, RAR, TXT")
		return
	}

	// Create task
	task := &models.Task{
		ID:             uuid.New().String(),
		UserID:         message.From.ID,
		ChatID:         message.Chat.ID,
		FileName:       doc.FileName,
		FileSize:       int64(doc.FileSize),
		FileType:       fileType,
		TelegramFileID: doc.FileID,
		Status:         models.TaskStatusPending,
		RetryCount:     0,
		Notified:       false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Save to database
	err := tb.taskStore.Create(task)
	if err != nil {
		tb.logger.WithError(err).Error("Failed to create task")
		tb.SendMessage(message.Chat.ID, "❌ Error queuing file for processing. Please try again.")
		return
	}

	// Send confirmation
	confirmText := fmt.Sprintf(`✅ File received!

📄 Filename: %s
📦 Size: %.2f MB
🆔 Task ID: %s

You'll receive a notification when processing completes.`,
		doc.FileName,
		float64(doc.FileSize)/(1024*1024),
		task.ID[:8]) // Show first 8 chars of UUID

	tb.SendMessage(message.Chat.ID, confirmText)

	tb.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"filename":  doc.FileName,
		"file_type": fileType,
		"file_size": doc.FileSize,
		"user_id":   message.From.ID,
	}).Info("File queued for processing")
}

func (tb *TelegramBot) detectFileType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".zip", ".rar":
		return "archive"
	case ".txt":
		return "txt"
	default:
		return ""
	}
}
