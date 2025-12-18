package logger

import (
	"go.uber.org/zap"
	"strings"
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
	l.SugaredLogger.Debugw(msg, keysAndValues...)
}
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Infow(msg, keysAndValues...)
}
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Warnw(msg, keysAndValues...)
}
func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Errorw(msg, keysAndValues...)
}
func (l *Logger) Fatal(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Fatalw(msg, keysAndValues...)
}
func (l *Logger) With(keysAndValues ...interface{}) *Logger {
	newSugared := l.SugaredLogger.With(keysAndValues...)
	return &Logger{SugaredLogger: newSugared}
}
