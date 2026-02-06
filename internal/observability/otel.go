package observability

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type OtelConfig struct {
	ServiceName string
	Environment string
	Version     string
}

var (
	otelOnce     sync.Once
	otelShutdown func(context.Context) error
)

func InitOTel(ctx context.Context, log *logger.Logger, cfg OtelConfig) func(context.Context) error {
	otelOnce.Do(func() {
		if !otelEnabled() {
			return
		}
		serviceName := strings.TrimSpace(cfg.ServiceName)
		if serviceName == "" {
			serviceName = "neurobridge"
		}
		res, err := resource.New(
			ctx,
			resource.WithAttributes(
				semconv.ServiceNameKey.String(serviceName),
				attribute.String("deployment.environment", strings.TrimSpace(cfg.Environment)),
				semconv.ServiceVersionKey.String(strings.TrimSpace(cfg.Version)),
				attribute.String("service.component", serviceName),
			),
		)
		if err != nil && log != nil {
			log.Warn("otel resource init failed (continuing)", "error", err)
		}

		exporter, expErr := buildTraceExporter(ctx, log)
		if expErr != nil && log != nil {
			log.Warn("otel exporter init failed (continuing)", "error", expErr)
		}
		var tp *sdktrace.TracerProvider
		if exporter != nil {
			tp = sdktrace.NewTracerProvider(
				sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
				sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(otelSampleRatio()))),
				sdktrace.WithResource(res),
			)
		} else {
			tp = sdktrace.NewTracerProvider(
				sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(otelSampleRatio()))),
				sdktrace.WithResource(res),
			)
		}
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		otelShutdown = tp.Shutdown
		if log != nil {
			log.Info("otel tracing initialized", "service", serviceName, "endpoint", otelEndpoint())
		}
	})
	return otelShutdown
}

func otelEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(getEnv("OTEL_ENABLED")))
	if v == "" {
		return false
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func otelSampleRatio() float64 {
	v := strings.TrimSpace(getEnv("OTEL_SAMPLER_RATIO"))
	if v == "" {
		return 0.1
	}
	if f, err := strconvParseFloat(v); err == nil {
		if f < 0 {
			return 0
		}
		if f > 1 {
			return 1
		}
		return f
	}
	return 0.1
}

func otelEndpoint() string {
	return strings.TrimSpace(getEnv("OTEL_EXPORTER_OTLP_ENDPOINT"))
}

func otelHeaders() map[string]string {
	raw := strings.TrimSpace(getEnv("OTEL_EXPORTER_OTLP_HEADERS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	headers := map[string]string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" || val == "" {
			continue
		}
		headers[key] = val
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func otelInsecure() bool {
	v := strings.TrimSpace(strings.ToLower(getEnv("OTEL_EXPORTER_OTLP_INSECURE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func buildTraceExporter(ctx context.Context, log *logger.Logger) (sdktrace.SpanExporter, error) {
	endpoint := otelEndpoint()
	if endpoint != "" {
		opts := []otlptracehttp.Option{}
		if otelInsecure() {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		headers := otelHeaders()
		if headers != nil {
			opts = append(opts, otlptracehttp.WithHeaders(headers))
		}
		if endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
		}
		return otlptracehttp.New(ctx, opts...)
	}
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}
	if log != nil {
		log.Warn("otel using stdout exporter (no OTLP endpoint configured)")
	}
	return exp, nil
}

func getEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func strconvParseFloat(raw string) (float64, error) {
	return strconv.ParseFloat(raw, 64)
}
