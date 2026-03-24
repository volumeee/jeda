package logger

import (
	"fmt"
	"log/slog"
)

// AsynqLogger wraps slog to implement asynq.Logger interface
type AsynqLogger struct{}

func (l *AsynqLogger) Debug(args ...interface{}) {
	slog.Debug(fmt.Sprint(args...))
}

func (l *AsynqLogger) Info(args ...interface{}) {
	slog.Info(fmt.Sprint(args...))
}

func (l *AsynqLogger) Warn(args ...interface{}) {
	slog.Warn(fmt.Sprint(args...))
}

func (l *AsynqLogger) Error(args ...interface{}) {
	slog.Error(fmt.Sprint(args...))
}

func (l *AsynqLogger) Fatal(args ...interface{}) {
	slog.Error("FATAL: " + fmt.Sprint(args...))
}
