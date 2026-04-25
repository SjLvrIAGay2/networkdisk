package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"networkdisk/internal/middleware"
	"networkdisk/internal/service"
)

type AuthHandler struct {
	svc *service.UserService
}

func NewAuthHandler(svc *service.UserService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var in service.RegisterInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	user, err := h.svc.Register(in)
	if err != nil {
		if errors.Is(err, service.ErrUsernameTaken) {
			http.Error(w, `{"error":"username already taken"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var in service.LoginInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	user, tokens, err := h.svc.Login(in)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			http.Error(w, `{"error":"invalid username or password"}`, http.StatusUnauthorized)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    tokens.RefreshToken,
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   604800,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    tokens.CSRFToken,
		Path:     "/",
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

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
		h.svc.Logout(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	userIDStr, _ := r.Context().Value(middleware.UserIDKey).(string)
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid user"}`, http.StatusUnauthorized)
		return
	}
	user, err := h.svc.UserByID(userID)
	if err != nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
	})
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	userIDStr, _ := r.Context().Value(middleware.UserIDKey).(string)
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid user"}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.svc.ChangePassword(userID, body.OldPassword, body.NewPassword); err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			http.Error(w, `{"error":"invalid old password"}`, http.StatusUnauthorized)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "password changed"})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie("refresh_token")
	if err != nil || cookie.Value == "" {
		http.Error(w, `{"error":"no refresh token"}`, http.StatusUnauthorized)
		return
	}
	user, tokens, err := h.svc.RefreshAccessToken(cookie.Value)
	if err != nil {
		if errors.Is(err, service.ErrTokenRevoked) {
			http.SetCookie(w, &http.Cookie{
				Name:     "refresh_token",
				Value:    "",
				Path:     "/api/auth",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   -1,
			})
			http.Error(w, `{"error":"token revoked, possible theft detected"}`, http.StatusUnauthorized)
			return
		}
		if errors.Is(err, service.ErrTokenExpired) || errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"refresh token expired"}`, http.StatusUnauthorized)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    tokens.RefreshToken,
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   604800,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    tokens.CSRFToken,
		Path:     "/",
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
