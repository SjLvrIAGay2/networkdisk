package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"networkdisk/internal/logging"
	"networkdisk/internal/service"
)

type TagHandler struct {
	svc *service.TagService
}

func NewTagHandler(svc *service.TagService) *TagHandler {
	return &TagHandler{svc: svc}
}

func (h *TagHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	tags, err := h.svc.List(userID)
	if err != nil {
		logging.Error(r.Context(), "tag", "list tags failed", "error", err)
		writeError(w, http.StatusInternalServerError, "获取标签列表失败")
		return
	}
	type entry struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Color     string `json:"color"`
		CreatedAt string `json:"created_at"`
	}
	entries := make([]entry, 0, len(tags))
	for _, t := range tags {
		entries = append(entries, entry{
			ID:        t.ID,
			Name:      t.Name,
			Color:     t.Color,
			CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	if entries == nil {
		entries = []entry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tags": entries})
}

func (h *TagHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "标签名称不能为空")
		return
	}
	tag, err := h.svc.Create(userID, body.Name, body.Color)
	if err != nil {
		logging.Error(r.Context(), "tag", "create tag failed", "error", err)
		writeError(w, http.StatusInternalServerError, "创建标签失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         tag.ID,
		"name":       tag.Name,
		"color":      tag.Color,
		"created_at": tag.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

func (h *TagHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的标签ID")
		return
	}
	if err := h.svc.Delete(userID, id); err != nil {
		logging.Error(r.Context(), "tag", "delete tag failed", "error", err)
		writeError(w, http.StatusInternalServerError, "删除标签失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "标签已删除"})
}

func (h *TagHandler) FilesByTag(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的标签ID")
		return
	}
	files, err := h.svc.FilesByTag(userID, id)
	if err != nil {
		logging.Error(r.Context(), "tag", "files by tag failed", "error", err)
		writeError(w, http.StatusInternalServerError, "获取标签文件失败")
		return
	}
	entries := newFileEntries(files)
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": entries})
}

func (h *TagHandler) BatchAttach(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		TagIDs  []int64 `json:"tag_ids"`
		FileIDs []int64 `json:"file_ids"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if len(body.TagIDs) == 0 || len(body.FileIDs) == 0 {
		writeError(w, http.StatusBadRequest, "标签ID和文件ID不能为空")
		return
	}
	if err := h.svc.AttachFiles(userID, body.TagIDs, body.FileIDs); err != nil {
		logging.Error(r.Context(), "tag", "batch attach tags failed", "error", err)
		writeError(w, http.StatusInternalServerError, "关联标签失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "标签已关联"})
}
