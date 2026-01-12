package ctxutil

import "context"

// Default returns context.Background() when ctx is nil.
func Default(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
