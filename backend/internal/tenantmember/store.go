package tenantmember

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetconfig"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Store struct {
	Now               func() time.Time
	RefreshTTL        time.Duration
	RefreshRetryGrace time.Duration
}

func (Store) UpdateWidgetAppearance(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	appearance widgetconfig.AppearanceOverrides,
) error {
	payload, err := json.Marshal(appearance)
	if err != nil {
		return fmt.Errorf("encode widget appearance defaults: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenants SET widget_appearance_defaults = $2::jsonb WHERE id = $1
	`, tenantID, string(payload)); err != nil {
		return fmt.Errorf("update widget appearance defaults: %w", err)
	}
	return nil
}

func (Store) UpdateReporterAccess(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	enabled bool,
) error {
	_, err := query.Exec(ctx, `
		UPDATE tenants SET reporter_access_enabled = $2 WHERE id = $1
	`, tenantID, enabled)
	if err != nil {
		return fmt.Errorf("update reporter access policy: %w", err)
	}
	if !enabled {
		if _, err := query.Exec(ctx, `
			DELETE FROM reporter_access_tokens WHERE tenant_id = $1
		`, tenantID); err != nil {
			return fmt.Errorf("revoke tenant reporter access tokens: %w", err)
		}
	}
	return nil
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Store) refreshTTL() time.Duration {
	if s.RefreshTTL > 0 {
		return s.RefreshTTL
	}
	return 30 * 24 * time.Hour
}

func (s Store) refreshRetryGrace() time.Duration {
	if s.RefreshRetryGrace > 0 {
		return s.RefreshRetryGrace
	}
	return 0
}

func (s Store) FindMemberByEmail(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	email string,
) (Member, error) {
	member, err := scanMemberWithRole(query.QueryRow(ctx, `
		SELECT member.id, member.tenant_id, member.email, member.name, member.avatar_version,
		       member.locale, member.timezone, member.permissions,
		       member.status, member.authorization_version,
		       member.id = tenant.owner_member_id, member.last_login_at IS NOT NULL
		FROM tenant_members AS member
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		WHERE member.tenant_id = $1 AND member.email = $2
	`, tenantID, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrMemberNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("find tenant member: %w", err)
	}
	return member, nil
}

// FindMemberByID loads one active member of the workspace. Two-factor
// enrolment needs the member's email to label the authenticator entry.
func (s Store) FindMemberByID(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) (Member, error) {
	member, err := scanMemberWithRole(query.QueryRow(ctx, `
		SELECT member.id, member.tenant_id, member.email, member.name, member.avatar_version,
		       member.locale, member.timezone, member.permissions,
		       member.status, member.authorization_version,
		       member.id = tenant.owner_member_id, member.last_login_at IS NOT NULL
		FROM tenant_members AS member
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		WHERE member.tenant_id = $1 AND member.id = $2 AND member.status = 'active'
	`, tenantID, memberID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrMemberNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("find tenant member by ID: %w", err)
	}
	return member, nil
}

func (s Store) IsMemberEmailDomainAllowed(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	email string,
) (bool, error) {
	var lockedTenantID string
	if err := query.QueryRow(ctx, `
		SELECT id FROM tenants WHERE id = $1 FOR UPDATE
	`, tenantID).Scan(&lockedTenantID); err != nil {
		return false, fmt.Errorf("lock tenant member domain policy: %w", err)
	}
	var allowed bool
	err := query.QueryRow(ctx, `
		SELECT NOT EXISTS (
		         SELECT 1 FROM tenant_member_domains
		         WHERE tenant_id = $1 AND verified_at IS NOT NULL
		       ) OR EXISTS (
		         SELECT 1 FROM tenant_member_domains
		         WHERE tenant_id = $1 AND verified_at IS NOT NULL
		           AND domain = split_part($2, '@', 2)
		       )
	`, tenantID, email).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check member email domain: %w", err)
	}
	return allowed, nil
}

func (s Store) ListMemberDomains(
	ctx context.Context,
	query db.Querier,
	tenantID string,
) ([]MemberDomain, error) {
	rows, err := query.Query(ctx, `
		SELECT id, tenant_id, domain, verification_email, verification_expires_at,
		       verified_at, CASE WHEN verified_at IS NULL THEN 'pending' ELSE 'verified' END,
		       created_at, updated_at
		FROM tenant_member_domains
		WHERE tenant_id = $1
		ORDER BY verified_at NULLS LAST, domain
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list member domains: %w", err)
	}
	defer rows.Close()
	domains := make([]MemberDomain, 0)
	for rows.Next() {
		domain, err := scanMemberDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("scan member domain: %w", err)
		}
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list member domains: %w", err)
	}
	return domains, nil
}

func (s Store) RequestMemberDomainVerification(
	ctx context.Context,
	query db.Querier,
	tenantID, domain, verificationEmail, tokenHash string,
	expiresAt time.Time,
) (MemberDomain, error) {
	memberDomain, err := scanMemberDomain(query.QueryRow(ctx, `
		INSERT INTO tenant_member_domains (
			tenant_id, domain, verification_email, verification_token_hash, verification_expires_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, domain) DO UPDATE
		SET verification_email = EXCLUDED.verification_email,
		    verification_token_hash = EXCLUDED.verification_token_hash,
		    verification_expires_at = EXCLUDED.verification_expires_at
		WHERE tenant_member_domains.verified_at IS NULL
		RETURNING id, tenant_id, domain, verification_email, verification_expires_at,
		          verified_at, CASE WHEN verified_at IS NULL THEN 'pending' ELSE 'verified' END,
		          created_at, updated_at
	`, tenantID, domain, verificationEmail, tokenHash, expiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberDomain{}, ErrMemberDomainAlreadyVerified
	}
	if err != nil {
		return MemberDomain{}, fmt.Errorf("request member domain verification: %w", err)
	}
	return memberDomain, nil
}

func (s Store) VerifyMemberDomain(
	ctx context.Context,
	query db.Querier,
	tenantID, tokenHash string,
) (MemberDomain, error) {
	var lockedTenantID string
	if err := query.QueryRow(ctx, `
		SELECT id FROM tenants WHERE id = $1 FOR UPDATE
	`, tenantID).Scan(&lockedTenantID); err != nil {
		return MemberDomain{}, fmt.Errorf("lock tenant member domain policy: %w", err)
	}
	memberDomain, err := scanMemberDomain(query.QueryRow(ctx, `
		UPDATE tenant_member_domains
		SET verified_at = $3
		WHERE tenant_id = $1 AND verification_token_hash = $2
		  AND verified_at IS NULL AND verification_expires_at > $3
		RETURNING id, tenant_id, domain, verification_email, verification_expires_at,
		          verified_at, 'verified', created_at, updated_at
	`, tenantID, tokenHash, s.now()))
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberDomain{}, ErrDomainVerificationInvalid
	}
	if err != nil {
		return MemberDomain{}, fmt.Errorf("verify member domain: %w", err)
	}
	return memberDomain, nil
}

// SSOMode reports the organization's single sign-on enforcement mode. A tenant
// with no SAML configuration has no row, which is the disabled default: SSO is
// opt-in and never changes authentication for organizations that never set it
// up. Read here rather than through internal/samlsso because that package
// depends on this one.
func (Store) SSOMode(
	ctx context.Context,
	query db.Querier,
	tenantID string,
) (auth.SSOMode, error) {
	var mode auth.SSOMode
	err := query.QueryRow(ctx, `
		SELECT mode::text FROM tenant_saml_configurations WHERE tenant_id = $1
	`, tenantID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.SSOModeDisabled, nil
	}
	if err != nil {
		return "", fmt.Errorf("read tenant SSO mode: %w", err)
	}
	return mode, nil
}

func (s Store) CreateMagicLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID *string,
	email string,
	permissions []string,
	tokenHash string,
	expiresAt time.Time,
) error {
	if err := tenant.LockForAuthentication(ctx, query, tenantID, s.now()); err != nil {
		return err
	}
	if permissions == nil {
		permissions = []string{}
	}
	_, err := query.Exec(ctx, `
		INSERT INTO tenant_magic_links (
			tenant_id, member_id, email, invited_permissions, token_hash, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, tenantID, memberID, email, permissions, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("create tenant magic link: %w", err)
	}
	return nil
}

// CreateSAMLMagicLink issues the short-lived, single-use handoff the assertion
// consumer redirects through. Reusing the magic-link table keeps SAML on the
// same audited session-issuing path as local login instead of duplicating it;
// the identity columns carry the authentication provenance onto the session.
// The member must already exist — SAML never creates one, so an assertion for
// an unknown subject cannot enrol itself into a workspace.
func (s Store) CreateSAMLMagicLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	email string,
	tokenHash string,
	expiresAt time.Time,
	idpEntityID string,
	subjectID string,
) error {
	if idpEntityID == "" || subjectID == "" {
		return errors.New("SAML magic link requires an identity provider and subject")
	}
	if err := tenant.LockForAuthentication(ctx, query, tenantID, s.now()); err != nil {
		return err
	}
	_, err := query.Exec(ctx, `
		INSERT INTO tenant_magic_links (
			tenant_id, member_id, email, invited_permissions, token_hash, expires_at,
			authentication_method, saml_idp_entity_id, saml_subject_id
		) VALUES ($1, $2, $3, '{}', $4, $5, 'saml', $6, $7)
	`, tenantID, memberID, email, tokenHash, expiresAt, idpEntityID, subjectID)
	if err != nil {
		return fmt.Errorf("create tenant SAML magic link: %w", err)
	}
	return nil
}

func (s Store) ConsumeMagicLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	tokenHash string,
	refreshHash string,
	userAgent string,
	ip string,
) (Member, SessionSecurity, error) {
	if err := tenant.LockForAuthentication(ctx, query, tenantID, s.now()); err != nil {
		if errors.Is(err, tenant.ErrUnknownTenant) {
			return Member{}, SessionSecurity{}, ErrMagicLinkInvalid
		}
		return Member{}, SessionSecurity{}, err
	}
	ssoMode, ownerMemberID, err := lockTenantAuthenticationPolicy(ctx, query, tenantID)
	if err != nil {
		return Member{}, SessionSecurity{}, err
	}
	var linkID string
	var memberID *string
	var email string
	var invitedPermissions []string
	var linkMethod string
	var linkIdPEntityID, linkSubjectID *string
	err = query.QueryRow(ctx, `
		SELECT id, member_id, email, invited_permissions,
		       authentication_method, saml_idp_entity_id, saml_subject_id
		FROM tenant_magic_links
		WHERE tenant_id = $1 AND token_hash = $2
		  AND consumed_at IS NULL AND expires_at > $3
		FOR UPDATE
	`, tenantID, tokenHash, s.now()).Scan(
		&linkID, &memberID, &email, &invitedPermissions,
		&linkMethod, &linkIdPEntityID, &linkSubjectID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, SessionSecurity{}, ErrMagicLinkInvalid
	}
	if err != nil {
		return Member{}, SessionSecurity{}, fmt.Errorf("lock tenant magic link: %w", err)
	}
	if ssoMode == auth.SSOModeRequired && linkMethod == AuthenticationMethodLocal &&
		(memberID == nil || ownerMemberID == nil || *memberID != *ownerMemberID) {
		return Member{}, SessionSecurity{}, ErrMagicLinkInvalid
	}

	var member Member
	if memberID == nil && linkMethod == AuthenticationMethodSAML {
		// The SAML handoff always binds an existing member; an unbound link
		// reaching here would mean an assertion could enrol itself.
		return Member{}, SessionSecurity{}, ErrMagicLinkInvalid
	}
	if memberID == nil {
		allowed, err := s.IsMemberEmailDomainAllowed(ctx, query, tenantID, email)
		if err != nil {
			return Member{}, SessionSecurity{}, err
		}
		if !allowed {
			return Member{}, SessionSecurity{}, ErrMemberDomainNotVerified
		}
		member, err = scanMember(query.QueryRow(ctx, `
			INSERT INTO tenant_members (tenant_id, email, permissions)
			VALUES ($1, $2, $3)
			RETURNING id, tenant_id, email, name, avatar_version, locale, timezone,
			          permissions, status, authorization_version
		`, tenantID, email, invitedPermissions))
		if err != nil {
			return Member{}, SessionSecurity{}, fmt.Errorf("accept tenant invitation: %w", err)
		}
		memberID = &member.ID
	} else {
		member, err = scanMemberWithRole(query.QueryRow(ctx, `
				SELECT member.id, member.tenant_id, member.email, member.name, member.avatar_version,
				       member.locale, member.timezone, member.permissions,
				       member.status, member.authorization_version,
				       member.id = tenant.owner_member_id, member.last_login_at IS NOT NULL
			FROM tenant_members AS member
			JOIN tenants AS tenant ON tenant.id = member.tenant_id
			WHERE member.tenant_id = $1 AND member.id = $2 AND member.status = 'active'
		`, tenantID, *memberID))
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, SessionSecurity{}, ErrMagicLinkInvalid
		}
		if err != nil {
			return Member{}, SessionSecurity{}, fmt.Errorf("load tenant member for magic link: %w", err)
		}
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_magic_links SET consumed_at = $2, member_id = $3 WHERE id = $1
	`, linkID, s.now(), member.ID); err != nil {
		return Member{}, SessionSecurity{}, fmt.Errorf("consume tenant magic link: %w", err)
	}
	if err := lockTenantMember(ctx, query, tenantID, member.ID); err != nil {
		return Member{}, SessionSecurity{}, err
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, data)
		SELECT tenant.id, 'tenant.owner_activated', 'tenant', tenant.id,
		       jsonb_build_object('member_id', $2::text)
		FROM tenants AS tenant
		WHERE tenant.id = $1 AND tenant.welcome_email_sent_at IS NULL
		  AND tenant.owner_member_id = $2::uuid
		ON CONFLICT (event, resource_type, resource_id, event_key) DO NOTHING
	`, tenantID, member.ID); err != nil {
		return Member{}, SessionSecurity{}, fmt.Errorf("schedule owner welcome email: %w", err)
	}
	if _, err := query.Exec(ctx,
		`UPDATE tenant_members SET last_login_at = $3 WHERE tenant_id = $1 AND id = $2`,
		tenantID, member.ID, s.now(),
	); err != nil {
		return Member{}, SessionSecurity{}, fmt.Errorf("update tenant member login: %w", err)
	}
	member.Verified = true
	security, err := s.sessionSecurity(ctx, query, tenantID, member.ID)
	if err != nil {
		return Member{}, SessionSecurity{}, err
	}
	// A local magic link is a Fidibeki proof of identity, so the session stays
	// subject to the Fidibeki two-factor policy and an enrolled member owes the
	// TOTP challenge before the session becomes usable. A SAML handoff instead
	// delegates that assurance to the identity provider, so it is never
	// challenged — and the enrolment it bypasses is left untouched, ready to
	// apply again if local authentication is restored.
	security.AuthenticationMethod = AuthenticationMethodLocal
	security.TwoFactorState = TwoFactorStateNotRequired
	if linkMethod == AuthenticationMethodSAML {
		security.AuthenticationMethod = AuthenticationMethodSAML
		if linkIdPEntityID != nil {
			security.SAMLIdPEntityID = *linkIdPEntityID
		}
		if linkSubjectID != nil {
			security.SAMLSubjectID = *linkSubjectID
		}
	} else if security.Enrolled {
		security.TwoFactorState = TwoFactorStatePending
	}
	security = security.NormalizedForPersistence()
	security.breakGlass = ssoMode == auth.SSOModeRequired &&
		linkMethod == AuthenticationMethodLocal && member.IsOwner()
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_member_sessions (
			tenant_id, member_id, refresh_hash, user_agent, ip,
			authentication_method, two_factor_state, saml_idp_entity_id, saml_subject_id
		)
		VALUES (
			$1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet, $6, $7,
			NULLIF($8, ''), NULLIF($9, '')
		)
	`, tenantID, member.ID, refreshHash, userAgent, ip,
		security.AuthenticationMethod, security.TwoFactorState,
		security.SAMLIdPEntityID, security.SAMLSubjectID); err != nil {
		return Member{}, SessionSecurity{}, fmt.Errorf("create tenant member session: %w", err)
	}
	return member, security, nil
}

func lockTenantAuthenticationPolicy(
	ctx context.Context,
	query db.Querier,
	tenantID string,
) (auth.SSOMode, *string, error) {
	var mode auth.SSOMode
	var ownerMemberID *string
	if err := query.QueryRow(ctx, `
		SELECT COALESCE(configuration.mode::text, 'disabled'), tenant.owner_member_id
		FROM tenants AS tenant
		LEFT JOIN tenant_saml_configurations AS configuration
		  ON configuration.tenant_id = tenant.id
		WHERE tenant.id = $1
		FOR SHARE OF tenant
	`, tenantID).Scan(&mode, &ownerMemberID); errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrMagicLinkInvalid
	} else if err != nil {
		return "", nil, fmt.Errorf("lock tenant authentication policy: %w", err)
	}
	return mode, ownerMemberID, nil
}

type memberDomainScanner interface {
	Scan(...any) error
}

func scanMemberDomain(row memberDomainScanner) (MemberDomain, error) {
	var domain MemberDomain
	err := row.Scan(
		&domain.ID, &domain.TenantID, &domain.Domain, &domain.VerificationEmail,
		&domain.VerificationExpiresAt, &domain.VerifiedAt, &domain.Status,
		&domain.CreatedAt, &domain.UpdatedAt,
	)
	return domain, err
}

func (s Store) RotateSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	currentHash string,
	nextHash string,
	userAgent string,
	ip string,
) (Member, SessionSecurity, error) {
	if err := tenant.LockForAuthentication(ctx, query, tenantID, s.now()); err != nil {
		if errors.Is(err, tenant.ErrUnknownTenant) {
			return Member{}, SessionSecurity{}, ErrRefreshInvalid
		}
		return Member{}, SessionSecurity{}, err
	}
	ssoMode, _, err := lockTenantAuthenticationPolicy(ctx, query, tenantID)
	if errors.Is(err, ErrMagicLinkInvalid) {
		return Member{}, SessionSecurity{}, ErrRefreshInvalid
	}
	if err != nil {
		return Member{}, SessionSecurity{}, err
	}
	checkedAt := s.now()
	retryGrace := s.refreshRetryGrace()
	if _, err := lockTenantMemberForSession(ctx, query, tenantID, currentHash); err != nil {
		return Member{}, SessionSecurity{}, err
	}
	var sessionID string
	var issuedAt time.Time
	var revokedAt pgtype.Timestamptz
	var retryAllowed bool
	security := SessionSecurity{}
	member, err := scanSessionMember(query.QueryRow(ctx, `
		SELECT session.id, session.issued_at, session.revoked_at,
		       session.authentication_method, session.two_factor_state,
		       session.saml_idp_entity_id, session.saml_subject_id,
		       tenant.two_factor_policy = 'required',
		       EXISTS (
		           SELECT 1 FROM tenant_member_totp AS enrolment
		           WHERE enrolment.tenant_id = session.tenant_id
		             AND enrolment.member_id = session.member_id
		             AND enrolment.confirmed_at IS NOT NULL
		       ),
		       session.rotated_at IS NOT NULL
		         AND session.rotated_at >= $4
		         AND $8
		         AND session.replacement_hash = $3
		         AND session.user_agent IS NOT DISTINCT FROM NULLIF($5, '')
		         AND session.ip IS NOT DISTINCT FROM NULLIF($6, '')::inet
		         AND EXISTS (
		           SELECT 1 FROM tenant_member_sessions AS replacement
		           WHERE replacement.tenant_id = session.tenant_id
		             AND replacement.refresh_hash = session.replacement_hash
		             AND replacement.revoked_at IS NULL
		             AND replacement.issued_at > $7
		         ),
		       member.id, member.tenant_id, member.email, member.name, member.avatar_version,
		       member.locale, member.timezone, member.permissions,
		       member.status, member.authorization_version,
		       member.id = tenant.owner_member_id, member.last_login_at IS NOT NULL
		FROM tenant_member_sessions AS session
		JOIN tenant_members AS member
		  ON member.tenant_id = session.tenant_id AND member.id = session.member_id
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		WHERE session.tenant_id = $1 AND session.refresh_hash = $2
		  AND member.status = 'active'
		FOR UPDATE OF session
	`, tenantID, currentHash, nextHash, checkedAt.Add(-retryGrace), userAgent, ip,
		checkedAt.Add(-s.refreshTTL()), retryGrace > 0),
		&sessionID, &issuedAt, &revokedAt, &retryAllowed, &security,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, SessionSecurity{}, ErrRefreshInvalid
	}
	if err != nil {
		return Member{}, SessionSecurity{}, fmt.Errorf("lock tenant member session: %w", err)
	}
	if ssoMode == auth.SSOModeRequired && !security.IsSAML() && !member.IsOwner() {
		if _, err := query.Exec(ctx, `
			UPDATE tenant_member_sessions
			SET revoked_at = COALESCE(revoked_at, $3),
			    rotated_at = NULL,
			    replacement_hash = NULL
			WHERE tenant_id = $1
			  AND member_id = $2
			  AND authentication_method = 'local'
		`, tenantID, member.ID, checkedAt); err != nil {
			return Member{}, SessionSecurity{}, fmt.Errorf(
				"revoke local sessions blocked by required SSO: %w", err,
			)
		}
		return Member{}, SessionSecurity{}, errors.Join(ErrRefreshInvalid, ErrRefreshSSORequired)
	}
	if revokedAt.Valid && !retryAllowed {
		if _, err := query.Exec(ctx, `
			UPDATE tenant_member_sessions
			SET revoked_at = COALESCE(revoked_at, $3), rotated_at = NULL, replacement_hash = NULL
			WHERE tenant_id = $1 AND member_id = $2
		`, tenantID, member.ID, checkedAt); err != nil {
			return Member{}, SessionSecurity{}, fmt.Errorf("revoke reused tenant member sessions: %w", err)
		}
		return Member{}, SessionSecurity{}, ErrRefreshReuse
	}
	if revokedAt.Valid {
		// A retried rotation returns the successor that already exists, so the
		// session's two-factor state is reported unchanged.
		return member, security, nil
	}
	if !issuedAt.Add(s.refreshTTL()).After(checkedAt) {
		if _, err := query.Exec(ctx,
			`UPDATE tenant_member_sessions
			 SET revoked_at = $3, rotated_at = NULL, replacement_hash = NULL
			 WHERE tenant_id = $1 AND id = $2`,
			tenantID, sessionID, checkedAt,
		); err != nil {
			return Member{}, SessionSecurity{}, fmt.Errorf("revoke expired tenant member session: %w", err)
		}
		return Member{}, SessionSecurity{}, ErrRefreshExpired
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_member_sessions
		SET revoked_at = $3, last_used_at = $3, rotated_at = $3, replacement_hash = $4
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, sessionID, checkedAt, nextHash); err != nil {
		return Member{}, SessionSecurity{}, fmt.Errorf("rotate old tenant member session: %w", err)
	}
	// The successor inherits how the session authenticated and whether it still
	// owes a challenge, so refreshing can never launder a pending session into
	// a satisfied one.
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_member_sessions (
			tenant_id, member_id, refresh_hash, user_agent, ip,
			authentication_method, two_factor_state, two_factor_verified_at,
			saml_idp_entity_id, saml_subject_id
		)
		SELECT $1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet,
		       previous.authentication_method, previous.two_factor_state, previous.two_factor_verified_at,
		       previous.saml_idp_entity_id, previous.saml_subject_id
		FROM tenant_member_sessions AS previous
		WHERE previous.tenant_id = $1 AND previous.id = $6
	`, tenantID, member.ID, nextHash, userAgent, ip, sessionID); err != nil {
		return Member{}, SessionSecurity{}, fmt.Errorf("create rotated tenant member session: %w", err)
	}
	return member, security, nil
}

func (s Store) RevokeSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	refreshHash string,
) error {
	if _, err := lockTenantMemberForSession(ctx, query, tenantID, refreshHash); err != nil {
		if errors.Is(err, ErrRefreshInvalid) {
			return nil
		}
		return err
	}
	checkedAt := s.now()
	if _, err := query.Exec(ctx, `
		WITH RECURSIVE session_chain AS (
			SELECT id, replacement_hash
			FROM tenant_member_sessions
			WHERE tenant_id = $1 AND refresh_hash = $2
			UNION
			SELECT successor.id, successor.replacement_hash
			FROM tenant_member_sessions AS successor
			JOIN session_chain AS predecessor
			  ON successor.refresh_hash = predecessor.replacement_hash
			WHERE successor.tenant_id = $1
		)
		UPDATE tenant_member_sessions AS session
		SET revoked_at = COALESCE(session.revoked_at, $3), last_used_at = $3,
		    rotated_at = NULL, replacement_hash = NULL
		WHERE session.tenant_id = $1
		  AND session.id IN (SELECT id FROM session_chain)
	`, tenantID, refreshHash, checkedAt); err != nil {
		return fmt.Errorf("revoke tenant member session: %w", err)
	}
	return nil
}

func lockTenantMemberForSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	refreshHash string,
) (string, error) {
	var memberID string
	if err := query.QueryRow(ctx, `
		SELECT member_id FROM tenant_member_sessions
		WHERE tenant_id = $1 AND refresh_hash = $2
	`, tenantID, refreshHash).Scan(&memberID); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRefreshInvalid
	} else if err != nil {
		return "", fmt.Errorf("find tenant member session owner: %w", err)
	}
	if err := lockTenantMember(ctx, query, tenantID, memberID); err != nil {
		return "", err
	}
	return memberID, nil
}

func lockTenantMember(ctx context.Context, query db.Querier, tenantID, memberID string) error {
	var lockedID string
	if err := query.QueryRow(ctx, `
		SELECT id FROM tenant_members WHERE tenant_id = $1 AND id = $2 FOR UPDATE
	`, tenantID, memberID).Scan(&lockedID); err != nil {
		return fmt.Errorf("lock tenant member session owner: %w", err)
	}
	return nil
}

func (s Store) CountMembers(ctx context.Context, query db.Querier, tenantID string) (int, error) {
	var count int
	if err := query.QueryRow(ctx,
		`SELECT count(*) FROM tenant_members WHERE tenant_id = $1 AND status <> 'removed'`, tenantID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count tenant members: %w", err)
	}
	return count, nil
}

func (s Store) ListMembers(ctx context.Context, query db.Querier, tenantID string) ([]Member, error) {
	rows, err := query.Query(ctx, `
		SELECT member.id, member.tenant_id, member.email, member.name, member.avatar_version,
		       member.locale, member.timezone, member.permissions,
		       member.status, member.authorization_version,
		       member.id = tenant.owner_member_id, member.last_login_at IS NOT NULL
		FROM tenant_members AS member
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		WHERE member.tenant_id = $1
		ORDER BY member.created_at, member.id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant members: %w", err)
	}
	defer rows.Close()
	members := make([]Member, 0)
	for rows.Next() {
		member, err := scanMemberWithRole(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant members: %w", err)
	}
	return members, nil
}

func (s Store) UpdatePermissions(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	memberID string,
	permissions []string,
) (Member, error) {
	ownerMemberID, current, err := lockWorkspaceMember(ctx, query, tenantID, memberID)
	if err != nil {
		return Member{}, err
	}
	if err := requireActiveMemberActor(ctx, query, tenantID, actorMemberID); err != nil {
		if errors.Is(err, ErrMemberActorInvalid) {
			if recordErr := recordTenantAccessEvent(ctx, query, tenantID, "member.permission_change_denied", &actorMemberID, &memberID, map[string]any{
				"reason": "actor_is_not_active_member",
			}); recordErr != nil {
				return Member{}, recordErr
			}
		}
		return Member{}, err
	}
	if ownerMemberID != nil && *ownerMemberID == memberID {
		if err := recordTenantAccessEvent(ctx, query, tenantID, "member.permission_change_denied", &actorMemberID, &memberID, map[string]any{
			"reason": "subject_is_owner",
		}); err != nil {
			return Member{}, err
		}
		return Member{}, ErrOwnerProtection
	}
	if current.Status == MemberStatusRemoved {
		if err := recordTenantAccessEvent(ctx, query, tenantID, "member.permission_change_denied", &actorMemberID, &memberID, map[string]any{
			"reason": "subject_is_removed",
		}); err != nil {
			return Member{}, err
		}
		return Member{}, ErrMemberAlreadyRemoved
	}
	if current.Status != MemberStatusActive {
		return Member{}, ErrMemberStatusInvalid
	}
	if isRecoverableAdministrator(current.Permissions) && !isRecoverableAdministrator(permissions) {
		last, err := isLastRecoverableAdministrator(ctx, query, tenantID, memberID)
		if err != nil {
			return Member{}, err
		}
		if last {
			if err := recordTenantAccessEvent(ctx, query, tenantID, "member.permission_change_denied", &actorMemberID, &memberID, map[string]any{
				"reason": "last_recoverable_administrator",
			}); err != nil {
				return Member{}, err
			}
			return Member{}, ErrLastAdministrator
		}
	}
	member, err := scanMember(query.QueryRow(ctx, `
		UPDATE tenant_members
		SET permissions = $3, authorization_version = authorization_version + 1
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, email, name, avatar_version, locale, timezone,
		          permissions, status, authorization_version
	`, tenantID, memberID, permissions))
	if err != nil {
		return Member{}, fmt.Errorf("update tenant member permissions: %w", err)
	}
	revokedAt := s.now()
	revokedSessions, err := revokeMemberAccess(ctx, query, tenantID, memberID, revokedAt)
	if err != nil {
		return Member{}, err
	}
	if err := recordTenantAccessEvent(ctx, query, tenantID, "member.permissions_changed", &actorMemberID, &memberID, map[string]any{
		"previous_permissions":  current.Permissions,
		"permissions":           member.Permissions,
		"revoked_session_count": revokedSessions,
		"sessions_revoked_at":   revokedAt,
	}); err != nil {
		return Member{}, err
	}
	return member, nil
}

func (s Store) UpdateMemberStatus(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	memberID string,
	status MemberStatus,
) (Member, error) {
	if status != MemberStatusActive && status != MemberStatusDeactivated {
		return Member{}, ErrMemberStatusInvalid
	}
	return s.changeMemberLifecycle(ctx, query, tenantID, actorMemberID, memberID, status)
}

func (s Store) RemoveMember(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	memberID string,
) (Member, error) {
	return s.changeMemberLifecycle(ctx, query, tenantID, actorMemberID, memberID, MemberStatusRemoved)
}

func (s Store) changeMemberLifecycle(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	memberID string,
	status MemberStatus,
) (Member, error) {
	ownerMemberID, current, err := lockWorkspaceMember(ctx, query, tenantID, memberID)
	if err != nil {
		return Member{}, err
	}
	if err := requireActiveMemberActor(ctx, query, tenantID, actorMemberID); err != nil {
		if errors.Is(err, ErrMemberActorInvalid) {
			if recordErr := recordTenantAccessEvent(ctx, query, tenantID, "member.lifecycle_change_denied", &actorMemberID, &memberID, map[string]any{
				"reason": "actor_is_not_active_member",
			}); recordErr != nil {
				return Member{}, recordErr
			}
		}
		return Member{}, err
	}
	deny := func(reason string, domainErr error) (Member, error) {
		if err := recordTenantAccessEvent(ctx, query, tenantID, "member.lifecycle_change_denied", &actorMemberID, &memberID, map[string]any{
			"reason": reason,
			"status": status,
		}); err != nil {
			return Member{}, err
		}
		return Member{}, domainErr
	}
	if ownerMemberID != nil && *ownerMemberID == memberID {
		return deny("subject_is_owner", ErrOwnerProtection)
	}
	if current.Status == MemberStatusRemoved {
		return deny("subject_is_removed", ErrMemberAlreadyRemoved)
	}
	if current.Status == status {
		return current, nil
	}
	if status == MemberStatusActive && current.Status != MemberStatusDeactivated {
		return Member{}, ErrMemberStatusInvalid
	}
	if status != MemberStatusActive && isRecoverableAdministrator(current.Permissions) {
		last, err := isLastRecoverableAdministrator(ctx, query, tenantID, memberID)
		if err != nil {
			return Member{}, err
		}
		if last {
			return deny("last_recoverable_administrator", ErrLastAdministrator)
		}
	}
	member, err := scanMember(query.QueryRow(ctx, `
		UPDATE tenant_members
		SET status = $3,
		    permissions = CASE WHEN $3 = 'active'::member_status THEN permissions ELSE '{}'::text[] END,
		    authorization_version = authorization_version + 1
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, email, name, avatar_version, locale, timezone,
		          permissions, status, authorization_version
	`, tenantID, memberID, status))
	if err != nil {
		return Member{}, fmt.Errorf("update tenant member lifecycle: %w", err)
	}
	revokedAt := s.now()
	revokedSessions, err := revokeMemberAccess(ctx, query, tenantID, memberID, revokedAt)
	if err != nil {
		return Member{}, err
	}
	if status != MemberStatusActive {
		if _, err := query.Exec(ctx, `
			UPDATE tenant_magic_links
			SET consumed_at = COALESCE(consumed_at, $3)
			WHERE tenant_id = $1 AND (member_id = $2 OR email = $4)
		`, tenantID, memberID, s.now(), current.Email); err != nil {
			return Member{}, fmt.Errorf("consume inactive tenant member links: %w", err)
		}
	}
	kind := map[MemberStatus]string{
		MemberStatusActive:      "member.reactivated",
		MemberStatusDeactivated: "member.deactivated",
		MemberStatusRemoved:     "member.removed",
	}[status]
	if err := recordTenantAccessEvent(ctx, query, tenantID, kind, &actorMemberID, &memberID, map[string]any{
		"previous_status":       current.Status,
		"status":                member.Status,
		"previous_permissions":  current.Permissions,
		"permissions":           member.Permissions,
		"revoked_session_count": revokedSessions,
		"sessions_revoked_at":   revokedAt,
	}); err != nil {
		return Member{}, err
	}
	return member, nil
}

func lockWorkspaceMember(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) (*string, Member, error) {
	var ownerMemberID *string
	if err := query.QueryRow(ctx, `
		SELECT owner_member_id FROM tenants WHERE id = $1 FOR UPDATE
	`, tenantID).Scan(&ownerMemberID); err != nil {
		return nil, Member{}, fmt.Errorf("lock workspace owner: %w", err)
	}
	member, err := scanMember(query.QueryRow(ctx, `
		SELECT id, tenant_id, email, name, avatar_version, locale, timezone,
		       permissions, status, authorization_version
		FROM tenant_members WHERE tenant_id = $1 AND id = $2 FOR UPDATE
	`, tenantID, memberID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Member{}, ErrMemberNotFound
	}
	if err != nil {
		return nil, Member{}, fmt.Errorf("lock tenant member: %w", err)
	}
	return ownerMemberID, member, nil
}

func isRecoverableAdministrator(permissions []string) bool {
	return auth.HasPermission(permissions, auth.PermissionMemberUpdate) &&
		auth.HasPermission(permissions, auth.PermissionTenantUpdate)
}

func requireActiveMemberActor(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
) error {
	var active bool
	if err := query.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tenant_members
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		)
	`, tenantID, actorMemberID).Scan(&active); err != nil {
		return fmt.Errorf("validate tenant member actor: %w", err)
	}
	if !active {
		return ErrMemberActorInvalid
	}
	return nil
}

func isLastRecoverableAdministrator(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) (bool, error) {
	var others int
	if err := query.QueryRow(ctx, `
		SELECT count(*)
		FROM tenant_members
		WHERE tenant_id = $1 AND id <> $2 AND status = 'active'
		  AND permissions @> ARRAY['member:update', 'tenant:update']::text[]
	`, tenantID, memberID).Scan(&others); err != nil {
		return false, fmt.Errorf("count recoverable administrators: %w", err)
	}
	return others == 0, nil
}

func revokeMemberAccess(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	revokedAt time.Time,
) (int64, error) {
	result, err := query.Exec(ctx, `
		UPDATE tenant_member_sessions
		SET revoked_at = COALESCE(revoked_at, $3), rotated_at = NULL, replacement_hash = NULL
		WHERE tenant_id = $1 AND member_id = $2
		  AND (revoked_at IS NULL OR rotated_at IS NOT NULL OR replacement_hash IS NOT NULL)
	`, tenantID, memberID, revokedAt)
	if err != nil {
		return 0, fmt.Errorf("revoke tenant member access: %w", err)
	}
	return result.RowsAffected(), nil
}

func (s Store) TransferOwnership(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	targetMemberID string,
	confirmation string,
) (Member, error) {
	var slug string
	var ownerMemberID *string
	if err := query.QueryRow(ctx, `
		SELECT slug, owner_member_id FROM tenants WHERE id = $1 FOR UPDATE
	`, tenantID).Scan(&slug, &ownerMemberID); err != nil {
		return Member{}, fmt.Errorf("lock workspace ownership: %w", err)
	}
	deny := func(reason string, target *string, err error) (Member, error) {
		if insertErr := recordOwnershipDenied(ctx, query, tenantID, actorMemberID, target, reason); insertErr != nil {
			return Member{}, insertErr
		}
		return Member{}, err
	}
	if confirmation != slug {
		return deny("confirmation_mismatch", nil, ErrOwnershipConfirmation)
	}
	if ownerMemberID == nil || *ownerMemberID != actorMemberID {
		return deny("actor_is_not_owner", nil, ErrOwnerProtection)
	}
	if targetMemberID == actorMemberID {
		return deny("target_is_current_owner", &targetMemberID, ErrOwnershipTargetInvalid)
	}
	var actorLastLogin *time.Time
	if err := query.QueryRow(ctx, `
		SELECT last_login_at FROM tenant_members
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE
	`, tenantID, actorMemberID).Scan(&actorLastLogin); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny("actor_is_not_active", nil, ErrOwnerProtection)
		}
		return Member{}, fmt.Errorf("lock workspace owner member: %w", err)
	}
	if actorLastLogin == nil || actorLastLogin.Before(s.now().Add(-10*time.Minute)) {
		return deny("actor_authentication_is_stale", nil, ErrRecentAuthenticationRequired)
	}
	var targetLastLogin *time.Time
	if err := query.QueryRow(ctx, `
		SELECT last_login_at FROM tenant_members
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE
	`, tenantID, targetMemberID).Scan(&targetLastLogin); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny("target_is_not_active", nil, ErrOwnershipTargetInvalid)
		}
		return Member{}, fmt.Errorf("lock ownership target: %w", err)
	}
	if targetLastLogin == nil {
		return deny("target_is_not_verified", &targetMemberID, ErrOwnershipTargetInvalid)
	}

	target, err := scanMember(query.QueryRow(ctx, `
		UPDATE tenant_members
		SET permissions = $3, authorization_version = authorization_version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		RETURNING id, tenant_id, email, name, avatar_version, locale, timezone,
		          permissions, status, authorization_version
	`, tenantID, targetMemberID, auth.TenantPermissionStrings()))
	if err != nil {
		return Member{}, fmt.Errorf("elevate workspace owner: %w", err)
	}
	revokedAt := s.now()
	revokedSessions, err := revokeMemberAccess(ctx, query, tenantID, targetMemberID, revokedAt)
	if err != nil {
		return Member{}, err
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenants SET owner_member_id = $2 WHERE id = $1
	`, tenantID, targetMemberID); err != nil {
		return Member{}, fmt.Errorf("transfer workspace ownership: %w", err)
	}
	if err := recordTenantAccessEvent(ctx, query, tenantID, "ownership.transferred", &actorMemberID, &targetMemberID, map[string]any{
		"previous_owner_member_id": actorMemberID,
		"new_owner_member_id":      targetMemberID,
		"revoked_session_count":    revokedSessions,
		"sessions_revoked_at":      revokedAt,
	}); err != nil {
		return Member{}, err
	}
	target.Role = "owner"
	target.Verified = true
	return target, nil
}

func recordOwnershipDenied(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	targetMemberID *string,
	reason string,
) error {
	return recordTenantAccessEvent(
		ctx, query, tenantID, "ownership.transfer_denied", &actorMemberID, targetMemberID,
		map[string]any{"reason": reason},
	)
}

func recordTenantAccessEvent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	kind string,
	actorMemberID *string,
	subjectMemberID *string,
	data map[string]any,
) error {
	if actorMemberID != nil {
		data["actor_credential_id"] = *actorMemberID
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode tenant access event: %w", err)
	}
	_, err = query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id, data, created_at
		)
		SELECT $1::uuid, $2::text,
		       CASE WHEN $3::uuid IS NOT NULL AND EXISTS (
		           SELECT 1 FROM tenant_members WHERE tenant_id = $1::uuid AND id = $3::uuid
		       ) THEN $3::uuid END,
		       CASE WHEN $4::uuid IS NOT NULL AND EXISTS (
		           SELECT 1 FROM tenant_members WHERE tenant_id = $1::uuid AND id = $4::uuid
		       ) THEN $4::uuid END,
		       $5::jsonb, now()
	`, tenantID, kind, actorMemberID, subjectMemberID, string(payload))
	if err != nil {
		return fmt.Errorf("record tenant access event: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanMember(row rowScanner) (Member, error) {
	var member Member
	err := row.Scan(
		&member.ID, &member.TenantID, &member.Email, &member.Name, &member.AvatarVersion,
		&member.Locale, &member.Timezone, &member.Permissions,
		&member.Status, &member.AuthorizationVersion,
	)
	member.Role = "member"
	return member, err
}

func scanMemberWithRole(row rowScanner) (Member, error) {
	var member Member
	var owner bool
	err := row.Scan(
		&member.ID, &member.TenantID, &member.Email, &member.Name, &member.AvatarVersion,
		&member.Locale, &member.Timezone, &member.Permissions,
		&member.Status, &member.AuthorizationVersion, &owner, &member.Verified,
	)
	member.Role = "member"
	if owner {
		member.Role = "owner"
	}
	return member, err
}

func scanSessionMember(
	row rowScanner,
	sessionID *string,
	issuedAt *time.Time,
	revokedAt *pgtype.Timestamptz,
	retryAllowed *bool,
	security *SessionSecurity,
) (Member, error) {
	var member Member
	var owner bool
	var idpEntityID, subjectID *string
	err := row.Scan(
		sessionID, issuedAt, revokedAt,
		&security.AuthenticationMethod, &security.TwoFactorState,
		&idpEntityID, &subjectID,
		&security.PolicyRequired, &security.Enrolled,
		retryAllowed,
		&member.ID, &member.TenantID, &member.Email, &member.Name, &member.AvatarVersion,
		&member.Locale, &member.Timezone, &member.Permissions,
		&member.Status, &member.AuthorizationVersion, &owner, &member.Verified,
	)
	if idpEntityID != nil {
		security.SAMLIdPEntityID = *idpEntityID
	}
	if subjectID != nil {
		security.SAMLSubjectID = *subjectID
	}
	member.Role = "member"
	if owner {
		member.Role = "owner"
	}
	return member, err
}
