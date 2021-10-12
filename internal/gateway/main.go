package gateway

import (
	"context"
)

type Gateway interface {
	GetLimitPerMinute() int
	GetLimitPerHour() int
	GetLimitPerDay() int
	GetConcurrencyMax() int
}

type SenderClient interface {
	PreSend(ctx context.Context) error
	Send(ctx context.Context, to string, subject, message, broadcastID string) error
	PostSend(ctx context.Context) error
}
