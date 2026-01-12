package ctxutil

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

type sseDataKey struct{}

type SSEData struct {
	Messages []realtime.SSEMessage
}

func WithSSEData(ctx context.Context) context.Context {
	data := &SSEData{
		Messages: make([]realtime.SSEMessage, 0),
	}
	return context.WithValue(ctx, sseDataKey{}, data)
}

func GetSSEData(ctx context.Context) *SSEData {
	val := ctx.Value(sseDataKey{})
	ssd, ok := val.(*SSEData)
	if !ok {
		return nil
	}
	return ssd
}

func (d *SSEData) AppendMessage(msg realtime.SSEMessage) {
	d.Messages = append(d.Messages, msg)
}
