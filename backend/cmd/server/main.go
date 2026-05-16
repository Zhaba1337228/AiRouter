package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/airouter/backend/internal/config"
	"github.com/airouter/backend/internal/db"
	"github.com/airouter/backend/internal/handlers"
	mw "github.com/airouter/backend/internal/middleware"
	"github.com/airouter/backend/internal/proxy"
	"github.com/airouter/backend/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	// Connect DB
	sqlDB, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer sqlDB.Close()

	// Run migrations — path can be overridden via env MIGRATIONS_PATH
	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		_, filename, _, _ := runtime.Caller(0)
		migrationsPath = filepath.Join(filepath.Dir(filename), "..", "..", "migrations", "001_init.sql")
		// In Docker the binary is at /app/server, migrations at /app/migrations/
		if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
			migrationsPath = "/app/migrations/001_init.sql"
		}
	}
	if err := db.RunMigrations(sqlDB, migrationsPath); err != nil {
		log.Fatalf("run migrations: %v", err)
	}
	log.Println("migrations applied")

	// Connect Redis
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(redisOpts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("connect redis: %v", err)
	}
	log.Println("redis connected")

	// Repositories
	keyRepo := repository.NewAPIKeyRepo(sqlDB)
	logRepo := repository.NewLogRepo(sqlDB)

	// Handlers
	adminHandler := handlers.NewAdminHandler(keyRepo, logRepo)
	proxyHandler := proxy.NewHandler(cfg.UpstreamBaseURL, cfg.UpstreamAPIKey, logRepo)

	// Router
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Admin routes (protected by admin token)
	r.Route("/admin", func(r chi.Router) {
		r.Use(mw.AdminAuth(cfg.AdminToken))

		r.Get("/keys", adminHandler.ListKeys)
		r.Post("/keys", adminHandler.CreateKey)
		r.Delete("/keys/{id}", adminHandler.DeleteKey)
		r.Patch("/keys/{id}/toggle", adminHandler.ToggleKey)

		r.Get("/stats", adminHandler.Stats)
		r.Get("/stats/daily", adminHandler.StatsByDay)
		r.Get("/logs", adminHandler.Logs)
	})

	// Proxy routes (protected by user API key)
	proxyRouter := chi.NewRouter()
	proxyRouter.Use(mw.APIKeyAuth(keyRepo))
	proxyRouter.Use(mw.RateLimit(rdb))
	proxyRouter.HandleFunc("/*", proxyHandler.Proxy)

	r.Mount("/v1", proxyRouter)
	r.Mount("/v1beta", proxyRouter)

	// Start server
	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 130 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
