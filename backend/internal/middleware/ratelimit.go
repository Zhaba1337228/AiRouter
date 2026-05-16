package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/airouter/backend/internal/models"
	"github.com/redis/go-redis/v9"
)

const defaultRPM = 60 // requests per minute per key

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
