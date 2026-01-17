package httputil

import (
	"context"
	"strings"
)

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, strings.TrimSpace(id))
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
