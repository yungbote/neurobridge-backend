package services

import (
	"context"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

type ChatNotifier interface {
	ThreadCreated(userID uuid.UUID, thread *types.ChatThread)
	MessageCreated(userID uuid.UUID, threadID uuid.UUID, msg *types.ChatMessage, meta map[string]any)
	MessageDelta(userID uuid.UUID, threadID uuid.UUID, messageID uuid.UUID, delta string, meta map[string]any)
	MessageDone(userID uuid.UUID, threadID uuid.UUID, msg *types.ChatMessage, meta map[string]any)
	MessageError(userID uuid.UUID, threadID uuid.UUID, messageID uuid.UUID, errMsg string, meta map[string]any)
}

type chatNotifier struct {
	emit SSEEmitter
}

func NewChatNotifier(emit SSEEmitter) ChatNotifier {
	return &chatNotifier{emit: emit}
}

func (n *chatNotifier) ThreadCreated(userID uuid.UUID, thread *types.ChatThread) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventChatThreadCreated,
		Data:    map[string]any{"thread": thread},
	})
}

func (n *chatNotifier) MessageCreated(userID uuid.UUID, threadID uuid.UUID, msg *types.ChatMessage, meta map[string]any) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	data := map[string]any{"thread_id": threadID, "message": msg}
	for k, v := range meta {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventChatMessageCreated,
		Data:    data,
	})
}

func (n *chatNotifier) MessageDelta(userID uuid.UUID, threadID uuid.UUID, messageID uuid.UUID, delta string, meta map[string]any) {
	if n == nil || n.emit == nil || userID == uuid.Nil || delta == "" {
		return
	}
	data := map[string]any{
		"thread_id":  threadID,
		"message_id": messageID,
		"delta":      delta,
	}
	for k, v := range meta {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventChatMessageDelta,
		Data:    data,
	})
}

func (n *chatNotifier) MessageDone(userID uuid.UUID, threadID uuid.UUID, msg *types.ChatMessage, meta map[string]any) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	data := map[string]any{"thread_id": threadID, "message": msg}
	for k, v := range meta {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventChatMessageDone,
		Data:    data,
	})
}

func (n *chatNotifier) MessageError(userID uuid.UUID, threadID uuid.UUID, messageID uuid.UUID, errMsg string, meta map[string]any) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	data := map[string]any{
		"thread_id":  threadID,
		"message_id": messageID,
		"error":      errMsg,
	}
	for k, v := range meta {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventChatMessageError,
		Data:    data,
	})
}
