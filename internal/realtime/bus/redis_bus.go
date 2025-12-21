package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

type redisBus struct {
	log     *logger.Logger
	rdb     *goredis.Client
	channel string
}

func NewRedisBus(log *logger.Logger) (Bus, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}

	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		return nil, fmt.Errorf("missing REDIS_ADDR")
	}
	ch := strings.TrimSpace(os.Getenv("REDIS_CHANNEL"))
	if ch == "" {
		ch = "sse"
	}

	rdb := goredis.NewClient(&goredis.Options{
		Addr:        addr,
		DialTimeout: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &redisBus{
		log:     log.With("service", "RedisSSEBus"),
		rdb:     rdb,
		channel: ch,
	}, nil
}

func NewSSEBus(log *logger.Logger) (Bus, error) { return NewRedisBus(log) }

func (b *redisBus) Publish(ctx context.Context, msg realtime.SSEMessage) error {
	if b == nil || b.rdb == nil {
		return fmt.Errorf("redis SSE bus not initialized")
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, b.channel, raw).Err()
}

func (b *redisBus) StartForwarder(ctx context.Context, onMsg func(m realtime.SSEMessage)) error {
	if b == nil || b.rdb == nil {
		return fmt.Errorf("redis SSE bus not initialized")
	}
	if onMsg == nil {
		return fmt.Errorf("onMsg callback required")
	}

	sub := b.rdb.Subscribe(ctx, b.channel)

	// ensures subscription actually started
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return fmt.Errorf("redis subscribe: %w", err)
	}

	go func() {
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				_ = sub.Close()
				return
			case m, ok := <-ch:
				if !ok || m == nil {
					_ = sub.Close()
					return
				}
				var msg realtime.SSEMessage
				if err := json.Unmarshal([]byte(m.Payload), &msg); err != nil {
					b.log.Warn("bad redis SSE payload", "error", err)
					continue
				}
				onMsg(msg)
			}
		}
	}()

	return nil
}

func (b *redisBus) Close() error {
	if b == nil || b.rdb == nil {
		return nil
	}
	return b.rdb.Close()
}










