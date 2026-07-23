package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type RowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresResolver struct {
	Admin RowQuerier
}

func (r PostgresResolver) Resolve(ctx context.Context, slug string) (Tenant, error) {
	var resolved Tenant
	var plan string
	var status string
	var trialEndsAt pgtype.Timestamptz
	var deletionRequestedAt pgtype.Timestamptz
	err := r.Admin.QueryRow(ctx, `
		SELECT id, name, short_name, slug, plan, status, trial_ends_at, deletion_requested_at,
		       reporter_access_enabled, widget_appearance_defaults, website_url, support_email,
		       default_locale, default_timezone, privacy_policy_url, terms_url, logo_version
		FROM tenants
		WHERE slug = $1
	`, slug).Scan(
		&resolved.ID,
		&resolved.Name,
		&resolved.ShortName,
		&resolved.Slug,
		&plan,
		&status,
		&trialEndsAt,
		&deletionRequestedAt,
		&resolved.ReporterAccessEnabled,
		&resolved.WidgetAppearanceDefaults,
		&resolved.WebsiteURL,
		&resolved.SupportEmail,
		&resolved.DefaultLocale,
		&resolved.DefaultTimezone,
		&resolved.PrivacyPolicyURL,
		&resolved.TermsURL,
		&resolved.LogoVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrUnknownTenant
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("resolve tenant %q: %w", slug, err)
	}
	resolved.Plan = Plan(plan)
	resolved.Status = Status(status)
	if trialEndsAt.Valid {
		resolved.TrialEndsAt = &trialEndsAt.Time
	}
	if deletionRequestedAt.Valid {
		resolved.DeletionRequestedAt = &deletionRequestedAt.Time
	}
	return resolved, nil
}
