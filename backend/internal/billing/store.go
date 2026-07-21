package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type AdminDatabase interface {
	db.Querier
	Begin(context.Context) (pgx.Tx, error)
}

type Store struct {
	Database AdminDatabase
	Now      func() time.Time
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Store) Signup(ctx context.Context, input SignupInput) (SignupResult, error) {
	rawToken, tokenHash, err := auth.NewOpaqueToken()
	if err != nil {
		return SignupResult{}, err
	}
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return SignupResult{}, fmt.Errorf("begin signup: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	trialEnd := s.now().Add(TrialDuration)
	var result SignupResult
	result.Token = rawToken
	err = tx.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status, billing_email, trial_ends_at)
		VALUES ($1, $2, $3, 'trial', $4, $5)
		RETURNING id, name, slug, plan, status, billing_email, trial_ends_at
	`, input.Name, input.Slug, input.Plan, input.Email, trialEnd).Scan(
		&result.Tenant.ID, &result.Tenant.Name, &result.Tenant.Slug, &result.Tenant.Plan,
		&result.Tenant.Status, &result.Tenant.BillingEmail, &result.Tenant.TrialEndsAt,
	)
	if isUniqueViolation(err) {
		return SignupResult{}, ErrSlugConflict
	}
	if err != nil {
		return SignupResult{}, fmt.Errorf("create signup tenant: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, $2, $3)
		RETURNING id
	`, result.Tenant.ID, input.Email, auth.TenantPermissionStrings()).Scan(&result.MemberID); err != nil {
		return SignupResult{}, fmt.Errorf("create signup owner: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_magic_links (tenant_id, member_id, email, invited_permissions, token_hash, expires_at)
		VALUES ($1, $2, $3, '{}', $4, $5)
	`, result.Tenant.ID, result.MemberID, input.Email, tokenHash, s.now().Add(10*time.Minute)); err != nil {
		return SignupResult{}, fmt.Errorf("create signup magic link: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SignupResult{}, fmt.Errorf("commit signup: %w", err)
	}
	return result, nil
}

func (s Store) Get(ctx context.Context, tenantID string) (Tenant, error) {
	return scanTenant(s.Database.QueryRow(ctx, tenantSelect+` WHERE id = $1`, tenantID))
}

func (s Store) FindByStripeReference(ctx context.Context, customerID, subscriptionID string) (Tenant, error) {
	resolved, err := scanTenant(s.Database.QueryRow(ctx, tenantSelect+`
		WHERE ($1 <> '' AND stripe_subscription_id = $1)
		   OR ($2 <> '' AND stripe_customer_id = $2)
		LIMIT 1
	`, subscriptionID, customerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	return resolved, err
}

func (s Store) SaveCustomer(ctx context.Context, tenantID, customerID string) (string, error) {
	var stored string
	err := s.Database.QueryRow(ctx, `
		UPDATE tenants
		SET stripe_customer_id = COALESCE(stripe_customer_id, $2)
		WHERE id = $1
		RETURNING stripe_customer_id
	`, tenantID, customerID).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("save Stripe customer: %w", err)
	}
	return stored, nil
}

func (s Store) ApplyEvent(
	ctx context.Context,
	event EventSignal,
	subscription Subscription,
	remotePlan tenant.Plan,
) (bool, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin Stripe event: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var inserted string
	err = tx.QueryRow(ctx, `
		INSERT INTO stripe_events (event_id) VALUES ($1)
		ON CONFLICT DO NOTHING RETURNING event_id
	`, event.ID).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("record Stripe event: %w", err)
	}
	current, err := scanTenant(tx.QueryRow(ctx, tenantSelect+`
		WHERE ($1 <> '' AND stripe_subscription_id = $1)
		   OR ($2 <> '' AND stripe_customer_id = $2)
		FOR UPDATE
	`, subscription.ID, subscription.CustomerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("lock tenant for Stripe event: %w", err)
	}

	status := stripeTenantStatus(subscription.Status)
	plan := current.Plan
	var pending *tenant.Plan
	if current.PendingPlan != nil {
		value := *current.PendingPlan
		pending = &value
	}
	switch event.Type {
	case "customer.subscription.deleted":
		status = tenant.StatusCancelled
	case "invoice.payment_failed":
		status = tenant.StatusSuspended
	case "invoice.paid":
		if pending != nil && *pending == remotePlan {
			plan = remotePlan
			pending = nil
		} else if planRank(remotePlan) >= planRank(plan) {
			plan = remotePlan
			pending = nil
		}
	default:
		if planRank(remotePlan) < planRank(plan) {
			value := remotePlan
			pending = &value
		} else {
			plan = remotePlan
			pending = nil
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tenants
		SET stripe_customer_id = $2, stripe_subscription_id = $3,
		    plan = $4, pending_plan = $5, status = $6, trial_ends_at = $7
		WHERE id = $1
	`, current.ID, subscription.CustomerID, subscription.ID, plan, pending, status, subscription.TrialEndsAt); err != nil {
		return false, fmt.Errorf("apply Stripe event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit Stripe event: %w", err)
	}
	return true, nil
}

func (s Store) RequestDeletion(ctx context.Context, tenantID, memberID, confirmation string) (Tenant, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return Tenant{}, fmt.Errorf("begin deletion request: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanTenant(tx.QueryRow(ctx, tenantSelect+` WHERE id = $1 FOR UPDATE`, tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("lock tenant for deletion: %w", err)
	}
	if confirmation != current.Slug {
		return Tenant{}, ErrConfirmation
	}
	if current.DeletionRequestedAt != nil {
		return Tenant{}, ErrDeletionPending
	}
	now := s.now()
	if _, err := tx.Exec(ctx, `
		UPDATE tenants SET deletion_requested_at = $2, deletion_requested_by_member_id = $3
		WHERE id = $1
	`, tenantID, now, memberID); err != nil {
		return Tenant{}, fmt.Errorf("request tenant deletion: %w", err)
	}
	current.DeletionRequestedAt = &now
	current.DeletionRequestedByMemberID = &memberID
	if err := tx.Commit(ctx); err != nil {
		return Tenant{}, fmt.Errorf("commit deletion request: %w", err)
	}
	return current, nil
}

func (s Store) CancelDeletion(ctx context.Context, tenantID string) error {
	result, err := s.Database.Exec(ctx, `
		UPDATE tenants
		SET deletion_requested_at = NULL, deletion_requested_by_member_id = NULL
		WHERE id = $1 AND deletion_requested_at IS NOT NULL
	`, tenantID)
	if err != nil {
		return fmt.Errorf("cancel tenant deletion: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrDeletionNotPending
	}
	return nil
}

func (s Store) MemberEmails(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := s.Database.Query(ctx, `SELECT email FROM tenant_members WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant member emails: %w", err)
	}
	defer rows.Close()
	emails := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan tenant member email: %w", err)
		}
		emails = append(emails, value)
	}
	return emails, rows.Err()
}

func (s Store) DueDeletions(ctx context.Context) ([]Tenant, error) {
	return s.listTenants(ctx, tenantSelect+`
		WHERE deletion_requested_at IS NOT NULL AND deletion_requested_at <= $1
		ORDER BY deletion_requested_at LIMIT 100
	`, s.now().Add(-DeletionGrace))
}

func (s Store) UnverifiedTenants(ctx context.Context) ([]Tenant, error) {
	return s.listTenants(ctx, tenantSelect+`
		WHERE created_at <= $1 AND status = 'trial' AND stripe_subscription_id IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM tenant_members m
		      WHERE m.tenant_id = tenants.id AND m.last_login_at IS NOT NULL
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM tenant_magic_links l
		      WHERE l.tenant_id = tenants.id AND l.consumed_at IS NOT NULL
		  )
		ORDER BY created_at LIMIT 100
	`, s.now().Add(-24*time.Hour))
}

func (s Store) TrialsForDunning(ctx context.Context) ([]Tenant, error) {
	return s.listTenants(ctx, tenantSelect+`
		WHERE status = 'trial' AND trial_ends_at IS NOT NULL
		  AND trial_ends_at <= $1
		ORDER BY trial_ends_at LIMIT 500
	`, s.now().Add(4*24*time.Hour))
}

func (s Store) SuspendExpiredUnpaid(ctx context.Context, tenantID string) error {
	_, err := s.Database.Exec(ctx, `
		UPDATE tenants SET status = 'suspended'
		WHERE id = $1 AND status = 'trial' AND trial_ends_at <= $2
		  AND stripe_subscription_id IS NULL
	`, tenantID, s.now())
	if err != nil {
		return fmt.Errorf("suspend expired unpaid trial: %w", err)
	}
	return nil
}

func (s Store) listTenants(ctx context.Context, query string, args ...any) ([]Tenant, error) {
	rows, err := s.Database.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list billing tenants: %w", err)
	}
	defer rows.Close()
	items := make([]Tenant, 0)
	for rows.Next() {
		item, err := scanTenant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan billing tenant: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s Store) DeleteTenant(ctx context.Context, candidate Tenant, forcedByAdminID string) error {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tenant deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanTenant(tx.QueryRow(ctx, tenantSelect+` WHERE id = $1 FOR UPDATE`, candidate.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock tenant for deletion: %w", err)
	}
	requestedAt := s.now()
	if current.DeletionRequestedAt != nil {
		requestedAt = *current.DeletionRequestedAt
	}
	var feedbackCount, attachmentCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM feedbacks WHERE tenant_id = $1`, current.ID).Scan(&feedbackCount); err != nil {
		return fmt.Errorf("count tenant feedback: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE tenant_id = $1`, current.ID).Scan(&attachmentCount); err != nil {
		return fmt.Errorf("count tenant attachments: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE tenant_member_sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE tenant_id = $1`, current.ID, s.now()); err != nil {
		return fmt.Errorf("revoke tenant sessions: %w", err)
	}
	var requestedBy, forcedBy any
	if current.DeletionRequestedByMemberID != nil {
		requestedBy = *current.DeletionRequestedByMemberID
	}
	if forcedByAdminID != "" {
		forcedBy = forcedByAdminID
		requestedBy = nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_deletions (
			tenant_id, tenant_slug, tenant_name, requested_at, executed_at,
			requested_by_member_id, forced_by_platform_admin_id, feedback_count, attachment_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, current.ID, current.Slug, current.Name, requestedAt, s.now(), requestedBy, forcedBy, feedbackCount, attachmentCount); err != nil {
		return fmt.Errorf("audit tenant deletion: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, current.ID); err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant deletion: %w", err)
	}
	return nil
}

func (s Store) PurgeUnverified(ctx context.Context, tenantID string) error {
	_, err := s.Database.Exec(ctx, `
		DELETE FROM tenants
		WHERE id = $1 AND status = 'trial' AND stripe_subscription_id IS NULL
		  AND NOT EXISTS (SELECT 1 FROM tenant_members m WHERE m.tenant_id = tenants.id AND m.last_login_at IS NOT NULL)
		  AND NOT EXISTS (SELECT 1 FROM tenant_magic_links l WHERE l.tenant_id = tenants.id AND l.consumed_at IS NOT NULL)
	`, tenantID)
	if err != nil {
		return fmt.Errorf("purge unverified tenant: %w", err)
	}
	return nil
}

const tenantSelect = `
	SELECT id, name, slug, plan, pending_plan, status, COALESCE(billing_email::text, ''),
	       COALESCE(stripe_customer_id, ''), COALESCE(stripe_subscription_id, ''), trial_ends_at,
	       deletion_requested_at, deletion_requested_by_member_id
	FROM tenants`

type rowScanner interface{ Scan(...any) error }

func scanTenant(row rowScanner) (Tenant, error) {
	var result Tenant
	var pending pgtype.Text
	var trial, deletion pgtype.Timestamptz
	var requester pgtype.UUID
	err := row.Scan(
		&result.ID, &result.Name, &result.Slug, &result.Plan, &pending, &result.Status,
		&result.BillingEmail, &result.StripeCustomerID, &result.StripeSubscriptionID,
		&trial, &deletion, &requester,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, err
	}
	if pending.Valid {
		value := tenant.Plan(pending.String)
		result.PendingPlan = &value
	}
	if trial.Valid {
		result.TrialEndsAt = &trial.Time
	}
	if deletion.Valid {
		result.DeletionRequestedAt = &deletion.Time
	}
	if requester.Valid {
		value := requester.String()
		result.DeletionRequestedByMemberID = &value
	}
	return result, nil
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func stripeTenantStatus(status string) tenant.Status {
	switch status {
	case "active":
		return tenant.StatusActive
	case "trialing":
		return tenant.StatusTrial
	case "canceled", "incomplete_expired":
		return tenant.StatusCancelled
	default:
		return tenant.StatusSuspended
	}
}
