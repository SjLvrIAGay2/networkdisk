package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"networkdisk/internal/config"
	"networkdisk/internal/handler"
	"networkdisk/internal/router"
	"networkdisk/internal/service"
	"networkdisk/internal/store"
)

type App struct {
	cfg    *config.Config
	logger *slog.Logger
	store  *store.Store
	server *http.Server
}

func Run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := newLogger(cfg)

	if cfg.Auth.JWTSecret == "" {
		return fmt.Errorf("auth.jwt_secret must be set")
	}

	st, err := store.New(cfg.DSN(), cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, cfg.ConnMaxLifetimeDuration())
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer st.Close()

	if err := st.RunMigrations("migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	userSvc := service.NewUserService(st, cfg)
	authH := handler.NewAuthHandler(userSvc)
	mux := router.New(authH, cfg, logger)

	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeoutDuration(),
		WriteTimeout: cfg.WriteTimeoutDuration(),
	}

	reloadDone := config.StartReloadWatcher(configPath, func(newCfg *config.Config) {
		logger.Info("config reloaded")
	})

	go func() {
		logger.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutting down", "signal", sig.String())

	close(reloadDone)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeoutDuration())
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	return nil
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}
