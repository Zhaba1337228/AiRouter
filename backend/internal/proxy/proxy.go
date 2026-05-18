package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/airouter/backend/internal/middleware"
	"github.com/airouter/backend/internal/models"
	"github.com/airouter/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

const settingsCacheKey = "settings:compression_mode"
const settingsCacheTTL = 15 * time.Second

const providerCacheKeyBaseURL = "provider:default:base_url"
const providerCacheKeyAPIKey = "provider:default:api_key"
const providerCacheTTL = 15 * time.Second

type Handler struct {
	upstreamBaseURL string
	upstreamAPIKey  string
	logRepo         *repository.LogRepo
	settingsRepo    *repository.SettingsRepo
	providerRepo    *repository.ProviderRepo
	rdb             *redis.Client
	httpClient      *http.Client
}

func NewHandler(
	upstreamBaseURL, upstreamAPIKey string,
	logRepo *repository.LogRepo,
	settingsRepo *repository.SettingsRepo,
	providerRepo *repository.ProviderRepo,
	rdb *redis.Client,
) *Handler {
	// Tuned transport: persistent connections, no local decompression,
	// short dial/TLS timeouts so we fail-fast on network issues.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 60 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 0, // rely on request context timeout
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   256,
		MaxConnsPerHost:       0, // unlimited
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true, // upstream response piped as-is
		ForceAttemptHTTP2:     true,
	}
	return &Handler{
		upstreamBaseURL: strings.TrimRight(upstreamBaseURL, "/"),
		upstreamAPIKey:  upstreamAPIKey,
		logRepo:         logRepo,
		settingsRepo:    settingsRepo,
		providerRepo:    providerRepo,
		rdb:             rdb,
		httpClient: &http.Client{
			Timeout:   120 * time.Second,
			Transport: transport,
		},
	}
}

// activeProvider returns the base URL and API key to use for the current request.
// Priority: default provider from DB (cached) → env fallback.
func (h *Handler) activeProvider(ctx context.Context) (baseURL, apiKey string) {
	// Try Redis cache first
	if h.rdb != nil {
		cachedURL, errU := h.rdb.Get(ctx, providerCacheKeyBaseURL).Result()
		cachedKey, errK := h.rdb.Get(ctx, providerCacheKeyAPIKey).Result()
		if errU == nil && errK == nil && cachedURL != "" {
			return strings.TrimRight(cachedURL, "/"), cachedKey
		}
	}
	// Try DB
	if h.providerRepo != nil {
		if p, err := h.providerRepo.GetDefault(ctx); err == nil {
			if h.rdb != nil {
				h.rdb.SetEx(ctx, providerCacheKeyBaseURL, p.BaseURL, providerCacheTTL)
				h.rdb.SetEx(ctx, providerCacheKeyAPIKey, p.APIKey, providerCacheTTL)
			}
			return strings.TrimRight(p.BaseURL, "/"), p.APIKey
		}
	}
	// Fallback to env/config values
	return h.upstreamBaseURL, h.upstreamAPIKey
}

// compressionMode returns the current mode, reading from Redis cache first.
func (h *Handler) compressionMode(ctx context.Context) CompressionMode {
	if h.rdb != nil {
		if v, err := h.rdb.Get(ctx, settingsCacheKey).Result(); err == nil {
			return ParseMode(v)
		}
	}
	if h.settingsRepo != nil {
		if v, err := h.settingsRepo.Get(ctx, "compression_mode"); err == nil && v != "" {
			if h.rdb != nil {
				h.rdb.SetEx(ctx, settingsCacheKey, v, settingsCacheTTL)
			}
			return ParseMode(v)
		}
	}
	return ModeStandard
}

// Proxy forwards any request to upstream, replacing auth headers with our upstream key.
// Streaming responses (SSE) are piped in real-time while being buffered for logging.
func (h *Handler) Proxy(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MB limit
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	upstreamBaseURL, upstreamAPIKey := h.activeProvider(r.Context())
	upstreamURL := upstreamBaseURL + r.URL.Path
	if r.URL.RawQuery != "" {
		// Strip Anthropic SDK-specific query params that upstreams don't support.
		// e.g. ?beta=true is appended by Claude Code to signal beta feature use;
		// xynera returns an empty 400 when it receives unknown query parameters.
		q := r.URL.Query()
		q.Del("beta")
		if encoded := q.Encode(); encoded != "" {
			upstreamURL += "?" + encoded
		}
	}

	// Rewrite body: route model + compress context
	mode := h.compressionMode(r.Context())
	body, rewriteErr := rewriteRequest(body, mode)
	if rewriteErr != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, rewriteErr.Error()), http.StatusBadRequest)
		return
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":"failed to create upstream request"}`, http.StatusInternalServerError)
		return
	}

	// Forward all client headers except hop-by-hop and auth (which we replace).
	// This preserves Anthropic-Beta (multi-value, incl. interleaved-thinking),
	// MCP headers, and any future SDK headers without needing an explicit whitelist.
	// openlimits.app supports extended thinking natively.
	for k, vv := range r.Header {
		switch k {
		case "Authorization", "X-Api-Key", "X-API-Key",
			"Connection", "Keep-Alive", "Proxy-Authenticate",
			"Proxy-Authorization", "Te", "Trailers",
			"Transfer-Encoding", "Upgrade", "Content-Length":
			continue
		}
		for _, v := range vv {
			upReq.Header.Add(k, v)
		}
	}

	// Replace auth with our upstream key (both formats for compatibility)
	upReq.Header.Set("Authorization", "Bearer "+upstreamAPIKey)
	upReq.Header.Set("X-Api-Key", upstreamAPIKey)

	resp, err := h.httpClient.Do(upReq)
	latencyMs := int(time.Since(startTime).Milliseconds())

	if err != nil {
		errMsg := err.Error()
		h.asyncLog(r, body, nil, http.StatusBadGateway, latencyMs, &errMsg)
		http.Error(w, fmt.Sprintf(`{"error":"upstream error: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	// Detect streaming response — only enter SSE path when client asked for streaming
	// AND upstream actually returns SSE. This prevents ignoring stream:false.
	// Error responses (4xx/5xx) are always plain JSON.
	clientWantsStream := isStreamRequest(body)
	upstreamSSE := resp.StatusCode < 300 && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	if clientWantsStream && upstreamSSE {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(resp.StatusCode)

		flusher, _ := w.(http.Flusher)
		// StreamConvertSSE pipes upstream → client, converting any
		// [tool_call]...[/tool_call] envelopes into native Anthropic
		// tool_use SSE events on the fly.
		captured := StreamConvertSSE(w, flusher, resp.Body)
		h.asyncLog(r, body, captured, resp.StatusCode, latencyMs, nil)
		return
	}

	// Client asked for stream:false but upstream sent SSE — buffer, convert, return JSON.
	if !clientWantsStream && upstreamSSE {
		var captured bytes.Buffer
		flusher := &dummyFlusher{}
		StreamConvertSSE(&captured, flusher, resp.Body)

		respBody := convertSSEToJSONResponse(captured.Bytes())
		if resp.StatusCode >= 400 {
			log.Printf("upstream error %d for %s %s — body: %s",
				resp.StatusCode, r.Method, r.URL.Path, truncateLog(respBody, 512))
		}
		converted := ConvertResponse(respBody)
		if len(converted) != len(respBody) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(converted)))
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(converted)
		h.asyncLog(r, body, converted, resp.StatusCode, latencyMs, nil)
		return
	}

	// Non-streaming: buffer, convert text-tagged tool calls, forward.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read upstream response"}`, http.StatusBadGateway)
		return
	}
	if resp.StatusCode >= 400 {
		log.Printf("upstream error %d for %s %s — body: %s",
			resp.StatusCode, r.Method, r.URL.Path, truncateLog(respBody, 512))
	}
	converted := ConvertResponse(respBody)
	if len(converted) != len(respBody) {
		// Body was rewritten — fix Content-Length so chunked clients don't truncate.
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(converted)))
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(converted)
	h.asyncLog(r, body, converted, resp.StatusCode, latencyMs, nil)
}

// rewriteRequest applies model routing, strips unsupported params, then
// applies context compression.
// Returns (rewrittenBody, error). Body is only returned if error is nil.
func rewriteRequest(body []byte, mode CompressionMode) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil
	}

	changed := false

	// Route model — reject unknown models instead of silent mapping
	if modelRaw, ok := payload["model"]; ok {
		var model string
		if json.Unmarshal(modelRaw, &model) == nil && model != "" {
			routed, err := RouteModel(model)
			if err != nil {
				return nil, err
			}
			if routed != model {
				payload["model"], _ = json.Marshal(routed)
				changed = true
			}
		}
	}

	// Strip thinking parameter — when enabled it consumes max_tokens budget
	// and causes truncated responses with short token limits.
	if _, ok := payload["thinking"]; ok {
		delete(payload, "thinking")
		changed = true
	}

	if changed {
		if b, err := json.Marshal(payload); err == nil {
			body = b
		}
	}

	// Compress context with active mode
	return CompressBody(body, mode), nil
}

// isStreamRequest checks if the request body asks for streaming
func isStreamRequest(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	raw, ok := m["stream"]
	if !ok {
		return false
	}
	var v bool
	return json.Unmarshal(raw, &v) == nil && v
}

func (h *Handler) asyncLog(r *http.Request, reqBody, respBody []byte, statusCode, latencyMs int, errMsg *string) {
	go func() {
		log := &models.RequestLog{
			Endpoint:   r.URL.Path,
			Method:     r.Method,
			StatusCode: statusCode,
			LatencyMs:  latencyMs,
		}

		if key, ok := r.Context().Value(middleware.APIKeyContextKey).(*models.APIKey); ok {
			log.APIKeyID = &key.ID
			log.APIKeyPrefix = &key.KeyPrefix
		}

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

// extractTokens handles both JSON (non-streaming) and SSE (streaming) responses
// for both OpenAI and Anthropic API formats.
func extractTokens(body []byte, log *models.RequestLog) {
	if len(body) == 0 {
		return
	}

	// Try plain JSON first (non-streaming response)
	if tryExtractJSON(body, log) {
		return
	}

	// Fall back to SSE line-by-line scan
	tryExtractSSE(body, log)
}

// tryExtractJSON handles non-streaming JSON responses.
func tryExtractJSON(body []byte, log *models.RequestLog) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}

	usageRaw, ok := payload["usage"]
	if !ok {
		return false
	}

	var usage struct {
		// OpenAI
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		// Anthropic
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	if err := json.Unmarshal(usageRaw, &usage); err != nil {
		return false
	}

	if usage.TotalTokens > 0 {
		log.PromptTokens = usage.PromptTokens
		log.CompletionTokens = usage.CompletionTokens
		log.TotalTokens = usage.TotalTokens
	} else if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		log.PromptTokens = usage.InputTokens
		log.CompletionTokens = usage.OutputTokens
		log.TotalTokens = usage.InputTokens + usage.OutputTokens
	} else {
		return false
	}

	log.CostUSD = float64(log.TotalTokens) / 1_000_000 * models.TokenPricePerMillion
	return true
}

// tryExtractSSE scans SSE lines for token usage.
//
// OpenAI streaming (stream_options.include_usage=true) — last data chunk before [DONE]:
//
//	data: {"choices":[],"usage":{"prompt_tokens":N,"completion_tokens":N,"total_tokens":N}}
//
// Anthropic streaming:
//
//	event: message_start
//	data: {"type":"message_start","message":{"usage":{"input_tokens":N,"output_tokens":0}}}
//	event: message_delta
//	data: {"type":"message_delta","usage":{"output_tokens":N}}
func tryExtractSSE(body []byte, log *models.RequestLog) {
	var (
		promptTokens     int
		completionTokens int
		totalTokens      int
	)

	scanner := bufio.NewScanner(bytes.NewReader(body))
	// SSE `data:` lines can be very large (system prompt echo in message_start,
	// big tool_result echo, etc.). The default 64 KB scanner buffer silently
	// drops the rest of the stream when one line exceeds it — taking the
	// usage event with it. Match the size we already use in StreamConvertSSE.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" || data == "" {
			continue
		}

		var chunk map[string]json.RawMessage
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// ── OpenAI: top-level "usage" field ──────────────────────────
		if usageRaw, ok := chunk["usage"]; ok && usageRaw != nil && string(usageRaw) != "null" {
			var u struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
				// Anthropic message_delta carries output_tokens here too
				OutputTokens int `json:"output_tokens"`
				InputTokens  int `json:"input_tokens"`
			}
			if err := json.Unmarshal(usageRaw, &u); err == nil {
				if u.TotalTokens > 0 {
					promptTokens = u.PromptTokens
					completionTokens = u.CompletionTokens
					totalTokens = u.TotalTokens
				}
				// Anthropic message_delta: {"type":"message_delta","usage":{"output_tokens":N}}
				if u.OutputTokens > 0 {
					completionTokens = u.OutputTokens
				}
				// Anthropic message_start nested usage
				if u.InputTokens > 0 {
					promptTokens = u.InputTokens
				}
			}
		}

		// ── Anthropic: {"type":"message_start","message":{"usage":{...}}} ──
		if typeRaw, ok := chunk["type"]; ok {
			var msgType string
			if json.Unmarshal(typeRaw, &msgType) == nil && msgType == "message_start" {
				if msgRaw, ok := chunk["message"]; ok {
					var msg struct {
						Usage struct {
							InputTokens  int `json:"input_tokens"`
							OutputTokens int `json:"output_tokens"`
						} `json:"usage"`
					}
					if json.Unmarshal(msgRaw, &msg) == nil {
						promptTokens = msg.Usage.InputTokens
						if msg.Usage.OutputTokens > 0 {
							completionTokens = msg.Usage.OutputTokens
						}
					}
				}
			}
		}
	}

	// Regex fallback for cases where line-scanning missed the usage event:
	// truncated streams, long single lines that broke even our enlarged buffer,
	// non-LF-terminated chunked frames, etc. Picks the largest seen value of
	// each — matches the "last write wins / cumulative output" semantics of
	// the structured parser above.
	if promptTokens == 0 {
		if v := largestIntField(body, "input_tokens"); v > 0 {
			promptTokens = v
		} else if v := largestIntField(body, "prompt_tokens"); v > 0 {
			promptTokens = v
		}
	}
	if completionTokens == 0 {
		if v := largestIntField(body, "output_tokens"); v > 0 {
			completionTokens = v
		} else if v := largestIntField(body, "completion_tokens"); v > 0 {
			completionTokens = v
		}
	}

	if totalTokens > 0 {
		log.PromptTokens = promptTokens
		log.CompletionTokens = completionTokens
		log.TotalTokens = totalTokens
		log.CostUSD = float64(log.TotalTokens) / 1_000_000 * models.TokenPricePerMillion
	} else if promptTokens > 0 || completionTokens > 0 {
		log.PromptTokens = promptTokens
		log.CompletionTokens = completionTokens
		log.TotalTokens = promptTokens + completionTokens
		log.CostUSD = float64(log.TotalTokens) / 1_000_000 * models.TokenPricePerMillion
	}
}

// truncateLog returns at most maxBytes of body as a string, appending "…" if trimmed.
func truncateLog(b []byte, maxBytes int) string {
	if len(b) == 0 {
		return "(empty)"
	}
	if len(b) <= maxBytes {
		return string(b)
	}
	return string(b[:maxBytes]) + "…"
}

// largestIntField finds occurrences of `"<field>": <int>` in `body` and returns
// the largest one. Used as a structure-agnostic fallback for usage extraction
// when SSE line-scanning fails (huge lines, half-frames, etc.).
var usageFieldRe = regexp.MustCompile(`"([a-z_]+)"\s*:\s*(\d+)`)

func largestIntField(body []byte, field string) int {
	max := 0
	for _, m := range usageFieldRe.FindAllSubmatch(body, -1) {
		if string(m[1]) != field {
			continue
		}
		n, err := strconv.Atoi(string(m[2]))
		if err == nil && n > max {
			max = n
		}
	}
	return max
}

// dummyFlusher is a no-op flusher used when buffering SSE responses for conversion.
type dummyFlusher struct{}

func (d *dummyFlusher) Flush() {}

// convertSSEToJSONResponse converts a buffered SSE stream (already processed by
// StreamConvertSSE to emit canonical Anthropic SSE) into a non-streaming
// OpenAI /v1/chat/completions response by replaying the captured events.
func convertSSEToJSONResponse(sse []byte) []byte {
	type textPart struct {
		Text string `json:"text,omitempty"`
	}
	type toolUsePart struct {
		ID    string          `json:"id"`
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	type message struct {
		Role      string           `json:"role"`
		Content   []json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason,omitempty"`
	}
	type usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		InputTokens      int `json:"input_tokens,omitempty"`
		OutputTokens     int `json:"output_tokens,omitempty"`
	}

	var parts []json.RawMessage
	var stopReason string
	var msgUsage usage

	scanner := bufio.NewScanner(bytes.NewReader(sse))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" || data == "" {
			continue
		}
		var ev map[string]json.RawMessage
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		var typ string
		json.Unmarshal(ev["type"], &typ)

		switch typ {
		case "content_block_start":
			var info struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			json.Unmarshal([]byte(data), &info)
			if info.ContentBlock.Type == "tool_use" {
				tu, _ := json.Marshal(map[string]any{
					"type": "tool_use",
					"id":   info.ContentBlock.ID,
					"name": info.ContentBlock.Name,
				})
				parts = append(parts, tu)
			}

		case "content_block_delta":
			var info struct {
				Index int `json:"index"`
				Delta struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			json.Unmarshal([]byte(data), &info)
			if info.Delta.Type == "text_delta" && info.Delta.Text != "" {
				parts = append(parts, marshalMap(map[string]any{"type": "text", "text": info.Delta.Text}))
			}

		case "message_delta":
			var info struct {
				Index int `json:"index"`
				Delta struct {
					StopReason  string `json:"stop_reason"`
					StopSequence *string `json:"stop_sequence"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			json.Unmarshal([]byte(data), &info)
			if info.Delta.StopReason != "" {
				stopReason = info.Delta.StopReason
			}
			if info.Usage.OutputTokens > 0 {
				msgUsage.OutputTokens = info.Usage.OutputTokens
			}

		case "message_start":
			var info struct {
				Message struct {
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			json.Unmarshal([]byte(data), &info)
			if info.Message.Usage.InputTokens > 0 {
				msgUsage.InputTokens = info.Message.Usage.InputTokens
			}
			if info.Message.Usage.OutputTokens > 0 {
				msgUsage.OutputTokens = info.Message.Usage.OutputTokens
			}
		}
	}

	if parts == nil {
		parts = []json.RawMessage{}
	}

	// Build OpenAI-style chat.completions response
	resp := map[string]any{
		"id":      "chatcmpl-" + randomID(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "", // filled by caller if needed
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": assembleContent(parts),
			},
			"finish_reason": stopReason,
		}},
		"usage": map[string]int{
			"prompt_tokens":     msgUsage.InputTokens,
			"completion_tokens": msgUsage.OutputTokens,
			"total_tokens":      msgUsage.InputTokens + msgUsage.OutputTokens,
		},
	}
	out, _ := json.Marshal(resp)
	return out
}

// assembleContent converts Anthropic content blocks back to a plain text string
// for the OpenAI-compatible response format.
func assembleContent(parts []json.RawMessage) string {
	var b strings.Builder
	for _, p := range parts {
		var block map[string]json.RawMessage
		if json.Unmarshal(p, &block) != nil {
			continue
		}
		var typ string
		json.Unmarshal(block["type"], &typ)
		switch typ {
		case "text":
			if t, ok := block["text"]; ok {
				var s string
				json.Unmarshal(t, &s)
				b.WriteString(s)
			}
		case "tool_use":
			if name, ok := block["name"]; ok {
				var n string
				json.Unmarshal(name, &n)
				b.WriteString("[tool_call]{\"name\":\"" + n + "\"}[/tool_call]")
			}
		}
	}
	return b.String()
}

func marshalMap(m map[string]any) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
