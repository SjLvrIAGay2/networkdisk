package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"slices"
)

func CSRF() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("csrf_token")
			if err != nil || cookie.Value == "" {
				if slices.Contains([]string{"GET", "HEAD", "OPTIONS"}, r.Method) {
					token, err := generateCSRFToken()
					if err != nil {
						http.Error(w, `{"error":"服务器内部错误"}`, http.StatusInternalServerError)
						return
					}
					http.SetCookie(w, &http.Cookie{
						Name:     "csrf_token",
						Value:    token,
						Path:     "/",
						Secure:   r.TLS != nil,
						SameSite: http.SameSiteStrictMode,
						MaxAge:   86400,
					})
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, `{"error":"无效的CSRF令牌"}`, http.StatusForbidden)
				return
			}
			if slices.Contains([]string{"GET", "HEAD", "OPTIONS"}, r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("X-CSRF-Token")
			if header == "" {
				header = r.FormValue("csrf_token")
			}
			if header == "" || header != cookie.Value {
				http.Error(w, `{"error":"无效的CSRF令牌"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
