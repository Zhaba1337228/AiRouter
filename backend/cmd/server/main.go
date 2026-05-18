package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
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
	keyRepo      := repository.NewAPIKeyRepo(sqlDB)
	logRepo      := repository.NewLogRepo(sqlDB)
	settingsRepo := repository.NewSettingsRepo(sqlDB)
	providerRepo := repository.NewProviderRepo(sqlDB)

	// Handlers
	adminHandler := handlers.NewAdminHandler(keyRepo, logRepo, settingsRepo, providerRepo, rdb)
	chatHandler  := handlers.NewChatHandler(cfg.UpstreamBaseURL, cfg.UpstreamAPIKey)
	proxyHandler := proxy.NewHandler(cfg.UpstreamBaseURL, cfg.UpstreamAPIKey, logRepo, settingsRepo, providerRepo, rdb)

	// Router
	r := chi.NewRouter()

	// Plain-text request logger (no ANSI color codes)
	r.Use(middleware.RequestLogger(&middleware.DefaultLogFormatter{
		Logger:  log.New(os.Stdout, "", log.LstdFlags),
		NoColor: true,
	}))
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

	// GET /v1/models — returns supported model IDs (protected by API key)
	listModelsHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		models := proxy.ListModels()
		data, _ := json.Marshal(map[string]any{
			"object": "list",
			"data":   models,
		})
		w.Write(data)
	}

	// Admin routes (protected by admin token, mounted at configurable secret path)
	adminPath := "/" + strings.TrimPrefix(strings.TrimSuffix(cfg.AdminPath, "/"), "/")
	log.Printf("admin API mounted at %s", adminPath)

	adminRouter := chi.NewRouter()
	adminRouter.Use(mw.AdminAuth(cfg.AdminToken))

	adminRouter.Get("/keys", adminHandler.ListKeys)
	adminRouter.Post("/keys", adminHandler.CreateKey)
	adminRouter.Patch("/keys/{id}", adminHandler.UpdateKey)
	adminRouter.Delete("/keys/{id}", adminHandler.DeleteKey)
	adminRouter.Patch("/keys/{id}/toggle", adminHandler.ToggleKey)

	adminRouter.Get("/stats", adminHandler.Stats)
	adminRouter.Get("/stats/daily", adminHandler.StatsByDay)
	adminRouter.Get("/logs", adminHandler.Logs)

	adminRouter.Get("/models", chatHandler.ListModels)
	adminRouter.Post("/chat", chatHandler.Chat)

	adminRouter.Get("/settings", adminHandler.GetSettings)
	adminRouter.Put("/settings", adminHandler.PutSettings)

	adminRouter.Get("/providers", adminHandler.ListProviders)
	adminRouter.Post("/providers", adminHandler.CreateProvider)
	adminRouter.Patch("/providers/{id}", adminHandler.UpdateProvider)
	adminRouter.Delete("/providers/{id}", adminHandler.DeleteProvider)

	r.Mount(adminPath, adminRouter)

	// Proxy routes (protected by user API key)
	proxyRouter := chi.NewRouter()
	proxyRouter.Use(mw.APIKeyAuth(keyRepo))
	proxyRouter.Use(mw.TokenLimit(keyRepo, rdb))
	proxyRouter.Use(mw.RequestLimit(keyRepo, rdb))
	proxyRouter.Get("/models", listModelsHandler)
	proxyRouter.HandleFunc("/*", proxyHandler.Proxy)

	r.Mount("/v1", proxyRouter)
	r.Mount("/v1beta", proxyRouter)

	// Start server
	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0, // no limit — large Claude Code contexts can be many MB
		WriteTimeout:      0, // no limit — streaming responses can take minutes
		IdleTimeout:       120 * time.Second,
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
