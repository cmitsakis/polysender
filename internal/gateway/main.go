package gateway

import (
	"context"
)

type SenderClient interface {
	PreSend(ctx context.Context) error
	Send(ctx context.Context, to string, subject, message, broadcastID string) error
	PostSend(ctx context.Context) error
	GetLimitPerMinute() int
	GetLimitPerHour() int
	GetLimitPerDay() int
}
