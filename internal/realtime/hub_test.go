package realtime

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func mustTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	t.Cleanup(log.Sync)
	return log
}

func recvMessage(t *testing.T, ch <-chan SSEMessage, timeout time.Duration) SSEMessage {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for SSE message")
	}
	return SSEMessage{}
}

func TestSSEHubResilienceReconnectAndOrdering(t *testing.T) {
	hub := NewSSEHub(mustTestLogger(t))
	channel := uuid.New().String()

	clientA := hub.NewSSEClient(uuid.New())
	hub.AddChannel(clientA, channel)

	first := SSEMessage{Channel: channel, Event: SSEEventJobCreated, Data: map[string]any{"seq": 1}}
	second := SSEMessage{Channel: channel, Event: SSEEventJobProgress, Data: map[string]any{"seq": 2}}
	hub.Broadcast(first)
	hub.Broadcast(second)

	gotFirst := recvMessage(t, clientA.Outbound, time.Second)
	gotSecond := recvMessage(t, clientA.Outbound, time.Second)
	if gotFirst.Event != SSEEventJobCreated {
		t.Fatalf("first event: want=%s got=%s", SSEEventJobCreated, gotFirst.Event)
	}
	if gotSecond.Event != SSEEventJobProgress {
		t.Fatalf("second event: want=%s got=%s", SSEEventJobProgress, gotSecond.Event)
	}

	hub.CloseClient(clientA)
	select {
	case _, ok := <-clientA.Outbound:
		if ok {
			t.Fatalf("clientA outbound should be closed after disconnect")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for clientA channel close")
	}

	clientB := hub.NewSSEClient(uuid.New())
	hub.AddChannel(clientB, channel)
	reconnect := SSEMessage{Channel: channel, Event: SSEEventJobDone, Data: map[string]any{"seq": 3}}
	hub.Broadcast(reconnect)
	gotReconnect := recvMessage(t, clientB.Outbound, time.Second)
	if gotReconnect.Event != SSEEventJobDone {
		t.Fatalf("reconnect event: want=%s got=%s", SSEEventJobDone, gotReconnect.Event)
	}
}

func TestSSEHubDuplicateSuppressionExpectation(t *testing.T) {
	hub := NewSSEHub(mustTestLogger(t))
	channel := uuid.New().String()
	client := hub.NewSSEClient(uuid.New())
	hub.AddChannel(client, channel)

	dup := SSEMessage{Channel: channel, Event: SSEEventJobProgress, Data: map[string]any{"pct": 50}}
	hub.Broadcast(dup)
	hub.Broadcast(dup)

	gotOne := recvMessage(t, client.Outbound, time.Second)
	gotTwo := recvMessage(t, client.Outbound, time.Second)
	if gotOne.Event != SSEEventJobProgress || gotTwo.Event != SSEEventJobProgress {
		t.Fatalf("expected duplicate transition events to be delivered, got=%s and %s", gotOne.Event, gotTwo.Event)
	}
}
