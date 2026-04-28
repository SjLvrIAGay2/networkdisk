package middleware

import (
	"net/http"
	"time"

	"networkdisk/internal/logging"
)

type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

func Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)
			args := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.status,
				"size", wrapped.size,
				"duration", time.Since(start).String(),
				"remote", r.RemoteAddr,
			}
			switch {
			case wrapped.status >= 500:
				logging.Logger().Error("request", args...)
			case wrapped.status >= 400:
				logging.Logger().Warn("request", args...)
			default:
				logging.Logger().Info("request", args...)
			}
		})
	}
}
