package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"

	"networkdisk/internal/config"
	"networkdisk/internal/logging"
	"networkdisk/internal/storage"
)

type ThumbnailService struct {
	store *storage.Local
	cfg   *config.Config
}

func NewThumbnailService(store *storage.Local, cfg *config.Config) *ThumbnailService {
	return &ThumbnailService{store: store, cfg: cfg}
}

func (ts *ThumbnailService) Generate(fileID int64, storageKey string, mimeType string, onComplete func(fileID int64, thumbnailKey string)) {
	if !isImageMime(mimeType) {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logging.Logger().Error("thumbnail generation panicked",
					"panic", rec,
					"file_id", fileID,
					"storage_key", storageKey,
				)
			}
		}()
		thumbnailKey, err := ts.generate(storageKey)
		if err != nil {
			logging.Logger().Error("thumbnail generation failed",
				"error", err,
				"file_id", fileID,
				"storage_key", storageKey,
			)
			return
		}
		onComplete(fileID, thumbnailKey)
	}()
}

func (ts *ThumbnailService) generate(storageKey string) (string, error) {
	srcPath := ts.store.Path(storageKey)
	src, err := imaging.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}

	thumb := imaging.Fit(src, 256, 256, imaging.Lanczos)

	thumbKey := "thumb_" + storageKey + ".webp"
	thumbPath := ts.store.Path(thumbKey)

	if err := os.MkdirAll(filepath.Dir(thumbPath), 0755); err != nil {
		return "", fmt.Errorf("mkdir thumbnail dir: %w", err)
	}

	f, err := os.Create(thumbPath)
	if err != nil {
		return "", fmt.Errorf("create thumbnail file: %w", err)
	}
	defer f.Close()

	if err := webp.Encode(f, thumb, &webp.Options{Quality: float32(ts.cfg.Storage.ThumbnailQuality)}); err != nil {
		return "", fmt.Errorf("encode webp thumbnail: %w", err)
	}

	return thumbKey, nil
}

func isImageMime(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}
