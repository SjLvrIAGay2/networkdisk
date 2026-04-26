package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"networkdisk/internal/middleware"
	"networkdisk/internal/service"
)

type FileHandler struct {
	svc *service.FileService
}

func NewFileHandler(svc *service.FileService) *FileHandler {
	return &FileHandler{svc: svc}
}

func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var parentID *int64
	if dirStr := r.URL.Query().Get("dir_id"); dirStr != "" {
		id, err := strconv.ParseInt(dirStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid dir_id"}`, http.StatusBadRequest)
			return
		}
		parentID = &id
	}

	files, err := h.svc.ListDirectory(parentID, userID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	type fileEntry struct {
		ID           int64   `json:"id"`
		Name         string  `json:"name"`
		IsDir        bool    `json:"is_dir"`
		Size         int64   `json:"size"`
		MimeType     string  `json:"mime_type"`
		ThumbnailKey string  `json:"thumbnail_key"`
		ParentID     *int64  `json:"parent_id"`
		CreatedAt    string  `json:"created_at"`
	}

	entries := make([]fileEntry, 0, len(files))
	for _, f := range files {
		entries = append(entries, fileEntry{
			ID:           f.ID,
			Name:         f.Name,
			IsDir:        f.IsDir,
			Size:         f.Size,
			MimeType:     f.MimeType,
			ThumbnailKey: f.ThumbnailKey,
			ParentID:     f.ParentID,
			CreatedAt:    f.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	if entries == nil {
		entries = []fileEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"files": entries,
	})
}

func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, `{"error":"failed to parse multipart form"}`, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file field required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	var parentID *int64
	if dirStr := r.FormValue("dir_id"); dirStr != "" {
		id, err := strconv.ParseInt(dirStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid dir_id"}`, http.StatusBadRequest)
			return
		}
		parentID = &id
	}

	name := header.Filename

	result, err := h.svc.UploadFile(file, name, parentID, userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFileTooLarge):
			http.Error(w, `{"error":"file exceeds maximum size"}`, http.StatusRequestEntityTooLarge)
		case errors.Is(err, service.ErrExtensionBlocked):
			http.Error(w, `{"error":"file extension not allowed"}`, http.StatusUnprocessableEntity)
		default:
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":        result.File.ID,
		"name":      result.File.Name,
		"size":      result.File.Size,
		"mime_type": result.File.MimeType,
		"duplicate": result.Duplicate,
	})
}

func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid file id"}`, http.StatusBadRequest)
		return
	}

	f, reader, err := h.svc.OpenFile(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Disposition", `attachment; filename="`+f.Name+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

func (h *FileHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid file id"}`, http.StatusBadRequest)
		return
	}

	_, reader, err := h.svc.OpenThumbnail(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			http.Error(w, `{"error":"thumbnail not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "image/webp")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

func (h *FileHandler) Mkdir(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var body struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	dir, err := h.svc.CreateDir(body.Name, body.ParentID, userID)
	if err != nil {
		if errors.Is(err, service.ErrNameConflict) {
			http.Error(w, `{"error":"name already exists"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":        dir.ID,
		"name":      dir.Name,
		"parent_id": dir.ParentID,
		"is_dir":    true,
	})
}

func (h *FileHandler) Rename(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid file id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	file, err := h.svc.RenameFile(id, body.Name, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrNameConflict) {
			http.Error(w, `{"error":"name already exists"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":   file.ID,
		"name": file.Name,
	})
}

func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid file id"}`, http.StatusBadRequest)
		return
	}

	if err := h.svc.DeleteFile(id, userID); err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func userIDFromContext(r *http.Request) int64 {
	userIDStr, _ := r.Context().Value(middleware.UserIDKey).(string)
	userID, _ := strconv.ParseInt(userIDStr, 10, 64)
	return userID
}