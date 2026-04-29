package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"networkdisk/internal/config"
	"networkdisk/internal/logging"
)

type contextKey string

const (
	UserIDKey   contextKey = "user_id"
	UsernameKey contextKey = "username"
)

func Auth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				writeJSONError(w, http.StatusUnauthorized, "缺少授权头")
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return []byte(cfg.Auth.JWTSecret), nil
			},
				jwt.WithIssuer("networkdisk"),
				jwt.WithAudience("networkdisk"),
				jwt.WithExpirationRequired(),
			)
			if err != nil || !token.Valid {
				writeJSONError(w, http.StatusUnauthorized, "无效的令牌")
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "无效的令牌声明")
				return
			}
			sub, err := claims.GetSubject()
			if err != nil || sub == "" {
				writeJSONError(w, http.StatusUnauthorized, "无效的令牌声明")
				return
			}
			userID, err := strconv.ParseInt(sub, 10, 64)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "无效的用户标识")
				return
			}
			username, ok := claims["usr"].(string)
			if !ok {
				logging.Warn(r.Context(), "auth", "username claim type assertion failed")
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, UsernameKey, username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
