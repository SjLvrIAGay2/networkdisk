package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"networkdisk/internal/model"
)

func (s *Store) CreateFile(f *model.File) (*model.File, error) {
	id, err := mustBePositive(f.UserID)
	if err != nil {
		return nil, fmt.Errorf("创建文件：%w", err)
	}
	if _, err := mustBeValidName(f.Name); err != nil {
		return nil, fmt.Errorf("创建文件：%w", err)
	}
	result, err := s.DB.ExecContext(context.Background(),
		"INSERT INTO files (user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, f.ParentID, f.Name, f.IsDir, f.Size, f.FileHash, f.StorageKey, f.ThumbnailKey, f.MimeType, f.IsStarred,
	)
	if err != nil {
		return nil, fmt.Errorf("创建文件：%w", err)
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("创建文件获取ID：%w", err)
	}
	return s.FileByID(newID)
}

func (s *Store) FileByID(id int64) (*model.File, error) {
	id, err := mustBePositive(id)
	if err != nil {
		return nil, fmt.Errorf("通过ID查找文件：%w", err)
	}
	f := &model.File{}
	var deletedAt sql.NullTime
	err = s.DB.QueryRowContext(context.Background(),
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE id = ?",
		id,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("通过ID查找文件：%w", err)
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (s *Store) FilesByParentID(parentID *int64, userID int64) ([]*model.File, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return nil, fmt.Errorf("通过父目录查找文件：%w", err)
	}
	var rows *sql.Rows
	if parentID == nil {
		rows, err = s.DB.QueryContext(context.Background(),
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id IS NULL AND is_deleted = 0 ORDER BY is_dir DESC, name",
			userID,
		)
	} else {
		rows, err = s.DB.QueryContext(context.Background(),
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id = ? AND is_deleted = 0 ORDER BY is_dir DESC, name",
			userID, *parentID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("通过父目录查找文件：%w", err)
	}
	defer rows.Close()
	var files []*model.File
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
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

func (s *Store) CountByStorageKey(storageKey string) (int, error) {
	if storageKey == "" {
		return 0, nil
	}
	var count int
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT COUNT(1) FROM files WHERE storage_key = ?", storageKey,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("按存储key计数：%w", err)
	}
	return count, nil
}

func (s *Store) CountByThumbnailKey(thumbnailKey string) (int, error) {
	if thumbnailKey == "" {
		return 0, nil
	}
	var count int
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT COUNT(1) FROM files WHERE thumbnail_key = ?", thumbnailKey,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("按缩略图key计数：%w", err)
	}
	return count, nil
}

func (tx *Tx) CountByStorageKey(storageKey string) (int, error) {
	if storageKey == "" {
		return 0, nil
	}
	var count int
	err := tx.Tx.QueryRowContext(context.Background(),
		"SELECT COUNT(1) FROM files WHERE storage_key = ? FOR UPDATE", storageKey,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("按存储key计数（事务）：%w", err)
	}
	return count, nil
}

func (tx *Tx) CountByThumbnailKey(thumbnailKey string) (int, error) {
	if thumbnailKey == "" {
		return 0, nil
	}
	var count int
	err := tx.Tx.QueryRowContext(context.Background(),
		"SELECT COUNT(1) FROM files WHERE thumbnail_key = ? FOR UPDATE", thumbnailKey,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("按缩略图key计数（事务）：%w", err)
	}
	return count, nil
}

func (s *Store) FileByHash(hash string) (*model.File, error) {
	if hash == "" {
		return nil, fmt.Errorf("通过哈希查找文件：哈希不能为空")
	}
	f := &model.File{}
	var deletedAt sql.NullTime
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE file_hash = ? AND is_dir = 0 AND is_deleted = 0 LIMIT 1",
		hash,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("通过哈希查找文件：%w", err)
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (s *Store) FileByName(parentID *int64, userID int64, name string) (*model.File, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return nil, fmt.Errorf("通过名称查找文件：%w", err)
	}
	name, err = mustBeValidName(name)
	if err != nil {
		return nil, fmt.Errorf("通过名称查找文件：%w", err)
	}
	f := &model.File{}
	var deletedAt sql.NullTime
	if parentID == nil {
		err = s.DB.QueryRowContext(context.Background(),
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id IS NULL AND name = ? AND is_deleted = 0 LIMIT 1",
			userID, name,
		).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	} else {
		err = s.DB.QueryRowContext(context.Background(),
			"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND parent_id = ? AND name = ? AND is_deleted = 0 LIMIT 1",
			userID, *parentID, name,
		).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	}
	if err != nil {
		return nil, fmt.Errorf("通过名称查找文件：%w", err)
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (s *Store) UpdateFileName(id int64, name string) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("更新文件名：%w", err)
	}
	name, err = mustBeValidName(name)
	if err != nil {
		return fmt.Errorf("更新文件名：%w", err)
	}
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("更新文件名：%w", err)
	}
	return nil
}

func (s *Store) SoftDeleteFile(id int64) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("软删除文件：%w", err)
	}
	now := time.Now()
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET is_deleted = 1, deleted_at = ? WHERE id = ?", now, id)
	if err != nil {
		return fmt.Errorf("软删除文件：%w", err)
	}
	return nil
}

func (s *Store) FileNameExists(parentID *int64, userID int64, name string) (bool, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return false, fmt.Errorf("检查文件名是否存在：%w", err)
	}
	name, err = mustBeValidName(name)
	if err != nil {
		return false, fmt.Errorf("检查文件名是否存在：%w", err)
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
		return false, fmt.Errorf("检查文件名是否存在：%w", err)
	}
	return exists, nil
}

func (s *Store) UpdateFileStorageKey(id int64, storageKey string) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("更新文件存储key：%w", err)
	}
	if storageKey == "" {
		return fmt.Errorf("更新文件存储key：存储key不能为空")
	}
	if len(storageKey) > 64 {
		return fmt.Errorf("更新文件存储key：存储key超过64个字符限制")
	}
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET storage_key = ? WHERE id = ?", storageKey, id)
	if err != nil {
		return fmt.Errorf("更新文件存储key：%w", err)
	}
	return nil
}

func (s *Store) UpdateFileThumbnailKey(id int64, thumbnailKey string) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("更新文件缩略图key：%w", err)
	}
	if thumbnailKey == "" {
		return fmt.Errorf("更新文件缩略图key：缩略图key不能为空")
	}
	if len(thumbnailKey) > 64 {
		return fmt.Errorf("更新文件缩略图key：缩略图key超过64个字符限制")
	}
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET thumbnail_key = ? WHERE id = ?", thumbnailKey, id)
	if err != nil {
		return fmt.Errorf("更新文件缩略图key：%w", err)
	}
	return nil
}

func (s *Store) FilesInRecycleBin(userID int64) ([]*model.File, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return nil, fmt.Errorf("回收站：%w", err)
	}
	rows, err := s.DB.QueryContext(context.Background(),
		"SELECT f.id, f.user_id, f.parent_id, f.name, f.is_dir, f.size, f.file_hash, f.storage_key, f.thumbnail_key, f.mime_type, f.is_starred, f.is_deleted, f.deleted_at, f.created_at, f.updated_at FROM files f LEFT JOIN files p ON f.parent_id = p.id AND p.is_deleted = 1 WHERE f.user_id = ? AND f.is_deleted = 1 AND p.id IS NULL ORDER BY f.deleted_at DESC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("回收站：%w", err)
	}
	defer rows.Close()
	var files []*model.File
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("recycle bin scan: %w", err)
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recycle bin iterate: %w", err)
	}
	return files, nil
}

func (s *Store) RestoreFile(id int64) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("恢复文件：%w", err)
	}
	result, err := s.DB.ExecContext(context.Background(), "UPDATE files SET is_deleted = 0, deleted_at = NULL WHERE id = ? AND is_deleted = 1", id)
	if err != nil {
		return fmt.Errorf("恢复文件：%w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("restore file rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("恢复文件：文件不存在或未被删除")
	}
	return nil
}

func (s *Store) HardDeleteFile(id int64) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("永久删除文件：%w", err)
	}
	_, err = s.DB.ExecContext(context.Background(), "DELETE FROM files WHERE id = ? AND is_deleted = 1", id)
	if err != nil {
		return fmt.Errorf("永久删除文件：%w", err)
	}
	return nil
}

func (s *Store) UpdateFileParentID(id int64, parentID *int64) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("更新文件父目录：%w", err)
	}
	_, err = s.DB.ExecContext(context.Background(), "UPDATE files SET parent_id = ? WHERE id = ?", parentID, id)
	if err != nil {
		return fmt.Errorf("更新文件父目录：%w", err)
	}
	return nil
}

func (s *Store) ToggleStar(id int64) (bool, error) {
	id, err := mustBePositive(id)
	if err != nil {
		return false, fmt.Errorf("切换收藏：%w", err)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return false, fmt.Errorf("切换收藏：%w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(context.Background(), "UPDATE files SET is_starred = NOT is_starred WHERE id = ?", id)
	if err != nil {
		return false, fmt.Errorf("切换收藏更新：%w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("切换收藏获取行数：%w", err)
	}
	if affected == 0 {
		return false, fmt.Errorf("切换收藏：文件不存在")
	}
	var starred bool
	err = tx.QueryRowContext(context.Background(), "SELECT is_starred FROM files WHERE id = ?", id).Scan(&starred)
	if err != nil {
		return false, fmt.Errorf("切换收藏查询结果：%w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("切换收藏提交：%w", err)
	}
	return starred, nil
}

func (s *Store) StarredFiles(userID int64) ([]*model.File, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return nil, fmt.Errorf("已收藏文件：%w", err)
	}
	rows, err := s.DB.QueryContext(context.Background(),
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND is_starred = 1 AND is_deleted = 0 ORDER BY name",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("已收藏文件：%w", err)
	}
	defer rows.Close()
	var files []*model.File
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("starred files scan: %w", err)
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("starred files iterate: %w", err)
	}
	return files, nil
}

func (s *Store) BatchSoftDelete(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("批量软删除：ID列表不能为空")
	}
	now := time.Now()
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = now
	for i, id := range ids {
		if _, err := mustBePositive(id); err != nil {
			return fmt.Errorf("批量软删除：%w", err)
		}
		placeholders[i] = "?"
		args[i+1] = id
	}
	query := fmt.Sprintf("UPDATE files SET is_deleted = 1, deleted_at = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := s.DB.ExecContext(context.Background(), query, args...)
	if err != nil {
		return fmt.Errorf("批量软删除：%w", err)
	}
	return nil
}

func (s *Store) SumActiveStorage(userID int64) (int64, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return 0, fmt.Errorf("统计活跃存储：%w", err)
	}
	var total sql.NullInt64
	err = s.DB.QueryRowContext(context.Background(),
		"SELECT SUM(size) FROM files WHERE user_id = ? AND is_dir = 0 AND is_deleted = 0", userID,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("统计活跃存储：%w", err)
	}
	if total.Valid {
		return total.Int64, nil
	}
	return 0, nil
}

func (s *Store) SumRecycleStorage(userID int64) (int64, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return 0, fmt.Errorf("统计回收站存储：%w", err)
	}
	var total sql.NullInt64
	err = s.DB.QueryRowContext(context.Background(),
		"SELECT SUM(size) FROM files WHERE user_id = ? AND is_dir = 0 AND is_deleted = 1", userID,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("统计回收站存储：%w", err)
	}
	if total.Valid {
		return total.Int64, nil
	}
	return 0, nil
}

type RecycleCandidate struct {
	ID           int64
	UserID       int64
	IsDir        bool
	Size         int64
	StorageKey   string
	ThumbnailKey string
}

func (s *Store) ExpiredRecycleCandidates(cutoff time.Time) ([]RecycleCandidate, error) {
	rows, err := s.DB.QueryContext(context.Background(),
		"SELECT id, user_id, is_dir, size, storage_key, thumbnail_key FROM files WHERE is_deleted = 1 AND deleted_at < ?",
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("过期回收站候选项：%w", err)
	}
	defer rows.Close()
	var items []RecycleCandidate
	for rows.Next() {
		var c RecycleCandidate
		if err := rows.Scan(&c.ID, &c.UserID, &c.IsDir, &c.Size, &c.StorageKey, &c.ThumbnailKey); err != nil {
			return nil, fmt.Errorf("expired recycle scan: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expired recycle iterate: %w", err)
	}
	return items, nil
}

func (tx *Tx) FileByID(id int64) (*model.File, error) {
	id, err := mustBePositive(id)
	if err != nil {
		return nil, fmt.Errorf("通过ID查找文件（事务）：%w", err)
	}
	f := &model.File{}
	var deletedAt sql.NullTime
	err = tx.Tx.QueryRowContext(context.Background(),
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE id = ?",
		id,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("通过ID查找文件（事务）：%w", err)
	}
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}
	return f, nil
}

func (tx *Tx) BatchSoftDelete(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("批量软删除（事务）：ID列表不能为空")
	}
	now := time.Now()
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = now
	for i, id := range ids {
		if _, err := mustBePositive(id); err != nil {
			return fmt.Errorf("批量软删除（事务）：%w", err)
		}
		placeholders[i] = "?"
		args[i+1] = id
	}
	query := fmt.Sprintf("UPDATE files SET is_deleted = 1, deleted_at = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := tx.Tx.ExecContext(context.Background(), query, args...)
	if err != nil {
		return fmt.Errorf("批量软删除（事务）：%w", err)
	}
	return nil
}

func (tx *Tx) CreateFile(f *model.File) (*model.File, error) {
	id, err := mustBePositive(f.UserID)
	if err != nil {
		return nil, fmt.Errorf("创建文件（事务）：%w", err)
	}
	if _, err := mustBeValidName(f.Name); err != nil {
		return nil, fmt.Errorf("创建文件（事务）：%w", err)
	}
	result, err := tx.Tx.ExecContext(context.Background(),
		"INSERT INTO files (user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, f.ParentID, f.Name, f.IsDir, f.Size, f.FileHash, f.StorageKey, f.ThumbnailKey, f.MimeType, f.IsStarred,
	)
	if err != nil {
		return nil, fmt.Errorf("创建文件（事务）：%w", err)
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("创建文件获取ID（事务）：%w", err)
	}
	return tx.FileByID(newID)
}

func (tx *Tx) RestoreFile(id int64) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("恢复文件（事务）：%w", err)
	}
	result, err := tx.Tx.ExecContext(context.Background(), "UPDATE files SET is_deleted = 0, deleted_at = NULL WHERE id = ? AND is_deleted = 1", id)
	if err != nil {
		return fmt.Errorf("恢复文件（事务）：%w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("restore file tx rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("恢复文件（事务）：文件不存在或未被删除")
	}
	return nil
}

func (tx *Tx) UpdateFileParentID(id int64, parentID *int64) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("更新文件父目录（事务）：%w", err)
	}
	_, err = tx.Tx.ExecContext(context.Background(), "UPDATE files SET parent_id = ? WHERE id = ?", parentID, id)
	if err != nil {
		return fmt.Errorf("更新文件父目录（事务）：%w", err)
	}
	return nil
}

func (tx *Tx) UpdateFileName(id int64, name string) error {
	id, err := mustBePositive(id)
	if err != nil {
		return fmt.Errorf("更新文件名（事务）：%w", err)
	}
	name, err = mustBeValidName(name)
	if err != nil {
		return fmt.Errorf("更新文件名（事务）：%w", err)
	}
	_, err = tx.Tx.ExecContext(context.Background(), "UPDATE files SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("更新文件名（事务）：%w", err)
	}
	return nil
}

func (tx *Tx) FileNameExists(parentID *int64, userID int64, name string) (bool, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return false, fmt.Errorf("检查文件名是否存在（事务）：%w", err)
	}
	name, err = mustBeValidName(name)
	if err != nil {
		return false, fmt.Errorf("检查文件名是否存在（事务）：%w", err)
	}
	var exists bool
	if parentID == nil {
		err = tx.Tx.QueryRowContext(context.Background(),
			"SELECT EXISTS(SELECT 1 FROM files WHERE user_id = ? AND parent_id IS NULL AND name = ? AND is_deleted = 0)",
			userID, name,
		).Scan(&exists)
	} else {
		err = tx.Tx.QueryRowContext(context.Background(),
			"SELECT EXISTS(SELECT 1 FROM files WHERE user_id = ? AND parent_id = ? AND name = ? AND is_deleted = 0)",
			userID, *parentID, name,
		).Scan(&exists)
	}
	if err != nil {
		return false, fmt.Errorf("检查文件名是否存在（事务）：%w", err)
	}
	return exists, nil
}

func (s *Store) DescendantIDs(rootID int64, deleted bool) ([]int64, error) {
	id, err := mustBePositive(rootID)
	if err != nil {
		return nil, fmt.Errorf("查找后代：%w", err)
	}
	var deletedVal int
	if deleted {
		deletedVal = 1
	}
	rows, err := s.DB.QueryContext(context.Background(),
		"WITH RECURSIVE descendants AS ("+
			"SELECT id, is_deleted FROM files WHERE parent_id = ? "+
			"UNION ALL "+
			"SELECT f.id, f.is_deleted FROM files f JOIN descendants d ON f.parent_id = d.id"+
			") SELECT id FROM descendants WHERE is_deleted = ?",
		id, deletedVal,
	)
	if err != nil {
		return nil, fmt.Errorf("查找后代：%w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var childID int64
		if err := rows.Scan(&childID); err != nil {
			return nil, fmt.Errorf("descendant scan: %w", err)
		}
		ids = append(ids, childID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("descendant iterate: %w", err)
	}
	return ids, nil
}

func (s *Store) DescendantCounts(rootID int64) (int64, int64, error) {
	rootID, err := mustBePositive(rootID)
	if err != nil {
		return 0, 0, fmt.Errorf("descendant counts: %w", err)
	}
	var fileCount, dirCount int64
	err = s.DB.QueryRowContext(context.Background(),
		"WITH RECURSIVE descendants AS ("+
			"SELECT id, is_dir FROM files WHERE parent_id = ? AND is_deleted = 1 "+
			"UNION ALL "+
			"SELECT f.id, f.is_dir FROM files f JOIN descendants d ON f.parent_id = d.id WHERE f.is_deleted = 1"+
			") SELECT COALESCE(SUM(CASE WHEN is_dir = 0 THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN is_dir = 1 THEN 1 ELSE 0 END), 0) FROM descendants",
		rootID,
	).Scan(&fileCount, &dirCount)
	if err != nil {
		return 0, 0, fmt.Errorf("descendant counts: %w", err)
	}
	return fileCount, dirCount, nil
}

func (tx *Tx) RestoreFiles(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("批量恢复：ID列表不能为空")
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		if _, err := mustBePositive(id); err != nil {
			return fmt.Errorf("批量恢复：%w", err)
		}
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("UPDATE files SET is_deleted = 0, deleted_at = NULL WHERE id IN (%s) AND is_deleted = 1", strings.Join(placeholders, ","))
	_, err := tx.Tx.ExecContext(context.Background(), query, args...)
	if err != nil {
		return fmt.Errorf("批量恢复：%w", err)
	}
	return nil
}

func (s *Store) HardDeleteFiles(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("批量永久删除：ID列表不能为空")
	}
	placeholders, args := buildHardDeletePlaceholders(ids)
	query := fmt.Sprintf("DELETE FROM files WHERE id IN (%s)", strings.Join(placeholders, ","))
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("批量永久删除：%w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return fmt.Errorf("批量永久删除：%w", err)
	}
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("批量永久删除：%w", err)
	}
	if _, err := tx.Exec("SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		return fmt.Errorf("批量永久删除：%w", err)
	}
	return tx.Commit()
}

func (tx *Tx) HardDeleteFiles(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("批量永久删除：ID列表不能为空")
	}
	placeholders, args := buildHardDeletePlaceholders(ids)
	query := fmt.Sprintf("DELETE FROM files WHERE id IN (%s)", strings.Join(placeholders, ","))
	if _, err := tx.Tx.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return fmt.Errorf("批量永久删除：%w", err)
	}
	if _, err := tx.Tx.ExecContext(context.Background(), query, args...); err != nil {
		tx.Tx.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 1")
		return fmt.Errorf("批量永久删除：%w", err)
	}
	if _, err := tx.Tx.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		return fmt.Errorf("批量永久删除：%w", err)
	}
	return nil
}

func buildHardDeletePlaceholders(ids []int64) ([]string, []interface{}) {
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return placeholders, args
}

func (s *Store) FilesByIDs(ids []int64) ([]*model.File, error) {
	if len(ids) == 0 {
		return []*model.File{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		if _, err := mustBePositive(id); err != nil {
			return nil, fmt.Errorf("通过IDs批量查找文件：%w", err)
		}
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := s.DB.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("通过IDs批量查找文件：%w", err)
	}
	defer rows.Close()
	files := make([]*model.File, 0, len(ids))
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("files by IDs scan: %w", err)
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("files by IDs iterate: %w", err)
	}
	return files, nil
}

func (tx *Tx) FilesByIDs(ids []int64) ([]*model.File, error) {
	if len(ids) == 0 {
		return []*model.File{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		if _, err := mustBePositive(id); err != nil {
			return nil, fmt.Errorf("通过IDs批量查找文件（事务）：%w", err)
		}
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := tx.Tx.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("通过IDs批量查找文件（事务）：%w", err)
	}
	defer rows.Close()
	files := make([]*model.File, 0, len(ids))
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("files by IDs scan (tx): %w", err)
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("files by IDs iterate (tx): %w", err)
	}
	return files, nil
}

func (s *Store) RecentFiles(userID int64, limit int) ([]*model.File, error) {
	userID, err := mustBePositive(userID)
	if err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	rows, err := s.DB.QueryContext(context.Background(),
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND is_deleted = 0 ORDER BY updated_at DESC LIMIT ?",
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	defer rows.Close()
	var files []*model.File
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("recent files scan: %w", err)
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recent files iterate: %w", err)
	}
	return files, nil
}

func mustBePositive(id int64) (int64, error) {
	if id <= 0 {
		return 0, fmt.Errorf("ID必须为正数，当前值为%d", id)
	}
	return id, nil
}

func mustBeValidName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("名称不能为空")
	}
	if len(name) > 255 {
		return "", fmt.Errorf("名称超过255个字符限制")
	}
	return name, nil
}
