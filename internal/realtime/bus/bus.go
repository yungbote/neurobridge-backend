package bus

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

type Bus interface {
	Publish(ctx context.Context, msg realtime.SSEMessage) error
	StartForwarder(ctx context.Context, onMsg func(m realtime.SSEMessage)) error
	Close() error
}










