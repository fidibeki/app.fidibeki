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

// integrationColumns is the single projection every read and write returns, so the scan
// order can only be wrong in one place.
const integrationColumns = `id, tenant_id, kind, name, webhook_url, channel,
	          custom_headers_ciphertext, webhook_secret_fingerprint, events,
	          privacy_fields, privacy_profile_version, is_active, install_source,
	          slack_team_name, created_at, updated_at`

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
			webhook_secret_ciphertext, webhook_secret_fingerprint, events, privacy_fields, is_active
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+integrationColumns+`
	`, values.ID, tenantID, values.Kind, values.Name, values.WebhookURL, values.Channel,
		headersCiphertext, secretCiphertext, fingerprint, values.Events, values.PrivacyFields, values.Active))
	if err != nil {
		return Integration{}, fmt.Errorf("create integration: %w", err)
	}
	return integration, nil
}

func (s Store) List(ctx context.Context, query db.Querier, tenantID string) ([]Integration, error) {
	rows, err := query.Query(ctx, `
		SELECT `+integrationColumns+`
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

func (s Store) UpdatePrivacy(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	integrationID string,
	privacyFields []string,
) (Integration, error) {
	integration, _, err := s.scan(query.QueryRow(ctx, `
		UPDATE integrations
		SET privacy_fields = $3, privacy_profile_version = $4, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+integrationColumns+`
	`, tenantID, integrationID, privacyFields, PrivacyProfileVersion))
	if err != nil {
		return Integration{}, fmt.Errorf("update integration privacy profile: %w", err)
	}
	return integration, nil
}

// UpsertSlackInstall persists a completed one-click install. Re-installing the same channel
// updates the row already pointing at it rather than adding a second destination that would
// double-post every notification — which is what the partial unique index enforces.
//
// The bot token's associated data is the (tenant, workspace, channel) it was issued for
// rather than the surrogate row ID, so an upsert that keeps an existing row can still
// decrypt what it wrote. The token is only ever used to revoke the install.
func (s Store) UpsertSlackInstall(
	ctx context.Context, query db.Querier, tenantID string, values SlackInstallValues,
) (Integration, error) {
	tokenCiphertext, err := s.Cipher.Encrypt(
		[]byte(values.AccessToken), slackTokenAAD(tenantID, values.TeamID, values.ChannelID),
	)
	if err != nil {
		return Integration{}, err
	}
	integration, _, err := s.scan(query.QueryRow(ctx, `
		INSERT INTO integrations (
			id, tenant_id, kind, name, webhook_url, channel, events, privacy_fields,
			privacy_profile_version, is_active, install_source, slack_team_id,
			slack_team_name, slack_channel_id, slack_access_token_ciphertext
		) VALUES ($1, $2, 'slack', $3, $4, $5, $6, $7, $8, true, 'oauth', $9, $10, $11, $12)
		ON CONFLICT (tenant_id, slack_team_id, slack_channel_id) WHERE install_source = 'oauth'
		DO UPDATE SET
			name = EXCLUDED.name,
			webhook_url = EXCLUDED.webhook_url,
			channel = EXCLUDED.channel,
			events = EXCLUDED.events,
			privacy_fields = EXCLUDED.privacy_fields,
			slack_team_name = EXCLUDED.slack_team_name,
			slack_access_token_ciphertext = EXCLUDED.slack_access_token_ciphertext,
			is_active = true,
			updated_at = now()
		RETURNING `+integrationColumns+`
	`, values.ID, tenantID, values.Name, values.WebhookURL, nullableText(values.Channel),
		values.Events, values.PrivacyFields, PrivacyProfileVersion, values.TeamID,
		nullableText(values.TeamName), values.ChannelID, tokenCiphertext))
	if err != nil {
		return Integration{}, fmt.Errorf("upsert slack install: %w", err)
	}
	return integration, nil
}

// SlackAccessToken returns the bot token for an OAuth-installed integration so uninstalling
// can revoke it. A manually pasted integration has no token and yields an empty string
// rather than an error.
func (s Store) SlackAccessToken(
	ctx context.Context, query db.Querier, tenantID, integrationID string,
) (string, error) {
	var ciphertext []byte
	var teamID, channelID *string
	err := query.QueryRow(ctx, `
		SELECT slack_access_token_ciphertext, slack_team_id, slack_channel_id
		FROM integrations
		WHERE tenant_id = $1 AND id = $2 AND install_source = 'oauth'
	`, tenantID, integrationID).Scan(&ciphertext, &teamID, &channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read slack access token: %w", err)
	}
	if len(ciphertext) == 0 || teamID == nil || channelID == nil {
		return "", nil
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext, slackTokenAAD(tenantID, *teamID, *channelID))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// nullableText maps an absent value to SQL NULL. integrations.channel and
// integrations.slack_team_name both allow NULL but reject the empty string, so a Slack
// response that omits a channel or workspace name must store nothing rather than "".
func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func slackTokenAAD(tenantID, teamID, channelID string) string {
	return encryptionAAD(tenantID, teamID+"\x00"+channelID, "slack-access-token")
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
		&integration.WebhookSecretFingerprint, &integration.Events, &integration.PrivacyFields,
		&integration.PrivacyProfileVersion, &integration.Active, &integration.InstallSource,
		&integration.SlackTeamName, &integration.CreatedAt, &integration.UpdatedAt,
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
	integration.DestinationCapabilities = destinationCapabilities()
	return integration, tenantID, nil
}

func destinationCapabilities() DestinationCapabilities {
	return DestinationCapabilities{
		MessageUpdate:                 false,
		MessageDelete:                 false,
		MessageRecall:                 false,
		FidibekiLinksRequireAuth:      true,
		FidibekiLinksAccessControlled: true,
	}
}
