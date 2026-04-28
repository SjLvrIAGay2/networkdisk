package service

import (
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"

	"networkdisk/internal/config"
	"networkdisk/internal/logging"
	"networkdisk/internal/storage"
)

type ThumbnailService struct {
	store     *storage.Local
	cfg       *config.Config
	ffmpegPath string
}

func NewThumbnailService(store *storage.Local, cfg *config.Config) *ThumbnailService {
	return &ThumbnailService{
		store:     store,
		cfg:       cfg,
		ffmpegPath: resolveFfmpeg(cfg.Storage.FfmpegPath),
	}
}

func resolveFfmpeg(configPath string) string {
	if configPath != "" {
		if p, err := exec.LookPath(configPath); err == nil {
			return p
		}
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}
		return ""
	}
	localPath := filepath.Join("ffmpeg", "ffmpeg")
	if _, err := os.Stat(localPath); err == nil {
		return localPath
	}
	return ""
}

func (ts *ThumbnailService) UpdateConfig(cfg *config.Config) {
	ts.cfg = cfg
	ts.ffmpegPath = resolveFfmpeg(cfg.Storage.FfmpegPath)
}

func (ts *ThumbnailService) Generate(fileID int64, storageKey string, mimeType string, onComplete func(fileID int64, thumbnailKey string)) {
	if isImageMime(mimeType) {
		go ts.generateImage(fileID, storageKey, onComplete)
		return
	}
	if isVideoMime(mimeType) && ts.ffmpegPath != "" {
		go ts.generateVideo(fileID, storageKey, onComplete)
	}
}

func (ts *ThumbnailService) generateImage(fileID int64, storageKey string, onComplete func(fileID int64, thumbnailKey string)) {
	defer func() {
		if r := recover(); r != nil {
			logging.Logger().Error("thumbnail generation panicked", "panic", r, "file_id", fileID, "storage_key", storageKey)
		}
	}()
	srcPath := ts.store.Path(storageKey)
	src, err := imaging.Open(srcPath)
	if err != nil {
		logging.Logger().Error("thumbnail generation failed", "error", err, "file_id", fileID, "storage_key", storageKey)
		return
	}

	thumb := imaging.Fit(src, 256, 256, imaging.Lanczos)

	thumbKey := "thumb_" + storageKey + ".webp"
	thumbPath := ts.store.Path(thumbKey)

	if err := os.MkdirAll(filepath.Dir(thumbPath), 0755); err != nil {
		logging.Logger().Error("thumbnail generation failed", "error", fmt.Errorf("mkdir: %w", err), "file_id", fileID)
		return
	}

	f, err := os.Create(thumbPath)
	if err != nil {
		logging.Logger().Error("thumbnail generation failed", "error", fmt.Errorf("create: %w", err), "file_id", fileID)
		return
	}
	defer f.Close()

	if err := webp.Encode(f, thumb, &webp.Options{Quality: float32(ts.cfg.Storage.ThumbnailQuality)}); err != nil {
		logging.Logger().Error("thumbnail generation failed", "error", fmt.Errorf("encode: %w", err), "file_id", fileID)
		return
	}

	onComplete(fileID, thumbKey)
}

func (ts *ThumbnailService) generateVideo(fileID int64, storageKey string, onComplete func(fileID int64, thumbnailKey string)) {
	defer func() {
		if r := recover(); r != nil {
			logging.Logger().Error("video thumbnail panicked", "panic", r, "file_id", fileID, "storage_key", storageKey)
		}
	}()
	thumbnailKey, err := ts.extractVideoFrame(storageKey)
	if err != nil {
		logging.Logger().Error("video thumbnail failed", "error", err, "file_id", fileID, "storage_key", storageKey)
		return
	}
	onComplete(fileID, thumbnailKey)
}

func (ts *ThumbnailService) extractVideoFrame(storageKey string) (string, error) {
	srcPath := ts.store.Path(storageKey)

	cmd := exec.Command(ts.ffmpegPath, "-ss", "00:00:01", "-i", srcPath, "-vframes", "1", "-f", "image2pipe", "-vcodec", "png", "-")
	cmd.Stderr = nil
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ffmpeg start: %w", err)
	}

	frame, err := png.Decode(stdout)
	if err != nil {
		cmd.Wait()
		return "", fmt.Errorf("ffmpeg png decode: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("ffmpeg wait: %w", err)
	}

	thumb := imaging.Fit(frame, 256, 256, imaging.Lanczos)

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

func isVideoMime(mimeType string) bool {
	return strings.HasPrefix(mimeType, "video/")
}
