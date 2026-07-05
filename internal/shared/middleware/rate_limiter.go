package middleware

import (
	"net/http"
	"sync"
	"time"

	shared_response "rent_game_accs/internal/shared/response"
)

type TokenBucket struct {
	rate         float64
	capacity     float64
	tokens       float64
	lastRefilled time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*TokenBucket
	rate     float64
	capacity float64
}

func NewRateLimiter(rate float64, capacity float64) *RateLimiter {
	rl := &RateLimiter{
		buckets:  make(map[string]*TokenBucket),
		rate:     rate,
		capacity: capacity,
	}

	go rl.cleanupLoop()

	return rl
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, bucket := range rl.buckets {
			if now.Sub(bucket.lastRefilled) > 10*time.Minute {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	bucket, exists := rl.buckets[ip]
	if !exists {
		bucket = &TokenBucket{
			rate:         rl.rate,
			capacity:     rl.capacity,
			tokens:       rl.capacity,
			lastRefilled: now,
		}
		rl.buckets[ip] = bucket
	}

	elapsed := now.Sub(bucket.lastRefilled).Seconds()
	bucket.lastRefilled = now

	bucket.tokens = bucket.tokens + elapsed*bucket.rate
	if bucket.tokens > bucket.capacity {
		bucket.tokens = bucket.capacity
	}

	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return true
	}

	return false
}

func RateLimit(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				ip = xff
			}

			if !rl.Allow(ip) {
				shared_response.Error(
					w,
					http.StatusTooManyRequests,
					"TOO_MANY_REQUESTS",
					"Rate limit exceeded. Please try again later.",
				)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
