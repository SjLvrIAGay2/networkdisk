package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"networkdisk/internal/model"
)

func (s *Store) CreateFile(f *model.File) (*model.File, error) {
	id, err := mustBePositive(f.UserID)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	if _, err := mustBeValidName(f.Name); err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	result, err := s.DB.ExecContext(context.Background(),
		"INSERT INTO files (user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, f.ParentID, f.Name, f.IsDir, f.Size, f.FileHash, f.StorageKey, f.ThumbnailKey, f.MimeType,
	)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create file get id: %w", err)
	}
	return s.FileByID(newID)
}

func (s *Store) FileByID(id int64) (*model.File, error) {
	id, err := mustBePositive(id)
	if err != nil {
		return nil, fmt.Errorf("file by id: %w", err)
	}
	f := &model.File{}
	var deletedAt sql.NullTime
	err = s.DB.QueryRowContext(context.Background(),
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE id = ?",
		id,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("file by id: %w", err)
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (s *Store) FilesByParentID(parentID *int64, userID int64) ([]*model.File, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return nil, fmt.Errorf("files by parent: %w", err)
	}
	var rows *sql.Rows
	if parentID == nil {
		rows, err = s.DB.QueryContext(context.Background(),
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id IS NULL AND is_deleted = 0 ORDER BY is_dir DESC, name",
			userID,
		)
	} else {
		rows, err = s.DB.QueryContext(context.Background(),
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id = ? AND is_deleted = 0 ORDER BY is_dir DESC, name",
			userID, *parentID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("files by parent: %w", err)
	}
	defer rows.Close()
	var files []*model.File
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("files by parent scan: %w", err)
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("files by parent iterate: %w", err)
	}
	return files, nil
}

func (s *Store) FileByHash(hash string) (*model.File, error) {
	if hash == "" {
		return nil, fmt.Errorf("file by hash: hash must not be empty")
	}
	f := &model.File{}
	var deletedAt sql.NullTime
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE file_hash = ? AND is_dir = 0 AND is_deleted = 0 LIMIT 1",
		hash,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("file by hash: %w", err)
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (s *Store) FileByName(parentID *int64, userID int64, name string) (*model.File, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return nil, fmt.Errorf("file by name: %w", err)
	}
	name, err = mustBeValidName(name)
	if err != nil {
		return nil, fmt.Errorf("file by name: %w", err)
	}
	f := &model.File{}
	var deletedAt sql.NullTime
	if parentID == nil {
		err = s.DB.QueryRowContext(context.Background(),
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id IS NULL AND name = ? AND is_deleted = 0 LIMIT 1",
			userID, name,
		).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	} else {
		err = s.DB.QueryRowContext(context.Background(),
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id = ? AND name = ? AND is_deleted = 0 LIMIT 1",
			userID, *parentID, name,
		).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	}
	if err != nil {
		return nil, fmt.Errorf("file by name: %w", err)
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (s *Store) UpdateFileName(id int64, name string) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("update file name: %w", err)
	}
	name, err = mustBeValidName(name)
	if err != nil {
		return fmt.Errorf("update file name: %w", err)
	}
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("update file name: %w", err)
	}
	return nil
}

func (s *Store) SoftDeleteFile(id int64) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("soft delete file: %w", err)
	}
	now := time.Now()
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET is_deleted = 1, deleted_at = ? WHERE id = ?", now, id)
	if err != nil {
		return fmt.Errorf("soft delete file: %w", err)
	}
	return nil
}

func (s *Store) FileNameExists(parentID *int64, userID int64, name string) (bool, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return false, fmt.Errorf("file name exists: %w", err)
	}
	name, err = mustBeValidName(name)
	if err != nil {
		return false, fmt.Errorf("file name exists: %w", err)
	}
	var exists bool
	if parentID == nil {
		err = s.DB.QueryRowContext(context.Background(),
			"SELECT EXISTS(SELECT 1 FROM files WHERE user_id = ? AND parent_id IS NULL AND name = ? AND is_deleted = 0)",
			userID, name,
		).Scan(&exists)
	} else {
		err = s.DB.QueryRowContext(context.Background(),
			"SELECT EXISTS(SELECT 1 FROM files WHERE user_id = ? AND parent_id = ? AND name = ? AND is_deleted = 0)",
			userID, *parentID, name,
		).Scan(&exists)
	}
	if err != nil {
		return false, fmt.Errorf("file name exists: %w", err)
	}
	return exists, nil
}

func (s *Store) UpdateFileStorageKey(id int64, storageKey string) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("update file storage key: %w", err)
	}
	if storageKey == "" {
		return fmt.Errorf("update file storage key: storage key must not be empty")
	}
	if len(storageKey) > 512 {
		return fmt.Errorf("update file storage key: storage key exceeds 512 characters")
	}
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET storage_key = ? WHERE id = ?", storageKey, id)
	if err != nil {
		return fmt.Errorf("update file storage key: %w", err)
	}
	return nil
}

func (s *Store) UpdateFileThumbnailKey(id int64, thumbnailKey string) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("update file thumbnail key: %w", err)
	}
	if thumbnailKey == "" {
		return fmt.Errorf("update file thumbnail key: thumbnail key must not be empty")
	}
	if len(thumbnailKey) > 512 {
		return fmt.Errorf("update file thumbnail key: thumbnail key exceeds 512 characters")
	}
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET thumbnail_key = ? WHERE id = ?", thumbnailKey, id)
	if err != nil {
		return fmt.Errorf("update file thumbnail key: %w", err)
	}
	return nil
}

func mustBePositive(id int64) (int64, error) {
	if id <= 0 {
		return 0, fmt.Errorf("id must be positive, got %d", id)
	}
	return id, nil
}

func mustBeValidName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name must not be empty")
	}
	if len(name) > 255 {
		return "", fmt.Errorf("name exceeds 255 characters")
	}
	return name, nil
}
