package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/airouter/backend/internal/models"
	"github.com/airouter/backend/internal/repository"
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

// BudgetLimit blocks requests when the key has spent its entire USD budget.
// budget_usd = 0 means unlimited.
// Uses Redis to cache the spent amount for 60 seconds to reduce DB load.
func BudgetLimit(keyRepo *repository.APIKeyRepo, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := r.Context().Value(APIKeyContextKey).(*models.APIKey)
			if !ok || key.BudgetUSD == 0 {
				next.ServeHTTP(w, r)
				return
			}

			cacheKey := fmt.Sprintf("bl:%s", key.ID)
			ctx := context.Background()

			// Redis stores cost*1e8 as integer to avoid float issues
			raw, err := rdb.Get(ctx, cacheKey).Float64()
			if err != nil {
				// cache miss — query DB
				raw, err = keyRepo.TotalCostSpent(ctx, key.ID)
				if err != nil {
					next.ServeHTTP(w, r) // fail open
					return
				}
				rdb.SetEx(ctx, cacheKey, raw, time.Minute)
			}

			if raw >= key.BudgetUSD {
				http.Error(w, `{"error":"budget limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
