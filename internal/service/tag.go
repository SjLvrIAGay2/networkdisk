package service

import (
	"fmt"
	"regexp"

	"networkdisk/internal/model"
	"networkdisk/internal/store"
)

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)

type TagService struct {
	store *store.Store
}

func NewTagService(s *store.Store) *TagService {
	return &TagService{store: s}
}

func (svc *TagService) Create(userID int64, name, color string) (*model.Tag, error) {
	if color == "" {
		color = "#3b82f6"
	}
	if !hexColorPattern.MatchString(color) {
		return nil, fmt.Errorf("无效的颜色格式")
	}
	tag, err := svc.store.CreateTag(&model.Tag{
		UserID: userID,
		Name:   name,
		Color:  color,
	})
	if err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return tag, nil
}

func (svc *TagService) List(userID int64) ([]*model.Tag, error) {
	return svc.store.TagsByUser(userID)
}

func (svc *TagService) Delete(userID int64, tagID int64) error {
	tag, err := svc.store.TagByID(tagID)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	if tag.UserID != userID {
		return fmt.Errorf("标签不属于当前用户")
	}
	return svc.store.DeleteTag(tagID)
}

func (svc *TagService) AttachFiles(userID int64, tagIDs []int64, fileIDs []int64) error {
	for _, tid := range tagIDs {
		tag, err := svc.store.TagByID(tid)
		if err != nil {
			return fmt.Errorf("attach files: tag %d: %w", tid, err)
		}
		if tag.UserID != userID {
			return fmt.Errorf("标签 %d 不属于当前用户", tid)
		}
	}
	files, err := svc.store.FilesByIDs(fileIDs)
	if err != nil {
		return fmt.Errorf("attach files: lookup files: %w", err)
	}
	for _, f := range files {
		if f.UserID != userID {
			return fmt.Errorf("文件 %d 不属于当前用户", f.ID)
		}
	}
	for _, fid := range fileIDs {
		if err := svc.store.AttachFileTags(fid, tagIDs); err != nil {
			return fmt.Errorf("attach files: %w", err)
		}
	}
	return nil
}

func (svc *TagService) FilesByTag(userID int64, tagID int64) ([]*model.File, error) {
	tag, err := svc.store.TagByID(tagID)
	if err != nil {
		return nil, fmt.Errorf("files by tag: %w", err)
	}
	if tag.UserID != userID {
		return nil, fmt.Errorf("标签不属于当前用户")
	}
	ids, err := svc.store.FileIDsByTag(tagID, userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []*model.File{}, nil
	}
	return svc.store.FilesByIDs(ids)
}
