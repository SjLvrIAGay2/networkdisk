package store

import (
	"context"
	"fmt"

	"networkdisk/internal/model"
)

func (s *Store) CreateTag(tag *model.Tag) (*model.Tag, error) {
	if tag.UserID <= 0 {
		return nil, fmt.Errorf("创建标签：用户ID无效")
	}
	if tag.Name == "" {
		return nil, fmt.Errorf("创建标签：名称不能为空")
	}
	if len(tag.Name) > 64 {
		return nil, fmt.Errorf("创建标签：名称不能超过64个字符")
	}
	result, err := s.DB.ExecContext(context.Background(),
		"INSERT INTO tags (user_id, name, color) VALUES (?, ?, ?)",
		tag.UserID, tag.Name, tag.Color,
	)
	if err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create tag get id: %w", err)
	}
	return s.TagByID(id)
}

func (s *Store) TagByID(id int64) (*model.Tag, error) {
	if id <= 0 {
		return nil, fmt.Errorf("通过ID查找标签：ID无效")
	}
	tag := &model.Tag{}
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, user_id, name, color, created_at FROM tags WHERE id = ?", id,
	).Scan(&tag.ID, &tag.UserID, &tag.Name, &tag.Color, &tag.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("tag by id: %w", err)
	}
	return tag, nil
}

func (s *Store) TagsByUser(userID int64) ([]*model.Tag, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("列出用户标签：用户ID无效")
	}
	rows, err := s.DB.QueryContext(context.Background(),
		"SELECT id, user_id, name, color, created_at FROM tags WHERE user_id = ? ORDER BY name", userID,
	)
	if err != nil {
		return nil, fmt.Errorf("tags by user: %w", err)
	}
	defer rows.Close()
	var tags []*model.Tag
	for rows.Next() {
		tag := &model.Tag{}
		if err := rows.Scan(&tag.ID, &tag.UserID, &tag.Name, &tag.Color, &tag.CreatedAt); err != nil {
			return nil, fmt.Errorf("tags by user scan: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tags by user iterate: %w", err)
	}
	return tags, nil
}

func (s *Store) DeleteTag(id int64) error {
	if id <= 0 {
		return fmt.Errorf("删除标签：ID无效")
	}
	_, err := s.DB.ExecContext(context.Background(), "DELETE FROM tags WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

func (s *Store) AttachFileTags(fileID int64, tagIDs []int64) error {
	if fileID <= 0 || len(tagIDs) == 0 {
		return nil
	}
	for _, tid := range tagIDs {
		if tid <= 0 {
			return fmt.Errorf("关联文件标签：标签ID无效")
		}
	}
	tx, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("attach file tags begin tx: %w", err)
	}
	defer tx.Rollback()
	for _, tid := range tagIDs {
		_, err := tx.ExecContext(context.Background(),
			"INSERT IGNORE INTO file_tags (file_id, tag_id) VALUES (?, ?)", fileID, tid,
		)
		if err != nil {
			return fmt.Errorf("attach file tags: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) DetachFileTags(fileID int64, tagIDs []int64) error {
	if fileID <= 0 || len(tagIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(tagIDs))
	args := make([]interface{}, len(tagIDs)+1)
	args[0] = fileID
	for i, tid := range tagIDs {
		placeholders[i] = "?"
		args[i+1] = tid
	}
	query := fmt.Sprintf("DELETE FROM file_tags WHERE file_id = ? AND tag_id IN (%s)",
		joinPlaceholders(placeholders))
	_, err := s.DB.ExecContext(context.Background(), query, args...)
	if err != nil {
		return fmt.Errorf("detach file tags: %w", err)
	}
	return nil
}

func (s *Store) FileIDsByTag(tagID int64, userID int64) ([]int64, error) {
	if tagID <= 0 || userID <= 0 {
		return nil, fmt.Errorf("按标签查找文件：参数无效")
	}
	rows, err := s.DB.QueryContext(context.Background(),
		`SELECT ft.file_id FROM file_tags ft
		 JOIN files f ON ft.file_id = f.id
		 WHERE ft.tag_id = ? AND f.user_id = ? AND f.is_deleted = 0
		 ORDER BY f.name`, tagID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("file ids by tag: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("file ids by tag scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file ids by tag iterate: %w", err)
	}
	return ids, nil
}

func (s *Store) FileTags(fileID int64) ([]*model.Tag, error) {
	if fileID <= 0 {
		return nil, fmt.Errorf("获取文件标签：文件ID无效")
	}
	rows, err := s.DB.QueryContext(context.Background(),
		`SELECT t.id, t.user_id, t.name, t.color, t.created_at
		 FROM tags t JOIN file_tags ft ON t.id = ft.tag_id
		 WHERE ft.file_id = ? ORDER BY t.name`, fileID,
	)
	if err != nil {
		return nil, fmt.Errorf("file tags: %w", err)
	}
	defer rows.Close()
	var tags []*model.Tag
	for rows.Next() {
		tag := &model.Tag{}
		if err := rows.Scan(&tag.ID, &tag.UserID, &tag.Name, &tag.Color, &tag.CreatedAt); err != nil {
			return nil, fmt.Errorf("file tags scan: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file tags iterate: %w", err)
	}
	return tags, nil
}

func joinPlaceholders(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "," + parts[i]
	}
	return result
}
