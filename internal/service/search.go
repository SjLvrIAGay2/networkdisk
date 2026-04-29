package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"networkdisk/internal/model"
	"networkdisk/internal/store"
)

type SearchService struct {
	store *store.Store
}

func NewSearchService(s *store.Store) *SearchService {
	return &SearchService{store: s}
}

func (svc *SearchService) Search(userID int64, query string) ([]*model.File, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []*model.File{}, nil
	}
	files, err := svc.fulltextSearch(userID, query)
	if err != nil {
		return svc.likeSearch(userID, query)
	}
	if len(files) == 0 {
		return svc.likeSearch(userID, query)
	}
	return files, nil
}

func (svc *SearchService) Suggest(userID int64, query string) ([]string, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 {
		return []string{}, nil
	}
	escaped := strings.ReplaceAll(strings.ReplaceAll(query, "\\", "\\\\"), "%", "\\%")
	escaped = strings.ReplaceAll(escaped, "_", "\\_")
	rows, err := svc.store.DB.QueryContext(context.Background(),
		"SELECT DISTINCT name FROM files WHERE user_id = ? AND is_deleted = 0 AND name LIKE ? ESCAPE '\\' ORDER BY name LIMIT 10",
		userID, "%"+escaped+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("search suggest: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("search suggest scan: %w", err)
		}
		names = append(names, name)
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

func (svc *SearchService) fulltextSearch(userID int64, query string) ([]*model.File, error) {
	rows, err := svc.store.DB.QueryContext(context.Background(),
		"SELECT f.id, f.user_id, f.parent_id, f.name, f.is_dir, f.size, f.file_hash, f.storage_key, f.thumbnail_key, f.mime_type, f.is_starred, f.is_deleted, f.deleted_at, f.created_at, f.updated_at FROM files f WHERE f.user_id = ? AND f.is_deleted = 0 AND MATCH(f.name) AGAINST(? IN BOOLEAN MODE) ORDER BY f.is_dir DESC, f.name LIMIT 100",
		userID, "+"+query+"*",
	)
	if err != nil {
		return nil, err
	}
	return scanFileRows(rows)
}

func (svc *SearchService) likeSearch(userID int64, query string) ([]*model.File, error) {
	escaped := strings.ReplaceAll(strings.ReplaceAll(query, "\\", "\\\\"), "%", "\\%")
	escaped = strings.ReplaceAll(escaped, "_", "\\_")
	pattern := "%" + escaped + "%"
	rows, err := svc.store.DB.QueryContext(context.Background(),
		"SELECT id, user_id, parent_id, name, is_dir, size, file_hash, storage_key, thumbnail_key, mime_type, is_starred, is_deleted, deleted_at, created_at, updated_at FROM files WHERE user_id = ? AND is_deleted = 0 AND name LIKE ? ESCAPE '\\' ORDER BY is_dir DESC, name LIMIT 100",
		userID, pattern,
	)
	if err != nil {
		return nil, fmt.Errorf("like search: %w", err)
	}
	return scanFileRows(rows)
}

func scanFileRows(rows *sql.Rows) ([]*model.File, error) {
	defer rows.Close()
	var files []*model.File
	for rows.Next() {
		f := &model.File{}
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.IsDir, &f.Size, &f.FileHash, &f.StorageKey, &f.ThumbnailKey, &f.MimeType, &f.IsStarred, &f.IsDeleted, &deletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files: %w", err)
	}
	if files == nil {
		files = []*model.File{}
	}
	return files, nil
}
