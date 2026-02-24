package core

import (
	"context"
	"time"
)

type Message struct {
	Channel   string
	UserID    string
	ChatID    string
	Text      string
	Timestamp time.Time
}

type Processor interface {
	Process(ctx context.Context, msg Message) (string, error)
}
