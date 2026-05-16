package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/airouter/backend/internal/middleware"
	"github.com/airouter/backend/internal/models"
	"github.com/airouter/backend/internal/repository"
)

type Handler struct {
	upstreamBaseURL string
	upstreamAPIKey  string
	logRepo         *repository.LogRepo
	httpClient      *http.Client
}

func NewHandler(upstreamBaseURL, upstreamAPIKey string, logRepo *repository.LogRepo) *Handler {
	return &Handler{
		upstreamBaseURL: strings.TrimRight(upstreamBaseURL, "/"),
		upstreamAPIKey:  upstreamAPIKey,
		logRepo:         logRepo,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Proxy forwards any request to upstream xynera.vip, replacing the Authorization header
func (h *Handler) Proxy(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10MB limit
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Build upstream URL
	upstreamURL := h.upstreamBaseURL + r.URL.Path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	// Create upstream request
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":"failed to create upstream request"}`, http.StatusInternalServerError)
		return
	}

	// Copy relevant headers
	for _, hdr := range []string{"Content-Type", "Accept", "User-Agent", "Anthropic-Version", "Anthropic-Beta"} {
		if v := r.Header.Get(hdr); v != "" {
			upReq.Header.Set(hdr, v)
		}
	}

	// Use our upstream key
	upReq.Header.Set("Authorization", "Bearer "+h.upstreamAPIKey)

	// Forward the request
	resp, err := h.httpClient.Do(upReq)

	latencyMs := int(time.Since(startTime).Milliseconds())

	if err != nil {
		errMsg := err.Error()
		h.asyncLog(r, body, nil, http.StatusBadGateway, latencyMs, &errMsg)
		http.Error(w, fmt.Sprintf(`{"error":"upstream error: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read upstream response"}`, http.StatusBadGateway)
		return
	}

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	// Log async
	h.asyncLog(r, body, respBody, resp.StatusCode, latencyMs, nil)
}

func (h *Handler) asyncLog(r *http.Request, reqBody, respBody []byte, statusCode, latencyMs int, errMsg *string) {
	go func() {
		log := &models.RequestLog{
			Endpoint:   r.URL.Path,
			Method:     r.Method,
			StatusCode: statusCode,
			LatencyMs:  latencyMs,
		}

		// Extract api key info from context
		if key, ok := r.Context().Value(middleware.APIKeyContextKey).(*models.APIKey); ok {
			log.APIKeyID = &key.ID
			log.APIKeyPrefix = &key.KeyPrefix
		}

		// Try to extract model and token usage from request/response
		extractModel(reqBody, log)
		if respBody != nil {
			extractTokens(respBody, log)
		}

		if errMsg != nil {
			log.ErrorMessage = errMsg
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.logRepo.Insert(ctx, log)
	}()
}

// extractModel tries to get the model name from OpenAI/Anthropic request body
func extractModel(body []byte, log *models.RequestLog) {
	if len(body) == 0 {
		return
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}
	if modelRaw, ok := payload["model"]; ok {
		var model string
		if err := json.Unmarshal(modelRaw, &model); err == nil && model != "" {
			log.Model = &model
		}
	}
}

// extractTokens tries to extract token usage from OpenAI/Anthropic response
func extractTokens(body []byte, log *models.RequestLog) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	// OpenAI format: {"usage": {"prompt_tokens": N, "completion_tokens": N, "total_tokens": N}}
	if usageRaw, ok := payload["usage"]; ok {
		var usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
			// Anthropic format
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}
		if err := json.Unmarshal(usageRaw, &usage); err == nil {
			if usage.TotalTokens > 0 {
				log.PromptTokens = usage.PromptTokens
				log.CompletionTokens = usage.CompletionTokens
				log.TotalTokens = usage.TotalTokens
			} else if usage.InputTokens > 0 || usage.OutputTokens > 0 {
				// Anthropic SDK format
				log.PromptTokens = usage.InputTokens
				log.CompletionTokens = usage.OutputTokens
				log.TotalTokens = usage.InputTokens + usage.OutputTokens
			}
		}
	}
}
