package apikey

import (
	"context"
	"errors"
	"fmt"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Store struct{}

func (Store) Create(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	values CreateValues,
) (APIKey, error) {
	key, err := scanAPIKey(query.QueryRow(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions, rate_limit_per_min, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, tenant_id, name, token_prefix, permissions, rate_limit_per_min,
		          last_used_at, expires_at, created_at, updated_at
	`, tenantID, values.Name, values.TokenHash, values.TokenPrefix, values.Permissions,
		values.RateLimitPerMin, values.ExpiresAt))
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return APIKey{}, ErrTokenConflict
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("create API key: %w", err)
	}
	return key, nil
}

func (Store) List(ctx context.Context, query db.Querier, tenantID string) ([]APIKey, error) {
	rows, err := query.Query(ctx, `
		SELECT id, tenant_id, name, token_prefix, permissions, rate_limit_per_min,
		       last_used_at, expires_at, created_at, updated_at
		FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC, id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	defer rows.Close()
	keys := make([]APIKey, 0)
	for rows.Next() {
		key, scanErr := scanAPIKey(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan API key: %w", scanErr)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API keys: %w", err)
	}
	return keys, nil
}

func (Store) Delete(ctx context.Context, query db.Querier, tenantID, keyID string) error {
	result, err := query.Exec(ctx, `DELETE FROM api_keys WHERE tenant_id = $1 AND id = $2`, tenantID, keyID)
	if err != nil {
		return fmt.Errorf("delete API key: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanAPIKey(row rowScanner) (APIKey, error) {
	var key APIKey
	err := row.Scan(
		&key.ID, &key.TenantID, &key.Name, &key.TokenPrefix, &key.Permissions,
		&key.RateLimitPerMin, &key.LastUsedAt, &key.ExpiresAt, &key.CreatedAt, &key.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	return key, err
}
