package platformadmin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type AdminDatabase interface {
	db.Querier
	Begin(context.Context) (pgx.Tx, error)
}

type Store struct {
	Database          AdminDatabase
	Now               func() time.Time
	RefreshTTL        time.Duration
	RefreshRetryGrace time.Duration
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

func (s Store) refreshRetryGrace() time.Duration {
	if s.RefreshRetryGrace > 0 {
		return s.RefreshRetryGrace
	}
	return 0
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
	if err := lockPlatformAdmin(ctx, tx, admin.ID); err != nil {
		return Admin{}, err
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

	checkedAt := s.now()
	retryGrace := s.refreshRetryGrace()
	if _, err := lockPlatformAdminForSession(ctx, tx, currentHash); err != nil {
		return Admin{}, err
	}
	var sessionID string
	var issuedAt time.Time
	var revokedAt pgtype.Timestamptz
	var retryAllowed bool
	admin, err := scanSessionAdmin(tx.QueryRow(ctx, `
		SELECT session.id, session.issued_at, session.revoked_at,
		       session.rotated_at IS NOT NULL
		         AND session.rotated_at >= $3
		         AND $7
		         AND session.replacement_hash = $2
		         AND session.user_agent IS NOT DISTINCT FROM NULLIF($4, '')
		         AND session.ip IS NOT DISTINCT FROM NULLIF($5, '')::inet
		         AND EXISTS (
		           SELECT 1 FROM platform_admin_sessions AS replacement
		           WHERE replacement.refresh_hash = session.replacement_hash
		             AND replacement.revoked_at IS NULL
		             AND replacement.issued_at > $6
		         ),
		       admin.id, admin.email, admin.name, admin.permissions
		FROM platform_admin_sessions AS session
		JOIN platform_admins AS admin ON admin.id = session.admin_id
		WHERE session.refresh_hash = $1
		FOR UPDATE OF session
	`, currentHash, nextHash, checkedAt.Add(-retryGrace), userAgent, ip,
		checkedAt.Add(-s.refreshTTL()), retryGrace > 0),
		&sessionID, &issuedAt, &revokedAt, &retryAllowed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrRefreshInvalid
	}
	if err != nil {
		return Admin{}, fmt.Errorf("lock platform admin session: %w", err)
	}

	if revokedAt.Valid && !retryAllowed {
		if _, err := tx.Exec(ctx,
			`UPDATE platform_admin_sessions
			 SET revoked_at = COALESCE(revoked_at, $2), rotated_at = NULL, replacement_hash = NULL
			 WHERE admin_id = $1`,
			admin.ID, checkedAt,
		); err != nil {
			return Admin{}, fmt.Errorf("revoke reused platform admin sessions: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Admin{}, fmt.Errorf("commit reused refresh revocation: %w", err)
		}
		return Admin{}, ErrRefreshReuse
	}
	if revokedAt.Valid {
		return admin, nil
	}
	if !issuedAt.Add(s.refreshTTL()).After(checkedAt) {
		if _, err := tx.Exec(ctx,
			`UPDATE platform_admin_sessions
			 SET revoked_at = $2, rotated_at = NULL, replacement_hash = NULL
			 WHERE id = $1`, sessionID, checkedAt,
		); err != nil {
			return Admin{}, fmt.Errorf("revoke expired platform admin session: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Admin{}, fmt.Errorf("commit expired refresh revocation: %w", err)
		}
		return Admin{}, ErrRefreshExpired
	}

	if _, err := tx.Exec(ctx,
		`UPDATE platform_admin_sessions
		 SET revoked_at = $2, last_used_at = $2, rotated_at = $2, replacement_hash = $3
		 WHERE id = $1`, sessionID, checkedAt, nextHash,
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
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke session transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockPlatformAdminForSession(ctx, tx, refreshHash); err != nil {
		if errors.Is(err, ErrRefreshInvalid) {
			return nil
		}
		return err
	}
	checkedAt := s.now()
	if _, err := tx.Exec(ctx, `
		WITH RECURSIVE session_chain AS (
			SELECT id, replacement_hash
			FROM platform_admin_sessions
			WHERE refresh_hash = $1
			UNION
			SELECT successor.id, successor.replacement_hash
			FROM platform_admin_sessions AS successor
			JOIN session_chain AS predecessor
			  ON successor.refresh_hash = predecessor.replacement_hash
		)
		UPDATE platform_admin_sessions AS session
		SET revoked_at = COALESCE(session.revoked_at, $2), last_used_at = $2,
		    rotated_at = NULL, replacement_hash = NULL
		WHERE session.id IN (SELECT id FROM session_chain)
	`, refreshHash, checkedAt); err != nil {
		return fmt.Errorf("revoke platform admin session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke session transaction: %w", err)
	}
	return nil
}

func lockPlatformAdminForSession(
	ctx context.Context,
	query db.Querier,
	refreshHash string,
) (string, error) {
	var adminID string
	if err := query.QueryRow(ctx, `
		SELECT admin_id FROM platform_admin_sessions WHERE refresh_hash = $1
	`, refreshHash).Scan(&adminID); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRefreshInvalid
	} else if err != nil {
		return "", fmt.Errorf("find platform admin session owner: %w", err)
	}
	if err := lockPlatformAdmin(ctx, query, adminID); err != nil {
		return "", err
	}
	return adminID, nil
}

func lockPlatformAdmin(ctx context.Context, query db.Querier, adminID string) error {
	var lockedID string
	if err := query.QueryRow(ctx, `
		SELECT id FROM platform_admins WHERE id = $1 FOR UPDATE
	`, adminID).Scan(&lockedID); err != nil {
		return fmt.Errorf("lock platform admin session owner: %w", err)
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

// RecordQueueTaskRetry writes the operator intent before a background task is
// moved in Redis. The ledger is append-only and owned by the admin role; the
// actor is the resource because asynchronous task IDs are not guaranteed to be
// UUIDs while platform_security_events resource IDs are.
func (s Store) RecordQueueTaskRetry(ctx context.Context, actorID, queue, taskID string) error {
	payload, err := json.Marshal(map[string]string{"queue": queue, "task_id": taskID})
	if err != nil {
		return fmt.Errorf("encode queue retry audit: %w", err)
	}
	if _, err := s.Database.Exec(ctx, `
		INSERT INTO platform_security_events (kind, resource_type, resource_id, data)
		VALUES ('operations.queue_task_retry_requested', 'platform_admin', $1, $2::jsonb)
	`, actorID, string(payload)); err != nil {
		return fmt.Errorf("record queue retry audit: %w", err)
	}
	return nil
}

// Overview returns bounded aggregate counts for the platform console. The
// query deliberately remains on the admin pool and does not join member,
// feedback, session, or attachment tables: the dashboard needs capacity
// signals, not personal or content data.
func (s Store) Overview(ctx context.Context) (BackofficeOverview, error) {
	now := s.now()
	var overview BackofficeOverview
	if err := s.Database.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(*) FILTER (WHERE status = 'trial'),
			COUNT(*) FILTER (WHERE status = 'suspended'),
			COUNT(*) FILTER (WHERE status = 'cancelled'),
			COUNT(*) FILTER (WHERE status = 'trial' AND trial_ends_at > $1 AND trial_ends_at <= $2),
			COUNT(*) FILTER (WHERE created_at >= $3)
		FROM tenants
	`, now, now.Add(7*24*time.Hour), now.Add(-30*24*time.Hour)).Scan(
		&overview.WorkspaceCount,
		&overview.ActiveWorkspaceCount,
		&overview.TrialWorkspaceCount,
		&overview.SuspendedWorkspaceCount,
		&overview.CancelledWorkspaceCount,
		&overview.TrialsEndingSoonCount,
		&overview.RecentWorkspaceCount,
	); err != nil {
		return BackofficeOverview{}, fmt.Errorf("backoffice overview totals: %w", err)
	}

	rows, err := s.Database.Query(ctx, `
		SELECT plan, COUNT(*)
		FROM tenants
		GROUP BY plan
		ORDER BY plan ASC
	`)
	if err != nil {
		return BackofficeOverview{}, fmt.Errorf("backoffice overview plans: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var count BackofficePlanCount
		if err := rows.Scan(&count.Plan, &count.WorkspaceCount); err != nil {
			return BackofficeOverview{}, fmt.Errorf("scan backoffice overview plan: %w", err)
		}
		overview.Plans = append(overview.Plans, count)
	}
	if err := rows.Err(); err != nil {
		return BackofficeOverview{}, fmt.Errorf("iterate backoffice overview plans: %w", err)
	}
	return overview, nil
}

type backofficeTenantCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

// ListBackofficeTenants uses a keyset cursor rather than offsets so inserts
// cannot shift a later page into duplicates or gaps while an operator works.
func (s Store) ListBackofficeTenants(
	ctx context.Context,
	params BackofficeTenantListParams,
) (BackofficeTenantPage, error) {
	var cursorCreatedAt any
	var cursorID any
	if params.Cursor != "" {
		cursor, err := decodeBackofficeTenantCursor(params.Cursor)
		if err != nil {
			return BackofficeTenantPage{}, err
		}
		cursorCreatedAt = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := s.Database.Query(ctx, `
		SELECT id, name, slug, plan, status, trial_ends_at, created_at, updated_at
		FROM tenants
		WHERE ($1 = '' OR name ILIKE '%' || $1 || '%' OR slug ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR status = $2::tenant_status)
		  AND ($3 = '' OR plan = $3::tenant_plan)
		  AND ($4::timestamptz IS NULL OR (created_at, id) < ($4::timestamptz, $5::uuid))
		ORDER BY created_at DESC, id DESC
		LIMIT $6
	`, params.Query, string(params.Status), string(params.Plan), cursorCreatedAt, cursorID, params.Limit+1)
	if err != nil {
		return BackofficeTenantPage{}, fmt.Errorf("list backoffice tenants: %w", err)
	}
	defer rows.Close()
	page := BackofficeTenantPage{Tenants: make([]BackofficeTenantSummary, 0, params.Limit)}
	for rows.Next() {
		var record BackofficeTenantSummary
		var trialEndsAt pgtype.Timestamptz
		if err := rows.Scan(
			&record.ID, &record.Name, &record.Slug, &record.Plan, &record.Status,
			&trialEndsAt, &record.CreatedAt, &record.UpdatedAt,
		); err != nil {
			return BackofficeTenantPage{}, fmt.Errorf("scan backoffice tenant: %w", err)
		}
		if trialEndsAt.Valid {
			record.TrialEndsAt = &trialEndsAt.Time
		}
		page.Tenants = append(page.Tenants, record)
	}
	if err := rows.Err(); err != nil {
		return BackofficeTenantPage{}, fmt.Errorf("iterate backoffice tenants: %w", err)
	}
	if len(page.Tenants) > params.Limit {
		last := page.Tenants[params.Limit-1]
		nextCursor, err := encodeBackofficeTenantCursor(backofficeTenantCursor{
			CreatedAt: last.CreatedAt, ID: last.ID,
		})
		if err != nil {
			return BackofficeTenantPage{}, err
		}
		page.NextCursor = nextCursor
		page.Tenants = page.Tenants[:params.Limit]
	}
	return page, nil
}

func (s Store) GetBackofficeTenant(ctx context.Context, tenantID string) (BackofficeTenantDetail, error) {
	var detail BackofficeTenantDetail
	var billingEmail pgtype.Text
	var trialEndsAt pgtype.Timestamptz
	err := s.Database.QueryRow(ctx, `
		SELECT t.id, t.name, t.slug, t.plan, t.status, t.billing_email, t.trial_ends_at, t.created_at, t.updated_at,
		       (SELECT count(*) FROM tenant_members WHERE tenant_id = t.id),
		       (SELECT count(*) FROM projects WHERE tenant_id = t.id),
		       (SELECT count(*) FROM feedbacks WHERE tenant_id = t.id)
		FROM tenants AS t WHERE t.id = $1
	`, tenantID).Scan(&detail.Tenant.ID, &detail.Tenant.Name, &detail.Tenant.Slug, &detail.Tenant.Plan, &detail.Tenant.Status,
		&billingEmail, &trialEndsAt, &detail.Tenant.CreatedAt, &detail.Tenant.UpdatedAt,
		&detail.MemberCount, &detail.ProjectCount, &detail.FeedbackCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return BackofficeTenantDetail{}, ErrTenantNotFound
	}
	if err != nil {
		return BackofficeTenantDetail{}, fmt.Errorf("get backoffice tenant: %w", err)
	}
	if billingEmail.Valid {
		detail.Tenant.BillingEmail = &billingEmail.String
	}
	if trialEndsAt.Valid {
		detail.Tenant.TrialEndsAt = &trialEndsAt.Time
	}
	var owner BackofficeTenantMember
	var ownerName pgtype.Text
	var ownerLastLogin pgtype.Timestamptz
	err = s.Database.QueryRow(ctx, `SELECT m.id, m.email::text, m.name, m.status::text, m.last_login_at FROM tenants t JOIN tenant_members m ON m.tenant_id=t.id AND m.id=t.owner_member_id WHERE t.id=$1`, tenantID).Scan(&owner.ID, &owner.Email, &ownerName, &owner.Status, &ownerLastLogin)
	if err == nil {
		if ownerName.Valid {
			owner.Name = &ownerName.String
		}
		if ownerLastLogin.Valid {
			owner.LastLoginAt = &ownerLastLogin.Time
		}
		detail.Owner = &owner
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BackofficeTenantDetail{}, fmt.Errorf("get backoffice tenant owner: %w", err)
	}
	return detail, nil
}

func (s Store) ListBackofficeRecoveryCandidates(ctx context.Context, tenantID string, params BackofficeRecoveryCandidateListParams) (BackofficeRecoveryCandidatePage, error) {
	var createdAt, memberID any
	if params.Cursor != "" {
		cursor, err := decodeBackofficeTenantCursor(params.Cursor)
		if err != nil {
			return BackofficeRecoveryCandidatePage{}, err
		}
		createdAt, memberID = cursor.CreatedAt, cursor.ID
	}
	rows, err := s.Database.Query(ctx, `SELECT m.id,m.email::text,m.name,m.status::text,m.last_login_at,m.created_at FROM tenant_members m JOIN tenants t ON t.id=m.tenant_id WHERE m.tenant_id=$1 AND m.status='active' AND m.last_login_at IS NOT NULL AND m.id IS DISTINCT FROM t.owner_member_id AND ($2::timestamptz IS NULL OR (m.created_at,m.id)<($2::timestamptz,$3::uuid)) ORDER BY m.created_at DESC,m.id DESC LIMIT $4`, tenantID, createdAt, memberID, params.Limit+1)
	if err != nil {
		return BackofficeRecoveryCandidatePage{}, fmt.Errorf("list recovery candidates: %w", err)
	}
	defer rows.Close()
	page := BackofficeRecoveryCandidatePage{Members: make([]BackofficeTenantMember, 0, params.Limit)}
	cursors := make([]backofficeTenantCursor, 0, params.Limit+1)
	for rows.Next() {
		var member BackofficeTenantMember
		var name pgtype.Text
		var login, created pgtype.Timestamptz
		if err := rows.Scan(&member.ID, &member.Email, &name, &member.Status, &login, &created); err != nil {
			return BackofficeRecoveryCandidatePage{}, fmt.Errorf("scan recovery candidate: %w", err)
		}
		if name.Valid {
			member.Name = &name.String
		}
		member.LastLoginAt = &login.Time
		page.Members = append(page.Members, member)
		cursors = append(cursors, backofficeTenantCursor{CreatedAt: created.Time, ID: member.ID})
	}
	if err := rows.Err(); err != nil {
		return BackofficeRecoveryCandidatePage{}, fmt.Errorf("iterate recovery candidates: %w", err)
	}
	if len(page.Members) > params.Limit {
		cursor, err := encodeBackofficeTenantCursor(cursors[params.Limit-1])
		if err != nil {
			return BackofficeRecoveryCandidatePage{}, err
		}
		page.NextCursor = cursor
		page.Members = page.Members[:params.Limit]
	}
	return page, nil
}

type backofficeAccessEventCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

// ListBackofficeAccessEvents paginates the metadata-only ledger with a
// keyset cursor. The event JSON data column is deliberately never selected.
func (s Store) ListBackofficeAccessEvents(ctx context.Context, tenantID string, params BackofficeAccessEventListParams) (BackofficeAccessEventPage, error) {
	var cursorCreatedAt any
	var cursorID any
	if params.Cursor != "" {
		cursor, err := decodeBackofficeAccessEventCursor(params.Cursor)
		if err != nil {
			return BackofficeAccessEventPage{}, err
		}
		cursorCreatedAt, cursorID = cursor.CreatedAt, cursor.ID
	}
	rows, err := s.Database.Query(ctx, `
		SELECT id, kind, actor_type, action, outcome, created_at
		FROM tenant_access_events
		WHERE tenant_id = $1
		  AND ($2::timestamptz IS NULL OR (created_at, id) < ($2::timestamptz, $3::uuid))
		ORDER BY created_at DESC, id DESC LIMIT $4
	`, tenantID, cursorCreatedAt, cursorID, params.Limit+1)
	if err != nil {
		return BackofficeAccessEventPage{}, fmt.Errorf("list backoffice access events: %w", err)
	}
	defer rows.Close()
	page := BackofficeAccessEventPage{Events: make([]BackofficeAccessEvent, 0, params.Limit)}
	for rows.Next() {
		var event BackofficeAccessEvent
		var action, outcome pgtype.Text
		if err := rows.Scan(&event.ID, &event.Kind, &event.ActorType, &action, &outcome, &event.CreatedAt); err != nil {
			return BackofficeAccessEventPage{}, fmt.Errorf("scan backoffice access event: %w", err)
		}
		if action.Valid {
			event.Action = &action.String
		}
		if outcome.Valid {
			event.Outcome = &outcome.String
		}
		page.Events = append(page.Events, event)
	}
	if err := rows.Err(); err != nil {
		return BackofficeAccessEventPage{}, fmt.Errorf("iterate backoffice access events: %w", err)
	}
	if len(page.Events) > params.Limit {
		last := page.Events[params.Limit-1]
		nextCursor, err := encodeBackofficeAccessEventCursor(backofficeAccessEventCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return BackofficeAccessEventPage{}, err
		}
		page.NextCursor = nextCursor
		page.Events = page.Events[:params.Limit]
	}
	return page, nil
}

func encodeBackofficeAccessEventCursor(cursor backofficeAccessEventCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode backoffice access event cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeBackofficeAccessEventCursor(value string) (backofficeAccessEventCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(payload) == 0 || len(payload) > 512 {
		return backofficeAccessEventCursor{}, errors.New("cursor is invalid")
	}
	var cursor backofficeAccessEventCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.CreatedAt.IsZero() {
		return backofficeAccessEventCursor{}, errors.New("cursor is invalid")
	}
	if _, err := uuid.Parse(cursor.ID); err != nil {
		return backofficeAccessEventCursor{}, errors.New("cursor is invalid")
	}
	return cursor, nil
}

func encodeBackofficeTenantCursor(cursor backofficeTenantCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode backoffice tenant cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeBackofficeTenantCursor(value string) (backofficeTenantCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(payload) == 0 || len(payload) > 512 {
		return backofficeTenantCursor{}, errors.New("cursor is invalid")
	}
	var cursor backofficeTenantCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.CreatedAt.IsZero() {
		return backofficeTenantCursor{}, errors.New("cursor is invalid")
	}
	if _, err := uuid.Parse(cursor.ID); err != nil {
		return backofficeTenantCursor{}, errors.New("cursor is invalid")
	}
	return cursor, nil
}

func (s Store) CreateTenant(ctx context.Context, input CreateTenantParams) (TenantRecord, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return TenantRecord{}, fmt.Errorf("begin tenant provisioning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanTenant(tx.QueryRow(ctx, `
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
	var ownerMemberID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, $2, $3)
		RETURNING id
	`, record.ID, normalizeEmail(input.OwnerEmail), auth.TenantPermissionStrings()).Scan(&ownerMemberID); err != nil {
		return TenantRecord{}, fmt.Errorf("create tenant owner: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tenants SET owner_member_id = $2 WHERE id = $1
	`, record.ID, ownerMemberID); err != nil {
		return TenantRecord{}, fmt.Errorf("assign tenant owner: %w", err)
	}
	if err := insertPlatformAccessEvent(ctx, tx, record.ID, "ownership.provisioned", input.ActorPlatformAdminID, &ownerMemberID, map[string]any{
		"owner_member_id": ownerMemberID,
		"owner_email":     normalizeEmail(input.OwnerEmail),
	}); err != nil {
		return TenantRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TenantRecord{}, fmt.Errorf("commit tenant provisioning: %w", err)
	}
	return record, nil
}

func (s Store) RecoverOwnership(ctx context.Context, input RecoverOwnershipParams) error {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin workspace ownership recovery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var slug string
	var previousOwnerMemberID *string
	if err := tx.QueryRow(ctx, `
		SELECT slug, owner_member_id FROM tenants WHERE id = $1 FOR UPDATE
	`, input.TenantID).Scan(&slug, &previousOwnerMemberID); errors.Is(err, pgx.ErrNoRows) {
		return ErrTenantNotFound
	} else if err != nil {
		return fmt.Errorf("lock workspace ownership recovery: %w", err)
	}
	deny := func(reason string, domainErr error) error {
		if err := insertPlatformAccessEvent(
			ctx, tx, input.TenantID, "ownership.recovery_denied", input.ActorPlatformAdminID, nil,
			map[string]any{"reason": reason, "verification_reference": input.VerificationReference},
		); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit denied workspace ownership recovery: %w", err)
		}
		return domainErr
	}
	var adminLastLogin *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT last_login_at FROM platform_admins WHERE id = $1 FOR UPDATE
	`, input.ActorPlatformAdminID).Scan(&adminLastLogin); err != nil {
		return fmt.Errorf("lock recovery platform admin: %w", err)
	}
	if adminLastLogin == nil || adminLastLogin.Before(s.now().Add(-10*time.Minute)) {
		return deny("platform_authentication_is_stale", ErrRecentAuthenticationRequired)
	}
	if input.Confirmation != slug {
		return deny("confirmation_mismatch", ErrOwnershipConfirmation)
	}
	if len(strings.TrimSpace(input.VerificationReference)) < 3 || len(strings.TrimSpace(input.Reason)) < 10 {
		return deny("verification_evidence_is_incomplete", ErrRecoveryVerificationInvalid)
	}
	var targetLastLogin *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT last_login_at
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE
	`, input.TenantID, input.TargetMemberID).Scan(&targetLastLogin); errors.Is(err, pgx.ErrNoRows) {
		return deny("target_is_not_active", ErrOwnershipTargetInvalid)
	} else if err != nil {
		return fmt.Errorf("lock recovery target: %w", err)
	}
	if targetLastLogin == nil {
		return deny("target_is_not_verified", ErrOwnershipTargetInvalid)
	}
	if previousOwnerMemberID != nil && *previousOwnerMemberID == input.TargetMemberID {
		return deny("target_is_current_owner", ErrOwnershipTargetInvalid)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tenant_members
		SET permissions = $3, authorization_version = authorization_version + 1
		WHERE tenant_id = $1 AND id = $2
	`, input.TenantID, input.TargetMemberID, auth.TenantPermissionStrings()); err != nil {
		return fmt.Errorf("elevate recovered workspace owner: %w", err)
	}
	revokedAt := s.now()
	targetRevocation, err := tx.Exec(ctx, `
		UPDATE tenant_member_sessions
		SET revoked_at = COALESCE(revoked_at, $3), rotated_at = NULL, replacement_hash = NULL
		WHERE tenant_id = $1 AND member_id = $2
		  AND (revoked_at IS NULL OR rotated_at IS NOT NULL OR replacement_hash IS NOT NULL)
	`, input.TenantID, input.TargetMemberID, revokedAt)
	if err != nil {
		return fmt.Errorf("revoke recovered owner sessions: %w", err)
	}
	formerOwnerRevokedSessions := int64(0)
	if previousOwnerMemberID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE tenant_members
			SET status = 'deactivated', permissions = '{}',
			    authorization_version = authorization_version + 1
			WHERE tenant_id = $1 AND id = $2
		`, input.TenantID, *previousOwnerMemberID); err != nil {
			return fmt.Errorf("deactivate inaccessible workspace owner: %w", err)
		}
		formerOwnerRevocation, err := tx.Exec(ctx, `
			UPDATE tenant_member_sessions
			SET revoked_at = COALESCE(revoked_at, $3), rotated_at = NULL, replacement_hash = NULL
			WHERE tenant_id = $1 AND member_id = $2
			  AND (revoked_at IS NULL OR rotated_at IS NOT NULL OR replacement_hash IS NOT NULL)
		`, input.TenantID, *previousOwnerMemberID, revokedAt)
		if err != nil {
			return fmt.Errorf("revoke inaccessible owner sessions: %w", err)
		}
		formerOwnerRevokedSessions = formerOwnerRevocation.RowsAffected()
		if _, err := tx.Exec(ctx, `
			UPDATE tenant_magic_links
			SET consumed_at = COALESCE(consumed_at, $3)
			WHERE tenant_id = $1 AND member_id = $2
		`, input.TenantID, *previousOwnerMemberID, revokedAt); err != nil {
			return fmt.Errorf("consume inaccessible owner links: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tenants SET owner_member_id = $2 WHERE id = $1
	`, input.TenantID, input.TargetMemberID); err != nil {
		return fmt.Errorf("recover workspace ownership: %w", err)
	}
	if err := insertPlatformAccessEvent(
		ctx, tx, input.TenantID, "ownership.recovered", input.ActorPlatformAdminID, &input.TargetMemberID,
		map[string]any{
			"previous_owner_member_id":           previousOwnerMemberID,
			"new_owner_member_id":                input.TargetMemberID,
			"target_revoked_session_count":       targetRevocation.RowsAffected(),
			"former_owner_revoked_session_count": formerOwnerRevokedSessions,
			"sessions_revoked_at":                revokedAt,
			"verification_reference":             strings.TrimSpace(input.VerificationReference),
			"reason":                             strings.TrimSpace(input.Reason),
		},
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit workspace ownership recovery: %w", err)
	}
	return nil
}

func insertPlatformAccessEvent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	kind string,
	actorPlatformAdminID string,
	subjectMemberID *string,
	data map[string]any,
) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode platform access event: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_type, actor_platform_admin_id, subject_member_id, data
		) VALUES ($1, $2, 'platform_admin', $3, $4, $5::jsonb)
	`, tenantID, kind, actorPlatformAdminID, subjectMemberID, string(payload)); err != nil {
		return fmt.Errorf("record platform access event: %w", err)
	}
	return nil
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

func (s Store) UpdateTenant(ctx context.Context, input TenantRecord, actorPlatformAdminID string) (TenantRecord, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return TenantRecord{}, fmt.Errorf("begin workspace update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	previous, err := scanTenant(tx.QueryRow(ctx, `
		SELECT id, name, slug, plan, status, billing_email, trial_ends_at, created_at, updated_at
		FROM tenants WHERE id = $1 FOR UPDATE
	`, input.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantRecord{}, ErrTenantNotFound
	}
	if err != nil {
		return TenantRecord{}, fmt.Errorf("lock workspace update: %w", err)
	}
	if !previous.UpdatedAt.Equal(input.UpdatedAt) {
		return TenantRecord{}, ErrTenantUpdateConflict
	}
	record, err := scanTenant(tx.QueryRow(ctx, `
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
	changedFields := changedTenantFields(previous, record)
	if err := insertPlatformAccessEvent(ctx, tx, input.ID, "workspace.updated", actorPlatformAdminID, nil, map[string]any{
		"changed_fields": changedFields,
		"plan_from":      previous.Plan,
		"plan_to":        record.Plan,
		"status_from":    previous.Status,
		"status_to":      record.Status,
	}); err != nil {
		return TenantRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TenantRecord{}, fmt.Errorf("commit workspace update: %w", err)
	}
	return record, nil
}

// changedTenantFields records the shape of an administrative lifecycle change
// without placing workspace names or billing addresses in the audit payload.
func changedTenantFields(previous TenantRecord, current TenantRecord) []string {
	fields := make([]string, 0, 7)
	if previous.Name != current.Name {
		fields = append(fields, "name")
	}
	if previous.Slug != current.Slug {
		fields = append(fields, "slug")
	}
	if previous.Plan != current.Plan {
		fields = append(fields, "plan")
	}
	if previous.Status != current.Status {
		fields = append(fields, "status")
	}
	if !sameOptionalString(previous.BillingEmail, current.BillingEmail) {
		fields = append(fields, "billing_email")
	}
	if !sameOptionalTime(previous.TrialEndsAt, current.TrialEndsAt) {
		fields = append(fields, "trial_ends_at")
	}
	return fields
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
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
	retryAllowed *bool,
) (Admin, error) {
	return scanAdminWithPrefix(row, sessionID, issuedAt, revokedAt, retryAllowed)
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
