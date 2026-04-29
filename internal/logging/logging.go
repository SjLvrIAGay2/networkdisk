package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"networkdisk/internal/config"
)

var (
	global   *slog.Logger
	logFile  *os.File
	mu       sync.RWMutex
)

func Init(cfg *config.Config) error {
	mu.Lock()
	defer mu.Unlock()

	level := parseLevel(cfg.Log.Level)
	filePath := cfg.Log.File
	if filePath == "" {
		filePath = "logs/log.log"
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create log directory %s: %w", dir, err)
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", filePath, err)
	}

	if logFile != nil {
		logFile.Close()
	}
	logFile = f
	writer := io.MultiWriter(os.Stdout, f)
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(writer, opts)
	} else {
		handler = newPatternHandler(writer, opts)
	}
	global = slog.New(handler)
	return nil
}

func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		err := logFile.Close()
		logFile = nil
		return err
	}
	return nil
}

func Logger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return global
}

func Info(ctx context.Context, typ string, msg string, args ...any) {
	logMsg(ctx, slog.LevelInfo, typ, msg, args)
}

func Warn(ctx context.Context, typ string, msg string, args ...any) {
	logMsg(ctx, slog.LevelWarn, typ, msg, args)
}

func Error(ctx context.Context, typ string, msg string, args ...any) {
	logMsg(ctx, slog.LevelError, typ, msg, args)
}

func Debug(ctx context.Context, typ string, msg string, args ...any) {
	logMsg(ctx, slog.LevelDebug, typ, msg, args)
}

func logMsg(ctx context.Context, level slog.Level, typ string, msg string, args []any) {
	mu.RLock()
	l := global
	mu.RUnlock()
	if l == nil {
		return
	}
	if !l.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	traceID := TraceID(ctx)
	if traceID != "" {
		r.AddAttrs(slog.String("__trace_id__", traceID))
	}
	if typ != "" {
		r.AddAttrs(slog.String("__type__", typ))
	}
	r.Add(args...)
	_ = l.Handler().Handle(ctx, r)
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
