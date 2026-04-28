package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"networkdisk/internal/config"
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
				http.Error(w, `{"error":"缺少授权头"}`, http.StatusUnauthorized)
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
				http.Error(w, `{"error":"无效的令牌"}`, http.StatusUnauthorized)
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, `{"error":"无效的令牌声明"}`, http.StatusUnauthorized)
				return
			}
			sub, err := claims.GetSubject()
			if err != nil || sub == "" {
				http.Error(w, `{"error":"无效的令牌声明"}`, http.StatusUnauthorized)
				return
			}
			userID, err := strconv.ParseInt(sub, 10, 64)
			if err != nil {
				http.Error(w, `{"error":"无效的用户标识"}`, http.StatusUnauthorized)
				return
			}
			username, _ := claims["usr"].(string)
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, UsernameKey, username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
