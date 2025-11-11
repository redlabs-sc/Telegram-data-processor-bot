package utils

import (
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Logger struct {
	*logrus.Logger
}

func NewLogger(config *Config) (*Logger, error) {
	logger := logrus.New()

	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		return nil, err
	}
	logger.SetLevel(level)

	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		ForceColors:   true,
	})

	logDir := filepath.Dir(config.LogFilePath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	fileLogger := &lumberjack.Logger{
		Filename:   config.LogFilePath,
		MaxSize:    100, // MB
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   true,
	}

	multiWriter := io.MultiWriter(os.Stdout, fileLogger)
	logger.SetOutput(multiWriter)

	return &Logger{Logger: logger}, nil
}

func (l *Logger) WithTaskID(taskID string) *logrus.Entry {
	return l.WithField("task_id", taskID)
}

func (l *Logger) WithUserID(userID int64) *logrus.Entry {
	return l.WithField("user_id", userID)
}

func (l *Logger) WithComponent(component string) *logrus.Entry {
	return l.WithField("component", component)
}