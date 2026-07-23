package tenant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/widgetconfig"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgresResolverResolve(t *testing.T) {
	t.Parallel()

	trialEndsAt := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	deletionRequestedAt := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	querier := &resolverQuerier{
		row: resolverRow{scan: func(destinations ...any) error {
			if len(destinations) != 9 {
				return errors.New("unexpected destination count")
			}
			*destinations[0].(*string) = "0762af3d-01b4-452f-81ad-dabf7652395f"
			*destinations[1].(*string) = "Acme"
			*destinations[2].(*string) = "acme"
			*destinations[3].(*string) = string(PlanGrowth)
			*destinations[4].(*string) = string(StatusTrial)
			*destinations[5].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: trialEndsAt, Valid: true}
			*destinations[6].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: deletionRequestedAt, Valid: true}
			*destinations[7].(*bool) = true
			accent := "#005f56"
			*destinations[8].(*widgetconfig.AppearanceOverrides) = widgetconfig.AppearanceOverrides{AccentColor: &accent}
			return nil
		}},
	}

	resolved, err := (PostgresResolver{Admin: querier}).Resolve(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if querier.slug != "acme" || !strings.Contains(querier.query, "FROM tenants") {
		t.Errorf("query = %q, slug = %q", querier.query, querier.slug)
	}
	if resolved.ID != "0762af3d-01b4-452f-81ad-dabf7652395f" || resolved.Name != "Acme" || resolved.Slug != "acme" {
		t.Errorf("resolved tenant identity = %#v", resolved)
	}
	if resolved.Plan != PlanGrowth || resolved.Status != StatusTrial {
		t.Errorf("resolved tenant state = %#v", resolved)
	}
	if resolved.TrialEndsAt == nil || !resolved.TrialEndsAt.Equal(trialEndsAt) {
		t.Errorf("TrialEndsAt = %v, want %v", resolved.TrialEndsAt, trialEndsAt)
	}
	if resolved.DeletionRequestedAt == nil || !resolved.DeletionRequestedAt.Equal(deletionRequestedAt) {
		t.Errorf("DeletionRequestedAt = %v, want %v", resolved.DeletionRequestedAt, deletionRequestedAt)
	}
	if !resolved.ReporterAccessEnabled {
		t.Error("ReporterAccessEnabled = false, want true")
	}
	if resolved.WidgetAppearanceDefaults.AccentColor == nil || *resolved.WidgetAppearanceDefaults.AccentColor != "#005f56" {
		t.Errorf("WidgetAppearanceDefaults = %#v", resolved.WidgetAppearanceDefaults)
	}
}

func TestPostgresResolverLeavesMissingTrialEndNil(t *testing.T) {
	t.Parallel()

	querier := &resolverQuerier{row: resolverRow{scan: func(destinations ...any) error {
		*destinations[0].(*string) = "tenant-id"
		*destinations[1].(*string) = "Acme"
		*destinations[2].(*string) = "acme"
		*destinations[3].(*string) = string(PlanStarter)
		*destinations[4].(*string) = string(StatusActive)
		*destinations[5].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
		*destinations[6].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
		*destinations[7].(*bool) = true
		*destinations[8].(*widgetconfig.AppearanceOverrides) = widgetconfig.AppearanceOverrides{}
		return nil
	}}}

	resolved, err := (PostgresResolver{Admin: querier}).Resolve(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.TrialEndsAt != nil {
		t.Errorf("TrialEndsAt = %v, want nil", resolved.TrialEndsAt)
	}
	if resolved.DeletionRequestedAt != nil {
		t.Errorf("DeletionRequestedAt = %v, want nil", resolved.DeletionRequestedAt)
	}
}

func TestPostgresResolverMapsQueryErrors(t *testing.T) {
	t.Parallel()

	databaseErr := errors.New("database unavailable")
	tests := []struct {
		name      string
		scanErr   error
		wantError error
		wantText  string
	}{
		{name: "missing tenant", scanErr: pgx.ErrNoRows, wantError: ErrUnknownTenant},
		{name: "database failure", scanErr: databaseErr, wantError: databaseErr, wantText: `resolve tenant "acme"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolver := PostgresResolver{Admin: &resolverQuerier{row: resolverRow{scan: func(...any) error {
				return tt.scanErr
			}}}}
			_, err := resolver.Resolve(context.Background(), "acme")
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("Resolve() error = %v, want error matching %v", err, tt.wantError)
			}
			if tt.wantText != "" && !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("Resolve() error = %q, want text %q", err, tt.wantText)
			}
		})
	}
}

type resolverQuerier struct {
	query string
	slug  string
	row   pgx.Row
}

func (q *resolverQuerier) QueryRow(_ context.Context, query string, arguments ...any) pgx.Row {
	q.query = query
	if len(arguments) == 1 {
		q.slug, _ = arguments[0].(string)
	}
	return q.row
}

type resolverRow struct {
	scan func(...any) error
}

func (r resolverRow) Scan(destinations ...any) error {
	return r.scan(destinations...)
}
