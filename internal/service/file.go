package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/h2non/filetype"
	"github.com/yuin/goldmark"

	"networkdisk/internal/config"
	"networkdisk/internal/logging"
	"networkdisk/internal/model"
	"networkdisk/internal/storage"
	"networkdisk/internal/store"
)

type FileService struct {
	store     *store.Store
	fileStore storage.StorageBackend
	thumbnail *ThumbnailService
	cfg       atomic.Value
	hashMu    sync.Mutex
	hashLocks map[string]*sync.Mutex
}

var (
	ErrFileNotFound     = errors.New("文件不存在")
	ErrNameConflict     = errors.New("名称冲突")
	ErrExtensionBlocked = errors.New("文件类型受限")
	ErrFileTooLarge     = errors.New("文件过大")
)

func NewFileService(s *store.Store, fs storage.StorageBackend, ts *ThumbnailService, cfg *config.Config) *FileService {
	svc := &FileService{
		store:     s,
		fileStore: fs,
		thumbnail: ts,
		hashLocks: make(map[string]*sync.Mutex),
	}
	svc.cfg.Store(cfg)
	return svc
}

func (svc *FileService) getCfg() *config.Config {
	return svc.cfg.Load().(*config.Config)
}

func (svc *FileService) UpdateConfig(cfg *config.Config) {
	svc.cfg.Store(cfg)
}

func (svc *FileService) lockHash(hash string) {
	svc.hashMu.Lock()
	mu, ok := svc.hashLocks[hash]
	if !ok {
		mu = &sync.Mutex{}
		svc.hashLocks[hash] = mu
	}
	svc.hashMu.Unlock()
	mu.Lock()
}

func (svc *FileService) unlockHash(hash string) {
	svc.hashMu.Lock()
	mu, ok := svc.hashLocks[hash]
	svc.hashMu.Unlock()
	if ok {
		mu.Unlock()
	}
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
		return nil, fmt.Errorf("缺少文件名")
	}
	if len(name) > 255 {
		return nil, fmt.Errorf("文件名过长")
	}

	ext := strings.ToLower(filepath.Ext(name))
	if err := svc.validateExtension(ext); err != nil {
		return nil, err
	}

	if parentID != nil {
		if err := svc.validateParentDir(*parentID, userID); err != nil {
			return nil, err
		}
	}

	tmpFile, tmpPath, err := svc.createTempFile()
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpPath)

	limited := io.LimitReader(r, svc.getCfg().Storage.MaxFileSize+1)
	hasher := sha256.New()
	tee := io.TeeReader(limited, hasher)
	written, err := io.Copy(tmpFile, tee)
	if err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("write file: %w", err)
	}
	tmpFile.Close()

	if written > svc.getCfg().Storage.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	hash := hex.EncodeToString(hasher.Sum(nil))

	var mimeType string
	if svc.getCfg().Upload.DetectMime {
		mimeType = svc.detectMime(tmpPath)
	}

	svc.lockHash(hash)
	existingFile, err := svc.store.FileByHash(hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		svc.unlockHash(hash)
		return nil, fmt.Errorf("check hash: %w", err)
	}

	if existingFile != nil {
		svc.unlockHash(hash)
		var oldFile *overwriteTarget
		name, oldFile, err = svc.resolveNameConflict(name, ext, parentID, userID)
		if err != nil {
			return nil, fmt.Errorf("resolve name conflict: %w", err)
		}
		tx, err := svc.store.BeginTx()
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback()
		if oldFile != nil {
			if err := tx.BatchSoftDelete([]int64{oldFile.ID}); err != nil {
				return nil, fmt.Errorf("soft delete old: %w", err)
			}
			if !oldFile.IsDir && oldFile.Size > 0 {
				if err := tx.UpdateUserStorageUsed(userID, -oldFile.Size); err != nil {
					return nil, fmt.Errorf("update storage for overwrite: %w", err)
				}
			}
		}
		file, err := tx.CreateFile(&model.File{
			UserID: userID, ParentID: parentID, Name: name, Size: written,
			FileHash: hash, StorageKey: existingFile.StorageKey, MimeType: mimeType,
		})
		if err != nil {
			return nil, err
		}
		if err := tx.UpdateUserStorageUsed(userID, written); err != nil {
			return nil, fmt.Errorf("update storage used: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
		return &UploadResult{File: file, Duplicate: true}, nil
	}
	svc.unlockHash(hash)

	var oldFile *overwriteTarget
	name, oldFile, err = svc.resolveNameConflict(name, ext, parentID, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve name conflict: %w", err)
	}
	storageKey, err := generateStorageKey(ext)
	if err != nil {
		return nil, fmt.Errorf("generate storage key: %w", err)
	}
	finalPath := svc.fileStore.Path(storageKey)

	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return nil, fmt.Errorf("mkdir for storage: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return nil, fmt.Errorf("move to storage: %w", err)
	}

	tx, err := svc.store.BeginTx()
	if err != nil {
		os.Remove(finalPath)
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if oldFile != nil {
		if err := tx.BatchSoftDelete([]int64{oldFile.ID}); err != nil {
			os.Remove(finalPath)
			return nil, fmt.Errorf("soft delete old: %w", err)
		}
		if !oldFile.IsDir && oldFile.Size > 0 {
			if err := tx.UpdateUserStorageUsed(userID, -oldFile.Size); err != nil {
				os.Remove(finalPath)
				return nil, fmt.Errorf("update storage for overwrite: %w", err)
			}
		}
	}
	file, err := tx.CreateFile(&model.File{
		UserID: userID, ParentID: parentID, Name: name, Size: written,
		FileHash: hash, StorageKey: storageKey, MimeType: mimeType,
	})
	if err != nil {
		os.Remove(finalPath)
		return nil, err
	}
	if err := tx.UpdateUserStorageUsed(userID, written); err != nil {
		return nil, fmt.Errorf("update storage used: %w", err)
	}
	if err := tx.Commit(); err != nil {
		os.Remove(finalPath)
		return nil, fmt.Errorf("commit: %w", err)
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
		return nil, fmt.Errorf("缺少目录名")
	}
	if len(name) > 255 {
		return nil, fmt.Errorf("目录名过长")
	}

	if parentID != nil {
		if err := svc.validateParentDir(*parentID, userID); err != nil {
			return nil, err
		}
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
		return nil, fmt.Errorf("缺少名称")
	}
	if len(name) > 255 {
		return nil, fmt.Errorf("名称过长")
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return nil, fmt.Errorf("rename begin tx: %w", err)
	}
	defer tx.Rollback()
	f, err := tx.FileByID(fileID)
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
		return f, tx.Commit()
	}
	exists, err := tx.FileNameExists(f.ParentID, userID, name)
	if err != nil {
		return nil, fmt.Errorf("check name exists: %w", err)
	}
	if exists {
		return nil, ErrNameConflict
	}
	if err := tx.UpdateFileName(fileID, name); err != nil {
		return nil, fmt.Errorf("update name: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("rename commit: %w", err)
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
	deleteIDs := []int64{fileID}
	var sizeDelta int64
	if f.IsDir {
		descendants, err := svc.store.DescendantIDs(fileID, false)
		if err != nil {
			return fmt.Errorf("collect descendants: %w", err)
		}
		if len(descendants) > 0 {
			children, err := svc.store.FilesByIDs(descendants)
			if err != nil {
				return fmt.Errorf("lookup descendants: %w", err)
			}
			for _, child := range children {
				if !child.IsDir {
					sizeDelta -= child.Size
				}
				deleteIDs = append(deleteIDs, child.ID)
			}
		}
	} else {
		sizeDelta = -f.Size
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return fmt.Errorf("delete begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := tx.BatchSoftDelete(deleteIDs); err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}
	if sizeDelta != 0 {
		if err := tx.UpdateUserStorageUsed(userID, sizeDelta); err != nil {
			return fmt.Errorf("update storage used: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete commit: %w", err)
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

func (svc *FileService) CleanupTempFiles() error {
	tmpDir := svc.fileStore.Path("tmp")
	matches, err := filepath.Glob(filepath.Join(tmpDir, "upload-*.tmp"))
	if err != nil {
		return fmt.Errorf("cleanup temp files glob: %w", err)
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			return fmt.Errorf("cleanup temp file %s: %w", m, err)
		}
	}
	return nil
}

func (svc *FileService) validateParentDir(parentID int64, userID int64) error {
	dir, err := svc.store.FileByID(parentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("父目录不存在")
		}
		return fmt.Errorf("lookup parent directory: %w", err)
	}
	if !dir.IsDir {
		return fmt.Errorf("父路径不是目录")
	}
	if dir.UserID != userID {
		return ErrFileNotFound
	}
	if dir.IsDeleted {
		return fmt.Errorf("父目录已被删除")
	}
	return nil
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
	if len(svc.getCfg().Upload.AllowedExtensions) > 0 {
		for _, allowed := range svc.getCfg().Upload.AllowedExtensions {
			if ext == strings.ToLower(allowed) {
				return nil
			}
		}
		return ErrExtensionBlocked
	}
	for _, blocked := range svc.getCfg().Upload.BlockedExtensions {
		if ext == strings.ToLower(blocked) {
			return ErrExtensionBlocked
		}
	}
	return nil
}

type overwriteTarget struct {
	ID    int64
	Size  int64
	IsDir bool
}

func (svc *FileService) resolveNameConflict(desiredName, ext string, parentID *int64, userID int64) (string, *overwriteTarget, error) {
	exists, err := svc.store.FileNameExists(parentID, userID, desiredName)
	if err != nil {
		return "", nil, fmt.Errorf("check name exists: %w", err)
	}
	if !exists {
		return desiredName, nil, nil
	}

	switch svc.getCfg().Upload.OnNameConflict {
	case "overwrite":
		existing, err := svc.store.FileByName(parentID, userID, desiredName)
		if err != nil {
			return "", nil, fmt.Errorf("lookup existing file: %w", err)
		}
		if existing == nil {
			return desiredName, nil, nil
		}
		return desiredName, &overwriteTarget{ID: existing.ID, Size: existing.Size, IsDir: existing.IsDir}, nil
	default:
		base := strings.TrimSuffix(desiredName, ext)
		const maxRenames = 1000
		for i := 1; i < maxRenames; i++ {
			candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
			dupExists, err := svc.store.FileNameExists(parentID, userID, candidate)
			if err != nil {
				return "", nil, fmt.Errorf("check name candidate: %w", err)
			}
			if !dupExists {
				return candidate, nil, nil
			}
		}
		return "", nil, ErrNameConflict
	}
}

func (svc *FileService) detectMime(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 8192)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return ""
	}
	kind, err := filetype.Match(head[:n])
	if err != nil {
		return ""
	}
	return kind.MIME.Value
}

func (svc *FileService) createTempFile() (*os.File, string, error) {
	tmpDir := svc.fileStore.Path("tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, "", err
	}
	tmpFile, err := os.CreateTemp(tmpDir, "upload-*.tmp")
	if err != nil {
		return nil, "", err
	}
	return tmpFile, tmpFile.Name(), nil
}

func (svc *FileService) FilesInRecycleBin(userID int64) ([]*model.File, error) {
	return svc.store.FilesInRecycleBin(userID)
}

func (svc *FileService) DescendantCounts(fileID int64, userID int64) (int64, int64, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		return 0, 0, fmt.Errorf("descendant counts lookup: %w", err)
	}
	if f.UserID != userID || !f.IsDeleted || !f.IsDir {
		return 0, 0, ErrFileNotFound
	}
	return svc.store.DescendantCounts(fileID)
}

func (svc *FileService) RestoreFile(fileID int64, userID int64) (*model.File, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		return nil, fmt.Errorf("restore lookup: %w", err)
	}
	if f.UserID != userID || !f.IsDeleted {
		return nil, ErrFileNotFound
	}
	needsParentFix := false
	if f.ParentID != nil {
		parent, err := svc.store.FileByID(*f.ParentID)
		if err != nil || parent.UserID != userID || parent.IsDeleted {
			if f.IsDir {
				needsParentFix = true
			} else {
				return nil, fmt.Errorf("恢复失败：父目录不存在")
			}
		}
	}
	restoreIDs := []int64{fileID}
	var sizeDelta int64
	if f.IsDir {
		descendants, err := svc.store.DescendantIDs(fileID, true)
		if err != nil {
			return nil, fmt.Errorf("collect descendants: %w", err)
		}
		if len(descendants) > 0 {
			children, err := svc.store.FilesByIDs(descendants)
			if err != nil {
				return nil, fmt.Errorf("lookup descendants: %w", err)
			}
			for _, child := range children {
				if !child.IsDir {
					sizeDelta += child.Size
				}
				restoreIDs = append(restoreIDs, child.ID)
			}
		}
	} else {
		sizeDelta = f.Size
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return nil, fmt.Errorf("restore begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := tx.RestoreFiles(restoreIDs); err != nil {
		return nil, fmt.Errorf("restore: %w", err)
	}
	if needsParentFix {
		if err := tx.UpdateFileParentID(fileID, nil); err != nil {
			return nil, fmt.Errorf("restore fix parent: %w", err)
		}
	}
	if sizeDelta != 0 {
		if err := tx.UpdateUserStorageUsed(userID, sizeDelta); err != nil {
			return nil, fmt.Errorf("restore update storage: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("restore commit: %w", err)
	}
	return svc.store.FileByID(fileID)
}

func (svc *FileService) PermanentDeleteFile(fileID int64, userID int64) error {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		return fmt.Errorf("permanent delete lookup: %w", err)
	}
	if f.UserID != userID || !f.IsDeleted {
		return ErrFileNotFound
	}
	deleteIDs := []int64{fileID}
	physDelStorages := []string{}
	physDelThumbnails := []string{}
	var sizeDelta int64
	if f.IsDir {
		descendants, err := svc.store.DescendantIDs(fileID, true)
		if err != nil {
			return fmt.Errorf("collect descendants: %w", err)
		}
		if len(descendants) > 0 {
			children, err := svc.store.FilesByIDs(descendants)
			if err != nil {
				return fmt.Errorf("lookup descendants: %w", err)
			}
			for _, child := range children {
				if !child.IsDir && child.Size > 0 {
					sizeDelta -= child.Size
				}
				deleteIDs = append(deleteIDs, child.ID)
			}
		}
	}
	if !f.IsDir && f.Size > 0 {
		sizeDelta -= f.Size
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return fmt.Errorf("permanent delete begin tx: %w", err)
	}
	defer tx.Rollback()
	checked := map[string]bool{}
	if len(deleteIDs) > 0 {
		batchFiles, err := tx.FilesByIDs(deleteIDs)
		if err != nil {
			return fmt.Errorf("permanent delete lookup batch in tx: %w", err)
		}
		for _, df := range batchFiles {
			if df.StorageKey != "" && !checked[df.StorageKey] {
				checked[df.StorageKey] = true
				count, err := tx.CountByStorageKey(df.StorageKey)
				if err == nil && count <= 1 {
					physDelStorages = append(physDelStorages, df.StorageKey)
				}
			}
			if df.ThumbnailKey != "" && !checked[df.ThumbnailKey] {
				checked[df.ThumbnailKey] = true
				count, err := tx.CountByThumbnailKey(df.ThumbnailKey)
				if err == nil && count <= 1 {
					physDelThumbnails = append(physDelThumbnails, df.ThumbnailKey)
				}
			}
		}
	}
	if err := tx.HardDeleteFiles(deleteIDs); err != nil {
		return fmt.Errorf("permanent delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("permanent delete commit: %w", err)
	}
	for _, sk := range physDelStorages {
		svc.fileStore.Delete(sk)
	}
	for _, tk := range physDelThumbnails {
		svc.fileStore.Delete(tk)
	}
	if sizeDelta != 0 {
		if err := svc.store.UpdateUserStorageUsed(userID, sizeDelta); err != nil {
			return fmt.Errorf("permanent delete update storage: %w", err)
		}
	}
	return nil
}

func (svc *FileService) MoveFile(fileID int64, targetParentID *int64, userID int64) (*model.File, error) {
	if targetParentID != nil {
		if *targetParentID == fileID {
			return nil, fmt.Errorf("不能移动到自身")
		}
		if err := svc.validateParentDir(*targetParentID, userID); err != nil {
			return nil, err
		}
	}
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		return nil, fmt.Errorf("move lookup: %w", err)
	}
	if f.UserID != userID || f.IsDeleted {
		return nil, ErrFileNotFound
	}
	if f.IsDir && targetParentID != nil {
		if isDescendant, err := svc.isDescendantOf(*targetParentID, fileID); err != nil {
			return nil, fmt.Errorf("move check cycle: %w", err)
		} else if isDescendant {
			return nil, fmt.Errorf("不能将目录移动到自身的子目录中")
		}
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return nil, fmt.Errorf("move begin tx: %w", err)
	}
	defer tx.Rollback()
	f, err = tx.FileByID(fileID)
	if err != nil {
		return nil, fmt.Errorf("move lookup: %w", err)
	}
	if f.UserID != userID || f.IsDeleted {
		return nil, ErrFileNotFound
	}
	exists, err := tx.FileNameExists(targetParentID, userID, f.Name)
	if err != nil {
		return nil, fmt.Errorf("move check name: %w", err)
	}
	if exists {
		return nil, ErrNameConflict
	}
	if err := tx.UpdateFileParentID(fileID, targetParentID); err != nil {
		return nil, fmt.Errorf("move: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("move commit: %w", err)
	}
	return svc.store.FileByID(fileID)
}

func (svc *FileService) isDescendantOf(ancestorID, descendantID int64) (bool, error) {
	if ancestorID == descendantID {
		return true, nil
	}
	ids, err := svc.store.DescendantIDs(ancestorID, false)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == descendantID {
			return true, nil
		}
	}
	return false, nil
}

func (svc *FileService) CopyFile(fileID int64, targetParentID *int64, userID int64) (*model.File, error) {
	src, err := svc.store.FileByID(fileID)
	if err != nil {
		return nil, fmt.Errorf("copy lookup: %w", err)
	}
	if src.UserID != userID || src.IsDir || src.IsDeleted {
		return nil, ErrFileNotFound
	}
	if targetParentID != nil {
		if err := svc.validateParentDir(*targetParentID, userID); err != nil {
			return nil, err
		}
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return nil, fmt.Errorf("copy begin tx: %w", err)
	}
	defer tx.Rollback()
	name := src.Name
	exists, err := tx.FileNameExists(targetParentID, userID, name)
	if err != nil {
		return nil, fmt.Errorf("copy check name: %w", err)
	}
	if exists {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		const maxRenames = 1000
		resolved := false
		for i := 1; i < maxRenames; i++ {
			candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
			dupExists, err := tx.FileNameExists(targetParentID, userID, candidate)
			if err != nil {
				return nil, fmt.Errorf("copy check name candidate: %w", err)
			}
			if !dupExists {
				name = candidate
				resolved = true
				break
			}
		}
		if !resolved {
			return nil, ErrNameConflict
		}
	}
	newFile := &model.File{
		UserID:       userID,
		ParentID:     targetParentID,
		Name:         name,
		IsDir:        false,
		Size:         src.Size,
		FileHash:     src.FileHash,
		StorageKey:   src.StorageKey,
		ThumbnailKey: src.ThumbnailKey,
		MimeType:     src.MimeType,
	}
	copied, err := tx.CreateFile(newFile)
	if err != nil {
		return nil, fmt.Errorf("copy create record: %w", err)
	}
	if err := tx.UpdateUserStorageUsed(userID, src.Size); err != nil {
		return nil, fmt.Errorf("copy update storage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("copy commit: %w", err)
	}
	return copied, nil
}

func (svc *FileService) ToggleStar(fileID int64, userID int64) (bool, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		return false, fmt.Errorf("toggle star lookup: %w", err)
	}
	if f.UserID != userID || f.IsDeleted {
		return false, ErrFileNotFound
	}
	return svc.store.ToggleStar(fileID)
}

func (svc *FileService) StarredFiles(userID int64) ([]*model.File, error) {
	return svc.store.StarredFiles(userID)
}

func (svc *FileService) BatchDelete(ids []int64, userID int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("批量删除：未选择文件")
	}
	allIDs := make([]int64, 0, len(ids))
	var sizeDelta int64
	for _, id := range ids {
		f, err := svc.store.FileByID(id)
		if err != nil {
			return fmt.Errorf("batch delete lookup %d: %w", id, err)
		}
		if f.UserID != userID || f.IsDeleted {
			return fmt.Errorf("批量删除：文件 %d 不存在", id)
		}
		allIDs = append(allIDs, id)
		if f.IsDir {
			descendants, err := svc.store.DescendantIDs(id, false)
			if err != nil {
				return fmt.Errorf("batch delete descendants %d: %w", id, err)
			}
			if len(descendants) > 0 {
				children, err := svc.store.FilesByIDs(descendants)
				if err != nil {
					return fmt.Errorf("batch delete lookup descendants: %w", err)
				}
				for _, child := range children {
					if !child.IsDir {
						sizeDelta -= child.Size
					}
					allIDs = append(allIDs, child.ID)
				}
			}
		} else if f.Size > 0 {
			sizeDelta -= f.Size
		}
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return fmt.Errorf("batch delete begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := tx.BatchSoftDelete(allIDs); err != nil {
		return fmt.Errorf("batch delete: %w", err)
	}
	if sizeDelta != 0 {
		if err := tx.UpdateUserStorageUsed(userID, sizeDelta); err != nil {
			return fmt.Errorf("batch delete storage: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("batch delete commit: %w", err)
	}
	return nil
}

func (svc *FileService) BatchMove(ids []int64, targetParentID *int64, userID int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("批量移动：未选择文件")
	}
	if targetParentID != nil {
		if err := svc.validateParentDir(*targetParentID, userID); err != nil {
			return err
		}
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return fmt.Errorf("batch move begin tx: %w", err)
	}
	defer tx.Rollback()
	for _, id := range ids {
		f, err := tx.FileByID(id)
		if err != nil {
			return fmt.Errorf("文件 %d: %w", id, err)
		}
		if f.UserID != userID || f.IsDeleted {
			return fmt.Errorf("文件 %d: 不存在", id)
		}
		if targetParentID != nil && *targetParentID == id {
			return fmt.Errorf("文件 %d: 不能移动到自身", id)
		}
		if f.IsDir && targetParentID != nil {
			if isDescendant, err := svc.isDescendantOf(*targetParentID, id); err != nil {
				return fmt.Errorf("文件 %d: 循环检测失败: %w", id, err)
			} else if isDescendant {
				return fmt.Errorf("文件 %d: 不能将目录移动到自身的子目录中", id)
			}
		}
		exists, err := tx.FileNameExists(targetParentID, userID, f.Name)
		if err != nil {
			return fmt.Errorf("文件 %d: 名称冲突检查失败: %w", id, err)
		}
		if exists {
			return fmt.Errorf("文件 %d: 名称冲突", id)
		}
	}
	for _, id := range ids {
		if err := tx.UpdateFileParentID(id, targetParentID); err != nil {
			return fmt.Errorf("batch move update %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("batch move commit: %w", err)
	}
	return nil
}

type zipEntry struct {
	name string
	path string
}

func (svc *FileService) validateZipEntries(ids []int64, userID int64) ([]zipEntry, error) {
	entries := make([]zipEntry, 0, len(ids))
	for _, id := range ids {
		f, err := svc.store.FileByID(id)
		if err != nil {
			return nil, fmt.Errorf("下载压缩包查找 %d: %w", id, err)
		}
		if f.UserID != userID || f.IsDir || f.IsDeleted || f.StorageKey == "" {
			return nil, fmt.Errorf("文件 %d 不存在", id)
		}
		diskPath := svc.fileStore.Path(f.StorageKey)
		if _, err := os.Stat(diskPath); err != nil {
			return nil, fmt.Errorf("文件 %d 在磁盘上不存在", id)
		}
		entries = append(entries, zipEntry{name: f.Name, path: diskPath})
	}
	return entries, nil
}

func (svc *FileService) ValidateZipDownload(ids []int64, userID int64) error {
	_, err := svc.validateZipEntries(ids, userID)
	return err
}

func sanitizeZipName(name string) string {
	cleaned := filepath.Clean(name)
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = strings.ReplaceAll(cleaned, "\\", "_")
	cleaned = strings.ReplaceAll(cleaned, "/", "_")
	if cleaned == "" || cleaned == "." {
		return "unnamed"
	}
	return cleaned
}
func (svc *FileService) DownloadZip(ids []int64, userID int64, w io.Writer) (err error) {
	if len(ids) == 0 {
		return fmt.Errorf("下载压缩包：未选择文件")
	}
	entries, err := svc.validateZipEntries(ids, userID)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	defer func() {
		if cerr := zw.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	for _, e := range entries {
		fi, err := os.Stat(e.path)
		if err != nil {
			return fmt.Errorf("download zip stat %s: %w", e.name, err)
		}
		header, err := zip.FileInfoHeader(fi)
		if err != nil {
			return fmt.Errorf("download zip header %s: %w", e.name, err)
		}
		header.Name = sanitizeZipName(e.name)
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("download zip create entry %s: %w", e.name, err)
		}
		src, err := os.Open(e.path)
		if err != nil {
			return fmt.Errorf("download zip open %s: %w", e.name, err)
		}
		if _, err := io.Copy(writer, src); err != nil {
			src.Close()
			return fmt.Errorf("download zip copy %s: %w", e.name, err)
		}
		src.Close()
	}
	return nil
}

func (svc *FileService) StorageStats(userID int64) (active int64, recycle int64, used int64, err error) {
	active, err = svc.store.SumActiveStorage(userID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("storage stats active: %w", err)
	}
	recycle, err = svc.store.SumRecycleStorage(userID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("storage stats recycle: %w", err)
	}
	u, err := svc.store.UserByID(userID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("storage stats user: %w", err)
	}
	used = u.StorageUsed
	return active, recycle, used, nil
}

func (svc *FileService) CalibrateStorage(userID int64) error {
	actual, err := svc.store.SumActiveStorage(userID)
	if err != nil {
		return fmt.Errorf("calibrate: %w", err)
	}
	u, err := svc.store.UserByID(userID)
	if err != nil {
		return fmt.Errorf("calibrate lookup user: %w", err)
	}
	delta := actual - u.StorageUsed
	if delta != 0 {
		if err := svc.store.UpdateUserStorageUsed(userID, delta); err != nil {
			return fmt.Errorf("calibrate update: %w", err)
		}
	}
	return nil
}

func (svc *FileService) CalibrateAllStorage() (int64, error) {
	const pageSize = 100
	var fixed int64
	offset := 0
	for {
		ids, err := svc.store.AllUserIDsPaginated(pageSize, offset)
		if err != nil {
			return fixed, fmt.Errorf("calibrate all: %w", err)
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if err := svc.CalibrateStorage(id); err != nil {
				logging.Error(context.Background(), "file", "calibrate storage failed", "user_id", id, "error", err)
				continue
			}
			fixed++
		}
		offset += pageSize
	}
	return fixed, nil
}

func (svc *FileService) AutoCleanRecycle() (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -svc.getCfg().Storage.AutoCleanRecycleDays)
	candidates, err := svc.store.ExpiredRecycleCandidates(cutoff)
	if err != nil {
		return 0, fmt.Errorf("auto clean recycle: %w", err)
	}
	type cleanItem struct {
		id           int64
		userID       int64
		storageKey   string
		thumbnailKey string
		isDir        bool
		size         int64
	}
	processed := map[int64]bool{}
	var items []cleanItem
	for _, c := range candidates {
		if processed[c.ID] {
			continue
		}
		items = append(items, cleanItem{
			id: c.ID, userID: c.UserID, storageKey: c.StorageKey,
			thumbnailKey: c.ThumbnailKey, isDir: c.IsDir, size: c.Size,
		})
		processed[c.ID] = true
		if c.IsDir {
			descendants, err := svc.store.DescendantIDs(c.ID, true)
			if err != nil {
				logging.Error(context.Background(), "file", "auto clean collect descendants failed", "dir_id", c.ID, "error", err)
				continue
			}
			if len(descendants) > 0 {
				children, err := svc.store.FilesByIDs(descendants)
				if err != nil {
					logging.Error(context.Background(), "file", "auto clean descendant lookup failed", "dir_id", c.ID, "error", err)
					continue
				}
				for _, child := range children {
					if processed[child.ID] {
						continue
					}
					items = append(items, cleanItem{
						id: child.ID, userID: child.UserID, storageKey: child.StorageKey,
						thumbnailKey: child.ThumbnailKey, isDir: child.IsDir, size: child.Size,
					})
					processed[child.ID] = true
				}
			}
		}
	}
	type physDel struct {
		storageKey   string
		thumbnailKey string
	}
	allIDs := make([]int64, len(items))
	userDeltas := map[int64]int64{}
	var physDeletes []physDel
	for i, item := range items {
		allIDs[i] = item.id
		var sk, tk string
		if !item.isDir && item.storageKey != "" {
			count, err := svc.store.CountByStorageKey(item.storageKey)
			if err == nil && count <= 1 {
				sk = item.storageKey
			}
		}
		if item.thumbnailKey != "" {
			count, err := svc.store.CountByThumbnailKey(item.thumbnailKey)
			if err == nil && count <= 1 {
				tk = item.thumbnailKey
			}
		}
		if sk != "" || tk != "" {
			physDeletes = append(physDeletes, physDel{storageKey: sk, thumbnailKey: tk})
		}
		if !item.isDir && item.size > 0 {
			userDeltas[item.userID] -= item.size
		}
	}
	var cleaned int64
	if err := svc.store.HardDeleteFiles(allIDs); err != nil {
		logging.Error(context.Background(), "file", "auto clean recycle batch delete failed", "error", err)
		for _, id := range allIDs {
			if err := svc.store.HardDeleteFile(id); err != nil {
				logging.Error(context.Background(), "file", "auto clean recycle single delete failed", "file_id", id, "error", err)
			} else {
				cleaned++
			}
		}
	} else {
		cleaned = int64(len(allIDs))
	}
	for _, pd := range physDeletes {
		if pd.storageKey != "" {
			svc.fileStore.Delete(pd.storageKey)
		}
		if pd.thumbnailKey != "" {
			svc.fileStore.Delete(pd.thumbnailKey)
		}
	}
	for userID, delta := range userDeltas {
		if err := svc.store.UpdateUserStorageUsed(userID, delta); err != nil {
			logging.Error(context.Background(), "file", "auto clean recycle storage update failed", "user_id", userID, "error", err)
		}
	}
	return cleaned, nil
}

type uploadSession struct {
	UploadID   string `json:"upload_id"`
	UserID     int64  `json:"user_id"`
	ParentID   *int64 `json:"parent_id"`
	Name       string `json:"name"`
	TotalSize  int64  `json:"total_size"`
	ChunkSize  int64  `json:"chunk_size"`
	ChunkCount int    `json:"chunk_count"`
	FileHash   string `json:"file_hash"`
	MimeType   string `json:"mime_type"`
	CreatedAt  int64  `json:"created_at"`
}

type uploadInitResult struct {
	UploadID  string `json:"upload_id"`
	ChunkSize int64  `json:"chunk_size"`
}

func (svc *FileService) InitUpload(userID int64, parentID *int64, name string, totalSize int64) (*uploadInitResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("缺少文件名")
	}
	if totalSize <= 0 {
		return nil, fmt.Errorf("文件大小必须大于0")
	}
	if totalSize > svc.getCfg().Storage.MaxFileSize {
		return nil, ErrFileTooLarge
	}
	if parentID != nil {
		if err := svc.validateParentDir(*parentID, userID); err != nil {
			return nil, err
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	if err := svc.validateExtension(ext); err != nil {
		return nil, err
	}
	chunkSize := svc.getCfg().Storage.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 10 << 20
	}
	chunkCount := int((totalSize + chunkSize - 1) / chunkSize)
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate upload id: %w", err)
	}
	uploadID := hex.EncodeToString(b)
	session := uploadSession{
		UploadID:   uploadID,
		UserID:     userID,
		ParentID:   parentID,
		Name:       name,
		TotalSize:  totalSize,
		ChunkSize:  chunkSize,
		ChunkCount: chunkCount,
		CreatedAt:  time.Now().Unix(),
	}
	if err := svc.saveUploadSession(uploadID, &session); err != nil {
		return nil, fmt.Errorf("save upload session: %w", err)
	}
	return &uploadInitResult{UploadID: uploadID, ChunkSize: chunkSize}, nil
}

func (svc *FileService) UploadChunk(uploadID string, index int, r io.Reader, userID int64) error {
	session, err := svc.loadUploadSession(uploadID)
	if err != nil {
		return fmt.Errorf("upload chunk: %w", err)
	}
	if session.UserID != userID {
		return fmt.Errorf("上传分片：会话不属于当前用户")
	}
	if index < 0 || index >= session.ChunkCount {
		return fmt.Errorf("分片索引 %d 超出范围 [0, %d)", index, session.ChunkCount)
	}
	chunkDir := svc.chunkDir(uploadID)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("create chunk dir: %w", err)
	}
	chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%06d", index))
	f, err := os.Create(chunkPath)
	if err != nil {
		return fmt.Errorf("create chunk file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(r, session.ChunkSize)); err != nil {
		os.Remove(chunkPath)
		return fmt.Errorf("write chunk: %w", err)
	}
	return nil
}

func (svc *FileService) CompleteUpload(uploadID string, userID int64) (*UploadResult, error) {
	session, err := svc.loadUploadSession(uploadID)
	if err != nil {
		return nil, fmt.Errorf("complete upload: %w", err)
	}
	if session.UserID != userID {
		return nil, fmt.Errorf("完成上传：会话不属于当前用户")
	}
	chunkDir := svc.chunkDir(uploadID)
	for i := 0; i < session.ChunkCount; i++ {
		chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%06d", i))
		if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("分片 %d 缺失", i)
		}
	}
	tmpFile, tmpPath, err := svc.createTempFile()
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpPath)
	hasher := sha256.New()
	for i := 0; i < session.ChunkCount; i++ {
		chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%06d", i))
		cf, err := os.Open(chunkPath)
		if err != nil {
			tmpFile.Close()
			return nil, fmt.Errorf("open chunk %d: %w", i, err)
		}
		mw := io.MultiWriter(tmpFile, hasher)
		if _, err := io.Copy(mw, cf); err != nil {
			cf.Close()
			tmpFile.Close()
			return nil, fmt.Errorf("merge chunk %d: %w", i, err)
		}
		cf.Close()
	}
	tmpFile.Close()
	hash := hex.EncodeToString(hasher.Sum(nil))
	if session.FileHash != "" && hash != session.FileHash {
		svc.cleanupUploadSession(uploadID)
		return nil, fmt.Errorf("文件哈希不匹配")
	}
	svc.lockHash(hash)
	existingFile, err := svc.store.FileByHash(hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check hash: %w", err)
	}
	if existingFile != nil {
		svc.unlockHash(hash)
		svc.cleanupUploadSession(uploadID)
		ext := strings.ToLower(filepath.Ext(session.Name))
		name, _, err := svc.resolveNameConflict(session.Name, ext, session.ParentID, session.UserID)
		if err != nil {
			return nil, fmt.Errorf("resolve name conflict: %w", err)
		}
		tx, err := svc.store.BeginTx()
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback()
		file, err := tx.CreateFile(&model.File{
			UserID: session.UserID, ParentID: session.ParentID, Name: name, Size: session.TotalSize,
			FileHash: hash, StorageKey: existingFile.StorageKey, MimeType: session.MimeType,
		})
		if err != nil {
			return nil, err
		}
		if err := tx.UpdateUserStorageUsed(session.UserID, session.TotalSize); err != nil {
			return nil, fmt.Errorf("update storage used: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
		return &UploadResult{File: file, Duplicate: true}, nil
	}
	svc.unlockHash(hash)
	ext := strings.ToLower(filepath.Ext(session.Name))
	storageKey, err := generateStorageKey(ext)
	if err != nil {
		return nil, fmt.Errorf("generate storage key: %w", err)
	}
	finalPath := svc.fileStore.Path(storageKey)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return nil, fmt.Errorf("mkdir for storage: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return nil, fmt.Errorf("move to storage: %w", err)
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		os.Remove(finalPath)
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	file, err := tx.CreateFile(&model.File{
		UserID: session.UserID, ParentID: session.ParentID, Name: session.Name, Size: session.TotalSize,
		FileHash: hash, StorageKey: storageKey, MimeType: session.MimeType,
	})
	if err != nil {
		os.Remove(finalPath)
		return nil, err
	}
	if err := tx.UpdateUserStorageUsed(session.UserID, session.TotalSize); err != nil {
		return nil, fmt.Errorf("update storage used: %w", err)
	}
	if err := tx.Commit(); err != nil {
		os.Remove(finalPath)
		return nil, fmt.Errorf("commit: %w", err)
	}
	svc.thumbnail.Generate(file.ID, storageKey, session.MimeType, func(fileID int64, thumbnailKey string) {
		svc.store.UpdateFileThumbnailKey(fileID, thumbnailKey)
	})
	svc.cleanupUploadSession(uploadID)
	return &UploadResult{File: file, Duplicate: false}, nil
}

func (svc *FileService) UploadStatus(uploadID string, userID int64) (*uploadSession, []int, error) {
	session, err := svc.loadUploadSession(uploadID)
	if err != nil {
		return nil, nil, err
	}
	if session.UserID != userID {
		return nil, nil, fmt.Errorf("上传会话不属于当前用户")
	}
	chunkDir := svc.chunkDir(uploadID)
	var completed []int
	for i := 0; i < session.ChunkCount; i++ {
		chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%06d", i))
		if _, err := os.Stat(chunkPath); err == nil {
			completed = append(completed, i)
		}
	}
	return session, completed, nil
}

func (svc *FileService) CleanStaleChunks() (int64, error) {
	timeout := svc.getCfg().ChunkCleanTimeoutDuration()
	cutoff := time.Now().Add(-timeout)
	chunksDir := svc.fileStore.Path(filepath.Join("tmp", "chunks"))
	entries, err := os.ReadDir(chunksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read chunks dir: %w", err)
	}
	var cleaned int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessionPath := filepath.Join(chunksDir, e.Name(), "session.json")
		info, err := os.Stat(sessionPath)
		if err != nil || info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(chunksDir, e.Name())); err != nil {
				logging.Error(context.Background(), "file", "clean stale chunks failed", "upload_id", e.Name(), "error", err)
			} else {
				cleaned++
			}
		}
	}
	return cleaned, nil
}

func (svc *FileService) saveUploadSession(uploadID string, session *uploadSession) error {
	chunkDir := svc.chunkDir(uploadID)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(chunkDir, "session.json"), data, 0644)
}

func (svc *FileService) loadUploadSession(uploadID string) (*uploadSession, error) {
	sessionPath := filepath.Join(svc.chunkDir(uploadID), "session.json")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("上传会话不存在: %w", err)
	}
	var session uploadSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("上传会话数据无效: %w", err)
	}
	if session.UploadID != uploadID {
		return nil, fmt.Errorf("上传会话ID不匹配")
	}
	return &session, nil
}

func (svc *FileService) chunkDir(uploadID string) string {
	return svc.fileStore.Path(filepath.Join("tmp", "chunks", uploadID))
}

func (svc *FileService) cleanupUploadSession(uploadID string) {
	os.RemoveAll(svc.chunkDir(uploadID))
}

func (svc *FileService) RecordAudit(userID int64, action, targetType string, targetID int64, detail, ip string) {
	log := &model.AuditLog{
		UserID:     userID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detail,
		IP:         ip,
	}
	if err := svc.store.CreateAuditLog(log); err != nil {
		logging.Error(context.Background(), "file", "record audit failed", "error", err)
	}
}

func (svc *FileService) AuditLogs(userID int64, action string, limit, offset int) ([]*model.AuditLog, error) {
	return svc.store.AuditLogsByUser(userID, action, limit, offset)
}

func (svc *FileService) AuditLogsWithRange(userID int64, action, startTime, endTime string, limit, offset int) ([]*model.AuditLog, error) {
	return svc.store.AuditLogsByUserWithRange(userID, action, startTime, endTime, limit, offset)
}

func (svc *FileService) CleanExpiredAuditLogs() (int64, error) {
	return svc.store.DeleteExpiredAuditLogs(svc.getCfg().Log.AuditRetentionDays)
}

func (svc *FileService) HealthCheck() error {
	if err := svc.store.DB.Ping(); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	testPath := "_health_check_test"
	if _, err := svc.fileStore.Save(testPath, strings.NewReader("health")); err != nil {
		return fmt.Errorf("storage writable check: %w", err)
	}
	if err := svc.fileStore.Delete(testPath); err != nil {
		logging.Warn(context.Background(), "file", "failed to clean up health check file", "error", err)
	}
	return nil
}

func (svc *FileService) DeviceFamilies(userID int64) ([]*model.RefreshToken, error) {
	return svc.store.DistinctDeviceFamilies(userID)
}

func (svc *FileService) RevokeDevice(userID int64, familyID string) error {
	return svc.store.RevokeDeviceFamily(userID, familyID)
}

func (svc *FileService) PreviewType(mimeType string) string {
	if strings.HasPrefix(mimeType, "image/") {
		return "image"
	}
	if strings.HasPrefix(mimeType, "video/") {
		return "video"
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return "audio"
	}
	if mimeType == "application/pdf" {
		return "pdf"
	}
	return "download"
}

func (svc *FileService) PreviewContent(fileID int64, userID int64) (*model.File, string, string, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", "", ErrFileNotFound
		}
		return nil, "", "", fmt.Errorf("preview lookup: %w", err)
	}
	if f.UserID != userID || f.IsDeleted || f.IsDir {
		return nil, "", "", ErrFileNotFound
	}

	mimeType := f.MimeType
	if mimeType == "" {
		mimeType = "text/plain"
	}

	previewType := svc.PreviewType(mimeType)
	if previewType == "image" || previewType == "video" || previewType == "audio" || previewType == "pdf" {
		return f, previewType, "", nil
	}

	diskPath := svc.fileStore.Path(f.StorageKey)
	file, err := os.Open(diskPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("preview open: %w", err)
	}
	defer file.Close()
	const maxPreviewSize = 2 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxPreviewSize))
	if err != nil {
		return nil, "", "", fmt.Errorf("preview read: %w", err)
	}

	previewType = "text"
	content := string(data)

	if strings.HasPrefix(mimeType, "text/markdown") || strings.HasPrefix(mimeType, "text/x-markdown") {
		previewType = "markdown"
		var buf bytes.Buffer
		if err := goldmark.Convert(data, &buf); err != nil {
			return nil, "", "", fmt.Errorf("markdown render: %w", err)
		}
		content = buf.String()
	}

	return f, previewType, content, nil
}

func (svc *FileService) OpenPreviewFile(fileID int64, userID int64) (*model.File, io.ReadCloser, error) {
	return svc.OpenFile(fileID, userID)
}

func (svc *FileService) OpenFileReader(f *model.File) (io.ReadCloser, error) {
	if f.IsDir || f.StorageKey == "" {
		return nil, fmt.Errorf("cannot open directory or file without storage key")
	}
	diskPath := svc.fileStore.Path(f.StorageKey)
	reader, err := os.Open(diskPath)
	if err != nil {
		return nil, fmt.Errorf("open file reader: %w", err)
	}
	return reader, nil
}

func (svc *FileService) ShareFileContent(f *model.File) (string, string) {
	const maxPreviewSize = 2 << 20
	diskPath := svc.fileStore.Path(f.StorageKey)
	data, err := os.ReadFile(diskPath)
	if err != nil || len(data) == 0 {
		return "", "text"
	}
	if len(data) > maxPreviewSize {
		data = data[:maxPreviewSize]
	}
	previewType := "text"
	content := string(data)
	if strings.HasPrefix(f.MimeType, "text/markdown") {
		previewType = "markdown"
		var buf bytes.Buffer
		if err := goldmark.Convert(data, &buf); err == nil {
			content = buf.String()
		}
	}
	return content, previewType
}

func (svc *FileService) FileNameByID(fileID int64) string {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		return ""
	}
	return f.Name
}

func (svc *FileService) CreateTempLink(fileID int64, userID int64) (*model.TempDownload, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("create temp link lookup: %w", err)
	}
	if f.UserID != userID || f.IsDeleted || f.IsDir {
		return nil, ErrFileNotFound
	}
	token, err := generateShareToken()
	if err != nil {
		return nil, fmt.Errorf("generate temp token: %w", err)
	}
	td := &model.TempDownload{
		Token:    token,
		FileID:   fileID,
		UserID:   userID,
		ExpireAt: time.Now().Add(svc.getCfg().TempLinkTTLDuration()),
	}
	return svc.store.CreateTempDownload(td)
}

func (svc *FileService) RecentFiles(userID int64, limit int) ([]*model.File, error) {
	return svc.store.RecentFiles(userID, limit)
}

func generateStorageKey(ext string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}
	key := hex.EncodeToString(b)
	if ext != "" {
		key += ext
	}
	return key, nil
}
