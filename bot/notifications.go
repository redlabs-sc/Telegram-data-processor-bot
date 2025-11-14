package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// SendCompletionNotifications sends notifications for completed tasks
// This is called periodically by the processing orchestrator
func (tb *TelegramBot) SendCompletionNotifications() error {
	// Get tasks that were completed but not yet notified
	tasks, err := tb.taskStore.GetCompletedUnnotifiedTasks()
	if err != nil {
		return fmt.Errorf("failed to get unnotified tasks: %w", err)
	}

	if len(tasks) == 0 {
		return nil // No tasks to notify
	}

	// Group tasks by chat ID
	tasksByChat := make(map[int64][]string)
	for _, task := range tasks {
		tasksByChat[task.ChatID] = append(tasksByChat[task.ChatID], task.FileName)
	}

	// Send batched notifications (respect 20 msg/min limit)
	for chatID, filenames := range tasksByChat {
		message := tb.formatCompletionMessage(filenames)

		err := tb.SendMessage(chatID, message)
		if err != nil {
			tb.logger.WithError(err).
				WithField("chat_id", chatID).
				Error("Failed to send completion notification")
			continue
		}

		// Mark tasks as notified
		for _, task := range tasks {
			if task.ChatID == chatID {
				if err := tb.taskStore.MarkNotified(task.ID); err != nil {
					tb.logger.WithError(err).
						WithField("task_id", task.ID).
						Error("Failed to mark task as notified")
				}
			}
		}

		// Rate limit: wait 3 seconds between messages to different chats
		time.Sleep(3 * time.Second)
	}

	tb.logger.WithFields(logrus.Fields{
		"task_count": len(tasks),
		"chat_count": len(tasksByChat),
	}).Info("Sent completion notifications")

	return nil
}

func (tb *TelegramBot) formatCompletionMessage(filenames []string) string {
	if len(filenames) == 1 {
		return fmt.Sprintf(`✅ *Processing Complete*

📄 File: %s

Your file has been successfully processed and stored!`,
			filenames[0])
	}

	// Multiple files - create a bulleted list
	fileList := make([]string, len(filenames))
	for i, filename := range filenames {
		fileList[i] = fmt.Sprintf("• %s", filename)
	}

	return fmt.Sprintf(`✅ *Processing Complete*

📦 %d files processed:
%s

All files have been successfully processed and stored!`,
		len(filenames),
		strings.Join(fileList, "\n"))
}

// SendErrorNotification sends a notification for a failed task
func (tb *TelegramBot) SendErrorNotification(chatID int64, filename string, errorMsg string) error {
	message := fmt.Sprintf(`❌ *Processing Failed*

📄 File: %s
⚠️ Error: %s

Please try uploading the file again or contact support if the issue persists.`,
		filename,
		errorMsg)

	return tb.SendMessage(chatID, message)
}
