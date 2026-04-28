package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

if cfg.Auth.JWTSecret == "" {
		return fmt.Errorf("auth.jwt_secret must be set")
	}
	if len(cfg.Auth.JWTSecret) < 32 {
		return fmt.Errorf("auth.jwt_secret must be at least 32 characters")
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
	fileSvc.CleanupTempFiles()

	shareSvc := service.NewShareService(st, fileSvc, cfg)
	searchSvc := service.NewSearchService(st)
	tagSvc := service.NewTagService(st)

	authH := handler.NewAuthHandler(userSvc, fileSvc)
	fileH := handler.NewFileHandler(fileSvc)
	sysH := handler.NewSystemHandler(fileSvc)
	shareH := handler.NewShareHandler(shareSvc, fileSvc, cfg)
	searchH := handler.NewSearchHandler(searchSvc)
	tagH := handler.NewTagHandler(tagSvc)
	mux, stopRateLimiter := router.New(authH, fileH, sysH, shareH, searchH, tagH, cfg)
	defer stopRateLimiter()

	recycleDone := startDailyTimer(3, func() {
		cleaned, err := fileSvc.AutoCleanRecycle()
		if err != nil {
			logging.Error(context.Background(), "background", "auto clean recycle failed", "error", err)
		} else if cleaned > 0 {
			logging.Info(context.Background(), "background", "auto clean recycle completed", "cleaned", cleaned)
		}
	})
	defer close(recycleDone)

	chunkDone := startHourlyTimer(func() {
		cleaned, err := fileSvc.CleanStaleChunks()
		if err != nil {
			logging.Error(context.Background(), "background", "clean stale chunks failed", "error", err)
		} else if cleaned > 0 {
			logging.Info(context.Background(), "background", "cleaned stale upload chunks", "sessions", cleaned)
		}
	})
	defer close(chunkDone)

	calibrateDone := startDailyTimer(4, func() {
		fixed, err := fileSvc.CalibrateAllStorage()
		if err != nil {
			logging.Error(context.Background(), "background", "storage calibration failed", "error", err)
		} else if fixed > 0 {
			logging.Info(context.Background(), "background", "storage calibration completed", "users_calibrated", fixed)
		}
	})
	defer close(calibrateDone)

	auditCleanDone := startDailyTimer(5, func() {
		deleted, err := fileSvc.CleanExpiredAuditLogs()
		if err != nil {
			logging.Error(context.Background(), "background", "audit log cleanup failed", "error", err)
		} else if deleted > 0 {
			logging.Info(context.Background(), "background", "cleaned expired audit logs", "deleted", deleted)
		}
	})
	defer close(auditCleanDone)

	tempDownloadCleanDone := startHourlyTimer(func() {
		cleaned, err := shareSvc.CleanExpiredTempDownloads()
		if err != nil {
			logging.Error(context.Background(), "background", "temp download cleanup failed", "error", err)
		} else if cleaned > 0 {
			logging.Info(context.Background(), "background", "cleaned expired temp downloads", "count", cleaned)
		}
	})
	defer close(tempDownloadCleanDone)

	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeoutDuration(),
		WriteTimeout: cfg.WriteTimeoutDuration(),
	}

	reloadDone := config.StartReloadWatcher(configPath, func(newCfg *config.Config) {
		userSvc.UpdateConfig(newCfg)
		thumbnailSvc.UpdateConfig(newCfg)
		fileSvc.UpdateConfig(newCfg)
		shareSvc.UpdateConfig(newCfg)
		logging.Info(context.Background(), "background", "config reloaded")
	})

	serverErr := make(chan error, 1)
	go func() {
		logging.Info(context.Background(), "background", "server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		logging.Error(context.Background(), "background", "server error", "error", err)
		close(reloadDone)
		return err
	case sig := <-quit:
		logging.Info(context.Background(), "background", "shutting down", "signal", sig.String())
	}

	close(reloadDone)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeoutDuration())
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logging.Error(context.Background(), "background", "server shutdown error", "error", err)
	}

	return nil
}

func safeCall(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error(context.Background(), "background", "background task panicked", "panic", r)
		}
	}()
	fn()
}

func startDailyTimer(hourOffset int, fn func()) chan struct{} {
	done := make(chan struct{})
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, hourOffset, 0, 0, 0, now.Location())
			d := next.Sub(now)
			if d < 0 {
				d = time.Hour
			}
			select {
			case <-time.After(d):
				safeCall(fn)
			case <-done:
				return
			}
		}
	}()
	return done
}

func startHourlyTimer(fn func()) chan struct{} {
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-time.After(time.Hour):
				safeCall(fn)
			case <-done:
				return
			}
		}
	}()
	return done
}
