package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/airouter/backend/internal/models"
	"github.com/airouter/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

const (
	defaultRPM      = 60 // requests per minute per key
	adminRPM        = 30 // requests per minute per IP for /admin
	adminFailMax    = 10 // failed admin auths per 10 min per IP -> lockout
	adminFailWindow = 10 * time.Minute
)

// clientIP extracts the request IP, taking RealIP middleware into account.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// AdminRateLimit limits requests to /admin per source IP and tracks failed
// auth attempts to mitigate brute-force of ADMIN_TOKEN.
func AdminRateLimit(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			ctx := context.Background()

			lockKey := fmt.Sprintf("admin:lock:%s", ip)
			if locked, _ := rdb.Exists(ctx, lockKey).Result(); locked > 0 {
				http.Error(w, `{"error":"too many failed attempts, try again later"}`, http.StatusTooManyRequests)
				return
			}

			rlKey := fmt.Sprintf("admin:rl:%s", ip)
			count, err := rdb.Incr(ctx, rlKey).Result()
			if err == nil && count == 1 {
				rdb.Expire(ctx, rlKey, time.Minute)
			}
			if err == nil && count > adminRPM {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			if rec.status == http.StatusUnauthorized {
				failKey := fmt.Sprintf("admin:fail:%s", ip)
				fails, _ := rdb.Incr(ctx, failKey).Result()
				if fails == 1 {
					rdb.Expire(ctx, failKey, adminFailWindow)
				}
				if fails >= adminFailMax {
					rdb.Set(ctx, lockKey, "1", adminFailWindow)
				}
			}
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// RateLimit enforces a per-minute request cap per API key (sliding window via Redis).
func RateLimit(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := r.Context().Value(APIKeyContextKey).(*models.APIKey)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			redisKey := fmt.Sprintf("rl:%s", key.ID)
			ctx := context.Background()

			count, err := rdb.Incr(ctx, redisKey).Result()
			if err == nil && count == 1 {
				rdb.Expire(ctx, redisKey, time.Minute)
			}

			if err == nil && count > defaultRPM {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"rate limit exceeded","retry_after":60}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// TokenLimit blocks requests when the key has consumed its total token budget.
// token_limit = 0 means unlimited. Uses a 60-second Redis cache to reduce DB load.
func TokenLimit(keyRepo *repository.APIKeyRepo, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := r.Context().Value(APIKeyContextKey).(*models.APIKey)
			if !ok || key.TokenLimit == 0 {
				next.ServeHTTP(w, r)
				return
			}

			cacheKey := fmt.Sprintf("tl:%s", key.ID)
			ctx := context.Background()

			used, err := rdb.Get(ctx, cacheKey).Int64()
			if err != nil {
				// cache miss — query DB
				used, err = keyRepo.TotalTokensUsed(ctx, key.ID)
				if err != nil {
					next.ServeHTTP(w, r) // fail open
					return
				}
				rdb.SetEx(ctx, cacheKey, used, time.Minute)
			}

			if used >= key.TokenLimit {
				http.Error(w, `{"error":"token limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestLimit blocks requests when the key has reached its total request count limit.
// request_limit = 0 means unlimited. Uses a 60-second Redis cache to reduce DB load.
func RequestLimit(keyRepo *repository.APIKeyRepo, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := r.Context().Value(APIKeyContextKey).(*models.APIKey)
			if !ok || key.RequestLimit == 0 {
				next.ServeHTTP(w, r)
				return
			}

			cacheKey := fmt.Sprintf("rq:%s", key.ID)
			ctx := context.Background()

			count, err := rdb.Get(ctx, cacheKey).Int64()
			if err != nil {
				// cache miss — query DB
				count, err = keyRepo.TotalRequestsCount(ctx, key.ID)
				if err != nil {
					next.ServeHTTP(w, r) // fail open
					return
				}
				rdb.SetEx(ctx, cacheKey, count, time.Minute)
			}

			if count >= key.RequestLimit {
				http.Error(w, `{"error":"request limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
