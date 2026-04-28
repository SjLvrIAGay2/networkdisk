package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"networkdisk/internal/logging"
	"networkdisk/internal/middleware"
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	user, err := h.svc.Register(in)
	if err != nil {
		if errors.Is(err, service.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "用户名已被占用")
			return
		}
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	user, tokens, err := h.svc.Login(in)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			logging.Logger().Warn("login failed", "username", in.Username, "remote", middleware.ClientIP(r))
			writeError(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if err := h.svc.ChangePassword(userID, body.OldPassword, body.NewPassword); err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "原密码错误")
			return
		}
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
			logging.Logger().Warn("refresh token revoked", "remote", middleware.ClientIP(r))
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	body, err := json.Marshal(data)
	if err != nil {
		http.Error(w, `{"error":"服务器内部错误"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
