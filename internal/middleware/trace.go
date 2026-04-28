package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"networkdisk/internal/logging"
)

func TraceID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if id == "" {
				var b [8]byte
				if _, err := rand.Read(b[:]); err != nil {
					next.ServeHTTP(w, r)
					return
				}
				id = hex.EncodeToString(b[:])
			}
			ctx := logging.WithTraceID(r.Context(), id)
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}
