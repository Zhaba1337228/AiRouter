package repository

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

type Provider struct {
	ID        string     `db:"id"         json:"id"`
	Name      string     `db:"name"       json:"name"`
	BaseURL   string     `db:"base_url"   json:"base_url"`
	APIKey    string     `db:"api_key"    json:"api_key"`
	IsActive  bool       `db:"is_active"  json:"is_active"`
	IsDefault bool       `db:"is_default" json:"is_default"`
	Note      *string    `db:"note"       json:"note"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
}

type ProviderRepo struct {
	db *sqlx.DB
}

func NewProviderRepo(db *sqlx.DB) *ProviderRepo {
	return &ProviderRepo{db: db}
}

func (r *ProviderRepo) List(ctx context.Context) ([]Provider, error) {
	var rows []Provider
	err := r.db.SelectContext(ctx, &rows,
		`SELECT id, name, base_url, api_key, is_active, is_default, note, created_at, updated_at
		 FROM providers ORDER BY is_default DESC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *ProviderRepo) GetDefault(ctx context.Context) (*Provider, error) {
	var p Provider
	err := r.db.GetContext(ctx, &p,
		`SELECT id, name, base_url, api_key, is_active, is_default, note, created_at, updated_at
		 FROM providers WHERE is_default = TRUE AND is_active = TRUE LIMIT 1`)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ProviderRepo) GetByID(ctx context.Context, id string) (*Provider, error) {
	var p Provider
	err := r.db.GetContext(ctx, &p,
		`SELECT id, name, base_url, api_key, is_active, is_default, note, created_at, updated_at
		 FROM providers WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

type CreateProviderInput struct {
	Name      string
	BaseURL   string
	APIKey    string
	IsDefault bool
	Note      *string
}

func (r *ProviderRepo) Create(ctx context.Context, input CreateProviderInput) (*Provider, error) {
	if input.IsDefault {
		_, _ = r.db.ExecContext(ctx, `UPDATE providers SET is_default = FALSE`)
	}
	var p Provider
	err := r.db.GetContext(ctx, &p, `
		INSERT INTO providers (name, base_url, api_key, is_default, note)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, base_url, api_key, is_active, is_default, note, created_at, updated_at`,
		input.Name, input.BaseURL, input.APIKey, input.IsDefault, input.Note)
	return &p, err
}

type UpdateProviderInput struct {
	Name      *string
	BaseURL   *string
	APIKey    *string
	IsActive  *bool
	IsDefault *bool
	Note      *string
}

func (r *ProviderRepo) Update(ctx context.Context, id string, input UpdateProviderInput) (*Provider, error) {
	// If setting as default, clear others first
	if input.IsDefault != nil && *input.IsDefault {
		_, _ = r.db.ExecContext(ctx, `UPDATE providers SET is_default = FALSE`)
	}

	_, err := r.db.ExecContext(ctx, `
		UPDATE providers SET
			name       = COALESCE($2, name),
			base_url   = COALESCE($3, base_url),
			api_key    = COALESCE($4, api_key),
			is_active  = COALESCE($5, is_active),
			is_default = COALESCE($6, is_default),
			note       = COALESCE($7, note),
			updated_at = NOW()
		WHERE id = $1`,
		id, input.Name, input.BaseURL, input.APIKey, input.IsActive, input.IsDefault, input.Note)
	if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

func (r *ProviderRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM providers WHERE id = $1`, id)
	return err
}
