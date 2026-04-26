package store

import (
	"database/sql"
	"time"

	"networkdisk/internal/model"
)

func (s *Store) CreateFile(f *model.File) (*model.File, error) {
	result, err := s.DB.Exec(
		"INSERT INTO files (user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		f.UserID, f.ParentID, f.Name, f.IsDir, f.Size, f.FileHash, f.StorageKey, f.ThumbnailKey, f.MimeType,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.FileByID(id)
}

func (s *Store) FileByID(id int64) (*model.File, error) {
	f := &model.File{}
	var deletedAt sql.NullTime
	err := s.DB.QueryRow(
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE id = ?",
		id,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (s *Store) FilesByParentID(parentID *int64, userID int64) ([]*model.File, error) {
	var rows *sql.Rows
	var err error
	if parentID == nil {
		rows, err = s.DB.Query(
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id IS NULL AND is_deleted = 0 ORDER BY is_dir DESC, name",
			userID,
		)
	} else {
		rows, err = s.DB.Query(
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id = ? AND is_deleted = 0 ORDER BY is_dir DESC, name",
			userID, *parentID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []*model.File
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *Store) FileByHash(hash string) (*model.File, error) {
	if hash == "" {
		return nil, sql.ErrNoRows
	}
	f := &model.File{}
	var deletedAt sql.NullTime
	err := s.DB.QueryRow(
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_deleted, deleted_at, created_at, updated_at FROM files WHERE file_hash = ? AND is_dir = 0 AND is_deleted = 0 LIMIT 1",
		hash,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (s *Store) UpdateFileName(id int64, name string) error {
	_, err := s.DB.Exec("UPDATE files SET name = ? WHERE id = ?", name, id)
	return err
}

func (s *Store) SoftDeleteFile(id int64) error {
	now := time.Now()
	_, err := s.DB.Exec("UPDATE files SET is_deleted = 1, deleted_at = ? WHERE id = ?", now, id)
	return err
}

func (s *Store) UpdateUserStorageUsed(userID int64, delta int64) error {
	_, err := s.DB.Exec("UPDATE users SET storage_used = storage_used + ? WHERE id = ?", delta, userID)
	return err
}

func (s *Store) FileNameExists(parentID *int64, userID int64, name string) (bool, error) {
	var exists bool
	var err error
	if parentID == nil {
		err = s.DB.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM files WHERE user_id = ? AND parent_id IS NULL AND name = ? AND is_deleted = 0)",
			userID, name,
		).Scan(&exists)
	} else {
		err = s.DB.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM files WHERE user_id = ? AND parent_id = ? AND name = ? AND is_deleted = 0)",
			userID, *parentID, name,
		).Scan(&exists)
	}
	return exists, err
}

func (s *Store) UpdateFileStorageKey(id int64, storageKey string) error {
	_, err := s.DB.Exec("UPDATE files SET storage_key = ? WHERE id = ?", storageKey, id)
	return err
}

func (s *Store) UpdateFileThumbnailKey(id int64, thumbnailKey string) error {
	_, err := s.DB.Exec("UPDATE files SET thumbnail_key = ? WHERE id = ?", thumbnailKey, id)
	return err
}
