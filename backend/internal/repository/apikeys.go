package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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
	Name         string
	Note         *string
	ExpiresAt    *time.Time
	// TokenLimit is the max total tokens (raw count, 0 = unlimited).
	TokenLimit   int64
	// RequestLimit is the max total API requests (0 = unlimited).
	RequestLimit int64
}

func (r *APIKeyRepo) Create(ctx context.Context, input CreateKeyInput) (key *models.APIKey, plaintext string, err error) {
	plain, hash, prefix, err := GenerateKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate key: %w", err)
	}

	key = &models.APIKey{}
	err = r.db.QueryRowxContext(ctx, `
		INSERT INTO api_keys (name, key_hash, key_prefix, note, expires_at, token_limit, request_limit)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note,
		          token_limit, request_limit
	`, input.Name, hash, prefix, input.Note, input.ExpiresAt, input.TokenLimit, input.RequestLimit).StructScan(key)
	if err != nil {
		return nil, "", fmt.Errorf("insert api key: %w", err)
	}

	return key, plain, nil
}

// List returns all keys with their live usage counters.
func (r *APIKeyRepo) List(ctx context.Context) ([]*models.APIKeyWithUsage, error) {
	keys := make([]*models.APIKeyWithUsage, 0)
	err := r.db.SelectContext(ctx, &keys, `
		SELECT
			ak.id, ak.name, ak.key_hash, ak.key_prefix, ak.is_active,
			ak.created_at, ak.last_used_at, ak.expires_at, ak.note,
			ak.token_limit, ak.request_limit,
			COALESCE(u.tokens_used,    0) AS tokens_used,
			COALESCE(u.requests_count, 0) AS requests_count,
			COALESCE(u.total_cost_usd, 0) AS total_cost_usd
		FROM api_keys ak
		LEFT JOIN (
			SELECT api_key_id,
			       SUM(total_tokens) AS tokens_used,
			       COUNT(*)          AS requests_count,
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
		SELECT id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note,
		       token_limit, request_limit
		FROM api_keys WHERE id = $1
	`, id)
	return key, err
}

func (r *APIKeyRepo) ValidateKey(ctx context.Context, plaintext string) (*models.APIKey, error) {
	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])

	key := &models.APIKey{}
	err := r.db.GetContext(ctx, key, `
		SELECT id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note,
		       token_limit, request_limit
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

// TotalTokensUsed returns the total tokens consumed by a key across all requests.
func (r *APIKeyRepo) TotalTokensUsed(ctx context.Context, keyID string) (int64, error) {
	var n int64
	err := r.db.QueryRowxContext(ctx, `
		SELECT COALESCE(SUM(total_tokens), 0)
		FROM request_logs
		WHERE api_key_id = $1
	`, keyID).Scan(&n)
	return n, err
}

// TotalRequestsCount returns the total number of requests made by a key.
func (r *APIKeyRepo) TotalRequestsCount(ctx context.Context, keyID string) (int64, error) {
	var n int64
	err := r.db.QueryRowxContext(ctx, `
		SELECT COUNT(*)
		FROM request_logs
		WHERE api_key_id = $1
	`, keyID).Scan(&n)
	return n, err
}

func (r *APIKeyRepo) SetActive(ctx context.Context, id string, active bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET is_active = $1 WHERE id = $2`, active, id)
	return err
}

func (r *APIKeyRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	return err
}

// UpdateFields contains the editable fields for an API key.
type UpdateKeyInput struct {
	Name         *string
	Note         *string
	ExpiresAt    *time.Time
	ClearExpiry  bool       // if true, sets expires_at to NULL
	TokenLimit   *int64
	RequestLimit *int64
	IsActive     *bool
}

// Update modifies the editable fields of an existing API key.
func (r *APIKeyRepo) Update(ctx context.Context, id string, input UpdateKeyInput) error {
	// Build dynamic SET clause
	var args []interface{}
	argIdx := 1
	setClauses := []string{}

	if input.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *input.Name)
		argIdx++
	}
	if input.Note != nil {
		setClauses = append(setClauses, fmt.Sprintf("note = $%d", argIdx))
		args = append(args, *input.Note)
		argIdx++
	}
	if input.ClearExpiry {
		setClauses = append(setClauses, fmt.Sprintf("expires_at = NULL"))
	} else if input.ExpiresAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("expires_at = $%d", argIdx))
		args = append(args, *input.ExpiresAt)
		argIdx++
	}
	if input.TokenLimit != nil {
		setClauses = append(setClauses, fmt.Sprintf("token_limit = $%d", argIdx))
		args = append(args, *input.TokenLimit)
		argIdx++
	}
	if input.RequestLimit != nil {
		setClauses = append(setClauses, fmt.Sprintf("request_limit = $%d", argIdx))
		args = append(args, *input.RequestLimit)
		argIdx++
	}
	if input.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *input.IsActive)
		argIdx++
	}

	if len(setClauses) == 0 {
		return nil // nothing to update
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE id = $%d", strings.Join(setClauses, ", "), argIdx)
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}
