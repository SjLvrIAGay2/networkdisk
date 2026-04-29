package handler

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"networkdisk/internal/config"
	"networkdisk/internal/logging"
	"networkdisk/internal/middleware"
	"networkdisk/internal/model"
	"networkdisk/internal/service"
)

type ShareHandler struct {
	svc        *service.ShareService
	fileSvc    *service.FileService
	cfg        *config.Config
	accessTmpl *template.Template
	viewTmpl   *template.Template
}

func NewShareHandler(svc *service.ShareService, fileSvc *service.FileService, cfg *config.Config) *ShareHandler {
	return &ShareHandler{
		svc:        svc,
		fileSvc:    fileSvc,
		cfg:        cfg,
		accessTmpl: template.Must(template.ParseFiles("web/templates/base.html", "web/templates/share_access.html")),
		viewTmpl:   template.Must(template.ParseFiles("web/templates/base.html", "web/templates/share_view.html")),
	}
}

func (h *ShareHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		FileID       int64  `json:"file_id"`
		Password     string `json:"password"`
		ExpireDays   int    `json:"expire_days"`
		MaxDownloads int    `json:"max_downloads"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}

	if body.FileID <= 0 {
		writeError(w, http.StatusBadRequest, "缺少文件ID")
		return
	}
	if body.MaxDownloads < 0 {
		writeError(w, http.StatusBadRequest, "下载次数不能为负数")
		return
	}

	var expireAt *time.Time
	if body.ExpireDays > 0 {
		t := time.Now().Add(time.Duration(body.ExpireDays) * 24 * time.Hour)
		expireAt = &t
	}

	share, err := h.svc.CreateShare(body.FileID, userID, body.Password, expireAt, body.MaxDownloads)
	if err != nil {
		if errors.Is(err, service.ErrFileNotFound) {
			if errors.Is(err, service.ErrFolderNotAllowed) {
				writeError(w, http.StatusBadRequest, "不支持分享文件夹")
				return
			}
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		logging.Error(r.Context(), "share", "create share failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	h.fileSvc.RecordAudit(userID, "share_create", "share", share.ID, "创建分享", middleware.ClientIP(r, ""))

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":            share.ID,
		"token":         share.Token,
		"file_id":       share.FileID,
		"password_set":  share.PasswordHash != "",
		"expire_at":     nilSafeTime(share.ExpireAt),
		"max_downloads": share.MaxDownloads,
	})
}

func (h *ShareHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	shares, err := h.svc.MyShares(userID)
	if err != nil {
		logging.Error(r.Context(), "share", "list shares failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	type entry struct {
		ID           int64       `json:"id"`
		Token        string      `json:"token"`
		FileID       int64       `json:"file_id"`
		FileName     string      `json:"file_name"`
		PasswordSet  bool        `json:"password_set"`
		ExpireAt     interface{} `json:"expire_at,omitempty"`
		MaxDownloads int64       `json:"max_downloads"`
		ViewCount    int64       `json:"view_count"`
		CreatedAt    string      `json:"created_at"`
	}

	entries := make([]entry, 0, len(shares))
	for _, s := range shares {
		fileName := h.fileSvc.FileNameByID(s.FileID)
		entries = append(entries, entry{
			ID:           s.ID,
			Token:        s.Token,
			FileID:       s.FileID,
			FileName:     fileName,
			PasswordSet:  s.PasswordHash != "",
			ExpireAt:     nilSafeTime(s.ExpireAt),
			MaxDownloads: s.MaxDownloads,
			ViewCount:    s.ViewCount,
			CreatedAt:    s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	if entries == nil {
		entries = []entry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"shares": entries})
}

func (h *ShareHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的分享ID")
		return
	}

	if err := h.svc.DeleteShare(id, userID); err != nil {
		if errors.Is(err, service.ErrShareNotFound) {
			writeError(w, http.StatusNotFound, "分享不存在")
			return
		}
		logging.Error(r.Context(), "share", "delete share failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	h.fileSvc.RecordAudit(userID, "share_delete", "share", id, "取消分享", middleware.ClientIP(r, ""))

	writeJSON(w, http.StatusOK, map[string]string{"message": "分享已取消"})
}

func (h *ShareHandler) ServePublicPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	share, f, err := h.svc.GetShareByToken(token)
	if err != nil {
		h.renderShareError(w, r, err)
		return
	}

	if share.PasswordHash != "" {
		h.accessTmpl.ExecuteTemplate(w, "share_access.html", map[string]interface{}{
			"token":     share.Token,
			"fileName":  f.Name,
			"fileSize":  f.Size,
			"mimeType":  f.MimeType,
		})
		return
	}

	h.viewTmpl.ExecuteTemplate(w, "share_view.html", h.shareViewData(share, f))
}

func (h *ShareHandler) VerifyPassword(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "请求格式无效"})
		return
	}

	clientIP := middleware.ClientIP(r, "")
	maxAttempts := h.cfg.Share.MaxPasswordAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	f, verifyToken, err := h.svc.VerifySharePassword(token, body.Password, clientIP, maxAttempts)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSharePassword):
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "密码错误"})
		case errors.Is(err, service.ErrTooManyAttempts):
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "尝试次数过多，请15分钟后重试"})
		default:
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "验证失败"})
		}
		return
	}

	h.fileSvc.RecordAudit(0, "share_verify", "share", 0, "share:"+token, middleware.ClientIP(r, ""))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"file_id":      f.ID,
		"name":         f.Name,
		"size":         f.Size,
		"mime_type":    f.MimeType,
		"verify_token": verifyToken,
	})
}

func (h *ShareHandler) Download(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	verifyToken := r.URL.Query().Get("verify_token")
	inline := r.URL.Query().Get("inline") == "1"

	var f *model.File
	var err error

	if verifyToken != "" {
		f, err = h.svc.DownloadWithVerifyToken(token, verifyToken)
	} else {
		f, _, err = h.svc.DownloadSharedFile(token)
	}

	if err != nil {
		switch {
		case errors.Is(err, service.ErrShareNotFound), errors.Is(err, service.ErrShareExpired):
			writeError(w, http.StatusNotFound, "分享链接无效或已过期")
		case errors.Is(err, service.ErrShareMaxReached):
			case errors.Is(err, service.ErrSharePassword):
				writeError(w, http.StatusForbidden, "需要密码验证")
			writeError(w, http.StatusGone, "分享链接已达到下载上限")
		default:
			logging.Error(r.Context(), "share", "share download failed", "error", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
		}
		return
	}

	reader, err := h.fileSvc.OpenFileReader(f)
	if err != nil {
		logging.Error(r.Context(), "share", "share download open file failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	defer reader.Close()

	if inline {
		mimeType := f.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("Content-Type", mimeType)
	} else {
		h.fileSvc.RecordAudit(0, "share_download", "share", 0, "share:"+token, middleware.ClientIP(r, ""))
		cd := mime.FormatMediaType("attachment", map[string]string{"filename": f.Name})
		w.Header().Set("Content-Disposition", cd)
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		logging.Error(r.Context(), "share", "share download copy failed", "error", err)
	}
}

func (h *ShareHandler) TempDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	_, f, err := h.svc.GetTempDownload(token)
	if err != nil {
		writeError(w, http.StatusNotFound, "下载链接无效或已过期")
		return
	}

	reader, err := h.fileSvc.OpenFileReader(f)
	if err != nil {
		logging.Error(r.Context(), "share", "temp download open file failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	defer reader.Close()

	cd := mime.FormatMediaType("attachment", map[string]string{"filename": f.Name})
	w.Header().Set("Content-Disposition", cd)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		logging.Error(r.Context(), "share", "temp download copy failed", "error", err)
	}
}

func (h *ShareHandler) shareViewData(share *model.Share, f *model.File) map[string]interface{} {
	previewType := h.fileSvc.PreviewType(f.MimeType)
	data := map[string]interface{}{
		"token":       share.Token,
		"fileName":    f.Name,
		"fileSize":    f.Size,
		"mimeType":    f.MimeType,
		"previewType": previewType,
	}
	if previewType == "download" && f.MimeType != "" {
		if strings.HasPrefix(f.MimeType, "text/") {
			content, pt := h.fileSvc.ShareFileContent(f)
			data["content"] = content
			data["previewType"] = pt
		}
	}
	return data
}

func (h *ShareHandler) renderShareError(w http.ResponseWriter, r *http.Request, err error) {
	var msg string
	switch {
	case errors.Is(err, service.ErrShareNotFound):
		msg = "分享链接不存在，文件可能已被删除"
	case errors.Is(err, service.ErrShareExpired):
		msg = "分享链接已过期"
	case errors.Is(err, service.ErrShareMaxReached):
			case errors.Is(err, service.ErrSharePassword):
				writeError(w, http.StatusForbidden, "需要密码验证")
		msg = "分享链接已达到下载上限"
	default:
		msg = "服务器内部错误"
	}
	h.accessTmpl.ExecuteTemplate(w, "share_access.html", map[string]interface{}{
		"error": msg,
	})
}

func nilSafeTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02T15:04:05Z")
}
