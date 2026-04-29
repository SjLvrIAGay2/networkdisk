package middleware

import (
	"net/http"
	"runtime/debug"

	"networkdisk/internal/logging"
)

func Recover() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logging.Error(r.Context(), "panic", "panic recovered",
						"panic", rec,
						"stack", string(debug.Stack()),
						"path", r.URL.Path,
						"method", r.Method,
					)
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error":"服务器内部错误"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
