package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type ChatHandler struct {
	upstreamBaseURL string
	upstreamAPIKey  string
	httpClient      *http.Client
}

func NewChatHandler(upstreamBaseURL, upstreamAPIKey string) *ChatHandler {
	return &ChatHandler{
		upstreamBaseURL: strings.TrimRight(upstreamBaseURL, "/"),
		upstreamAPIKey:  upstreamAPIKey,
		httpClient:      &http.Client{Timeout: 120 * time.Second},
	}
}

// GET /admin/models — fetch model list from upstream
func (h *ChatHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.upstreamBaseURL+"/v1/models", nil)
	if err != nil {
		jsonError(w, "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	upReq.Header.Set("Authorization", "Bearer "+h.upstreamAPIKey)
	upReq.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(upReq)
	if err != nil {
		// upstream unreachable — return a static fallback list so the UI still works
		jsonOK(w, http.StatusOK, fallbackModels())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Try to parse OpenAI-style model list
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && len(parsed.Data) > 0 {
		ids := make([]string, 0, len(parsed.Data))
		for _, m := range parsed.Data {
			ids = append(ids, m.ID)
		}
		jsonOK(w, http.StatusOK, map[string][]string{"models": ids})
		return
	}

	// Return fallback if upstream gave something unexpected
	jsonOK(w, http.StatusOK, fallbackModels())
}

func fallbackModels() map[string][]string {
	return map[string][]string{
		"models": {
			"gpt-4.1",
			"gpt-4.1-mini",
			"gpt-4o",
			"gpt-4o-mini",
			"gpt-4-turbo",
			"gpt-3.5-turbo",
			"gpt-5",
			"gpt-5.5",
			"claude-opus-4-5",
			"claude-sonnet-4-5",
			"claude-haiku-4-5",
			"claude-3-7-sonnet-20250219",
			"claude-3-5-sonnet-20241022",
			"claude-3-5-haiku-20241022",
			"gemini-2.5-pro",
			"gemini-2.5-flash",
			"gemini-2.0-flash",
		},
	}
}

// POST /admin/chat — proxies to /v1/chat/completions with streaming support
func (h *ChatHandler) Chat(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		jsonError(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Ensure stream flag is present as requested by the client
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Detect if client wants streaming
	wantsStream := false
	if sv, ok := payload["stream"]; ok {
		_ = json.Unmarshal(sv, &wantsStream)
	}

	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		h.upstreamBaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		jsonError(w, "failed to build request", http.StatusInternalServerError)
		return
	}
	upReq.Header.Set("Authorization", "Bearer "+h.upstreamAPIKey)
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", "text/event-stream")

	resp, err := h.httpClient.Do(upReq)
	if err != nil {
		jsonError(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Pass through content-type and status
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	if wantsStream {
		// Required headers for SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(resp.StatusCode)

		flusher, canFlush := w.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				if canFlush {
					flusher.Flush()
				}
			}
			if readErr != nil {
				break
			}
		}
		return
	}

	// Non-streaming: just forward the response body
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
