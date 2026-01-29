package steps

import (
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func llmTimer(log *logger.Logger, name string, fields map[string]any) func(error) {
	start := time.Now()
	return func(err error) {
		if log == nil {
			return
		}
		kv := make([]any, 0, 4+len(fields)*2+2)
		kv = append(kv, "llm_call", name, "elapsed_ms", time.Since(start).Milliseconds())
		for k, v := range fields {
			kv = append(kv, k, v)
		}
		if err != nil {
			kv = append(kv, "error", err.Error())
			log.Warn("llm call finished", kv...)
			return
		}
		log.Info("llm call finished", kv...)
	}
}
