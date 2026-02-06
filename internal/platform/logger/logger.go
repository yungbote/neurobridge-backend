package logger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
)

type Logger struct {
	SugaredLogger *zap.SugaredLogger
}

func New(mode string) (*Logger, error) {
	var cfg zap.Config
	switch strings.ToLower(mode) {
	case "prod", "production":
		cfg = zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	default:
		cfg = zap.NewDevelopmentConfig()
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	}
	zapLogger, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	sugar := zapLogger.Sugar()
	return &Logger{SugaredLogger: sugar}, nil
}

func (l *Logger) Sync() {
	_ = l.SugaredLogger.Sync()
}

func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Debugw(msg, sanitizeKVs(keysAndValues)...)
}
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Infow(msg, sanitizeKVs(keysAndValues)...)
}
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Warnw(msg, sanitizeKVs(keysAndValues)...)
}
func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Errorw(msg, sanitizeKVs(keysAndValues)...)
}
func (l *Logger) Fatal(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Fatalw(msg, sanitizeKVs(keysAndValues)...)
}
func (l *Logger) With(keysAndValues ...interface{}) *Logger {
	newSugared := l.SugaredLogger.With(sanitizeKVs(keysAndValues)...)
	return &Logger{SugaredLogger: newSugared}
}

var (
	redactOnce       sync.Once
	redactionEnabled bool
	hashSalt         string
)

func sanitizeKVs(kv []interface{}) []interface{} {
	if len(kv) == 0 {
		return kv
	}
	if !redactionOn() {
		return kv
	}
	out := make([]interface{}, 0, len(kv))
	for i := 0; i < len(kv); i += 2 {
		if i == len(kv)-1 {
			out = append(out, kv[i])
			break
		}
		key := strings.TrimSpace(strings.ToLower(toString(kv[i])))
		out = append(out, toString(kv[i]), sanitizeValue(key, kv[i+1]))
	}
	return out
}

func sanitizeValue(key string, val interface{}) interface{} {
	if key == "" {
		return val
	}
	if isRedactKey(key) {
		return "[REDACTED]"
	}
	if isHashKey(key) {
		return hashValue(val)
	}
	switch v := val.(type) {
	case map[string]interface{}:
		return sanitizeMap(v)
	case []interface{}:
		return sanitizeSlice(v)
	default:
		if s, ok := val.(string); ok && looksLikeJWT(s) {
			return "[REDACTED]"
		}
		return val
	}
}

func sanitizeMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	out := make(map[string]interface{}, len(input))
	for k, v := range input {
		key := strings.TrimSpace(strings.ToLower(k))
		out[k] = sanitizeValue(key, v)
	}
	return out
}

func sanitizeSlice(input []interface{}) []interface{} {
	if input == nil {
		return nil
	}
	out := make([]interface{}, 0, len(input))
	for _, v := range input {
		out = append(out, sanitizeValue("", v))
	}
	return out
}

func isRedactKey(key string) bool {
	switch {
	case strings.Contains(key, "token"),
		strings.Contains(key, "authorization"),
		strings.Contains(key, "password"),
		strings.Contains(key, "secret"),
		strings.Contains(key, "cookie"),
		strings.Contains(key, "api_key"),
		strings.Contains(key, "apikey"),
		strings.Contains(key, "email"),
		strings.Contains(key, "refresh"):
		return true
	default:
		return false
	}
}

func isHashKey(key string) bool {
	return strings.Contains(key, "user_id") || strings.Contains(key, "owner_user_id") || strings.Contains(key, "session_id")
}

func hashValue(val interface{}) string {
	raw := toString(val)
	if raw == "" {
		return ""
	}
	h := sha256.New()
	if hashSalt != "" {
		_, _ = h.Write([]byte(hashSalt))
	}
	_, _ = h.Write([]byte(raw))
	sum := hex.EncodeToString(h.Sum(nil))
	if len(sum) > 12 {
		sum = sum[:12]
	}
	return "hash:" + sum
}

func looksLikeJWT(s string) bool {
	if s == "" {
		return false
	}
	parts := strings.Split(s, ".")
	return len(parts) == 3 && len(parts[0]) > 10 && len(parts[1]) > 10
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func redactionOn() bool {
	redactOnce.Do(func() {
		val := strings.TrimSpace(strings.ToLower(os.Getenv("LOG_REDACTION_ENABLED")))
		switch val {
		case "0", "false", "no", "off":
			redactionEnabled = false
		default:
			redactionEnabled = true
		}
		hashSalt = strings.TrimSpace(os.Getenv("LOG_HASH_SALT"))
	})
	return redactionEnabled
}
