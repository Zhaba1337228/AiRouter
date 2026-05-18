package proxy

import "strings"

// defaultModel is the fallback when the requested model cannot be mapped.
// openlimits.app default: latest Sonnet.
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

// RouteModel maps any incoming model name to the best available upstream model.
// Priority: exact match (case-insensitive) → fuzzy keyword → defaultModel.
func RouteModel(model string) string {
	if model == "" {
		return defaultModel
	}

	lower := strings.ToLower(strings.TrimSpace(model))

	// Exact match
	if mapped, ok := modelRoutes[lower]; ok {
		return mapped
	}

	// Fuzzy: Claude family — map to latest openlimits.app model in each line
	if strings.Contains(lower, "claude") {
		switch {
		case strings.Contains(lower, "opus"):
			return "claude-opus-4-6"
		case strings.Contains(lower, "haiku"):
			return "claude-haiku-4-5"
		case strings.Contains(lower, "sonnet"):
			if strings.Contains(lower, "4-5") {
				return "claude-sonnet-4-5"
			}
			return "claude-sonnet-4-6"
		default:
			return "claude-sonnet-4-6"
		}
	}

	// Fuzzy: Codex family
	if strings.Contains(lower, "codex") {
		if strings.Contains(lower, "mini") {
			return "gpt-5-codex-mini"
		}
		return "gpt-5-codex"
	}

	return defaultModel
}
