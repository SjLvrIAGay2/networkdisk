package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"networkdisk/internal/config"
	"networkdisk/internal/handler"
	"networkdisk/internal/logging"
	"networkdisk/internal/router"
	"networkdisk/internal/service"
	"networkdisk/internal/storage"
	"networkdisk/internal/store"
)

type App struct {
	cfg    *config.Config
	store  *store.Store
	server *http.Server
}

func Run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := logging.Init(cfg); err != nil {
		return fmt.Errorf("init logging: %w", err)
	}
	logger := logging.Logger()

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

	fileStorage, err := storage.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("init file storage: %w", err)
	}

	userSvc := service.NewUserService(st, cfg)
	thumbnailSvc := service.NewThumbnailService(fileStorage, cfg)
	fileSvc := service.NewFileService(st, fileStorage, thumbnailSvc, cfg)

	authH := handler.NewAuthHandler(userSvc)
	fileH := handler.NewFileHandler(fileSvc)
	mux := router.New(authH, fileH, cfg)

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
