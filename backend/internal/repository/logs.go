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
	return stats, err
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
