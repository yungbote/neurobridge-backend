package realtime

import (
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type SSEClient struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Channels map[string]bool
	Outbound chan SSEMessage
	done     chan struct{}
	Logger   *logger.Logger
}
