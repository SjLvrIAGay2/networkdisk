package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var parentID *int64
	if dirStr := r.URL.Query().Get("dir_id"); dirStr != "" {
		id, err := strconv.ParseInt(dirStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid dir_id")
			return
		}
		parentID = &id
	}

	files, err := h.svc.ListDirectory(parentID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	var parentID *int64
	if dirStr := r.FormValue("dir_id"); dirStr != "" {
		id, err := strconv.ParseInt(dirStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid dir_id")
			return
		}
		parentID = &id
	}

	name := header.Filename

	result, err := h.svc.UploadFile(file, name, parentID, userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFileTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "file exceeds maximum size")
		case errors.Is(err, service.ErrExtensionBlocked):
			writeError(w, http.StatusUnprocessableEntity, "file extension not allowed")
		default:
			writeError(w, http.StatusInternalServerError, "internal server error")
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}

	f, reader, err := h.svc.OpenFile(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer reader.Close()

	cd := mime.FormatMediaType("attachment", map[string]string{"filename": f.Name})
	w.Header().Set("Content-Disposition", cd)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

func (h *FileHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}

	_, reader, err := h.svc.OpenThumbnail(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "thumbnail not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	dir, err := h.svc.CreateDir(body.Name, body.ParentID, userID)
	if err != nil {
		if errors.Is(err, service.ErrNameConflict) {
			writeError(w, http.StatusConflict, "name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	file, err := h.svc.RenameFile(id, body.Name, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		if errors.Is(err, service.ErrNameConflict) {
			writeError(w, http.StatusConflict, "name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}

	if err := h.svc.DeleteFile(id, userID); err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func userIDFromContext(r *http.Request) int64 {
	userIDStr, _ := r.Context().Value(middleware.UserIDKey).(string)
	userID, _ := strconv.ParseInt(userIDStr, 10, 64)
	return userID
}
