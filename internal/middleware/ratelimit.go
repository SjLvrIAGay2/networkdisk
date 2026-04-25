package middleware

import (
	"net/http"
	"sync"
	"time"
)

type rateBucket struct {
	requests []time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	limit    int
	window   time.Duration
	stopCh   chan struct{}
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*rateBucket),
		limit:   limit,
		window:  window,
		stopCh:  make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-rl.window)
			for ip, bucket := range rl.buckets {
				idx := 0
				for _, t := range bucket.requests {
					if t.After(cutoff) {
						break
					}
					idx++
				}
				bucket.requests = bucket.requests[idx:]
				if len(bucket.requests) == 0 {
					delete(rl.buckets, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCh:
			return
		}
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.window)
	bucket, exists := rl.buckets[key]
	if !exists {
		bucket = &rateBucket{}
		rl.buckets[key] = bucket
	}
	idx := 0
	for _, t := range bucket.requests {
		if t.After(cutoff) {
			break
		}
		idx++
	}
	bucket.requests = bucket.requests[idx:]
	if len(bucket.requests) >= rl.limit {
		return false
	}
	bucket.requests = append(bucket.requests, now)
	return true
}

func RateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	rl := NewRateLimiter(limit, window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}
			if !rl.Allow(ip) {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
