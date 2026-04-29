package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"slices"
)

func CSRF() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("csrf_token")
			if err != nil || cookie.Value == "" {
				token, genErr := generateCSRFToken()
				if genErr != nil {
					http.Error(w, `{"error":"服务器内部错误"}`, http.StatusInternalServerError)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     "csrf_token",
					Value:    token,
					Path:     "/",
					Secure:   IsSecureRequest(r),
					SameSite: http.SameSiteStrictMode,
					MaxAge:   86400,
				})
				if slices.Contains([]string{"GET", "HEAD", "OPTIONS"}, r.Method) {
					next.ServeHTTP(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"无效的CSRF令牌"}`))
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
			if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
				http.Error(w, `{"error":"无效的CSRF令牌"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func IsSecureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
