package workers

import (
	"context"
	"time"

	"telegram-archive-bot/models"
)

// Job interface to avoid import cycles
type Job interface {
	GetID() string
	GetTask() *models.Task
	GetType() string
	SetStatus(status string)
	SetError(error string)
	GetCreatedAt() time.Time
	SetStartedAt(time.Time)
	SetEndedAt(time.Time)
}

// Worker interface for processing jobs
type Worker interface {
	Process(ctx context.Context, job Job) error
}