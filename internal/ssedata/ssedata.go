package ssedata

import (
  "context"
  "github.com/yungbote/neurobridge-backend/internal/sse"
)

type key struct{}

var sseDataKey key

type SSEData struct {
  Messages []sse.SSEMessage
}

func WithSSEData(ctx context.Context) context.Context {
  data := &SSEData{
    Messages: make([]sse.SSEMessage, 0),
  }
  return context.WithValue(ctx, sseDataKey, data)
}

func GetSSEData(ctx context.Context) *SSEData {
  val := ctx.Value(sseDataKey)
  ssd, ok := val.(*SSEData)
  if !ok {
    return nil
  }
  return ssd
}

func (d *SSEData) AppendMessage(msg sse.SSEMessage) {
  d.Messages = append(d.Messages, msg)
}
