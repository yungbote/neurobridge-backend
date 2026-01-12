package temporalx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	temporalsdkclient "go.temporal.io/sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func NewClient(log *logger.Logger) (temporalsdkclient.Client, error) {
	cfg := LoadConfig()
	if cfg.Address == "" {
		if log != nil {
			log.Warn("TEMPORAL_ADDRESS not set; Temporal disabled")
		}
		return nil, nil
	}

	opts := temporalsdkclient.Options{
		HostPort:  cfg.Address,
		Namespace: cfg.Namespace,
		Logger:    log,
	}

	if cfg.ClientCertPath != "" || cfg.ClientKeyPath != "" || cfg.ClientCAPath != "" {
		tlsCfg, err := loadTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		opts.ConnectionOptions.TLS = tlsCfg
	}

	dialTimeout := durationSecondsFromEnv("TEMPORAL_DIAL_TIMEOUT_SECONDS", 5)
	maxWait := durationSecondsFromEnv("TEMPORAL_DIAL_MAX_WAIT_SECONDS", 60)
	backoff := durationMillisFromEnv("TEMPORAL_DIAL_BACKOFF_MS", 250)
	backoffMax := durationMillisFromEnv("TEMPORAL_DIAL_BACKOFF_MAX_MS", 5000)

	deadline := time.Now().Add(maxWait)
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
		c, err := temporalsdkclient.DialContext(ctx, opts)
		cancel()
		if err == nil {
			if log != nil && attempt > 1 {
				log.Info("Connected to Temporal", "address", cfg.Address, "namespace", cfg.Namespace, "attempts", attempt)
			}
			if envTrue("TEMPORAL_AUTO_REGISTER_NAMESPACE", false) {
				if err := EnsureNamespace(context.Background(), c, cfg.Namespace, log); err != nil {
					c.Close()
					return nil, err
				}
			}
			return c, nil
		}

		if maxWait <= 0 || time.Now().After(deadline) {
			return nil, fmt.Errorf("temporal dial failed (address=%s namespace=%s): %w", cfg.Address, cfg.Namespace, err)
		}

		if log != nil {
			log.Warn("Temporal not reachable; retrying", "address", cfg.Address, "namespace", cfg.Namespace, "attempt", attempt, "error", err)
		}

		sleep := clampBackoff(backoff, backoffMax, attempt)
		if sleep > 0 {
			time.Sleep(sleep)
		}
	}
}

func envTrue(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
}

// EnsureNamespace verifies the configured namespace exists and creates it when TEMPORAL_AUTO_REGISTER_NAMESPACE is enabled.
// This is intended for local/self-hosted Temporal; Temporal Cloud namespaces should be pre-provisioned.
func EnsureNamespace(ctx context.Context, c temporalsdkclient.Client, namespace string, log *logger.Logger) error {
	if c == nil {
		return nil
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil
	}

	cfg := LoadConfig()
	if strings.TrimSpace(cfg.Address) == "" {
		return nil
	}

	maxWait := durationSecondsFromEnv("TEMPORAL_NAMESPACE_ENSURE_TIMEOUT_SECONDS", 10)
	if maxWait <= 0 {
		maxWait = 10 * time.Second
	}
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, maxWait)
	defer cancel()

	// Important: use the NamespaceClient (no implicit namespace header) so we can create the namespace even when it doesn't exist yet.
	nsClientOpts := temporalsdkclient.Options{
		HostPort: cfg.Address,
		Logger:   log,
	}
	if cfg.ClientCertPath != "" || cfg.ClientKeyPath != "" || cfg.ClientCAPath != "" {
		tlsCfg, err := loadTLSConfig(cfg)
		if err != nil {
			return err
		}
		nsClientOpts.ConnectionOptions.TLS = tlsCfg
	}
	nsClient, err := temporalsdkclient.NewNamespaceClient(nsClientOpts)
	if err != nil {
		return fmt.Errorf("temporal namespace ensure: init namespace client: %w", err)
	}
	defer nsClient.Close()

	backoff := durationMillisFromEnv("TEMPORAL_NAMESPACE_ENSURE_BACKOFF_MS", 250)
	backoffMax := durationMillisFromEnv("TEMPORAL_NAMESPACE_ENSURE_BACKOFF_MAX_MS", 5000)

	deadline := time.Now().Add(maxWait)

	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return fmt.Errorf("temporal namespace ensure: timed out (namespace=%s): %w", namespace, ctx.Err())
		}

		_, err := nsClient.Describe(ctx, namespace)
		if err == nil {
			return nil
		}

		var nfe *serviceerror.NamespaceNotFound
		if errors.As(err, &nfe) {
			retentionDays := envInt("TEMPORAL_NAMESPACE_RETENTION_DAYS", 7)
			if retentionDays < 1 {
				retentionDays = 7
			}
			if retentionDays > 365 {
				retentionDays = 365
			}

			regErr := nsClient.Register(ctx, &workflowservice.RegisterNamespaceRequest{
				Namespace:                        namespace,
				Description:                      "neurobridge auto-registered namespace",
				WorkflowExecutionRetentionPeriod: durationpb.New(time.Duration(retentionDays) * 24 * time.Hour),
			})
			if regErr == nil {
				if log != nil {
					log.Info("Registered Temporal namespace", "namespace", namespace, "retention_days", retentionDays)
				}
				return nil
			}

			var already *serviceerror.NamespaceAlreadyExists
			if errors.As(regErr, &already) {
				return nil
			}

			if isRetryableRPC(regErr) && time.Now().Before(deadline) {
				if log != nil {
					log.Warn("Temporal namespace register retrying", "namespace", namespace, "attempt", attempt, "error", regErr)
				}
				time.Sleep(clampBackoff(backoff, backoffMax, attempt))
				continue
			}

			return fmt.Errorf("temporal namespace ensure: register namespace: %w", regErr)
		}

		if isRetryableRPC(err) && time.Now().Before(deadline) {
			if log != nil {
				log.Warn("Temporal namespace describe retrying", "namespace", namespace, "attempt", attempt, "error", err)
			}
			time.Sleep(clampBackoff(backoff, backoffMax, attempt))
			continue
		}

		return fmt.Errorf("temporal namespace ensure: describe namespace: %w", err)
	}
}

func loadTLSConfig(cfg Config) (*tls.Config, error) {
	if cfg.ClientCertPath == "" || cfg.ClientKeyPath == "" {
		return nil, fmt.Errorf("temporal tls: both TEMPORAL_CLIENT_CERT_PATH and TEMPORAL_CLIENT_KEY_PATH are required when enabling mTLS")
	}
	cert, err := tls.LoadX509KeyPair(cfg.ClientCertPath, cfg.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("temporal tls: load client cert/key: %w", err)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if cfg.ClientCAPath != "" {
		pem, err := os.ReadFile(cfg.ClientCAPath)
		if err != nil {
			return nil, fmt.Errorf("temporal tls: read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("temporal tls: invalid CA pem")
		}
		tlsCfg.RootCAs = pool
	}
	return tlsCfg, nil
}

func durationSecondsFromEnv(key string, defSeconds int) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(defSeconds) * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return time.Duration(defSeconds) * time.Second
	}
	if n < 0 {
		n = 0
	}
	return time.Duration(n) * time.Second
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func durationMillisFromEnv(key string, defMillis int) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(defMillis) * time.Millisecond
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return time.Duration(defMillis) * time.Millisecond
	}
	if n < 0 {
		n = 0
	}
	return time.Duration(n) * time.Millisecond
}

func clampBackoff(base time.Duration, max time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	sleep := base
	for i := 1; i < attempt; i++ {
		sleep *= 2
		if max > 0 && sleep >= max {
			return max
		}
	}
	if max > 0 && sleep > max {
		return max
	}
	return sleep
}

func isRetryableRPC(err error) bool {
	if err == nil {
		return false
	}
	s, ok := status.FromError(err)
	if !ok {
		// Best-effort: treat context timeouts as retryable to smooth startup.
		return errors.Is(err, context.DeadlineExceeded)
	}
	switch s.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}
