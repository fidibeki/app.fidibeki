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
	Now        func() time.Time
	RefreshTTL time.Duration
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

func (s Store) RotateSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	currentHash string,
	nextHash string,
	userAgent string,
	ip string,
) (Member, error) {
	var sessionID string
	var issuedAt time.Time
	var revokedAt pgtype.Timestamptz
	member, err := scanSessionMember(query.QueryRow(ctx, `
		SELECT session.id, session.issued_at, session.revoked_at,
		       member.id, member.tenant_id, member.email, member.name, member.permissions
		FROM tenant_member_sessions AS session
		JOIN tenant_members AS member
		  ON member.tenant_id = session.tenant_id AND member.id = session.member_id
		WHERE session.tenant_id = $1 AND session.refresh_hash = $2
		FOR UPDATE OF session
	`, tenantID, currentHash), &sessionID, &issuedAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrRefreshInvalid
	}
	if err != nil {
		return Member{}, fmt.Errorf("lock tenant member session: %w", err)
	}
	if revokedAt.Valid {
		if _, err := query.Exec(ctx, `
			UPDATE tenant_member_sessions
			SET revoked_at = COALESCE(revoked_at, $3)
			WHERE tenant_id = $1 AND member_id = $2
		`, tenantID, member.ID, s.now()); err != nil {
			return Member{}, fmt.Errorf("revoke reused tenant member sessions: %w", err)
		}
		return Member{}, ErrRefreshReuse
	}
	if issuedAt.Add(s.refreshTTL()).Before(s.now()) {
		if _, err := query.Exec(ctx,
			`UPDATE tenant_member_sessions SET revoked_at = $3 WHERE tenant_id = $1 AND id = $2`,
			tenantID, sessionID, s.now(),
		); err != nil {
			return Member{}, fmt.Errorf("revoke expired tenant member session: %w", err)
		}
		return Member{}, ErrRefreshInvalid
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_member_sessions SET revoked_at = $3, last_used_at = $3
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, sessionID, s.now()); err != nil {
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
	if _, err := query.Exec(ctx, `
		UPDATE tenant_member_sessions SET revoked_at = COALESCE(revoked_at, $3), last_used_at = $3
		WHERE tenant_id = $1 AND refresh_hash = $2
	`, tenantID, refreshHash, s.now()); err != nil {
		return fmt.Errorf("revoke tenant member session: %w", err)
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
) (Member, error) {
	var member Member
	err := row.Scan(
		sessionID, issuedAt, revokedAt,
		&member.ID, &member.TenantID, &member.Email, &member.Name, &member.Permissions,
	)
	return member, err
}
