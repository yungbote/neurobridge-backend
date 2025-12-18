package services

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/realtime"
	"github.com/yungbote/neurobridge-backend/internal/realtime/bus"
)

type SSEEmitter interface {
	Emit(ctx context.Context, msg realtime.SSEMessage)
}

type HubEmitter struct{ Hub *realtime.SSEHub }

func (e *HubEmitter) Emit(ctx context.Context, msg realtime.SSEMessage) {
	e.Hub.Broadcast(msg)
}

type RedisEmitter struct{ Bus bus.Bus }

func (e *RedisEmitter) Emit(ctx context.Context, msg realtime.SSEMessage) {
	_ = e.Bus.Publish(ctx, msg)
}
