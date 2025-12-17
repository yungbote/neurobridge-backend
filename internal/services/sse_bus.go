package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type SSEBus interface {
	Publish(ctx context.Context, msg sse.SSEMessage) error
	StartForwarder(ctx context.Context, onMsg func(m sse.SSEMessage)) error
	Close() error
}

type redisSSEBus struct {
	log     *logger.Logger
	rdb     *redis.Client
	channel string
}

func NewRedisSSEBus(log *logger.Logger) (SSEBus, error) {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		return nil, fmt.Errorf("missing REDIS_ADDR")
	}
	ch := strings.TrimSpace(os.Getenv("REDIS_CHANNEL"))
	if ch == "" {
		ch = "sse"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &redisSSEBus{
		log:     log.With("service", "RedisSSEBus"),
		rdb:     rdb,
		channel: ch,
	}, nil
}

func (b *redisSSEBus) Publish(ctx context.Context, msg sse.SSEMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, b.channel, raw).Err()
}

func (b *redisSSEBus) StartForwarder(ctx context.Context, onMsg func(m sse.SSEMessage)) error {
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
					return
				}
				var msg sse.SSEMessage
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

func (b *redisSSEBus) Close() error {
	if b == nil || b.rdb == nil {
		return nil
	}
	return b.rdb.Close()
}










