package proxy

import (
	"fmt"
	"strings"
)

// knownModels is the set of model IDs that openlimits.app actually supports.
// Any model not in this set gets a 400 error instead of silent remapping.
var knownModels = map[string]bool{
	// openlimits.app supported models (verified)
	"claude-sonnet-4-6":           true,
	"claude-opus-4-6":             true,
	"claude-haiku-4-5":            true,
	"claude-sonnet-4-5":           true,
	"claude-opus-4-5":             true,
	"claude-opus-4-1":             true,
	"claude-sonnet-4-0":           true,
	"claude-opus-4-0":             true,
	"claude-haiku-4-5-20251001":   true,
	"claude-sonnet-4-5-20250929":  true,
	"claude-opus-4-5-20251101":    true,
	"claude-sonnet-4-20250514":    true,
	"claude-opus-4-20250514":      true,
	// Codex via /v1/responses
	"gpt-5-codex":        true,
	"gpt-5.1-codex":      true,
	"gpt-5.2-codex":      true,
	"gpt-5.3-codex":      true,
	"gpt-5-codex-mini":   true,
	"gpt-5.1-codex-mini": true,
}

// knownModelPrefixes matches model names by prefix for variants not listed above.
var knownModelPrefixes = []string{
	"claude-sonnet-4-6",
	"claude-opus-4-6",
	"claude-haiku-4-5",
	"claude-sonnet-4-5",
	"claude-opus-4-5",
	"claude-opus-4-1",
	"claude-sonnet-4-0",
	"claude-opus-4-0",
	"claude-",
	"gpt-5-codex",
}

// defaultModel is the legacy fallback used only when the requested model
// is known-Claude-but-not-in-table (e.g. unknown dated snapshot → latest in line).
// It is NOT used for unknown/unrecognized models — those return an error.
const defaultModel = "claude-sonnet-4-6"

// modelRoutes maps incoming model names (lower-cased) to openlimits.app model IDs.
// Upstream: https://openlimits.app — Anthropic-native API proxy.
var modelRoutes = map[string]string{

	// ── Anthropic: openlimits.app native names (pass-through) ─────
	"claude-sonnet-4-6":           "claude-sonnet-4-6", // latest Sonnet
	"claude-opus-4-6":             "claude-opus-4-6",   // latest Opus
	"claude-haiku-4-5":            "claude-haiku-4-5",  // latest Haiku
	"claude-sonnet-4-5":           "claude-sonnet-4-5",
	"claude-opus-4-5":             "claude-opus-4-5",
	"claude-opus-4-1":             "claude-opus-4-1",
	"claude-sonnet-4-0":           "claude-sonnet-4-0",
	"claude-opus-4-0":             "claude-opus-4-0",
	"claude-haiku-4-5-20251001":   "claude-haiku-4-5-20251001",
	"claude-sonnet-4-5-20250929":  "claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101":    "claude-opus-4-5-20251101",
	"claude-sonnet-4-20250514":    "claude-sonnet-4-20250514",
	"claude-opus-4-20250514":      "claude-opus-4-20250514",

	// ── Anthropic: thinking variants sent by Claude Code ──────────
	"claude-sonnet-4-6-thinking-1m": "claude-sonnet-4-6",
	"claude-sonnet-4-6-thinking":    "claude-sonnet-4-6",

	// ── Anthropic: old xynera-style names → openlimits names ──────
	"claude-4-6-sonnet": "claude-sonnet-4-6",
	"claude-4-5-sonnet": "claude-sonnet-4-5",
	"claude-4-5-haiku":  "claude-haiku-4-5",
	"claude-opus-4-7":   "claude-opus-4-6",

	// ── Anthropic: SDK defaults & legacy IDs ──────────────────────
	"claude-sonnet-latest":       "claude-sonnet-4-6",
	"claude-opus-latest":         "claude-opus-4-6",
	"claude-haiku-latest":        "claude-haiku-4-5",
	"claude-3-7-sonnet-20250219": "claude-sonnet-4-5",
	"claude-3-7-sonnet-latest":   "claude-sonnet-4-5",
	"claude-3-5-sonnet-20241022": "claude-sonnet-4-5",
	"claude-3-5-sonnet-20240620": "claude-sonnet-4-5",
	"claude-3-5-sonnet-latest":   "claude-sonnet-4-5",
	"claude-3-sonnet-20240229":   "claude-sonnet-4-5",
	"claude-3-5-haiku-20241022":  "claude-haiku-4-5",
	"claude-3-5-haiku-latest":    "claude-haiku-4-5",
	"claude-3-haiku-20240307":    "claude-haiku-4-5",
	"claude-3-opus-20240229":     "claude-opus-4-5",
	"claude-3-opus-latest":       "claude-opus-4-6",
	"claude-opus-4-5-latest":     "claude-opus-4-6",
	"claude-sonnet-4-5-latest":   "claude-sonnet-4-6",

	// ── Codex / GPT — available via /v1/responses on openlimits ───
	"gpt-5-codex":        "gpt-5-codex",
	"gpt-5.1-codex":      "gpt-5.1-codex",
	"gpt-5.2-codex":      "gpt-5.2-codex",
	"gpt-5.3-codex":      "gpt-5.3-codex",
	"gpt-5-codex-mini":   "gpt-5-codex-mini",
	"gpt-5.1-codex-mini": "gpt-5.1-codex-mini",
}

// RouteModel maps an incoming model name to an openlimits.app model ID.
// Returns an error for unknown/unrecognized models (e.g. fake models,
// deepseek-chat, gpt-4, etc.) instead of silently mapping them to Claude.
func RouteModel(model string) (string, error) {
	if model == "" {
		return "", fmt.Errorf("model is required")
	}

	lower := strings.ToLower(strings.TrimSpace(model))

	// Explicit route in table
	if mapped, ok := modelRoutes[lower]; ok {
		return mapped, nil
	}

	// Known Claude family — fuzzy map to latest in line
	if strings.Contains(lower, "claude") {
		switch {
		case strings.Contains(lower, "opus"):
			return "claude-opus-4-6", nil
		case strings.Contains(lower, "haiku"):
			return "claude-haiku-4-5", nil
		case strings.Contains(lower, "sonnet"):
			if strings.Contains(lower, "4-5") {
				return "claude-sonnet-4-5", nil
			}
			return "claude-sonnet-4-6", nil
		default:
			return "claude-sonnet-4-6", nil
		}
	}

	// Known Codex family
	if strings.Contains(lower, "codex") {
		if strings.Contains(lower, "mini") {
			return "gpt-5-codex-mini", nil
		}
		return "gpt-5-codex", nil
	}

	// Completely unknown model — reject instead of silent mapping to Claude
	return "", fmt.Errorf("unsupported model: %q", model)
}

// isKnownModel returns true if the model is recognized as a valid Claude/Codex model.
func isKnownModel(model string) bool {
	if model == "" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(model))
	if knownModels[lower] {
		return true
	}
	for _, prefix := range knownModelPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
