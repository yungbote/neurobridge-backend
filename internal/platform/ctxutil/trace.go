package ctxutil

import "context"

type traceDataKey struct{}

type TraceData struct {
	TraceID   string
	RequestID string
}

func WithTraceData(ctx context.Context, td *TraceData) context.Context {
	return context.WithValue(ctx, traceDataKey{}, td)
}

func GetTraceData(ctx context.Context) *TraceData {
	val := ctx.Value(traceDataKey{})
	if td, ok := val.(*TraceData); ok {
		return td
	}
	return nil
}
