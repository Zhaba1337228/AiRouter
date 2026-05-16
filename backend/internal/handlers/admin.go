package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/airouter/backend/internal/repository"
	"github.com/go-chi/chi/v5"
)

type AdminHandler struct {
	keyRepo *repository.APIKeyRepo
	logRepo *repository.LogRepo
}

func NewAdminHandler(keyRepo *repository.APIKeyRepo, logRepo *repository.LogRepo) *AdminHandler {
	return &AdminHandler{keyRepo: keyRepo, logRepo: logRepo}
}

// POST /admin/keys
func (h *AdminHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string  `json:"name"`
		Note      *string `json:"note"`
		ExpiresAt *string `json:"expires_at"` // RFC3339 or null
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
		Name: req.Name,
		Note: req.Note,
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
