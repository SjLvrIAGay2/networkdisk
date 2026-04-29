package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"networkdisk/internal/logging"
)

type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
	stopCh   chan struct{}
}

type visitor struct {
	count    int
	resetAt  time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for k, v := range rl.visitors {
				if now.After(v.resetAt) {
					delete(rl.visitors, k)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCh:
			return
		}
	}
}

func (rl *RateLimiter) Allow(host string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	v, ok := rl.visitors[host]
	if !ok || now.After(v.resetAt) {
		rl.visitors[host] = &visitor{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	if v.count >= rl.limit {
		return false
	}
	v.count++
	return true
}

func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

func RateLimit(limit int, window time.Duration, trustedProxy string) (func(http.Handler) http.Handler, func()) {
	rl := NewRateLimiter(limit, window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := ClientIP(r, trustedProxy)
			if !rl.Allow(host) {
				logging.Warn(r.Context(), "ratelimit", "rate limited", "remote", host, "path", r.URL.Path)
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"请求过于频繁，请稍后重试"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, rl.Stop
}

func ClientIP(r *http.Request, trustedProxy string) string {
	remoteHost, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteHost == "" {
		remoteHost = r.RemoteAddr
	}
	if isTrustedRemote(remoteHost, trustedProxy) {
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			idx := strings.IndexByte(xff, ',')
			if idx > 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
	}
	return remoteHost
}

func isTrustedRemote(remote, trustedProxy string) bool {
	if trustedProxy != "" && remote == trustedProxy {
		return true
	}
	ip := net.ParseIP(remote)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
