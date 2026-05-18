package config

import (
	"os"
)

type Config struct {
	Port             string
	DatabaseURL      string
	RedisURL         string
	AdminToken       string
	// AdminPath is the URL prefix for all admin API routes (without leading slash).
	// Example: "xK3mP9aQ" → admin API lives at /xK3mP9aQ/keys, /xK3mP9aQ/stats, etc.
	// Defaults to "admin" (same as before) but should be a random string in production.
	AdminPath        string
	UpstreamBaseURL  string
	UpstreamAPIKey   string
	CompressionMode  string // off | lite | standard | aggressive | ultra | rtk | stacked
}

func Load() *Config {
	return &Config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://airouter:secret@localhost:5432/airouter?sslmode=disable"),
		RedisURL:        getEnv("REDIS_URL", "redis://localhost:6379"),
		AdminToken:      getEnv("ADMIN_TOKEN", "changeme-super-secret-token"),
		AdminPath:       getEnv("ADMIN_PATH", "admin"),
		UpstreamBaseURL: getEnv("UPSTREAM_BASE_URL", "https://api.ecomagent.in"),
		UpstreamAPIKey:  getEnv("UPSTREAM_API_KEY", ""),
		CompressionMode: getEnv("COMPRESSION_MODE", "standard"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
