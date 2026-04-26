package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"networkdisk/internal/config"
)

var (
	global *slog.Logger
	mu     sync.RWMutex
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

func Logger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return global
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
