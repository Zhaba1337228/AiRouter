package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/airouter/backend/internal/repository"
)

type contextKey string

const APIKeyContextKey contextKey = "apiKey"

// AdminAuth checks the Authorization: Bearer <ADMIN_TOKEN> header
func AdminAuth(adminToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r)
			if token != adminToken {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// APIKeyAuth validates an end-user API key for proxy endpoints
func APIKeyAuth(repo *repository.APIKeyRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r)
			if token == "" {
				http.Error(w, `{"error":"missing api key"}`, http.StatusUnauthorized)
				return
			}

			key, err := repo.ValidateKey(r.Context(), token)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired api key"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), APIKeyContextKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// also check x-api-key header for convenience
	return r.Header.Get("x-api-key")
}
