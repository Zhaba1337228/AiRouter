package repository

import (
	"context"

	"github.com/airouter/backend/internal/models"
	"github.com/jmoiron/sqlx"
)

type LogRepo struct {
	db *sqlx.DB
}

func NewLogRepo(db *sqlx.DB) *LogRepo {
	return &LogRepo{db: db}
}

func (r *LogRepo) Insert(ctx context.Context, log *models.RequestLog) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO request_logs
			(api_key_id, api_key_prefix, model, endpoint, method, status_code,
			 prompt_tokens, completion_tokens, total_tokens, cost_usd, latency_ms, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`,
		log.APIKeyID, log.APIKeyPrefix, log.Model, log.Endpoint, log.Method,
		log.StatusCode, log.PromptTokens, log.CompletionTokens, log.TotalTokens,
		log.CostUSD, log.LatencyMs, log.ErrorMessage,
	)
	return err
}

func (r *LogRepo) List(ctx context.Context, limit, offset int) ([]*models.RequestLog, error) {
	logs := make([]*models.RequestLog, 0)
	err := r.db.SelectContext(ctx, &logs, `
		SELECT * FROM request_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	return logs, err
}

func (r *LogRepo) Stats(ctx context.Context) (*models.Stats, error) {
	stats := &models.Stats{}

	// Main stats
	err := r.db.QueryRowxContext(ctx, `
		SELECT
			COUNT(*) AS total_requests,
			COUNT(*) FILTER (WHERE status_code < 400) AS success_requests,
			COUNT(*) FILTER (WHERE status_code >= 400) AS error_requests,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			COALESCE(SUM(cost_usd), 0) AS total_cost_usd,
			COALESCE(AVG(latency_ms), 0) AS avg_latency_ms,
			(SELECT COUNT(*) FROM api_keys WHERE is_active = TRUE) AS active_keys
		FROM request_logs
	`).Scan(
		&stats.TotalRequests,
		&stats.SuccessRequests,
		&stats.ErrorRequests,
		&stats.TotalTokens,
		&stats.TotalCostUSD,
		&stats.AvgLatencyMs,
		&stats.ActiveKeys,
	)
	if err != nil {
		return nil, err
	}

	// Derived rates
	if stats.TotalRequests > 0 {
		stats.SuccessRate = float64(stats.SuccessRequests) / float64(stats.TotalRequests) * 100
		stats.ErrorRate = float64(stats.ErrorRequests) / float64(stats.TotalRequests) * 100
		stats.AvgTokensPerReq = float64(stats.TotalTokens) / float64(stats.TotalRequests)
	}

	// Today's stats
	err = r.db.QueryRowxContext(ctx, `
		SELECT
			COUNT(*) AS today_requests,
			COALESCE(SUM(total_tokens), 0) AS today_tokens,
			COALESCE(SUM(cost_usd), 0) AS today_cost_usd
		FROM request_logs
		WHERE created_at >= CURRENT_DATE
	`).Scan(&stats.TodayRequests, &stats.TodayTokens, &stats.TodayCostUSD)
	if err != nil {
		return nil, err
	}

	// Hourly breakdown (last 24h)
	rows, err := r.db.QueryxContext(ctx, `
		SELECT
			EXTRACT(HOUR FROM created_at AT TIME ZONE 'UTC') AS hour,
			COUNT(*) AS requests,
			COALESCE(SUM(total_tokens), 0) AS tokens,
			COALESCE(SUM(cost_usd), 0) AS cost_usd
		FROM request_logs
		WHERE created_at >= NOW() - INTERVAL '24 hours'
		GROUP BY EXTRACT(HOUR FROM created_at AT TIME ZONE 'UTC')
		ORDER BY hour
	`)
	if err != nil {
		return stats, nil // non-fatal
	}
	defer rows.Close()

	for rows.Next() {
		var hour, requests, tokens int64
		var costUsd float64
		if err := rows.Scan(&hour, &requests, &tokens, &costUsd); err == nil {
			if hour >= 0 && hour < 24 {
				stats.HourlyRequests[hour] = requests
				stats.HourlyTokens[hour] = tokens
				stats.HourlyCostUSD[hour] = costUsd
			}
		}
	}

	return stats, nil
}

func (r *LogRepo) StatsByDay(ctx context.Context, days int) ([]map[string]interface{}, error) {
	rows, err := r.db.QueryxContext(ctx, `
		SELECT
			DATE(created_at) AS date,
			COUNT(*) AS requests,
			COALESCE(SUM(total_tokens), 0) AS tokens,
			COALESCE(SUM(cost_usd), 0) AS cost_usd
		FROM request_logs
		WHERE created_at >= NOW() - ($1 * INTERVAL '1 day')
		GROUP BY DATE(created_at)
		ORDER BY date ASC
	`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]map[string]interface{}, 0)
	for rows.Next() {
		m := make(map[string]interface{})
		if err := rows.MapScan(m); err != nil {
			return nil, err
		}
		// convert []byte date to string
		if d, ok := m["date"].([]byte); ok {
			m["date"] = string(d)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
