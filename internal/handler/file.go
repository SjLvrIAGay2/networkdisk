package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"networkdisk/internal/logging"
	"networkdisk/internal/middleware"
	"networkdisk/internal/model"
	"networkdisk/internal/service"
)

const maxUploadMemory = 32 << 20

type FileHandler struct {
	svc *service.FileService
}

func NewFileHandler(svc *service.FileService) *FileHandler {
	return &FileHandler{svc: svc}
}

func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	if filterType := r.URL.Query().Get("type"); filterType != "" {
		files, err := h.svc.ListFilesByType(userID, filterType)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"files": newFileEntries(files)})
		return
	}

	var parentID *int64
	if dirStr := r.URL.Query().Get("dir_id"); dirStr != "" {
		id, err := strconv.ParseInt(dirStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "无效的目录ID")
			return
		}
		parentID = &id
	}

	files, err := h.svc.ListDirectory(parentID, userID)
	if err != nil {
		logging.Error(r.Context(), "file", "list files failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	resp := map[string]interface{}{
		"files": newFileEntries(files),
	}
	if parentID != nil {
		breadcrumb, err := h.svc.GetBreadcrumb(*parentID)
		if err != nil {
			logging.Error(r.Context(), "file", "get breadcrumb failed", "error", err)
		} else {
			resp["breadcrumb"] = breadcrumb
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *FileHandler) SharedFiles(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	items, err := h.svc.SharedFiles(userID)
	if err != nil {
		logging.Error(r.Context(), "file", "shared files failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	entries := make([]fileEntry, 0, len(items))
	for _, item := range items {
		e := fileEntry{
			ID:           item.File.ID,
			Name:         item.File.Name,
			IsDir:        item.File.IsDir,
			Size:         item.File.Size,
			MimeType:     item.File.MimeType,
			ThumbnailKey: item.File.ThumbnailKey,
			ParentID:     item.File.ParentID,
			CreatedAt:    item.File.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:    item.File.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			ShareToken:   item.ShareToken,
		}
		if item.File.DeletedAt != nil {
			ds := item.File.DeletedAt.Format("2006-01-02T15:04:05Z")
			e.DeletedAt = &ds
		}
		sid := item.ShareID
		e.ShareID = &sid
		entries = append(entries, e)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": entries})
}

func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	if err := r.ParseMultipartForm(maxUploadMemory); err != nil {
		writeError(w, http.StatusBadRequest, "解析上传表单失败")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择文件")
		return
	}
	defer file.Close()

	var parentID *int64
	if dirStr := r.FormValue("dir_id"); dirStr != "" {
		id, err := strconv.ParseInt(dirStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "无效的目录ID")
			return
		}
		parentID = &id
	}

	name := header.Filename

	result, err := h.svc.UploadFile(file, name, parentID, userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFileTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "文件超过最大上传限制")
		case errors.Is(err, service.ErrExtensionBlocked):
			writeError(w, http.StatusUnprocessableEntity, "不支持的文件类型")
		default:
			logging.Error(r.Context(), "file", "upload file failed", "error", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
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
	h.svc.RecordAudit(userID, "upload", "file", result.File.ID, result.File.Name, middleware.ClientIP(r, ""))
}

func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}

	f, reader, err := h.svc.OpenFile(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "file", "download file failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	defer reader.Close()

	cd := mime.FormatMediaType("attachment", map[string]string{"filename": f.Name})
	w.Header().Set("Content-Disposition", cd)
	if seeker, ok := reader.(io.ReadSeeker); ok {
		http.ServeContent(w, r, f.Name, f.CreatedAt, seeker)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, reader); err != nil {
			logging.Error(r.Context(), "file", "download copy failed", "error", err)
			return
		}
	}
	h.svc.RecordAudit(userID, "download", "file", id, f.Name, middleware.ClientIP(r, ""))
}

func (h *FileHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}

	_, reader, err := h.svc.OpenThumbnail(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "缩略图不存在")
			return
		}
		logging.Error(r.Context(), "file", "thumbnail failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "image/webp")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		logging.Error(r.Context(), "file", "thumbnail copy failed", "error", err)
	}
}

func (h *FileHandler) Mkdir(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}

	dir, err := h.svc.CreateDir(body.Name, body.ParentID, userID)
	if err != nil {
		if errors.Is(err, service.ErrNameConflict) {
			writeError(w, http.StatusConflict, "名称已存在")
			return
		}
		logging.Error(r.Context(), "file", "mkdir failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
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
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}

	var file *model.File
	if body.ParentID != nil && body.Name != "" {
		file, err = h.svc.MoveFile(id, body.ParentID, userID)
		if err == nil {
			file, err = h.svc.RenameFile(id, body.Name, userID)
		}
	} else if body.ParentID != nil {
		file, err = h.svc.MoveFile(id, body.ParentID, userID)
	} else if body.Name != "" {
		file, err = h.svc.RenameFile(id, body.Name, userID)
	} else {
		writeError(w, http.StatusBadRequest, "缺少名称或父目录ID")
		return
	}
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		if errors.Is(err, service.ErrNameConflict) {
			writeError(w, http.StatusConflict, "名称已存在")
			return
		}
		logging.Error(r.Context(), "file", "rename/move file failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":        file.ID,
		"name":      file.Name,
		"parent_id": file.ParentID,
	})
}

func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}

	if err := h.svc.DeleteFile(id, userID); err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "file", "delete file failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "已删除"})
	h.svc.RecordAudit(userID, "delete", "file", id, "", middleware.ClientIP(r, ""))
}

func (h *FileHandler) RecycleList(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	files, err := h.svc.FilesInRecycleBin(userID)
	if err != nil {
		logging.Error(r.Context(), "file", "recycle list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	entries := newFileEntries(files)
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": entries})
}

func (h *FileHandler) Restore(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}
	file, err := h.svc.RestoreFile(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "file", "restore file failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": file.ID, "name": file.Name, "restored": true})
}

func (h *FileHandler) PermanentDelete(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}
	if err := h.svc.PermanentDeleteFile(id, userID); err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "file", "permanent delete failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "已永久删除"})
	h.svc.RecordAudit(userID, "permanent_delete", "file", id, "", middleware.ClientIP(r, ""))
}

func (h *FileHandler) PermanentDeletePreview(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}
	fileCount, dirCount, err := h.svc.DescendantCounts(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "file", "permanent delete preview failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"file_count": fileCount,
		"dir_count":  dirCount,
	})
}

func (h *FileHandler) Copy(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		ParentID *int64 `json:"parent_id"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	file, err := h.svc.CopyFile(id, body.ParentID, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		if errors.Is(err, service.ErrNameConflict) {
			writeError(w, http.StatusConflict, "名称已存在")
			return
		}
		logging.Error(r.Context(), "file", "copy file failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": file.ID, "name": file.Name, "parent_id": file.ParentID})
}

func (h *FileHandler) ToggleStar(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}
	starred, err := h.svc.ToggleStar(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "file", "toggle star failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "is_starred": starred})
}

func (h *FileHandler) StarredList(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	files, err := h.svc.StarredFiles(userID)
	if err != nil {
		logging.Error(r.Context(), "file", "starred list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	entries := newFileEntries(files)
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": entries})
}

func (h *FileHandler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		IDs []int64 `json:"ids"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if err := h.svc.BatchDelete(body.IDs, userID); err != nil {
		logging.Error(r.Context(), "file", "batch delete failed", "error", err)
		writeError(w, http.StatusBadRequest, "批量删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "批量删除成功"})
}

func (h *FileHandler) BatchMove(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		IDs      []int64 `json:"ids"`
		ParentID *int64  `json:"parent_id"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if err := h.svc.BatchMove(body.IDs, body.ParentID, userID); err != nil {
		logging.Error(r.Context(), "file", "batch move failed", "error", err)
		writeError(w, http.StatusBadRequest, "批量移动失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "批量移动成功"})
}

func (h *FileHandler) DownloadZip(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	idsStr := r.URL.Query().Get("ids")
	if idsStr == "" {
		writeError(w, http.StatusBadRequest, "缺少ids参数")
		return
	}
	var ids []int64
	for _, s := range strings.Split(idsStr, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "ids中包含无效ID")
			return
		}
		ids = append(ids, id)
	}
	if err := h.svc.ValidateZipDownload(ids, userID); err != nil {
		logging.Error(r.Context(), "file", "zip validate failed", "error", err)
		writeError(w, http.StatusBadRequest, "文件不存在或无法下载")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"download.zip\"")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.WriteHeader(http.StatusOK)
	if err := h.svc.DownloadZip(ids, userID, w); err != nil {
		logging.Error(r.Context(), "file", "download zip failed", "error", err)
	}
}

func (h *FileHandler) InitUpload(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name      string `json:"name"`
		ParentID  *int64 `json:"parent_id"`
		TotalSize int64  `json:"total_size"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	result, err := h.svc.InitUpload(userID, body.ParentID, body.Name, body.TotalSize)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFileTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "文件超过最大上传限制")
		case errors.Is(err, service.ErrExtensionBlocked):
			writeError(w, http.StatusUnprocessableEntity, "不支持的文件类型")
		default:
			logging.Error(r.Context(), "file", "init upload failed", "error", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *FileHandler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	if err := r.ParseMultipartForm(maxUploadMemory); err != nil {
		writeError(w, http.StatusBadRequest, "解析上传分片失败")
		return
	}
	defer r.MultipartForm.RemoveAll()
	uploadID := r.FormValue("upload_id")
	indexStr := r.FormValue("index")
	if uploadID == "" || indexStr == "" {
		writeError(w, http.StatusBadRequest, "缺少upload_id或index参数")
		return
	}
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的分片索引")
		return
	}
	file, _, err := r.FormFile("chunk")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择分片文件")
		return
	}
	defer file.Close()
	if err := h.svc.UploadChunk(uploadID, index, file, userID); err != nil {
		logging.Error(r.Context(), "file", "upload chunk failed", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"index": index, "ok": true})
}

func (h *FileHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		UploadID string `json:"upload_id"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	result, err := h.svc.CompleteUpload(body.UploadID, userID)
	if err != nil {
		logging.Error(r.Context(), "file", "complete upload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":        result.File.ID,
		"name":      result.File.Name,
		"size":      result.File.Size,
		"mime_type": result.File.MimeType,
		"duplicate": result.Duplicate,
	})
	h.svc.RecordAudit(userID, "upload", "file", result.File.ID, result.File.Name, middleware.ClientIP(r, ""))
}

func (h *FileHandler) UploadStatus(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	uploadID := r.PathValue("uploadId")
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "缺少上传ID")
		return
	}
	session, completed, err := h.svc.UploadStatus(uploadID, userID)
	if err != nil {
		logging.Error(r.Context(), "file", "upload status failed", "error", err)
		writeError(w, http.StatusNotFound, "上传会话不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"upload_id":   session.UploadID,
		"name":        session.Name,
		"total_size":  session.TotalSize,
		"chunk_size":  session.ChunkSize,
		"chunk_count": session.ChunkCount,
		"completed":   completed,
	})
}

func (h *FileHandler) CancelUpload(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	uploadID := r.PathValue("uploadId")
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "缺少上传ID")
		return
	}
	if err := h.svc.CancelUpload(uploadID, userID); err != nil {
		logging.Error(r.Context(), "file", "cancel upload failed", "error", err)
		writeError(w, http.StatusNotFound, "上传会话不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "已取消"})
}

func (h *FileHandler) Preview(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}

	f, previewType, content, err := h.svc.PreviewContent(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "file", "preview failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	if previewType == "image" || previewType == "video" || previewType == "audio" || previewType == "pdf" {
		_, reader, err := h.svc.OpenPreviewFile(id, userID)
		if err != nil {
			logging.Error(r.Context(), "file", "preview open file failed", "error", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		defer reader.Close()
		mimeType := f.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
		w.Header().Set("Content-Disposition", "inline")
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, reader); err != nil {
			logging.Error(r.Context(), "file", "preview stream copy failed", "error", err)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":           f.ID,
		"name":         f.Name,
		"mime_type":    f.MimeType,
		"preview_type": previewType,
		"content":      content,
	})
}

func (h *FileHandler) TempLink(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件ID")
		return
	}

	td, err := h.svc.CreateTempLink(id, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "file", "temp link failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"token":     td.Token,
		"file_id":   td.FileID,
		"expire_at": td.ExpireAt.Format("2006-01-02T15:04:05Z"),
	})
}

func userIDFromContext(r *http.Request) int64 {
	userID, ok := r.Context().Value(middleware.UserIDKey).(int64)
	if !ok {
		return 0
	}
	return userID
}
