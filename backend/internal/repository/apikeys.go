package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/airouter/backend/internal/models"
	"github.com/jmoiron/sqlx"
)

type APIKeyRepo struct {
	db *sqlx.DB
}

func NewAPIKeyRepo(db *sqlx.DB) *APIKeyRepo {
	return &APIKeyRepo{db: db}
}

// GenerateKey creates a new random API key in format "ar-<random32hex>"
func GenerateKey() (plaintext, hash, prefix string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return
	}
	plaintext = "ar-" + hex.EncodeToString(b)
	prefix = plaintext[:10] + "..."

	sum := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(sum[:])
	return
}

type CreateKeyInput struct {
	Name      string
	Note      *string
	ExpiresAt *time.Time
	BudgetUSD float64
}

func (r *APIKeyRepo) Create(ctx context.Context, input CreateKeyInput) (key *models.APIKey, plaintext string, err error) {
	plain, hash, prefix, err := GenerateKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate key: %w", err)
	}

	key = &models.APIKey{}
	err = r.db.QueryRowxContext(ctx, `
		INSERT INTO api_keys (name, key_hash, key_prefix, note, expires_at, budget_usd)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note, budget_usd
	`, input.Name, hash, prefix, input.Note, input.ExpiresAt, input.BudgetUSD).StructScan(key)
	if err != nil {
		return nil, "", fmt.Errorf("insert api key: %w", err)
	}

	return key, plain, nil
}

// List returns all keys with their total token usage and cost.
func (r *APIKeyRepo) List(ctx context.Context) ([]*models.APIKeyWithUsage, error) {
	keys := make([]*models.APIKeyWithUsage, 0)
	err := r.db.SelectContext(ctx, &keys, `
		SELECT
			ak.id, ak.name, ak.key_hash, ak.key_prefix, ak.is_active,
			ak.created_at, ak.last_used_at, ak.expires_at, ak.note, ak.budget_usd,
			COALESCE(u.tokens_used, 0)    AS tokens_used,
			COALESCE(u.total_cost_usd, 0) AS total_cost_usd
		FROM api_keys ak
		LEFT JOIN (
			SELECT api_key_id,
			       SUM(total_tokens) AS tokens_used,
			       SUM(cost_usd)     AS total_cost_usd
			FROM request_logs
			GROUP BY api_key_id
		) u ON u.api_key_id = ak.id
		ORDER BY ak.created_at DESC
	`)
	return keys, err
}

func (r *APIKeyRepo) GetByID(ctx context.Context, id string) (*models.APIKey, error) {
	key := &models.APIKey{}
	err := r.db.GetContext(ctx, key, `
		SELECT id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note, budget_usd
		FROM api_keys WHERE id = $1
	`, id)
	return key, err
}

func (r *APIKeyRepo) ValidateKey(ctx context.Context, plaintext string) (*models.APIKey, error) {
	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])

	key := &models.APIKey{}
	err := r.db.GetContext(ctx, key, `
		SELECT id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note, budget_usd
		FROM api_keys
		WHERE key_hash = $1
		  AND is_active = TRUE
		  AND (expires_at IS NULL OR expires_at > NOW())
	`, hash)
	if err != nil {
		return nil, err
	}

	go func() {
		_, _ = r.db.Exec(`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, key.ID)
	}()

	return key, nil
}

// TotalCostSpent returns the total USD spent by a key across all requests.
func (r *APIKeyRepo) TotalCostSpent(ctx context.Context, keyID string) (float64, error) {
	var spent float64
	err := r.db.QueryRowxContext(ctx, `
		SELECT COALESCE(SUM(cost_usd), 0)
		FROM request_logs
		WHERE api_key_id = $1
	`, keyID).Scan(&spent)
	return spent, err
}

func (r *APIKeyRepo) SetActive(ctx context.Context, id string, active bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET is_active = $1 WHERE id = $2`, active, id)
	return err
}

func (r *APIKeyRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	return err
}
