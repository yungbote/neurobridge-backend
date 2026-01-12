package neo4jdb

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Client struct {
	Driver   neo4j.DriverWithContext
	Database string
	log      *logger.Logger
}

func NewFromEnv(log *logger.Logger) (*Client, error) {
	if log == nil {
		return nil, fmt.Errorf("neo4jdb: logger required")
	}

	uri := strings.TrimSpace(os.Getenv("NEO4J_URI"))
	if uri == "" {
		return nil, nil
	}

	user := strings.TrimSpace(os.Getenv("NEO4J_USER"))
	if user == "" {
		user = "neo4j"
	}
	password := strings.TrimSpace(os.Getenv("NEO4J_PASSWORD"))
	database := strings.TrimSpace(os.Getenv("NEO4J_DATABASE"))

	timeoutSec := 10
	if v := strings.TrimSpace(os.Getenv("NEO4J_TIMEOUT_SECONDS")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			timeoutSec = parsed
		}
	}

	maxPool := 50
	if v := strings.TrimSpace(os.Getenv("NEO4J_MAX_POOL_SIZE")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			maxPool = parsed
		}
	}

	auth := neo4j.BasicAuth(user, password, "")
	driver, err := neo4j.NewDriverWithContext(uri, auth, func(cfg *neo4j.Config) {
		cfg.MaxConnectionPoolSize = maxPool
		cfg.SocketConnectTimeout = time.Duration(timeoutSec) * time.Second
	})
	if err != nil {
		return nil, fmt.Errorf("neo4jdb: init driver: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("neo4jdb: verify connectivity: %w", err)
	}

	return &Client{
		Driver:   driver,
		Database: database,
		log:      log.With("client", "Neo4jDB"),
	}, nil
}

func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.Driver == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := c.Driver.Close(ctx)
	c.Driver = nil
	return err
}
