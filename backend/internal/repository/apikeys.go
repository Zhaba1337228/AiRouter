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
}

func (r *APIKeyRepo) Create(ctx context.Context, input CreateKeyInput) (key *models.APIKey, plaintext string, err error) {
	plain, hash, prefix, err := GenerateKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate key: %w", err)
	}

	key = &models.APIKey{}
	err = r.db.QueryRowxContext(ctx, `
		INSERT INTO api_keys (name, key_hash, key_prefix, note, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note
	`, input.Name, hash, prefix, input.Note, input.ExpiresAt).StructScan(key)
	if err != nil {
		return nil, "", fmt.Errorf("insert api key: %w", err)
	}

	return key, plain, nil
}

func (r *APIKeyRepo) List(ctx context.Context) ([]*models.APIKey, error) {
	var keys []*models.APIKey
	err := r.db.SelectContext(ctx, &keys, `
		SELECT id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note
		FROM api_keys ORDER BY created_at DESC
	`)
	return keys, err
}

func (r *APIKeyRepo) GetByID(ctx context.Context, id string) (*models.APIKey, error) {
	key := &models.APIKey{}
	err := r.db.GetContext(ctx, key, `
		SELECT id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note
		FROM api_keys WHERE id = $1
	`, id)
	return key, err
}

func (r *APIKeyRepo) ValidateKey(ctx context.Context, plaintext string) (*models.APIKey, error) {
	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])

	key := &models.APIKey{}
	err := r.db.GetContext(ctx, key, `
		SELECT id, name, key_hash, key_prefix, is_active, created_at, last_used_at, expires_at, note
		FROM api_keys
		WHERE key_hash = $1
		  AND is_active = TRUE
		  AND (expires_at IS NULL OR expires_at > NOW())
	`, hash)
	if err != nil {
		return nil, err
	}

	// update last_used_at async (best effort)
	go func() {
		_, _ = r.db.Exec(`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, key.ID)
	}()

	return key, nil
}

func (r *APIKeyRepo) SetActive(ctx context.Context, id string, active bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET is_active = $1 WHERE id = $2`, active, id)
	return err
}

func (r *APIKeyRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	return err
}
