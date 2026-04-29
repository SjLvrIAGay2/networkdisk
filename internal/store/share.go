package store

import (
	"context"
	"database/sql"
	"fmt"

	"networkdisk/internal/model"
)

func (s *Store) CreateShare(share *model.Share) (*model.Share, error) {
	if share.Token == "" || share.FileID <= 0 || share.OwnerID <= 0 {
		return nil, fmt.Errorf("create share: invalid token, file_id or owner_id")
	}
	result, err := s.DB.ExecContext(context.Background(),
		"INSERT INTO shares (token, file_id, owner_id, password_hash, expire_at, max_downloads) VALUES (?, ?, ?, ?, ?, ?)",
		share.Token, share.FileID, share.OwnerID, share.PasswordHash, share.ExpireAt, share.MaxDownloads,
	)
	if err != nil {
		return nil, fmt.Errorf("create share: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create share get id: %w", err)
	}
	return s.ShareByID(id)
}

func (s *Store) ShareByID(id int64) (*model.Share, error) {
	if id <= 0 {
		return nil, fmt.Errorf("share by id: id must be positive")
	}
	share := &model.Share{}
	var expireAt sql.NullTime
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, token, file_id, owner_id, password_hash, expire_at, max_downloads, view_count, created_at, updated_at FROM shares WHERE id = ?",
		id,
	).Scan(&share.ID, &share.Token, &share.FileID, &share.OwnerID, &share.PasswordHash, &expireAt, &share.MaxDownloads, &share.ViewCount, &share.CreatedAt, &share.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("share by id: %w", err)
	}
	if expireAt.Valid {
		share.ExpireAt = &expireAt.Time
	}
	return share, nil
}

func (s *Store) ShareByToken(token string) (*model.Share, error) {
	if token == "" {
		return nil, fmt.Errorf("share by token: token must not be empty")
	}
	share := &model.Share{}
	var expireAt sql.NullTime
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, token, file_id, owner_id, password_hash, expire_at, max_downloads, view_count, created_at, updated_at FROM shares WHERE token = ?",
		token,
	).Scan(&share.ID, &share.Token, &share.FileID, &share.OwnerID, &share.PasswordHash, &expireAt, &share.MaxDownloads, &share.ViewCount, &share.CreatedAt, &share.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("share by token: %w", err)
	}
	if expireAt.Valid {
		share.ExpireAt = &expireAt.Time
	}
	return share, nil
}

func (s *Store) SharesByOwner(ownerID int64) ([]*model.Share, error) {
	if ownerID <= 0 {
		return nil, fmt.Errorf("shares by owner: owner_id must be positive")
	}
	rows, err := s.DB.QueryContext(context.Background(),
		"SELECT id, token, file_id, owner_id, password_hash, expire_at, max_downloads, view_count, created_at, updated_at FROM shares WHERE owner_id = ? ORDER BY created_at DESC",
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("shares by owner: %w", err)
	}
	defer rows.Close()
	var shares []*model.Share
	for rows.Next() {
		share := &model.Share{}
		var expireAt sql.NullTime
		if err := rows.Scan(&share.ID, &share.Token, &share.FileID, &share.OwnerID, &share.PasswordHash, &expireAt, &share.MaxDownloads, &share.ViewCount, &share.CreatedAt, &share.UpdatedAt); err != nil {
			return nil, fmt.Errorf("shares by owner scan: %w", err)
		}
		if expireAt.Valid {
			share.ExpireAt = &expireAt.Time
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("shares by owner iterate: %w", err)
	}
	return shares, nil
}

func (s *Store) DeleteShare(id int64) error {
	if id <= 0 {
		return fmt.Errorf("delete share: id must be positive")
	}
	_, err := s.DB.ExecContext(context.Background(), "DELETE FROM shares WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete share: %w", err)
	}
	return nil
}

func (s *Store) IncrementShareViewCount(id int64) error {
	if id <= 0 {
		return fmt.Errorf("increment share view count: id must be positive")
	}
	_, err := s.DB.ExecContext(context.Background(), "UPDATE shares SET view_count = view_count + 1 WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("increment share view count: %w", err)
	}
	return nil
}

func (s *Store) IncrementShareViewCountIfUnderMax(id int64, maxDownloads int64) error {
	if id <= 0 {
		return fmt.Errorf("increment share view count if under max: id must be positive")
	}
	if maxDownloads <= 0 {
		return s.IncrementShareViewCount(id)
	}
	result, err := s.DB.ExecContext(context.Background(),
		"UPDATE shares SET view_count = view_count + 1 WHERE id = ? AND view_count < ?",
		id, maxDownloads,
	)
	if err != nil {
		return fmt.Errorf("increment share view count if under max: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("increment share view count if under max rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("max downloads reached")
	}
	return nil
}

func (s *Store) CreateTempDownload(td *model.TempDownload) (*model.TempDownload, error) {
	if td.Token == "" || td.FileID <= 0 || td.UserID <= 0 {
		return nil, fmt.Errorf("create temp download: invalid token, file_id or user_id")
	}
	result, err := s.DB.ExecContext(context.Background(),
		"INSERT INTO temp_downloads (token, file_id, user_id, expire_at) VALUES (?, ?, ?, ?)",
		td.Token, td.FileID, td.UserID, td.ExpireAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create temp download: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create temp download get id: %w", err)
	}
	return s.TempDownloadByID(id)
}

func (s *Store) TempDownloadByID(id int64) (*model.TempDownload, error) {
	if id <= 0 {
		return nil, fmt.Errorf("temp download by id: id must be positive")
	}
	td := &model.TempDownload{}
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, token, file_id, user_id, expire_at, created_at FROM temp_downloads WHERE id = ?",
		id,
	).Scan(&td.ID, &td.Token, &td.FileID, &td.UserID, &td.ExpireAt, &td.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("temp download by id: %w", err)
	}
	return td, nil
}

func (s *Store) TempDownloadByToken(token string) (*model.TempDownload, error) {
	if token == "" {
		return nil, fmt.Errorf("temp download by token: token must not be empty")
	}
	td := &model.TempDownload{}
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, token, file_id, user_id, expire_at, created_at FROM temp_downloads WHERE token = ?",
		token,
	).Scan(&td.ID, &td.Token, &td.FileID, &td.UserID, &td.ExpireAt, &td.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("temp download by token: %w", err)
	}
	return td, nil
}

type FileWithShare struct {
	File       *model.File
	ShareID    int64
	ShareToken string
}

func (s *Store) FilesWithActiveShares(ownerID int64) ([]FileWithShare, error) {
	if ownerID <= 0 {
		return nil, fmt.Errorf("files with active shares: owner_id must be positive")
	}
	rows, err := s.DB.QueryContext(context.Background(),
		"SELECT f.id, f.user_id, f.parent_id, f.name, f.is_dir, f.size, f.file_hash, f.storage_key, f.thumbnail_key, f.mime_type, f.is_starred, f.is_deleted, f.deleted_at, f.created_at, f.updated_at, s.share_id, s.token FROM files f INNER JOIN (SELECT s1.file_id, s1.id AS share_id, s1.token, s1.created_at FROM shares s1 INNER JOIN (SELECT file_id, MAX(created_at) AS max_created FROM shares WHERE owner_id = ? AND (expire_at IS NULL OR expire_at > NOW()) GROUP BY file_id) s2 ON s1.file_id = s2.file_id AND s1.created_at = s2.max_created WHERE s1.owner_id = ? AND (s1.expire_at IS NULL OR s1.expire_at > NOW())) s ON f.id = s.file_id WHERE f.is_deleted = 0 ORDER BY s.created_at DESC",
		ownerID, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("files with active shares: %w", err)
	}
	defer rows.Close()
	var result []FileWithShare
	for rows.Next() {
		f := &model.File{}
		var shareID int64
		var shareToken string
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt, &shareID, &shareToken); err != nil {
			return nil, fmt.Errorf("files with active shares scan: %w", err)
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		result = append(result, FileWithShare{File: f, ShareID: shareID, ShareToken: shareToken})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("files with active shares iterate: %w", err)
	}
	return result, nil
}

func (s *Store) DeleteExpiredTempDownloads() (int64, error) {
	result, err := s.DB.ExecContext(context.Background(), "DELETE FROM temp_downloads WHERE expire_at < NOW()")
	if err != nil {
		return 0, fmt.Errorf("delete expired temp downloads: %w", err)
	}
	return result.RowsAffected()
}

func (s *Store) ShareByFileID(fileID int64) (*model.Share, error) {
	if fileID <= 0 {
		return nil, fmt.Errorf("share by file id: file_id must be positive")
	}
	share := &model.Share{}
	var expireAt sql.NullTime
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, token, file_id, owner_id, password_hash, expire_at, max_downloads, view_count, created_at, updated_at FROM shares WHERE file_id = ? ORDER BY created_at DESC LIMIT 1",
		fileID,
	).Scan(&share.ID, &share.Token, &share.FileID, &share.OwnerID, &share.PasswordHash, &expireAt, &share.MaxDownloads, &share.ViewCount, &share.CreatedAt, &share.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("share by file id: %w", err)
	}
	if expireAt.Valid {
		share.ExpireAt = &expireAt.Time
	}
	return share, nil
}
