package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
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
					n, _ := rand.Int(rand.Reader, big.NewInt(1<<48))
					id = hex.EncodeToString([]byte{byte(n.Uint64() >> 40), byte(n.Uint64() >> 32), byte(n.Uint64() >> 24), byte(n.Uint64() >> 16), byte(n.Uint64() >> 8), byte(n.Uint64())})
					logging.Warn(r.Context(), "trace", "trace id fallback used", "error", err)
				} else {
					id = hex.EncodeToString(b[:])
				}
			}
			ctx := logging.WithTraceID(r.Context(), id)
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}
