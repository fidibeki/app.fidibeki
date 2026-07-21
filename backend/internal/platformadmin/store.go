package platformadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type AdminDatabase interface {
	db.Querier
	Begin(context.Context) (pgx.Tx, error)
}

type Store struct {
	Database   AdminDatabase
	Now        func() time.Time
	RefreshTTL time.Duration
}

func (s Store) refreshTTL() time.Duration {
	if s.RefreshTTL > 0 {
		return s.RefreshTTL
	}
	return RefreshTTL
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Store) FindAdminByEmail(ctx context.Context, email string) (Admin, error) {
	admin, err := scanAdmin(s.Database.QueryRow(ctx, `
		SELECT id, email, name, permissions
		FROM platform_admins
		WHERE email = $1
	`, normalizeEmail(email)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrAdminNotFound
	}
	if err != nil {
		return Admin{}, fmt.Errorf("find platform admin: %w", err)
	}
	return admin, nil
}

func (s Store) CreateMagicLink(
	ctx context.Context,
	admin Admin,
	tokenHash string,
	expiresAt time.Time,
) error {
	_, err := s.Database.Exec(ctx, `
		INSERT INTO platform_admin_magic_links (admin_id, email, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, admin.ID, admin.Email, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("create platform admin magic link: %w", err)
	}
	return nil
}

func (s Store) DeleteMagicLink(ctx context.Context, tokenHash string) error {
	if _, err := s.Database.Exec(ctx,
		`DELETE FROM platform_admin_magic_links WHERE token_hash = $1 AND consumed_at IS NULL`, tokenHash,
	); err != nil {
		return fmt.Errorf("delete platform admin magic link: %w", err)
	}
	return nil
}

func (s Store) ConsumeMagicLink(
	ctx context.Context,
	tokenHash string,
	refreshHash string,
	userAgent string,
	ip string,
) (Admin, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return Admin{}, fmt.Errorf("begin magic link transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var linkID string
	admin, err := scanAdminWithPrefix(tx.QueryRow(ctx, `
		SELECT link.id, admin.id, admin.email, admin.name, admin.permissions
		FROM platform_admin_magic_links AS link
		JOIN platform_admins AS admin ON admin.id = link.admin_id
		WHERE link.token_hash = $1
		  AND link.consumed_at IS NULL
		  AND link.expires_at > $2
		FOR UPDATE OF link
	`, tokenHash, s.now()), &linkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrMagicLinkInvalid
	}
	if err != nil {
		return Admin{}, fmt.Errorf("lock platform admin magic link: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE platform_admin_magic_links SET consumed_at = $2 WHERE id = $1`, linkID, s.now(),
	); err != nil {
		return Admin{}, fmt.Errorf("consume platform admin magic link: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE platform_admins SET last_login_at = $2 WHERE id = $1`, admin.ID, s.now(),
	); err != nil {
		return Admin{}, fmt.Errorf("update platform admin login: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_admin_sessions (admin_id, refresh_hash, user_agent, ip)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, '')::inet)
	`, admin.ID, refreshHash, userAgent, ip); err != nil {
		return Admin{}, fmt.Errorf("create platform admin session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Admin{}, fmt.Errorf("commit magic link transaction: %w", err)
	}
	return admin, nil
}

func (s Store) RotateSession(
	ctx context.Context,
	currentHash string,
	nextHash string,
	userAgent string,
	ip string,
) (Admin, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return Admin{}, fmt.Errorf("begin refresh transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sessionID string
	var issuedAt time.Time
	var revokedAt pgtype.Timestamptz
	admin, err := scanSessionAdmin(tx.QueryRow(ctx, `
		SELECT session.id, session.issued_at, session.revoked_at,
		       admin.id, admin.email, admin.name, admin.permissions
		FROM platform_admin_sessions AS session
		JOIN platform_admins AS admin ON admin.id = session.admin_id
		WHERE session.refresh_hash = $1
		FOR UPDATE OF session
	`, currentHash), &sessionID, &issuedAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrRefreshInvalid
	}
	if err != nil {
		return Admin{}, fmt.Errorf("lock platform admin session: %w", err)
	}

	if revokedAt.Valid {
		if _, err := tx.Exec(ctx,
			`UPDATE platform_admin_sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE admin_id = $1`,
			admin.ID, s.now(),
		); err != nil {
			return Admin{}, fmt.Errorf("revoke reused platform admin sessions: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Admin{}, fmt.Errorf("commit reused refresh revocation: %w", err)
		}
		return Admin{}, ErrRefreshReuse
	}
	if issuedAt.Add(s.refreshTTL()).Before(s.now()) {
		if _, err := tx.Exec(ctx,
			`UPDATE platform_admin_sessions SET revoked_at = $2 WHERE id = $1`, sessionID, s.now(),
		); err != nil {
			return Admin{}, fmt.Errorf("revoke expired platform admin session: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Admin{}, fmt.Errorf("commit expired refresh revocation: %w", err)
		}
		return Admin{}, ErrRefreshInvalid
	}

	if _, err := tx.Exec(ctx,
		`UPDATE platform_admin_sessions SET revoked_at = $2, last_used_at = $2 WHERE id = $1`, sessionID, s.now(),
	); err != nil {
		return Admin{}, fmt.Errorf("rotate old platform admin session: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_admin_sessions (admin_id, refresh_hash, user_agent, ip)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, '')::inet)
	`, admin.ID, nextHash, userAgent, ip); err != nil {
		return Admin{}, fmt.Errorf("create rotated platform admin session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Admin{}, fmt.Errorf("commit refresh transaction: %w", err)
	}
	return admin, nil
}

func (s Store) RevokeSession(ctx context.Context, refreshHash string) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE platform_admin_sessions
		SET revoked_at = COALESCE(revoked_at, $2), last_used_at = $2
		WHERE refresh_hash = $1
	`, refreshHash, s.now()); err != nil {
		return fmt.Errorf("revoke platform admin session: %w", err)
	}
	return nil
}

func (s Store) BootstrapAdmin(ctx context.Context, email string, permissions []string) (Admin, error) {
	admin, err := scanAdmin(s.Database.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET permissions = EXCLUDED.permissions
		RETURNING id, email, name, permissions
	`, normalizeEmail(email), permissions))
	if err != nil {
		return Admin{}, fmt.Errorf("bootstrap platform admin: %w", err)
	}
	return admin, nil
}

func (s Store) CreateTenant(ctx context.Context, input CreateTenantParams) (TenantRecord, error) {
	record, err := scanTenant(s.Database.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status, billing_email, trial_ends_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, slug, plan, status, billing_email, trial_ends_at, created_at, updated_at
	`, input.Name, input.Slug, input.Plan, input.Status, input.BillingEmail, input.TrialEndsAt))
	if isUniqueViolation(err) {
		return TenantRecord{}, ErrSlugConflict
	}
	if err != nil {
		return TenantRecord{}, fmt.Errorf("create tenant: %w", err)
	}
	return record, nil
}

func (s Store) GetTenant(ctx context.Context, id string) (TenantRecord, error) {
	record, err := scanTenant(s.Database.QueryRow(ctx, `
		SELECT id, name, slug, plan, status, billing_email, trial_ends_at, created_at, updated_at
		FROM tenants WHERE id = $1
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantRecord{}, ErrTenantNotFound
	}
	if err != nil {
		return TenantRecord{}, fmt.Errorf("get tenant: %w", err)
	}
	return record, nil
}

func (s Store) ListTenants(ctx context.Context) ([]TenantRecord, error) {
	rows, err := s.Database.Query(ctx, `
		SELECT id, name, slug, plan, status, billing_email, trial_ends_at, created_at, updated_at
		FROM tenants ORDER BY created_at DESC, id DESC LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	records := make([]TenantRecord, 0)
	for rows.Next() {
		record, err := scanTenant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}
	return records, nil
}

func (s Store) UpdateTenant(ctx context.Context, input TenantRecord) (TenantRecord, error) {
	record, err := scanTenant(s.Database.QueryRow(ctx, `
		UPDATE tenants
		SET name = $2, slug = $3, plan = $4, status = $5, billing_email = $6, trial_ends_at = $7
		WHERE id = $1
		RETURNING id, name, slug, plan, status, billing_email, trial_ends_at, created_at, updated_at
	`, input.ID, input.Name, input.Slug, input.Plan, input.Status, input.BillingEmail, input.TrialEndsAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantRecord{}, ErrTenantNotFound
	}
	if isUniqueViolation(err) {
		return TenantRecord{}, ErrSlugConflict
	}
	if err != nil {
		return TenantRecord{}, fmt.Errorf("update tenant: %w", err)
	}
	return record, nil
}

func (s Store) DeleteTenant(ctx context.Context, id string) error {
	result, err := s.Database.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrTenantNotFound
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

type rowScanner interface {
	Scan(...any) error
}

func scanAdmin(row rowScanner) (Admin, error) {
	var admin Admin
	err := row.Scan(&admin.ID, &admin.Email, &admin.Name, &admin.Permissions)
	return admin, err
}

func scanAdminWithPrefix(row rowScanner, prefix ...any) (Admin, error) {
	var admin Admin
	destinations := append(prefix, &admin.ID, &admin.Email, &admin.Name, &admin.Permissions)
	err := row.Scan(destinations...)
	return admin, err
}

func scanSessionAdmin(
	row rowScanner,
	sessionID *string,
	issuedAt *time.Time,
	revokedAt *pgtype.Timestamptz,
) (Admin, error) {
	return scanAdminWithPrefix(row, sessionID, issuedAt, revokedAt)
}

func scanTenant(row rowScanner) (TenantRecord, error) {
	var record TenantRecord
	var billingEmail pgtype.Text
	var trialEndsAt pgtype.Timestamptz
	err := row.Scan(
		&record.ID, &record.Name, &record.Slug, &record.Plan, &record.Status,
		&billingEmail, &trialEndsAt, &record.CreatedAt, &record.UpdatedAt,
	)
	if billingEmail.Valid {
		record.BillingEmail = &billingEmail.String
	}
	if trialEndsAt.Valid {
		record.TrialEndsAt = &trialEndsAt.Time
	}
	return record, err
}
