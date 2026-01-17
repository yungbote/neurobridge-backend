package mock

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/inference/engine"
)

type Engine struct {
	EmbeddingDims int
}

func New() *Engine {
	return &Engine{EmbeddingDims: 8}
}

func (e *Engine) Embed(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	_ = ctx
	out := make([][]float32, len(inputs))
	for i, s := range inputs {
		h := sha256.Sum256([]byte(model + "\n" + s))
		vec := make([]float32, e.EmbeddingDims)
		for j := 0; j < e.EmbeddingDims; j++ {
			u := binary.LittleEndian.Uint32(h[(j*4)%len(h):])
			vec[j] = float32(u%10_000)/10_000.0 - 0.5
		}
		out[i] = vec
	}
	return out, nil
}

func (e *Engine) GenerateText(ctx context.Context, model string, messages []engine.Message, opts engine.GenerateOptions) (string, error) {
	_ = ctx
	_ = model
	_ = e

	if opts.JSONSchema != nil {
		obj := map[string]any{
			"ok":     true,
			"schema": opts.JSONSchema.Name,
		}
		b, _ := json.Marshal(obj)
		return string(b), nil
	}

	var user string
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			user = messages[i].Content
			break
		}
	}
	if strings.TrimSpace(user) == "" {
		return "mock: ok", nil
	}
	return fmt.Sprintf("mock: %s", user), nil
}

func (e *Engine) StreamText(ctx context.Context, model string, messages []engine.Message, opts engine.GenerateOptions, onDelta func(delta string)) (string, error) {
	full, err := e.GenerateText(ctx, model, messages, opts)
	if err != nil {
		return "", err
	}
	if onDelta == nil {
		return full, nil
	}
	const chunk = 16
	for i := 0; i < len(full); i += chunk {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		end := i + chunk
		if end > len(full) {
			end = len(full)
		}
		onDelta(full[i:end])
	}
	return full, nil
}
