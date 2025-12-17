package services

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type SSEEmitter interface {
	Emit(ctx context.Context, msg sse.SSEMessage)
}

type HubEmitter struct{ Hub *sse.SSEHub }

func (e *HubEmitter) Emit(ctx context.Context, msg sse.SSEMessage) {
	e.Hub.Broadcast(msg)
}

type RedisEmitter struct{ Bus SSEBus }

func (e *RedisEmitter) Emit(ctx context.Context, msg sse.SSEMessage) {
	_ = e.Bus.Publish(ctx, msg)
}










