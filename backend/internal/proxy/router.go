package proxy

import "strings"

// defaultModel is used when the requested model cannot be mapped to anything known.
const defaultModel = "auto"

// modelRoutes maps legacy / alias / SDK-default model names to actual upstream IDs.
// Keys are lower-cased for matching. Values are exact xynera model IDs.
var modelRoutes = map[string]string{
	// ── OpenAI: current aliases ───────────────────────────────────
	"gpt-4o":                  "gpt-4.1",
	"gpt-4o-2024-05-13":       "gpt-4.1",
	"gpt-4o-2024-08-06":       "gpt-4.1",
	"gpt-4o-2024-11-20":       "gpt-4.1",
	"gpt-4o-mini":             "gpt-4.1-mini",
	"gpt-4o-mini-2024-07-18":  "gpt-4.1-mini",
	"gpt-4-turbo":             "gpt-4.1",
	"gpt-4-turbo-preview":     "gpt-4.1",
	"gpt-4-0125-preview":      "gpt-4.1",
	"gpt-4-1106-preview":      "gpt-4.1",
	"gpt-4":                   "gpt-4.1",
	"gpt-4-0613":              "gpt-4.1",
	"gpt-4-32k":               "gpt-4.1",
	"gpt-4.1":                 "gpt-4.1",
	"gpt-4.1-mini":            "gpt-4.1-mini",
	"gpt-3.5-turbo":           "gpt-4.1-mini",
	"gpt-3.5-turbo-16k":       "gpt-4.1-mini",
	"gpt-3.5-turbo-0125":      "gpt-4.1-mini",
	"gpt-3.5-turbo-1106":      "gpt-4.1-mini",
	"o1":                      "gpt-5",
	"o1-preview":              "gpt-5",
	"o1-mini":                 "gpt-5-mini",
	"o3":                      "gpt-5",
	"o3-mini":                 "gpt-5-mini",
	"o4-mini":                 "gpt-5-mini",
	"gpt-5":                   "gpt-5",
	"gpt-5-mini":              "gpt-5-mini",

	// ── Anthropic: SDK defaults & legacy IDs ──────────────────────
	"claude-opus-4-5":                "claude-opus-4-7",
	"claude-opus-4-7":                "claude-opus-4-7",
	"claude-opus-4-0":                "claude-opus-4-7",
	"claude-3-opus-20240229":         "claude-opus-4-7",
	"claude-3-opus-latest":           "claude-opus-4-7",
	"claude-opus-latest":             "claude-opus-4-7",

	"claude-sonnet-4-5":              "claude-4-5-sonnet",
	"claude-4-5-sonnet":              "claude-4-5-sonnet",
	"claude-4-6-sonnet":              "claude-4-6-sonnet",
	"claude-3-5-sonnet-20241022":     "claude-4-5-sonnet",
	"claude-3-5-sonnet-20240620":     "claude-4-5-sonnet",
	"claude-3-5-sonnet-latest":       "claude-4-5-sonnet",
	"claude-3-7-sonnet-20250219":     "claude-4-5-sonnet",
	"claude-3-7-sonnet-latest":       "claude-4-5-sonnet",
	"claude-3-sonnet-20240229":       "claude-4-5-sonnet",
	"claude-sonnet-latest":           "claude-4-5-sonnet",

	"claude-haiku-4-5":               "claude-4-5-haiku",
	"claude-4-5-haiku":               "claude-4-5-haiku",
	"claude-3-5-haiku-20241022":      "claude-4-5-haiku",
	"claude-3-5-haiku-latest":        "claude-4-5-haiku",
	"claude-3-haiku-20240307":        "claude-4-5-haiku",
	"claude-haiku-latest":            "claude-4-5-haiku",

	// ── Gemini ────────────────────────────────────────────────────
	"gemini-pro":              "gemini-2.5-flash",
	"gemini-1.0-pro":         "gemini-2.5-flash",
	"gemini-1.5-pro":         "gemini-3-1-pro",
	"gemini-1.5-pro-latest":  "gemini-3-1-pro",
	"gemini-1.5-flash":       "gemini-2.5-flash",
	"gemini-2.0-flash":       "gemini-2.5-flash",
	"gemini-2.5-flash":       "gemini-2.5-flash",
	"gemini-2.5-pro":         "gemini-3-1-pro",
	"gemini-3-flash":         "gemini-3-flash",
	"gemini-3-1-pro":         "gemini-3-1-pro",
}

// RouteModel maps any incoming model name to the best available upstream model.
// Priority: exact match → prefix/keyword fuzzy → defaultModel.
func RouteModel(model string) string {
	if model == "" || model == "auto" {
		return defaultModel
	}

	lower := strings.ToLower(strings.TrimSpace(model))

	// Exact match (case-insensitive)
	if mapped, ok := modelRoutes[lower]; ok {
		return mapped
	}

	// Fuzzy: Claude family
	if strings.Contains(lower, "claude") {
		switch {
		case strings.Contains(lower, "opus"):
			return "claude-opus-4-7"
		case strings.Contains(lower, "sonnet"):
			return "claude-4-5-sonnet"
		case strings.Contains(lower, "haiku"):
			return "claude-4-5-haiku"
		default:
			return "claude-4-5-sonnet" // default Claude → Sonnet
		}
	}

	// Fuzzy: OpenAI family
	if strings.Contains(lower, "gpt") || strings.Contains(lower, "openai") {
		if strings.Contains(lower, "mini") || strings.Contains(lower, "3.5") || strings.Contains(lower, "3-5") {
			return "gpt-4.1-mini"
		}
		return "gpt-4.1"
	}

	// Fuzzy: o-series reasoning
	if strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4") {
		if strings.Contains(lower, "mini") {
			return "gpt-5-mini"
		}
		return "gpt-5"
	}

	// Fuzzy: Gemini family
	if strings.Contains(lower, "gemini") {
		if strings.Contains(lower, "pro") || strings.Contains(lower, "1.5") {
			return "gemini-3-1-pro"
		}
		return "gemini-2.5-flash"
	}

	// Fuzzy: Mistral / Mixtral → auto router
	if strings.Contains(lower, "mistral") || strings.Contains(lower, "mixtral") ||
		strings.Contains(lower, "llama") || strings.Contains(lower, "deepseek") {
		return defaultModel
	}

	return defaultModel
}
