package models

import "time"

type APIKey struct {
	ID                string     `db:"id"                  json:"id"`
	Name              string     `db:"name"                json:"name"`
	KeyHash           string     `db:"key_hash"            json:"-"`
	KeyPrefix         string     `db:"key_prefix"          json:"key_prefix"`
	IsActive          bool       `db:"is_active"           json:"is_active"`
	CreatedAt         time.Time  `db:"created_at"          json:"created_at"`
	LastUsedAt        *time.Time `db:"last_used_at"        json:"last_used_at"`
	ExpiresAt         *time.Time `db:"expires_at"          json:"expires_at"`
	Note              *string    `db:"note"                json:"note"`
	BudgetUSD         float64    `db:"budget_usd"          json:"budget_usd"`
}

// APIKeyWithUsage is returned by the list endpoint — includes total usage for this key.
type APIKeyWithUsage struct {
	ID           string     `db:"id"            json:"id"`
	Name         string     `db:"name"          json:"name"`
	KeyHash      string     `db:"key_hash"      json:"-"`
	KeyPrefix    string     `db:"key_prefix"    json:"key_prefix"`
	IsActive     bool       `db:"is_active"     json:"is_active"`
	CreatedAt    time.Time  `db:"created_at"    json:"created_at"`
	LastUsedAt   *time.Time `db:"last_used_at"  json:"last_used_at"`
	ExpiresAt    *time.Time `db:"expires_at"    json:"expires_at"`
	Note         *string    `db:"note"          json:"note"`
	BudgetUSD    float64    `db:"budget_usd"    json:"budget_usd"`
	TokensUsed   int64      `db:"tokens_used"   json:"tokens_used"`
	TotalCostUSD float64    `db:"total_cost_usd" json:"total_cost_usd"`
}

// TokenPricePerMillion is the cost in USD per 1 million tokens (input or output)
const TokenPricePerMillion = 0.1

type RequestLog struct {
	ID               int64      `db:"id"                json:"id"`
	APIKeyID         *string    `db:"api_key_id"        json:"api_key_id"`
	APIKeyPrefix     *string    `db:"api_key_prefix"    json:"api_key_prefix"`
	Model            *string    `db:"model"             json:"model"`
	Endpoint         string     `db:"endpoint"          json:"endpoint"`
	Method           string     `db:"method"            json:"method"`
	StatusCode       int        `db:"status_code"       json:"status_code"`
	PromptTokens     int        `db:"prompt_tokens"     json:"prompt_tokens"`
	CompletionTokens int        `db:"completion_tokens" json:"completion_tokens"`
	TotalTokens      int        `db:"total_tokens"      json:"total_tokens"`
	CostUSD          float64    `db:"cost_usd"          json:"cost_usd"`
	LatencyMs        int        `db:"latency_ms"        json:"latency_ms"`
	ErrorMessage     *string    `db:"error_message"     json:"error_message"`
	CreatedAt        time.Time  `db:"created_at"        json:"created_at"`
}

type Stats struct {
	TotalRequests    int64   `json:"total_requests"`
	SuccessRequests  int64   `json:"success_requests"`
	ErrorRequests    int64   `json:"error_requests"`
	TotalTokens      int64   `json:"total_tokens"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	ActiveKeys       int64   `json:"active_keys"`
}
