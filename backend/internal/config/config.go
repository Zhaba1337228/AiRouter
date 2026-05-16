package config

import (
	"os"
)

type Config struct {
	Port             string
	DatabaseURL      string
	RedisURL         string
	AdminToken       string
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
		UpstreamBaseURL: getEnv("UPSTREAM_BASE_URL", "https://www.xynera.vip"),
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
