package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"networkdisk/internal/logging"
	"networkdisk/internal/middleware"
	"networkdisk/internal/model"
	"networkdisk/internal/service"
)

type AuthHandler struct {
	svc     *service.UserService
	fileSvc *service.FileService
}

func NewAuthHandler(svc *service.UserService, fileSvc *service.FileService) *AuthHandler {
	return &AuthHandler{svc: svc, fileSvc: fileSvc}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in service.RegisterInput
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	user, err := h.svc.Register(in)
	if err != nil {
		if errors.Is(err, service.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "用户名已被占用")
			return
		}
		logging.Error(r.Context(), "auth", "register failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
	})
	h.fileSvc.RecordAudit(user.ID, "register", "user", user.ID, user.Username, middleware.ClientIP(r))
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in service.LoginInput
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	user, tokens, err := h.svc.Login(in)
	if err != nil {
		if errors.Is(err, service.ErrTOTPRequired) {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"totp_required": true,
				"user_id":       user.ID,
			})
			return
		}
		if errors.Is(err, service.ErrInvalidCredentials) || errors.Is(err, service.ErrInvalidTOTP) {
			logging.Warn(r.Context(), "auth", "login failed", "username", in.Username, "remote", middleware.ClientIP(r))
			writeError(w, http.StatusUnauthorized, "用户名或密码错误或两步验证码无效")
			return
		}
		logging.Error(r.Context(), "auth", "login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    tokens.RefreshToken,
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   604800,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    tokens.CSRFToken,
		Path:     "/",
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":           user.ID,
		"username":     user.Username,
		"access_token": tokens.AccessToken,
		"expires_in":   tokens.ExpiresIn,
		"csrf_token":   tokens.CSRFToken,
	})
	h.fileSvc.RecordAudit(user.ID, "login", "user", user.ID, user.Username, middleware.ClientIP(r))
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
		if err := h.svc.Logout(cookie.Value); err != nil {
			logging.Error(r.Context(), "auth", "logout failed", "error", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "已退出登录"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(middleware.UserIDKey).(int64)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "用户无效")
		return
	}
	user, err := h.svc.UserByID(userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
	})
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(middleware.UserIDKey).(int64)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "用户无效")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if err := h.svc.ChangePassword(userID, body.OldPassword, body.NewPassword); err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "原密码错误")
			return
		}
		logging.Error(r.Context(), "auth", "change password failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "密码已修改"})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refresh_token")
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "缺少刷新令牌")
		return
	}
	user, tokens, err := h.svc.RefreshAccessToken(cookie.Value)
	if err != nil {
		if errors.Is(err, service.ErrTokenRevoked) {
			logging.Warn(r.Context(), "auth", "refresh token revoked", "remote", middleware.ClientIP(r))
			http.SetCookie(w, &http.Cookie{
				Name:     "refresh_token",
				Value:    "",
				Path:     "/api/auth",
				HttpOnly: true,
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   -1,
			})
			writeError(w, http.StatusUnauthorized, "令牌已吊销，可能存在安全风险")
			return
		}
		if errors.Is(err, service.ErrTokenExpired) {
			writeError(w, http.StatusUnauthorized, "刷新令牌已过期")
			return
		}
		logging.Error(r.Context(), "auth", "refresh token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    tokens.RefreshToken,
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   604800,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    tokens.CSRFToken,
		Path:     "/",
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":           user.ID,
		"username":     user.Username,
		"access_token": tokens.AccessToken,
		"expires_in":   tokens.ExpiresIn,
		"csrf_token":   tokens.CSRFToken,
	})
}

func (h *AuthHandler) EnableTOTP(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	secret, url, err := h.svc.GenerateTOTP(userID)
	if err != nil {
		logging.Error(r.Context(), "auth", "generate totp failed", "error", err)
		writeError(w, http.StatusBadRequest, "生成两步验证密钥失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"secret": secret,
		"url":    url,
	})
}

func (h *AuthHandler) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Code string `json:"code"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if err := h.svc.EnableTOTP(userID, body.Code); err != nil {
		logging.Error(r.Context(), "auth", "verify totp failed", "error", err)
		writeError(w, http.StatusBadRequest, "验证码无效")
		return
	}
	h.fileSvc.RecordAudit(userID, "totp_enable", "user", userID, "开启两步验证", middleware.ClientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"message": "两步验证已开启"})
}

func (h *AuthHandler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Code string `json:"code"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if err := h.svc.DisableTOTP(userID, body.Code); err != nil {
		logging.Error(r.Context(), "auth", "disable totp failed", "error", err)
		writeError(w, http.StatusBadRequest, "验证码无效")
		return
	}
	h.fileSvc.RecordAudit(userID, "totp_disable", "user", userID, "关闭两步验证", middleware.ClientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"message": "两步验证已关闭"})
}

type fileEntry struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	IsDir        bool   `json:"is_dir"`
	Size         int64  `json:"size"`
	MimeType     string `json:"mime_type"`
	ThumbnailKey string `json:"thumbnail_key"`
	ParentID     *int64 `json:"parent_id"`
	CreatedAt    string `json:"created_at"`
}

func newFileEntries(files []*model.File) []fileEntry {
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
	return entries
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	body, err := json.Marshal(data)
	if err != nil {
		http.Error(w, `{"error":"服务器内部错误"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		logging.Error(context.Background(), "response", "write json response failed", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
