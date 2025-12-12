package sse

import (
  "encoding/json"
  "fmt"
  "net/http"
  "strings"
  "sync"
  "time"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
)

type SSEEvent string

const (
  SSEEventUserNameChanged       SSEEvent = "UserNameChanged"
  SSEEventUserAvatarUpdated     SSEEvent = "UserAvatarChanged"
  SSEEventUserCourseCreated     SSEEvent = "UserCourseCreated"
)

type SSEMessage struct {
  Channel     string      `json:"channel"`
  Event       SSEEvent    `json:"event"`
  Data        any         `json:"data,omitempty"`
}

type SSEClient struct {
  ID          uuid.UUID
  UserID      uuid.UUID
  Channels    map[string]bool
  Outbound    chan SSEMessage
  done        chan struct{}
  Logger      *logger.Logger
}

type SSEHub struct {
  mu              sync.RWMutex
  logger          *logger.Logger
  subscriptions   map[string]map[*SSEClient]bool
}

func NewSSEHub(log *logger.Logger) *SSEHub {
  return &SSEHub{
    logger: log.With("component", "SSEHub"),
    subscriptions: make(map[string]map[*SSEClient]bool),
  }
}

func (hub *SSEHub) NewSSEClient(userID uuid.UUID) *SSEClient {
  return &SSEClient{
    ID:       uuid.New(),
    UserID:   userID,
    Channels: make(map[string]bool),
    Outbound: make(chan SSEMessage, 10),
    done:     make(chan struct{}),
    Logger:   hub.logger.With("clientID", nil),
  }
}

func (hub *SSEHub) AddChannel(client *SSEClient, channel string) {
  hub.mu.Lock()
  defer hub.mu.Unlock()

  channel = strings.TrimSpace(channel)
  if channel ==  "" {
    return
  }

  client.Channels[channel] = true

  clients, exists := hub.subscriptions[channel]
  if !exists {
    clients = make(map[*SSEClient]bool)
    hub.subscriptions[channel] = clients
  }
  clients[client] = true

  hub.logger.Debug("SSE client subscribed", "clientID", client.ID, "channel", channel)
}

func (hub *SSEHub) RemoveChannel(client *SSEClient, channel string) {
  hub.mu.Lock()
  defer hub.mu.Unlock()

  channel = strings.TrimSpace(channel)
  if channel == "" {
    return
  }
  delete(client.Channels, channel)

  if subMap, ok := hub.subscriptions[channel]; ok {
    delete(subMap, client)
    if len(subMap) == 0 {
      delete(hub.subscriptions, channel)
    }
  }
  hub.logger.Debug("SSE client unsubscribed from channel", "clientID", client.ID, "channel", channel)
}

func (hub *SSEHub) RemoveClient(client *SSEClient) {
  hub.mu.Lock()
  defer hub.mu.Unlock()

  for ch := range client.Channels {
    if subMap, ok := hub.subscriptions[ch]; ok {
      delete(subMap, client)
      if len(subMap) == 0 {
        delete(hub.subscriptions, ch)
      }
    }
  }
  client.Channels = make(map[string]bool)
  hub.logger.Debug("SSE client unsubscribed from all channels", "clientID", client.ID)
}

func (hub *SSEHub) Broadcast(msg SSEMessage) {
  hub.mu.RLock()
  defer hub.mu.RUnlock()

  if msg.Channel == "" {
    return
  }
  clientsMap, ok := hub.subscriptions[msg.Channel]
  if !ok {
    return
  }
  for c := range clientsMap {
    select {
    case c.Outbound <- msg:
    default:
      hub.logger.Warn("Dropping SSE message; outbound buffer full", "clientID", c.ID)
    }
  }
}

func (hub *SSEHub) ServeHTTP(w http.ResponseWriter, r *http.Request, client *SSEClient) {
  w.Header().Set("Content-Type", "text/event-stream")
  w.Header().Set("Cache-Control", "no-cache")
  w.Header().Set("Connection", "keep-alive")
  w.Header().Set("Transfer-Encoding", "chunked")
  w.Header().Set("X-Accel-Buffering", "no")

  flusher, ok := w.(http.Flusher)
  if !ok {
    http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
    return
  }
  ctx := r.Context()

  heartbeat := time.NewTicker(15 * time.Second)
  defer heartbeat.Stop()

  for {
    select {
    case <-ctx.Done():
      hub.logger.Debug("SSE client context done", "clientID", client.ID, "err", ctx.Err())
      return
    case <-client.done:
      return
    case <-heartbeat.C:
      const pingChunkedSize = 8*1024 - len(": ping \n\n")
      fmt.Fprint(w, ": ping "+strings.Repeat("#", pingChunkedSize)+"\n\n")
      flusher.Flush()
    case msg := <-client.Outbound:
      _, _ = fmt.Fprintf(w, "event: message\n")
      jsonBytes, err := json.Marshal(msg)
      if err != nil {
        hub.logger.Warn("Failed to marshal SSE message", "error", err)
        continue
      }
      _, _ = fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes))
      flusher.Flush()
    }
  }
}

func (hub *SSEHub) CloseClient(client *SSEClient) {
  close(client.done)
  hub.RemoveClient(client)
  close(client.Outbound)
}

