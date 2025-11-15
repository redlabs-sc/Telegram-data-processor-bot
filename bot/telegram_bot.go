package bot

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/sirupsen/logrus"

	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
)

type TelegramBot struct {
	bot       *tgbotapi.BotAPI
	config    *utils.Config
	logger    *logrus.Logger
	taskStore *storage.TaskStore
	stopChan  chan struct{}
}

func NewTelegramBot(config *utils.Config, logger *logrus.Logger, taskStore *storage.TaskStore) (*TelegramBot, error) {
	var bot *tgbotapi.BotAPI
	var err error

	// Check if using Local Bot API
	if config.UseLocalBotAPI {
		bot, err = tgbotapi.NewBotAPIWithAPIEndpoint(
			config.TelegramBotToken,
			config.LocalBotAPIURL+"/bot%s/%s",
		)
	} else {
		bot, err = tgbotapi.NewBotAPI(config.TelegramBotToken)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	logger.WithField("username", bot.Self.UserName).Info("Telegram bot authorized")

	return &TelegramBot{
		bot:       bot,
		config:    config,
		logger:    logger,
		taskStore: taskStore,
		stopChan:  make(chan struct{}),
	}, nil
}

func (tb *TelegramBot) Start() error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := tb.bot.GetUpdatesChan(u)

	tb.logger.Info("Bot started, listening for updates...")

	for {
		select {
		case <-tb.stopChan:
			tb.logger.Info("Bot stopping...")
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}

			go tb.handleUpdate(update)
		}
	}
}

func (tb *TelegramBot) Stop() {
	close(tb.stopChan)
	tb.bot.StopReceivingUpdates()
}

func (tb *TelegramBot) GetBotAPI() *tgbotapi.BotAPI {
	return tb.bot
}

func (tb *TelegramBot) SendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, err := tb.bot.Send(msg)
	return err
}

// SendDocument sends a file document to the specified chat ID with a caption
func (tb *TelegramBot) SendDocument(chatID int64, filePath string, caption string) error {
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	doc.Caption = caption
	doc.ParseMode = "Markdown"

	_, err := tb.bot.Send(doc)
	if err != nil {
		return fmt.Errorf("failed to send document %s: %w", filePath, err)
	}

	tb.logger.WithFields(logrus.Fields{
		"chat_id":  chatID,
		"file":     filePath,
		"caption":  caption,
	}).Debug("Document sent successfully")

	return nil
}
