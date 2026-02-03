package services

import (
	"context"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

// RuntimeNotifier broadcasts runtime prompt events to connected clients.
type RuntimeNotifier interface {
	RuntimePrompt(userID uuid.UUID, payload any)
}

type runtimeNotifier struct {
	emitter SSEEmitter
}

func NewRuntimeNotifier(emitter SSEEmitter) RuntimeNotifier {
	return &runtimeNotifier{emitter: emitter}
}

func (n *runtimeNotifier) RuntimePrompt(userID uuid.UUID, payload any) {
	if n == nil || n.emitter == nil || userID == uuid.Nil {
		return
	}
	n.emitter.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventRuntimePrompt,
		Data:    payload,
	})
}
