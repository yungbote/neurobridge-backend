package services

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
	"github.com/yungbote/neurobridge-backend/internal/realtime/bus"
)

type SSEEmitter interface {
	Emit(ctx context.Context, msg realtime.SSEMessage)
}

type HubEmitter struct{ Hub *realtime.SSEHub }

func (e *HubEmitter) Emit(ctx context.Context, msg realtime.SSEMessage) {
	if td := ctxutil.GetTraceData(ctx); td != nil {
		if msg.TraceID == "" {
			msg.TraceID = td.TraceID
		}
		if msg.RequestID == "" {
			msg.RequestID = td.RequestID
		}
	}
	e.Hub.Broadcast(msg)
}

type RedisEmitter struct{ Bus bus.Bus }

func (e *RedisEmitter) Emit(ctx context.Context, msg realtime.SSEMessage) {
	if td := ctxutil.GetTraceData(ctx); td != nil {
		if msg.TraceID == "" {
			msg.TraceID = td.TraceID
		}
		if msg.RequestID == "" {
			msg.RequestID = td.RequestID
		}
	}
	_ = e.Bus.Publish(ctx, msg)
}
