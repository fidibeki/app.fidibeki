package samlsso

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

// Cipher seals the service-provider private key at rest. The API's existing
// AES-256-GCM cipher (INTEGRATION_ENCRYPTION_KEY) satisfies this.
type Cipher interface {
	Encrypt(plaintext []byte, associatedData string) ([]byte, error)
	Decrypt(ciphertext []byte, associatedData string) ([]byte, error)
}

type Store struct {
	Cipher Cipher
}

const maxOutstandingAuthRequests = 128

// EligibleMember is the identity a validated assertion is allowed to sign in
// as. SAML never creates a member from an assertion alone.
type EligibleMember struct {
	ID    string
	Email string
}

func privateKeyAAD(tenantID string) string {
	return tenantID + "\x00saml\x00sp_private_key"
}

// Get loads a tenant's configuration and its signing certificates.
func (s Store) Get(ctx context.Context, query db.Querier, tenantID string) (Configuration, error) {
	configuration, _, err := s.get(ctx, query, tenantID, false)
	return configuration, err
}

// GetWithKey additionally unseals the service-provider private key. Only the
// metadata, start, and ACS paths need it.
func (s Store) GetWithKey(
	ctx context.Context, query db.Querier, tenantID string,
) (Configuration, []byte, error) {
	return s.get(ctx, query, tenantID, true)
}

func (s Store) get(
	ctx context.Context, query db.Querier, tenantID string, withKey bool,
) (Configuration, []byte, error) {
	configuration := Configuration{TenantID: tenantID}
	var sealedKey []byte
	err := query.QueryRow(ctx, `
		SELECT mode::text, idp_entity_id, idp_sso_url, idp_metadata_url, email_domains,
		       allow_idp_initiated, sp_certificate, sp_private_key,
		       last_tested_at, last_test_error, last_successful_login_at,
		       created_at, updated_at, updated_by_member_id
		FROM tenant_saml_configurations
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&configuration.Mode, &configuration.IdPEntityID, &configuration.IdPSSOURL,
		&configuration.IdPMetadataURL, &configuration.EmailDomains,
		&configuration.AllowIDPInitiated, &configuration.SPCertificate, &sealedKey,
		&configuration.LastTestedAt, &configuration.LastTestError,
		&configuration.LastSuccessfulLoginAt,
		&configuration.CreatedAt, &configuration.UpdatedAt, &configuration.UpdatedByMemberID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Configuration{}, nil, ErrNotConfigured
	}
	if err != nil {
		return Configuration{}, nil, fmt.Errorf("load SAML configuration: %w", err)
	}
	certificates, err := s.listCertificates(ctx, query, tenantID)
	if err != nil {
		return Configuration{}, nil, err
	}
	configuration.Certificates = certificates
	if !withKey {
		return configuration, nil, nil
	}
	privateKey, err := s.Cipher.Decrypt(sealedKey, privateKeyAAD(tenantID))
	if err != nil {
		return Configuration{}, nil, fmt.Errorf("unseal SAML service provider key: %w", err)
	}
	return configuration, privateKey, nil
}

func (Store) listCertificates(
	ctx context.Context, query db.Querier, tenantID string,
) ([]Certificate, error) {
	rows, err := query.Query(ctx, `
		SELECT id, certificate, fingerprint, subject, not_before, not_after, created_at
		FROM tenant_saml_certificates
		WHERE tenant_id = $1
		ORDER BY not_after DESC, created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list SAML certificates: %w", err)
	}
	defer rows.Close()
	certificates := make([]Certificate, 0, 4)
	for rows.Next() {
		var certificate Certificate
		if err := rows.Scan(
			&certificate.ID, &certificate.Certificate, &certificate.Fingerprint,
			&certificate.Subject, &certificate.NotBefore, &certificate.NotAfter,
			&certificate.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan SAML certificate: %w", err)
		}
		certificates = append(certificates, certificate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list SAML certificates: %w", err)
	}
	return certificates, nil
}

// UpsertInput is a validated configuration change.
type UpsertInput struct {
	IdPEntityID       string
	IdPSSOURL         string
	IdPMetadataURL    *string
	EmailDomains      []string
	AllowIDPInitiated bool
	Certificates      []Certificate
	// ReplaceCertificates swaps the whole trusted set. A rotation that only
	// adds the IdP's next certificate leaves this false so both remain
	// accepted and no in-flight sign-in fails.
	ReplaceCertificates bool
	ActorMemberID       string
	KeyPair             *KeyPair
	Now                 time.Time
}

// Upsert writes an organization's configuration. Changing the IdP identity
// clears the successful-test marker and drops the tenant out of Required mode,
// because Required must never apply to an untested configuration.
func (s Store) Upsert(
	ctx context.Context, query db.Querier, tenantID string, input UpsertInput,
) (Configuration, error) {
	if input.KeyPair != nil {
		sealed, err := s.Cipher.Encrypt(input.KeyPair.PrivateKeyDER, privateKeyAAD(tenantID))
		if err != nil {
			return Configuration{}, fmt.Errorf("seal SAML service provider key: %w", err)
		}
		if _, err := query.Exec(ctx, `
			INSERT INTO tenant_saml_configurations (
				tenant_id, idp_entity_id, idp_sso_url, idp_metadata_url, email_domains,
				allow_idp_initiated, sp_private_key, sp_certificate, updated_by_member_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid)
			ON CONFLICT (tenant_id) DO NOTHING
		`, tenantID, input.IdPEntityID, input.IdPSSOURL, input.IdPMetadataURL, input.EmailDomains,
			input.AllowIDPInitiated, sealed, input.KeyPair.CertificatePEM, input.ActorMemberID,
		); err != nil {
			return Configuration{}, fmt.Errorf("create SAML configuration: %w", err)
		}
	}
	// The mode reset is expressed in SQL against the stored row so it depends on
	// what actually changed, not on what the caller believed was stored.
	if _, err := query.Exec(ctx, `
		UPDATE tenant_saml_configurations SET
			idp_entity_id = $2,
			idp_sso_url = $3,
			idp_metadata_url = $4,
			email_domains = $5,
			allow_idp_initiated = $6,
			updated_by_member_id = NULLIF($7, '')::uuid,
			last_tested_at = CASE
				WHEN idp_entity_id IS DISTINCT FROM $2 OR idp_sso_url IS DISTINCT FROM $3
				THEN NULL ELSE last_tested_at END,
			last_test_error = CASE
				WHEN idp_entity_id IS DISTINCT FROM $2 OR idp_sso_url IS DISTINCT FROM $3
				THEN NULL ELSE last_test_error END,
			mode = CASE
				WHEN mode = 'required'
				  AND (idp_entity_id IS DISTINCT FROM $2 OR idp_sso_url IS DISTINCT FROM $3)
				THEN 'optional'::tenant_sso_mode ELSE mode END
		WHERE tenant_id = $1
	`, tenantID, input.IdPEntityID, input.IdPSSOURL, input.IdPMetadataURL, input.EmailDomains,
		input.AllowIDPInitiated, input.ActorMemberID,
	); err != nil {
		return Configuration{}, fmt.Errorf("update SAML configuration: %w", err)
	}
	if input.ReplaceCertificates {
		if _, err := query.Exec(ctx,
			`DELETE FROM tenant_saml_certificates WHERE tenant_id = $1`, tenantID,
		); err != nil {
			return Configuration{}, fmt.Errorf("clear SAML certificates: %w", err)
		}
	}
	for _, certificate := range input.Certificates {
		if err := s.AddCertificate(ctx, query, tenantID, certificate); err != nil {
			return Configuration{}, err
		}
	}
	return s.Get(ctx, query, tenantID)
}

// AddCertificate registers one signing certificate. Re-adding an existing
// certificate is a no-op so a repeated metadata refresh is idempotent.
func (Store) AddCertificate(
	ctx context.Context, query db.Querier, tenantID string, certificate Certificate,
) error {
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_saml_certificates (
			tenant_id, certificate, fingerprint, subject, not_before, not_after
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, fingerprint) DO NOTHING
	`, tenantID, certificate.Certificate, certificate.Fingerprint, certificate.Subject,
		certificate.NotBefore, certificate.NotAfter,
	); err != nil {
		return fmt.Errorf("add SAML certificate: %w", err)
	}
	return nil
}

// RemoveCertificate retires one signing certificate.
func (Store) RemoveCertificate(
	ctx context.Context, query db.Querier, tenantID, certificateID string,
) error {
	tag, err := query.Exec(ctx,
		`DELETE FROM tenant_saml_certificates WHERE tenant_id = $1 AND id = $2`,
		tenantID, certificateID,
	)
	if err != nil {
		return fmt.Errorf("remove SAML certificate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCertificateNotFound
	}
	return nil
}

// SetMode changes the enforcement mode.
func (Store) SetMode(
	ctx context.Context, query db.Querier, tenantID string, mode auth.SSOMode, actorMemberID string,
) error {
	var ownerMemberID *string
	var currentMode auth.SSOMode
	if err := query.QueryRow(ctx, `
		SELECT tenant.owner_member_id, configuration.mode::text
		FROM tenants AS tenant
		JOIN tenant_saml_configurations AS configuration ON configuration.tenant_id = tenant.id
		WHERE tenant.id = $1
		FOR UPDATE OF tenant, configuration
	`, tenantID).Scan(&ownerMemberID, &currentMode); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotConfigured
	} else if err != nil {
		return fmt.Errorf("lock SAML authentication policy: %w", err)
	}
	if mode == auth.SSOModeRequired && currentMode != auth.SSOModeRequired {
		if _, err := query.Exec(ctx, `
			DELETE FROM tenant_magic_links
			WHERE tenant_id = $1
			  AND authentication_method = 'local'
			  AND (member_id IS NULL OR member_id IS DISTINCT FROM $2::uuid)
		`, tenantID, ownerMemberID); err != nil {
			return fmt.Errorf("invalidate local magic links for required SSO: %w", err)
		}
		if _, err := query.Exec(ctx, `
			UPDATE tenant_member_sessions
			SET revoked_at = COALESCE(revoked_at, now()),
			    rotated_at = NULL,
			    replacement_hash = NULL
			WHERE tenant_id = $1
			  AND authentication_method = 'local'
			  AND member_id IS DISTINCT FROM $2::uuid
			  AND revoked_at IS NULL
		`, tenantID, ownerMemberID); err != nil {
			return fmt.Errorf("revoke local sessions for required SSO: %w", err)
		}
		if _, err := query.Exec(ctx, `
			UPDATE tenant_members
			SET authorization_version = authorization_version + 1
			WHERE tenant_id = $1
			  AND id IS DISTINCT FROM $2::uuid
			  AND status = 'active'
		`, tenantID, ownerMemberID); err != nil {
			return fmt.Errorf("invalidate access tokens for required SSO: %w", err)
		}
	}
	tag, err := query.Exec(ctx, `
		UPDATE tenant_saml_configurations
		SET mode = $2::tenant_sso_mode, updated_by_member_id = NULLIF($3, '')::uuid
		WHERE tenant_id = $1
	`, tenantID, string(mode), actorMemberID)
	if err != nil {
		return fmt.Errorf("set SAML mode: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotConfigured
	}
	return nil
}

// Delete removes the configuration entirely, restoring local authentication.
func (Store) Delete(ctx context.Context, query db.Querier, tenantID string) error {
	tag, err := query.Exec(ctx,
		`DELETE FROM tenant_saml_configurations WHERE tenant_id = $1`, tenantID,
	)
	if err != nil {
		return fmt.Errorf("delete SAML configuration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotConfigured
	}
	return nil
}

// CreateAuthRequest atomically reserves bounded correlation capacity and records
// an issued AuthnRequest. It must run inside the caller's tenant transaction so
// the advisory lock covers the count and insert together.
func (Store) CreateAuthRequest(
	ctx context.Context, query db.Querier, request AuthRequest, now time.Time,
) error {
	lockKey := "fidibeki:saml-auth-request:" + request.TenantID
	if _, err := query.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey,
	); err != nil {
		return fmt.Errorf("lock SAML authentication request capacity: %w", err)
	}
	if _, err := query.Exec(ctx, `
		DELETE FROM tenant_saml_auth_requests
		WHERE tenant_id = $1 AND expires_at <= $2
	`, request.TenantID, now); err != nil {
		return fmt.Errorf("clear expired SAML authentication requests: %w", err)
	}
	var outstanding int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FROM tenant_saml_auth_requests
		WHERE tenant_id = $1 AND consumed_at IS NULL AND expires_at > $2
	`, request.TenantID, now).Scan(&outstanding); err != nil {
		return fmt.Errorf("count live SAML authentication requests: %w", err)
	}
	if outstanding >= maxOutstandingAuthRequests {
		return ErrAuthRequestCapacity
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_saml_auth_requests (
			id, tenant_id, purpose, relay_state, return_to, actor_member_id, expires_at
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7)
	`, request.ID, request.TenantID, string(request.Purpose), request.RelayState,
		request.ReturnTo, stringValue(request.ActorMemberID), request.ExpiresAt,
	); err != nil {
		return fmt.Errorf("record SAML authentication request: %w", err)
	}
	return nil
}

// IsAuthRequestLive checks one untrusted response candidate against the
// tenant-scoped correlation ledger. It never widens validation to other live
// requests.
func (Store) IsAuthRequestLive(
	ctx context.Context, query db.Querier, tenantID, requestID string, now time.Time,
) (bool, error) {
	var live bool
	if err := query.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tenant_saml_auth_requests
			WHERE tenant_id = $1 AND id = $2
			  AND consumed_at IS NULL AND expires_at > $3
		)
	`, tenantID, requestID, now).Scan(&live); err != nil {
		return false, fmt.Errorf("check live SAML request: %w", err)
	}
	return live, nil
}

// ConsumeAuthRequest marks a correlated request used. The conditional UPDATE
// is the single-use guard: a second response quoting the same InResponseTo
// affects no rows.
func (Store) ConsumeAuthRequest(
	ctx context.Context, query db.Querier, tenantID, requestID string, now time.Time,
) (AuthRequest, error) {
	request := AuthRequest{TenantID: tenantID}
	var purpose string
	err := query.QueryRow(ctx, `
		UPDATE tenant_saml_auth_requests
		SET consumed_at = $3
		WHERE tenant_id = $1 AND id = $2 AND consumed_at IS NULL AND expires_at > $3
		RETURNING id, purpose, relay_state, return_to, actor_member_id::text, expires_at
	`, tenantID, requestID, now).Scan(
		&request.ID, &purpose, &request.RelayState, &request.ReturnTo,
		&request.ActorMemberID, &request.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthRequest{}, ErrRequestNotFound
	}
	if err != nil {
		return AuthRequest{}, fmt.Errorf("consume SAML authentication request: %w", err)
	}
	request.Purpose = Purpose(purpose)
	return request, nil
}

// RecordAssertion inserts the assertion into the replay ledger. A conflict
// means this assertion was already consumed, which is a replay.
func (Store) RecordAssertion(
	ctx context.Context, query db.Querier, tenantID, assertionHash string, notOnOrAfter time.Time,
) error {
	tag, err := query.Exec(ctx, `
		INSERT INTO tenant_saml_assertions (tenant_id, assertion_hash, not_on_or_after)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, assertion_hash) DO NOTHING
	`, tenantID, assertionHash, notOnOrAfter)
	if err != nil {
		return fmt.Errorf("record SAML assertion: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAssertionReplayed
	}
	return nil
}

// MarkTested records the outcome of an administrator's validation run. Only a
// success sets last_tested_at, which is what gates Required mode.
func (Store) MarkTested(
	ctx context.Context, query db.Querier, tenantID string, now time.Time, testError string,
) error {
	if testError != "" {
		if _, err := query.Exec(ctx, `
			UPDATE tenant_saml_configurations SET last_test_error = $2 WHERE tenant_id = $1
		`, tenantID, testError); err != nil {
			return fmt.Errorf("record SAML test failure: %w", err)
		}
		return nil
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_saml_configurations
		SET last_tested_at = $2, last_test_error = NULL
		WHERE tenant_id = $1
	`, tenantID, now); err != nil {
		return fmt.Errorf("record SAML test success: %w", err)
	}
	return nil
}

// MarkLoginSuccess stamps the last working sign-in, which the settings UI shows
// as evidence the integration is healthy.
func (Store) MarkLoginSuccess(
	ctx context.Context, query db.Querier, tenantID string, now time.Time,
) error {
	if _, err := query.Exec(ctx, `
		UPDATE tenant_saml_configurations SET last_successful_login_at = $2 WHERE tenant_id = $1
	`, tenantID, now); err != nil {
		return fmt.Errorf("record SAML login: %w", err)
	}
	return nil
}

// ResolveMember finds the member a validated assertion may sign in as. It
// matches only within this organization, so an identity is never linked across
// organizations on a matching email alone. An outstanding invitation is
// accepted here — that is the "explicitly invited" case — and nothing else
// creates a member.
func (Store) ResolveMember(
	ctx context.Context, query db.Querier, tenantID, email string, now time.Time,
) (EligibleMember, error) {
	var member EligibleMember
	err := query.QueryRow(ctx, `
		SELECT id, email FROM tenant_members
		WHERE tenant_id = $1 AND email = $2 AND status = 'active'
	`, tenantID, email).Scan(&member.ID, &member.Email)
	if err == nil {
		return member, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return EligibleMember{}, fmt.Errorf("resolve SAML member: %w", err)
	}
	var invitedPermissions []string
	if err := query.QueryRow(ctx, `
		SELECT invited_permissions FROM tenant_magic_links
		WHERE tenant_id = $1 AND email = $2 AND member_id IS NULL
		  AND consumed_at IS NULL AND expires_at > $3
		ORDER BY created_at DESC
		LIMIT 1
	`, tenantID, email, now).Scan(&invitedPermissions); errors.Is(err, pgx.ErrNoRows) {
		return EligibleMember{}, ErrMemberNotEligible
	} else if err != nil {
		return EligibleMember{}, fmt.Errorf("find SAML invitation: %w", err)
	}
	if err := query.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, $2, $3)
		RETURNING id, email
	`, tenantID, email, invitedPermissions).Scan(&member.ID, &member.Email); err != nil {
		return EligibleMember{}, fmt.Errorf("accept SAML invitation: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_magic_links SET consumed_at = $3, member_id = $4
		WHERE tenant_id = $1 AND email = $2 AND member_id IS NULL AND consumed_at IS NULL
	`, tenantID, email, now, member.ID); err != nil {
		return EligibleMember{}, fmt.Errorf("consume SAML invitation: %w", err)
	}
	return member, nil
}

// PurgeExpired removes correlation and replay rows that can no longer affect a
// decision. Both are bounded by timestamps the validator already enforces, so a
// swept row could never have been accepted.
func (Store) PurgeExpired(ctx context.Context, query db.Querier, before time.Time) (int64, error) {
	requests, err := query.Exec(ctx,
		`DELETE FROM tenant_saml_auth_requests WHERE expires_at < $1`, before,
	)
	if err != nil {
		return 0, fmt.Errorf("purge SAML authentication requests: %w", err)
	}
	assertions, err := query.Exec(ctx,
		`DELETE FROM tenant_saml_assertions WHERE not_on_or_after < $1`, before,
	)
	if err != nil {
		return 0, fmt.Errorf("purge SAML assertions: %w", err)
	}
	return requests.RowsAffected() + assertions.RowsAffected(), nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
