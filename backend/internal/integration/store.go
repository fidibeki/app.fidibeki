package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type Store struct {
	Database db.Querier
	Cipher   *Cipher
}

func (s Store) Create(ctx context.Context, query db.Querier, tenantID string, values CreateValues) (Integration, error) {
	var headersCiphertext []byte
	if len(values.CustomHeaders) > 0 {
		encoded, err := json.Marshal(values.CustomHeaders)
		if err != nil {
			return Integration{}, fmt.Errorf("encode integration headers: %w", err)
		}
		headersCiphertext, err = s.Cipher.Encrypt(encoded, encryptionAAD(tenantID, values.ID, "headers"))
		if err != nil {
			return Integration{}, err
		}
	}
	var secretCiphertext []byte
	var fingerprint *string
	if values.Kind == KindWebhook {
		encrypted, err := s.Cipher.Encrypt(
			[]byte(values.WebhookSecret), encryptionAAD(tenantID, values.ID, "webhook-secret"),
		)
		if err != nil {
			return Integration{}, err
		}
		secretCiphertext = encrypted
		value := SecretFingerprint(values.WebhookSecret)
		fingerprint = &value
	}
	integration, _, err := s.scan(query.QueryRow(ctx, `
		INSERT INTO integrations (
			id, tenant_id, kind, name, webhook_url, channel, custom_headers_ciphertext,
			webhook_secret_ciphertext, webhook_secret_fingerprint, events, is_active
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, tenant_id, kind, name, webhook_url, channel,
		          custom_headers_ciphertext, webhook_secret_fingerprint, events,
		          is_active, created_at, updated_at
	`, values.ID, tenantID, values.Kind, values.Name, values.WebhookURL, values.Channel,
		headersCiphertext, secretCiphertext, fingerprint, values.Events, values.Active))
	if err != nil {
		return Integration{}, fmt.Errorf("create integration: %w", err)
	}
	return integration, nil
}

func (s Store) List(ctx context.Context, query db.Querier, tenantID string) ([]Integration, error) {
	rows, err := query.Query(ctx, `
		SELECT id, tenant_id, kind, name, webhook_url, channel,
		       custom_headers_ciphertext, webhook_secret_fingerprint, events,
		       is_active, created_at, updated_at
		FROM integrations
		WHERE tenant_id = $1
		ORDER BY created_at, id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	defer rows.Close()
	integrations := make([]Integration, 0)
	for rows.Next() {
		integration, _, scanErr := s.scan(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan integration: %w", scanErr)
		}
		integrations = append(integrations, integration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate integrations: %w", err)
	}
	return integrations, nil
}

func (s Store) Delete(ctx context.Context, query db.Querier, tenantID, integrationID string) error {
	tag, err := query.Exec(ctx, `DELETE FROM integrations WHERE tenant_id = $1 AND id = $2`, tenantID, integrationID)
	if err != nil {
		return fmt.Errorf("delete integration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface {
	Scan(...any) error
}

func (s Store) scan(row scanner) (Integration, string, error) {
	var integration Integration
	var tenantID string
	var headersCiphertext []byte
	err := row.Scan(
		&integration.ID, &tenantID, &integration.Kind, &integration.Name,
		&integration.WebhookURL, &integration.Channel, &headersCiphertext,
		&integration.WebhookSecretFingerprint, &integration.Events, &integration.Active,
		&integration.CreatedAt, &integration.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Integration{}, "", ErrNotFound
	}
	if err != nil {
		return Integration{}, "", err
	}
	integration.CustomHeaderNames = []string{}
	if len(headersCiphertext) > 0 {
		plaintext, err := s.Cipher.Decrypt(headersCiphertext, encryptionAAD(tenantID, integration.ID, "headers"))
		if err != nil {
			return Integration{}, "", err
		}
		var headers map[string]string
		if err := json.Unmarshal(plaintext, &headers); err != nil {
			return Integration{}, "", fmt.Errorf("decode integration headers: %w", err)
		}
		for name := range headers {
			integration.CustomHeaderNames = append(integration.CustomHeaderNames, name)
		}
		sort.Strings(integration.CustomHeaderNames)
	}
	return integration, tenantID, nil
}
