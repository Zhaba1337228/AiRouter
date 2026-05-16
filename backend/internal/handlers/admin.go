package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/airouter/backend/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type AdminHandler struct {
	keyRepo      *repository.APIKeyRepo
	logRepo      *repository.LogRepo
	settingsRepo *repository.SettingsRepo
	rdb          interface{ Del(ctx context.Context, keys ...string) *redis.IntCmd }
}

type redisDeleter interface {
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

func NewAdminHandler(
	keyRepo *repository.APIKeyRepo,
	logRepo *repository.LogRepo,
	settingsRepo *repository.SettingsRepo,
	rdb redisDeleter,
) *AdminHandler {
	return &AdminHandler{keyRepo: keyRepo, logRepo: logRepo, settingsRepo: settingsRepo, rdb: rdb}
}

// POST /admin/keys
func (h *AdminHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string  `json:"name"`
		Note       *string `json:"note"`
		ExpiresAt  *string `json:"expires_at"` // RFC3339 or null
		BudgetUSD  float64 `json:"budget_usd"` // 0 = unlimited
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	input := repository.CreateKeyInput{
		Name:      req.Name,
		Note:      req.Note,
		BudgetUSD: req.BudgetUSD,
	}
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			jsonError(w, "invalid expires_at format, use RFC3339", http.StatusBadRequest)
			return
		}
		input.ExpiresAt = &t
	}

	key, plaintext, err := h.keyRepo.Create(r.Context(), input)
	if err != nil {
		jsonError(w, "failed to create key: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, http.StatusCreated, map[string]interface{}{
		"key":    key,
		"secret": plaintext,
	})
}

// GET /admin/keys
func (h *AdminHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.keyRepo.List(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, http.StatusOK, keys)
}

// DELETE /admin/keys/{id}
func (h *AdminHandler) DeleteKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.keyRepo.Delete(r.Context(), id); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// PATCH /admin/keys/{id}/toggle
func (h *AdminHandler) ToggleKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		IsActive bool `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := h.keyRepo.SetActive(r.Context(), id, req.IsActive); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, http.StatusOK, map[string]bool{"is_active": req.IsActive})
}

// GET /admin/stats
func (h *AdminHandler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.logRepo.Stats(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, http.StatusOK, stats)
}

// GET /admin/stats/daily?days=7
func (h *AdminHandler) StatsByDay(w http.ResponseWriter, r *http.Request) {
	days := 7
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	data, err := h.logRepo.StatsByDay(r.Context(), days)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, http.StatusOK, data)
}

// GET /admin/logs?limit=50&offset=0
func (h *AdminHandler) Logs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	logs, err := h.logRepo.List(r.Context(), limit, offset)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, http.StatusOK, logs)
}

// GET /admin/settings
func (h *AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := h.settingsRepo.GetAll(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, http.StatusOK, all)
}

// PUT /admin/settings
func (h *AdminHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	for k, v := range body {
		if err := h.settingsRepo.Set(r.Context(), k, v); err != nil {
			jsonError(w, "failed to save setting: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	// Invalidate Redis cache for compression_mode if it was changed
	if h.rdb != nil {
		if _, changed := body["compression_mode"]; changed {
			h.rdb.Del(context.Background(), "settings:compression_mode")
		}
	}
	jsonOK(w, http.StatusOK, map[string]string{"status": "ok"})
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func jsonOK(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}
