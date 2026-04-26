package service

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/h2non/filetype"

	"networkdisk/internal/config"
	"networkdisk/internal/model"
	"networkdisk/internal/storage"
	"networkdisk/internal/store"
)

type FileService struct {
	store     *store.Store
	fileStore *storage.Local
	thumbnail *ThumbnailService
	cfg       *config.Config
}

var (
	ErrFileNotFound     = errors.New("file not found")
	ErrNameConflict     = errors.New("name conflict")
	ErrExtensionBlocked = errors.New("extension blocked")
	ErrFileTooLarge     = errors.New("file too large")
)

func NewFileService(s *store.Store, fs *storage.Local, ts *ThumbnailService, cfg *config.Config) *FileService {
	return &FileService{store: s, fileStore: fs, thumbnail: ts, cfg: cfg}
}

func (svc *FileService) ListDirectory(parentID *int64, userID int64) ([]*model.File, error) {
	return svc.store.FilesByParentID(parentID, userID)
}

type UploadResult struct {
	File      *model.File
	Duplicate bool
}

func (svc *FileService) UploadFile(r io.Reader, name string, parentID *int64, userID int64) (*UploadResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("file name required")
	}

	ext := strings.ToLower(filepath.Ext(name))
	if err := svc.validateExtension(ext); err != nil {
		return nil, err
	}

	tmpFile, tmpPath, err := svc.createTempFile()
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpPath)

	hasher := sha256.New()
	tee := io.TeeReader(r, hasher)
	written, err := io.Copy(tmpFile, tee)
	if err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("write file: %w", err)
	}
	tmpFile.Close()

	if written > svc.cfg.Storage.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	hash := hex.EncodeToString(hasher.Sum(nil))

	var mimeType string
	if svc.cfg.Upload.DetectMime {
		mimeType = svc.detectMime(tmpPath)
	}

	existingFile, err := svc.store.FileByHash(hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check hash: %w", err)
	}

	if existingFile != nil {
		name = svc.resolveNameConflict(name, ext, parentID, userID)
		file, err := svc.createFileRecord(userID, parentID, name, written, hash, existingFile.StorageKey, "", mimeType, false)
		if err != nil {
			return nil, err
		}
		if err := svc.store.UpdateUserStorageUsed(userID, written); err != nil {
			return nil, fmt.Errorf("update storage used: %w", err)
		}
		return &UploadResult{File: file, Duplicate: true}, nil
	}

	name = svc.resolveNameConflict(name, ext, parentID, userID)
	storageKey := svc.generateStorageKey(ext)
	finalPath := svc.fileStore.Path(storageKey)

	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return nil, fmt.Errorf("mkdir for storage: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return nil, fmt.Errorf("move to storage: %w", err)
	}

	file, err := svc.createFileRecord(userID, parentID, name, written, hash, storageKey, "", mimeType, false)
	if err != nil {
		os.Remove(finalPath)
		return nil, err
	}

	if err := svc.store.UpdateUserStorageUsed(userID, written); err != nil {
		return nil, fmt.Errorf("update storage used: %w", err)
	}

	svc.thumbnail.Generate(file.ID, storageKey, mimeType, func(fileID int64, thumbnailKey string) {
		svc.store.UpdateFileThumbnailKey(fileID, thumbnailKey)
	})

	return &UploadResult{File: file, Duplicate: false}, nil
}

func (svc *FileService) OpenFile(fileID int64, userID int64) (*model.File, io.ReadCloser, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrFileNotFound
		}
		return nil, nil, fmt.Errorf("lookup file: %w", err)
	}
	if f.UserID != userID || f.IsDir || f.IsDeleted {
		return nil, nil, ErrFileNotFound
	}
	diskPath := svc.fileStore.Path(f.StorageKey)
	reader, err := os.Open(diskPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open file: %w", err)
	}
	return f, reader, nil
}

func (svc *FileService) CreateDir(name string, parentID *int64, userID int64) (*model.File, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("directory name required")
	}
	exists, err := svc.store.FileNameExists(parentID, userID, name)
	if err != nil {
		return nil, fmt.Errorf("check name exists: %w", err)
	}
	if exists {
		return nil, ErrNameConflict
	}
	return svc.createFileRecord(userID, parentID, name, 0, "", "", "", "", true)
}

func (svc *FileService) RenameFile(fileID int64, name string, userID int64) (*model.File, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("lookup file: %w", err)
	}
	if f.UserID != userID {
		return nil, ErrFileNotFound
	}
	if f.Name == name {
		return f, nil
	}
	exists, err := svc.store.FileNameExists(f.ParentID, userID, name)
	if err != nil {
		return nil, fmt.Errorf("check name exists: %w", err)
	}
	if exists {
		return nil, ErrNameConflict
	}
	if err := svc.store.UpdateFileName(fileID, name); err != nil {
		return nil, fmt.Errorf("update name: %w", err)
	}
	return svc.store.FileByID(fileID)
}

func (svc *FileService) DeleteFile(fileID int64, userID int64) error {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFileNotFound
		}
		return fmt.Errorf("lookup file: %w", err)
	}
	if f.UserID != userID {
		return ErrFileNotFound
	}
	if err := svc.store.SoftDeleteFile(fileID); err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}
	if !f.IsDir {
		if err := svc.store.UpdateUserStorageUsed(userID, -f.Size); err != nil {
			return fmt.Errorf("update storage used: %w", err)
		}
	}
	return nil
}

func (svc *FileService) FileByID(fileID int64, userID int64) (*model.File, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	if f.UserID != userID || f.IsDeleted {
		return nil, ErrFileNotFound
	}
	return f, nil
}

func (svc *FileService) OpenThumbnail(fileID int64, userID int64) (*model.File, io.ReadCloser, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrFileNotFound
		}
		return nil, nil, fmt.Errorf("lookup file: %w", err)
	}
	if f.UserID != userID || f.IsDeleted || f.ThumbnailKey == "" {
		return nil, nil, ErrFileNotFound
	}
	diskPath := svc.fileStore.Path(f.ThumbnailKey)
	reader, err := os.Open(diskPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open thumbnail: %w", err)
	}
	return f, reader, nil
}

func (svc *FileService) createFileRecord(userID int64, parentID *int64, name string, size int64, fileHash, storageKey, thumbnailKey, mimeType string, isDir bool) (*model.File, error) {
	f := &model.File{
		UserID:       userID,
		ParentID:     parentID,
		Name:         name,
		IsDir:        isDir,
		Size:         size,
		FileHash:     fileHash,
		StorageKey:   storageKey,
		ThumbnailKey: thumbnailKey,
		MimeType:     mimeType,
	}
	return svc.store.CreateFile(f)
}

func (svc *FileService) validateExtension(ext string) error {
	if len(svc.cfg.Upload.AllowedExtensions) > 0 {
		for _, allowed := range svc.cfg.Upload.AllowedExtensions {
			if ext == strings.ToLower(allowed) {
				return nil
			}
		}
		return ErrExtensionBlocked
	}
	for _, blocked := range svc.cfg.Upload.BlockedExtensions {
		if ext == strings.ToLower(blocked) {
			return ErrExtensionBlocked
		}
	}
	return nil
}

func (svc *FileService) resolveNameConflict(desiredName, ext string, parentID *int64, userID int64) string {
	exists, err := svc.store.FileNameExists(parentID, userID, desiredName)
	if err != nil || !exists {
		return desiredName
	}

	switch svc.cfg.Upload.OnNameConflict {
	case "overwrite":
		return desiredName
	default:
		base := strings.TrimSuffix(desiredName, ext)
		for i := 1; i < 1000; i++ {
			candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
			exists, err := svc.store.FileNameExists(parentID, userID, candidate)
			if err != nil || !exists {
				return candidate
			}
		}
		return desiredName
	}
}

func (svc *FileService) detectMime(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 8192)
	n, _ := io.ReadFull(f, head)
	kind, err := filetype.Match(head[:n])
	if err != nil {
		return ""
	}
	return kind.MIME.Value
}

func (svc *FileService) createTempFile() (*os.File, string, error) {
	tmpDir := filepath.Join(svc.fileStore.Root, "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, "", err
	}
	tmpFile, err := os.CreateTemp(tmpDir, "upload-*.tmp")
	if err != nil {
		return nil, "", err
	}
	return tmpFile, tmpFile.Name(), nil
}

func (svc *FileService) generateStorageKey(ext string) string {
	b := make([]byte, 16)
	rand.Read(b)
	key := hex.EncodeToString(b)
	if ext != "" {
		key += ext
	}
	return key
}
