package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/hibiken/asynq"
)

type asynqLogger struct {
	logger *slog.Logger
}

func NewAsynq(logger *slog.Logger) asynq.Logger {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return asynqLogger{logger: logger.With("component", "asynq")}
}

func (l asynqLogger) Debug(args ...interface{}) {
	l.log(slog.LevelDebug, args...)
}

func (l asynqLogger) Info(args ...interface{}) {
	l.log(slog.LevelInfo, args...)
}

func (l asynqLogger) Warn(args ...interface{}) {
	l.log(slog.LevelWarn, args...)
}

func (l asynqLogger) Error(args ...interface{}) {
	l.log(slog.LevelError, args...)
}

func (l asynqLogger) Fatal(args ...interface{}) {
	l.logger.Error(fmt.Sprint(args...), "fatal", true)
	os.Exit(1)
}

func (l asynqLogger) log(level slog.Level, args ...interface{}) {
	l.logger.Log(context.Background(), level, fmt.Sprint(args...))
}
