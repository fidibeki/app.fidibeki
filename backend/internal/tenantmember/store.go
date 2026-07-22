package tenantmember

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Store struct {
	Now               func() time.Time
	RefreshTTL        time.Duration
	RefreshRetryGrace time.Duration
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
	member, err := scanMember(query.QueryRow(ctx, `
		SELECT id, tenant_id, email, name, permissions
		FROM tenant_members WHERE tenant_id = $1 AND email = $2
	`, tenantID, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrMemberNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("find tenant member: %w", err)
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

func (s Store) ConsumeMagicLink(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	tokenHash string,
	refreshHash string,
	userAgent string,
	ip string,
) (Member, error) {
	var linkID string
	var memberID *string
	var email string
	var invitedPermissions []string
	err := query.QueryRow(ctx, `
		SELECT id, member_id, email, invited_permissions
		FROM tenant_magic_links
		WHERE tenant_id = $1 AND token_hash = $2
		  AND consumed_at IS NULL AND expires_at > $3
		FOR UPDATE
	`, tenantID, tokenHash, s.now()).Scan(&linkID, &memberID, &email, &invitedPermissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrMagicLinkInvalid
	}
	if err != nil {
		return Member{}, fmt.Errorf("lock tenant magic link: %w", err)
	}

	var member Member
	if memberID == nil {
		allowed, err := s.IsMemberEmailDomainAllowed(ctx, query, tenantID, email)
		if err != nil {
			return Member{}, err
		}
		if !allowed {
			return Member{}, ErrMemberDomainNotVerified
		}
		member, err = scanMember(query.QueryRow(ctx, `
			INSERT INTO tenant_members (tenant_id, email, permissions)
			VALUES ($1, $2, $3)
			RETURNING id, tenant_id, email, name, permissions
		`, tenantID, email, invitedPermissions))
		if err != nil {
			return Member{}, fmt.Errorf("accept tenant invitation: %w", err)
		}
		memberID = &member.ID
	} else {
		member, err = scanMember(query.QueryRow(ctx, `
			SELECT id, tenant_id, email, name, permissions
			FROM tenant_members WHERE tenant_id = $1 AND id = $2
		`, tenantID, *memberID))
		if err != nil {
			return Member{}, fmt.Errorf("load tenant member for magic link: %w", err)
		}
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_magic_links SET consumed_at = $2, member_id = $3 WHERE id = $1
	`, linkID, s.now(), member.ID); err != nil {
		return Member{}, fmt.Errorf("consume tenant magic link: %w", err)
	}
	if err := lockTenantMember(ctx, query, tenantID, member.ID); err != nil {
		return Member{}, err
	}
	if _, err := query.Exec(ctx,
		`UPDATE tenant_members SET last_login_at = $3 WHERE tenant_id = $1 AND id = $2`,
		tenantID, member.ID, s.now(),
	); err != nil {
		return Member{}, fmt.Errorf("update tenant member login: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_member_sessions (tenant_id, member_id, refresh_hash, user_agent, ip)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet)
	`, tenantID, member.ID, refreshHash, userAgent, ip); err != nil {
		return Member{}, fmt.Errorf("create tenant member session: %w", err)
	}
	return member, nil
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
) (Member, error) {
	checkedAt := s.now()
	retryGrace := s.refreshRetryGrace()
	if _, err := lockTenantMemberForSession(ctx, query, tenantID, currentHash); err != nil {
		return Member{}, err
	}
	var sessionID string
	var issuedAt time.Time
	var revokedAt pgtype.Timestamptz
	var retryAllowed bool
	member, err := scanSessionMember(query.QueryRow(ctx, `
		SELECT session.id, session.issued_at, session.revoked_at,
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
		       member.id, member.tenant_id, member.email, member.name, member.permissions
		FROM tenant_member_sessions AS session
		JOIN tenant_members AS member
		  ON member.tenant_id = session.tenant_id AND member.id = session.member_id
		WHERE session.tenant_id = $1 AND session.refresh_hash = $2
		FOR UPDATE OF session
	`, tenantID, currentHash, nextHash, checkedAt.Add(-retryGrace), userAgent, ip,
		checkedAt.Add(-s.refreshTTL()), retryGrace > 0),
		&sessionID, &issuedAt, &revokedAt, &retryAllowed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrRefreshInvalid
	}
	if err != nil {
		return Member{}, fmt.Errorf("lock tenant member session: %w", err)
	}
	if revokedAt.Valid && !retryAllowed {
		if _, err := query.Exec(ctx, `
			UPDATE tenant_member_sessions
			SET revoked_at = COALESCE(revoked_at, $3), rotated_at = NULL, replacement_hash = NULL
			WHERE tenant_id = $1 AND member_id = $2
		`, tenantID, member.ID, checkedAt); err != nil {
			return Member{}, fmt.Errorf("revoke reused tenant member sessions: %w", err)
		}
		return Member{}, ErrRefreshReuse
	}
	if revokedAt.Valid {
		return member, nil
	}
	if !issuedAt.Add(s.refreshTTL()).After(checkedAt) {
		if _, err := query.Exec(ctx,
			`UPDATE tenant_member_sessions
			 SET revoked_at = $3, rotated_at = NULL, replacement_hash = NULL
			 WHERE tenant_id = $1 AND id = $2`,
			tenantID, sessionID, checkedAt,
		); err != nil {
			return Member{}, fmt.Errorf("revoke expired tenant member session: %w", err)
		}
		return Member{}, ErrRefreshExpired
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_member_sessions
		SET revoked_at = $3, last_used_at = $3, rotated_at = $3, replacement_hash = $4
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, sessionID, checkedAt, nextHash); err != nil {
		return Member{}, fmt.Errorf("rotate old tenant member session: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_member_sessions (tenant_id, member_id, refresh_hash, user_agent, ip)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet)
	`, tenantID, member.ID, nextHash, userAgent, ip); err != nil {
		return Member{}, fmt.Errorf("create rotated tenant member session: %w", err)
	}
	return member, nil
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
		`SELECT count(*) FROM tenant_members WHERE tenant_id = $1`, tenantID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count tenant members: %w", err)
	}
	return count, nil
}

func (s Store) ListMembers(ctx context.Context, query db.Querier, tenantID string) ([]Member, error) {
	rows, err := query.Query(ctx, `
		SELECT id, tenant_id, email, name, permissions
		FROM tenant_members WHERE tenant_id = $1 ORDER BY created_at, id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant members: %w", err)
	}
	defer rows.Close()
	members := make([]Member, 0)
	for rows.Next() {
		member, err := scanMember(rows)
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
	memberID string,
	permissions []string,
) (Member, error) {
	member, err := scanMember(query.QueryRow(ctx, `
		UPDATE tenant_members SET permissions = $3
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, email, name, permissions
	`, tenantID, memberID, permissions))
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrMemberNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("update tenant member permissions: %w", err)
	}
	return member, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanMember(row rowScanner) (Member, error) {
	var member Member
	err := row.Scan(&member.ID, &member.TenantID, &member.Email, &member.Name, &member.Permissions)
	return member, err
}

func scanSessionMember(
	row rowScanner,
	sessionID *string,
	issuedAt *time.Time,
	revokedAt *pgtype.Timestamptz,
	retryAllowed *bool,
) (Member, error) {
	var member Member
	err := row.Scan(
		sessionID, issuedAt, revokedAt, retryAllowed,
		&member.ID, &member.TenantID, &member.Email, &member.Name, &member.Permissions,
	)
	return member, err
}
