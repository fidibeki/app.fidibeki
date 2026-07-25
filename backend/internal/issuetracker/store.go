package issuetracker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/integration"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Store persists issue-tracker connections and external links. Credentials are encrypted with
// the shared integration cipher so this package never holds key material of its own.
type Store struct {
	// Database serves the worker path, which has no request transaction to borrow.
	Database db.Querier
	Cipher   *integration.Cipher
}

const connectionColumns = `
	id, provider, site_url, account_label, credential_fingerprint,
	destination_id, destination_key, destination_name, issue_type_id, issue_type_name,
	sub_destination_id, sub_destination_name, is_enabled, auto_create, validated_at, last_error,
	created_at, updated_at
`

const linkColumns = `
	id, issue_id, provider, status, request_title, request_description, request_priority,
	destination_id, destination_name, issue_type_id, external_id, external_key, external_url,
	last_error, attempt_count, created_at, updated_at
`

// ConnectValues is a validated credential ready to be stored.
type ConnectValues struct {
	Provider     Provider
	SiteURL      string
	AccountLabel string
	Email        string
	APIToken     string
	ValidatedAt  time.Time
}

// ListConnections returns every connected provider for the workspace.
func (s Store) ListConnections(ctx context.Context, query db.Querier, tenantID string) ([]Connection, error) {
	rows, err := query.Query(ctx, `
		SELECT `+connectionColumns+`
		FROM issue_tracker_connections
		WHERE tenant_id = $1
		ORDER BY provider
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list issue tracker connections: %w", err)
	}
	defer rows.Close()
	connections := make([]Connection, 0, len(Providers()))
	for rows.Next() {
		connection, scanErr := scanConnection(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		connections = append(connections, connection)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issue tracker connections: %w", err)
	}
	return connections, nil
}

// GetConnection returns one provider's connection.
func (s Store) GetConnection(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	provider Provider,
) (Connection, error) {
	connection, err := scanConnection(query.QueryRow(ctx, `
		SELECT `+connectionColumns+`
		FROM issue_tracker_connections
		WHERE tenant_id = $1 AND provider = $2
	`, tenantID, provider))
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotConnected
	}
	return connection, err
}

// Credentials decrypts the stored secret for a provider. It is only ever called on the request
// path that is about to reach the provider.
func (s Store) Credentials(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	provider Provider,
) (Credentials, error) {
	var connectionID, siteURL string
	var ciphertext []byte
	err := query.QueryRow(ctx, `
		SELECT id, site_url, credential_ciphertext
		FROM issue_tracker_connections
		WHERE tenant_id = $1 AND provider = $2
	`, tenantID, provider).Scan(&connectionID, &siteURL, &ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credentials{}, ErrNotConnected
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("load issue tracker credential: %w", err)
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext, credentialAAD(tenantID, connectionID))
	if err != nil {
		return Credentials{}, err
	}
	email, token, err := decodeCredential(plaintext)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{SiteURL: siteURL, Email: email, APIToken: token}, nil
}

// Connect stores validated credentials. Reconnecting replaces the credential and clears the
// previous failure, but never silently re-enables a provider an administrator switched off.
func (s Store) Connect(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	values ConnectValues,
) (Connection, error) {
	connectionID := uuid.NewString()
	var existingID string
	err := query.QueryRow(ctx, `
		SELECT id FROM issue_tracker_connections WHERE tenant_id = $1 AND provider = $2
	`, tenantID, values.Provider).Scan(&existingID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, fmt.Errorf("load issue tracker connection: %w", err)
	}
	if existingID != "" {
		connectionID = existingID
	}
	ciphertext, err := s.Cipher.Encrypt(
		encodeCredential(values.Email, values.APIToken), credentialAAD(tenantID, connectionID),
	)
	if err != nil {
		return Connection{}, err
	}
	fingerprint := integration.SecretFingerprint(values.APIToken)
	connection, err := scanConnection(query.QueryRow(ctx, `
		INSERT INTO issue_tracker_connections (
			id, tenant_id, provider, site_url, account_label,
			credential_ciphertext, credential_fingerprint, validated_at, last_error
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '')
		ON CONFLICT (tenant_id, provider) DO UPDATE SET
			site_url = EXCLUDED.site_url,
			account_label = EXCLUDED.account_label,
			credential_ciphertext = EXCLUDED.credential_ciphertext,
			credential_fingerprint = EXCLUDED.credential_fingerprint,
			validated_at = EXCLUDED.validated_at,
			last_error = '',
			updated_at = now()
		RETURNING `+connectionColumns+`
	`, connectionID, tenantID, values.Provider, values.SiteURL, values.AccountLabel,
		ciphertext, fingerprint, values.ValidatedAt.UTC()))
	if err != nil {
		return Connection{}, fmt.Errorf("store issue tracker connection: %w", err)
	}
	return connection, nil
}

// ConfigureValues carries a resolved destination plus the opt-in switch.
type ConfigureValues struct {
	DestinationID      string
	DestinationKey     string
	DestinationName    string
	IssueTypeID        string
	IssueTypeName      string
	SubDestinationID   string
	SubDestinationName string
	Enabled            bool
	AutoCreate         bool
	ValidatedAt        time.Time
}

// Configure writes the destination and enabled flag resolved against the live provider.
func (s Store) Configure(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	provider Provider,
	values ConfigureValues,
) (Connection, error) {
	connection, err := scanConnection(query.QueryRow(ctx, `
		UPDATE issue_tracker_connections
		SET destination_id = $3, destination_key = $4, destination_name = $5,
		    issue_type_id = $6, issue_type_name = $7,
		    sub_destination_id = $8, sub_destination_name = $9,
		    is_enabled = $10, auto_create = $11, validated_at = $12,
		    last_error = '', updated_at = now()
		WHERE tenant_id = $1 AND provider = $2
		RETURNING `+connectionColumns+`
	`, tenantID, provider, values.DestinationID, values.DestinationKey, values.DestinationName,
		values.IssueTypeID, values.IssueTypeName, values.SubDestinationID, values.SubDestinationName,
		values.Enabled, values.Enabled && values.AutoCreate, values.ValidatedAt.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotConnected
	}
	if err != nil {
		return Connection{}, fmt.Errorf("configure issue tracker connection: %w", err)
	}
	return connection, nil
}

// RecordConnectionError stores the last actionable provider failure against the connection so
// the settings page can explain why creation is failing without opening the issue.
func (s Store) RecordConnectionError(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	provider Provider,
	message string,
) error {
	if _, err := query.Exec(ctx, `
		UPDATE issue_tracker_connections
		SET last_error = $3, updated_at = now()
		WHERE tenant_id = $1 AND provider = $2
	`, tenantID, provider, truncate(message, 2000)); err != nil {
		return fmt.Errorf("record issue tracker error: %w", err)
	}
	return nil
}

// Disconnect removes the credential and the destination. Existing external links are kept:
// they are evidence that a copy exists in the provider.
func (s Store) Disconnect(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	provider Provider,
) (string, error) {
	var connectionID string
	err := query.QueryRow(ctx, `
		DELETE FROM issue_tracker_connections
		WHERE tenant_id = $1 AND provider = $2
		RETURNING id
	`, tenantID, provider).Scan(&connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotConnected
	}
	if err != nil {
		return "", fmt.Errorf("disconnect issue tracker: %w", err)
	}
	return connectionID, nil
}

// ListLinks returns every creation attempt recorded for an issue.
func (s Store) ListLinks(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
) ([]ExternalLink, error) {
	rows, err := query.Query(ctx, `
		SELECT `+linkColumns+`
		FROM issue_external_links
		WHERE tenant_id = $1 AND issue_id = $2
		ORDER BY created_at DESC, id
	`, tenantID, issueID)
	if err != nil {
		return nil, fmt.Errorf("list external issue links: %w", err)
	}
	defer rows.Close()
	links := make([]ExternalLink, 0)
	for rows.Next() {
		link, scanErr := scanLink(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external issue links: %w", err)
	}
	return links, nil
}

// GetLink returns one attempt by ID.
func (s Store) GetLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	linkID string,
) (ExternalLink, error) {
	link, err := scanLink(query.QueryRow(ctx, `
		SELECT `+linkColumns+`
		FROM issue_external_links
		WHERE tenant_id = $1 AND issue_id = $2 AND id = $3
	`, tenantID, issueID, linkID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExternalLink{}, ErrNotFound
	}
	return link, err
}

// FindLinkByIdempotencyKey resolves a repeated request to the attempt it already produced.
func (s Store) FindLinkByIdempotencyKey(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	key string,
) (ExternalLink, error) {
	link, err := scanLink(query.QueryRow(ctx, `
		SELECT `+linkColumns+`
		FROM issue_external_links
		WHERE tenant_id = $1 AND idempotency_key = $2
	`, tenantID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExternalLink{}, ErrNotFound
	}
	return link, err
}

// FindLiveLink returns the pending or created attempt for an issue and provider, if any.
func (s Store) FindLiveLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	provider Provider,
) (ExternalLink, error) {
	link, err := scanLink(query.QueryRow(ctx, `
		SELECT `+linkColumns+`
		FROM issue_external_links
		WHERE tenant_id = $1 AND issue_id = $2 AND provider = $3 AND status <> 'failed'
	`, tenantID, issueID, provider))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExternalLink{}, ErrNotFound
	}
	return link, err
}

// PendingLinkValues is the reviewed request captured before the provider is called.
type PendingLinkValues struct {
	IssueID         string
	Provider        Provider
	IdempotencyKey  string
	Title           string
	Description     string
	Priority        string
	DestinationID   string
	DestinationName string
	IssueTypeID     string
	MemberID        *string
}

// BeginLink inserts the pending attempt. Because the row is written before the provider call,
// a concurrent request either blocks on the unique index or is rejected with ErrLinkExists,
// which is what makes a double submit safe.
func (s Store) BeginLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	values PendingLinkValues,
) (ExternalLink, error) {
	link, err := scanLink(query.QueryRow(ctx, `
		INSERT INTO issue_external_links (
			tenant_id, issue_id, provider, status, idempotency_key,
			request_title, request_description, request_priority,
			destination_id, destination_name, issue_type_id, attempt_count, created_by_member_id
		) VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, $9, $10, 1, $11)
		RETURNING `+linkColumns+`
	`, tenantID, values.IssueID, values.Provider, values.IdempotencyKey,
		values.Title, values.Description, values.Priority,
		values.DestinationID, values.DestinationName, values.IssueTypeID, values.MemberID))
	if err != nil {
		return ExternalLink{}, translateLinkConflict(err)
	}
	return link, nil
}

// RetryLink moves a failed attempt back to pending, reusing its idempotency key so a retry can
// never produce a second external issue.
func (s Store) RetryLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	linkID string,
	values PendingLinkValues,
) (ExternalLink, error) {
	link, err := scanLink(query.QueryRow(ctx, `
		UPDATE issue_external_links
		SET status = 'pending', last_error = '', attempt_count = attempt_count + 1,
		    destination_id = $4, destination_name = $5, issue_type_id = $6, updated_at = now()
		WHERE tenant_id = $1 AND issue_id = $2 AND id = $3 AND status = 'failed'
		RETURNING `+linkColumns+`
	`, tenantID, issueID, linkID, values.DestinationID, values.DestinationName, values.IssueTypeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExternalLink{}, ErrLinkNotRetryable
	}
	if err != nil {
		return ExternalLink{}, translateLinkConflict(err)
	}
	return link, nil
}

// CompleteLink records a successful creation.
func (s Store) CompleteLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	linkID string,
	result CreateResult,
) (ExternalLink, error) {
	link, err := scanLink(query.QueryRow(ctx, `
		UPDATE issue_external_links
		SET status = 'created', external_id = $3, external_key = $4, external_url = $5,
		    last_error = '', updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+linkColumns+`
	`, tenantID, linkID, result.ID, result.Key, result.URL))
	if err != nil {
		return ExternalLink{}, fmt.Errorf("complete external issue link: %w", err)
	}
	return link, nil
}

// FailLink records an actionable failure. The Fidibeki issue is untouched.
func (s Store) FailLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	linkID string,
	message string,
) (ExternalLink, error) {
	link, err := scanLink(query.QueryRow(ctx, `
		UPDATE issue_external_links
		SET status = 'failed', last_error = $3, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+linkColumns+`
	`, tenantID, linkID, fallback(truncate(message, 2000), "the provider rejected the request")))
	if err != nil {
		return ExternalLink{}, fmt.Errorf("fail external issue link: %w", err)
	}
	return link, nil
}

// IssueSnapshot is the minimum an external issue needs from the Fidibeki record.
type IssueSnapshot struct {
	ID          string
	PublicID    string
	Title       string
	Description string
	Priority    string
}

// LoadIssue reads the issue the external copy is created from.
func (s Store) LoadIssue(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
) (IssueSnapshot, error) {
	var snapshot IssueSnapshot
	var priority *string
	err := query.QueryRow(ctx, `
		SELECT id::text, public_id, title, description, priority::text
		FROM issues
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, issueID).Scan(
		&snapshot.ID, &snapshot.PublicID, &snapshot.Title, &snapshot.Description, &priority,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueSnapshot{}, ErrNotFound
	}
	if err != nil {
		return IssueSnapshot{}, fmt.Errorf("load issue for external creation: %w", err)
	}
	if priority != nil {
		snapshot.Priority = *priority
	}
	return snapshot, nil
}

// translateLinkConflict turns the two unique constraints into the domain errors the handler
// maps to 409 responses.
func translateLinkConflict(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return fmt.Errorf("begin external issue link: %w", err)
	}
	switch pgErr.ConstraintName {
	case "issue_external_links_idempotency_key":
		return ErrLinkInFlight
	case "idx_issue_external_links_live":
		return ErrLinkExists
	default:
		return ErrLinkExists
	}
}

type scanner interface {
	Scan(...any) error
}

func scanConnection(row scanner) (Connection, error) {
	var connection Connection
	if err := row.Scan(
		&connection.ID, &connection.Provider, &connection.SiteURL, &connection.AccountLabel,
		&connection.CredentialFingerprint, &connection.DestinationID, &connection.DestinationKey,
		&connection.DestinationName, &connection.IssueTypeID, &connection.IssueTypeName,
		&connection.SubDestinationID, &connection.SubDestinationName, &connection.Enabled,
		&connection.AutoCreate, &connection.ValidatedAt, &connection.LastError,
		&connection.CreatedAt, &connection.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, err
		}
		return Connection{}, fmt.Errorf("scan issue tracker connection: %w", err)
	}
	return connection, nil
}

func scanLink(row scanner) (ExternalLink, error) {
	var link ExternalLink
	if err := row.Scan(
		&link.ID, &link.IssueID, &link.Provider, &link.Status, &link.RequestTitle,
		&link.RequestDescription, &link.RequestPriority, &link.DestinationID,
		&link.DestinationName, &link.IssueTypeID, &link.ExternalID, &link.ExternalKey,
		&link.ExternalURL, &link.LastError, &link.AttemptCount, &link.CreatedAt, &link.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ExternalLink{}, err
		}
		return ExternalLink{}, fmt.Errorf("scan external issue link: %w", err)
	}
	return link, nil
}

func credentialAAD(tenantID, connectionID string) string {
	return tenantID + "\x00" + connectionID + "\x00issue-tracker-credential"
}

// encodeCredential keeps the account identifier and the secret in one ciphertext so they cannot
// drift apart. Jira needs both; Linear only uses the token.
func encodeCredential(email, token string) []byte {
	return []byte(email + "\x00" + token)
}

func decodeCredential(plaintext []byte) (string, string, error) {
	email, token, found := strings.Cut(string(plaintext), "\x00")
	if !found {
		return "", "", errors.New("stored issue tracker credential is malformed")
	}
	return email, token, nil
}
