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
				token, err := generateCSRFToken()
				if err != nil {
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
					return
				}
				cookie = &http.Cookie{
					Name:     "csrf_token",
					Value:    token,
					Path:     "/",
					Secure:   true,
					SameSite: http.SameSiteStrictMode,
					MaxAge:   86400,
				}
				http.SetCookie(w, cookie)
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
				http.Error(w, `{"error":"invalid csrf token"}`, http.StatusForbidden)
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
