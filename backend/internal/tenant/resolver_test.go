package tenant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/widgetconfig"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgresResolverResolve(t *testing.T) {
	t.Parallel()

	trialEndsAt := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	deletionRequestedAt := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	querier := &resolverQuerier{
		row: resolverRow{scan: func(destinations ...any) error {
			if len(destinations) != 20 {
				return errors.New("unexpected destination count")
			}
			*destinations[0].(*string) = "0762af3d-01b4-452f-81ad-dabf7652395f"
			*destinations[1].(*string) = "Acme"
			shortName := "ACME"
			websiteURL := "https://acme.test"
			supportEmail := "support@acme.test"
			defaultLocale := "pt-AO"
			defaultTimezone := "Africa/Luanda"
			privacyURL := "https://acme.test/privacy"
			privacyContact := "privacy@acme.test"
			privacyRights := "rights@acme.test"
			termsURL := "https://acme.test/terms"
			logoVersion := "b6988409-e11f-4330-808f-e79e05fb160d"
			*destinations[2].(**string) = &shortName
			*destinations[3].(*string) = "acme"
			*destinations[4].(*string) = string(PlanGrowth)
			*destinations[5].(*string) = string(StatusTrial)
			*destinations[6].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: trialEndsAt, Valid: true}
			*destinations[7].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: deletionRequestedAt, Valid: true}
			*destinations[8].(*bool) = true
			accent := "#005f56"
			*destinations[9].(*widgetconfig.AppearanceOverrides) = widgetconfig.AppearanceOverrides{AccentColor: &accent}
			*destinations[10].(**string) = &websiteURL
			*destinations[11].(**string) = &supportEmail
			*destinations[12].(**string) = &defaultLocale
			*destinations[13].(**string) = &defaultTimezone
			*destinations[14].(**string) = &privacyURL
			*destinations[15].(**string) = &privacyContact
			*destinations[16].(**string) = &privacyRights
			*destinations[17].(*int) = 3
			*destinations[18].(**string) = &termsURL
			*destinations[19].(**string) = &logoVersion
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
	if resolved.ShortName == nil || *resolved.ShortName != "ACME" ||
		resolved.WebsiteURL == nil || *resolved.WebsiteURL != "https://acme.test" ||
		resolved.SupportEmail == nil || *resolved.SupportEmail != "support@acme.test" ||
		resolved.DefaultLocale == nil || *resolved.DefaultLocale != "pt-AO" ||
		resolved.DefaultTimezone == nil || *resolved.DefaultTimezone != "Africa/Luanda" ||
		resolved.PrivacyPolicyURL == nil || *resolved.PrivacyPolicyURL != "https://acme.test/privacy" ||
		resolved.PrivacyContactEmail == nil || *resolved.PrivacyContactEmail != "privacy@acme.test" ||
		resolved.PrivacyRightsEmail == nil || *resolved.PrivacyRightsEmail != "rights@acme.test" ||
		resolved.PrivacyProfileVersion != 3 ||
		resolved.TermsURL == nil || *resolved.TermsURL != "https://acme.test/terms" ||
		resolved.LogoVersion == nil || *resolved.LogoVersion != "b6988409-e11f-4330-808f-e79e05fb160d" {
		t.Errorf("resolved tenant profile = %#v", resolved)
	}
}

func TestPostgresResolverLeavesMissingTrialEndNil(t *testing.T) {
	t.Parallel()

	querier := &resolverQuerier{row: resolverRow{scan: func(destinations ...any) error {
		*destinations[0].(*string) = "tenant-id"
		*destinations[1].(*string) = "Acme"
		*destinations[2].(**string) = nil
		*destinations[3].(*string) = "acme"
		*destinations[4].(*string) = string(PlanStarter)
		*destinations[5].(*string) = string(StatusActive)
		*destinations[6].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
		*destinations[7].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
		*destinations[8].(*bool) = true
		*destinations[9].(*widgetconfig.AppearanceOverrides) = widgetconfig.AppearanceOverrides{}
		for index := 10; index <= 16; index++ {
			*destinations[index].(**string) = nil
		}
		*destinations[17].(*int) = 0
		for index := 18; index < 20; index++ {
			*destinations[index].(**string) = nil
		}
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

func TestPostgresResolverSupportsSchemaBeforeContextualPrivacyProfile(t *testing.T) {
	t.Parallel()

	querier := &resolverSequenceQuerier{rows: []pgx.Row{
		resolverRow{scan: func(...any) error {
			return &pgconn.PgError{
				Code:    "42703",
				Message: `column "privacy_contact_email" does not exist`,
			}
		}},
		resolverRow{scan: func(destinations ...any) error {
			if len(destinations) != 17 {
				return errors.New("unexpected legacy destination count")
			}
			*destinations[0].(*string) = "tenant-id"
			*destinations[1].(*string) = "Acme"
			*destinations[2].(**string) = nil
			*destinations[3].(*string) = "acme"
			*destinations[4].(*string) = string(PlanStarter)
			*destinations[5].(*string) = string(StatusActive)
			*destinations[6].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
			*destinations[7].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
			*destinations[8].(*bool) = true
			*destinations[9].(*widgetconfig.AppearanceOverrides) = widgetconfig.AppearanceOverrides{}
			for index := 10; index < 17; index++ {
				*destinations[index].(**string) = nil
			}
			return nil
		}},
	}}

	resolved, err := (PostgresResolver{Admin: querier}).Resolve(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(querier.queries) != 2 {
		t.Fatalf("query count = %d, want 2", len(querier.queries))
	}
	if !strings.Contains(querier.queries[0], "privacy_contact_email") ||
		strings.Contains(querier.queries[1], "privacy_contact_email") {
		t.Errorf("queries = %#v, want current schema followed by legacy schema", querier.queries)
	}
	if resolved.ID != "tenant-id" || resolved.Slug != "acme" || resolved.Plan != PlanStarter ||
		resolved.Status != StatusActive {
		t.Errorf("resolved tenant = %#v", resolved)
	}
	if resolved.PrivacyContactEmail != nil || resolved.PrivacyRightsEmail != nil ||
		resolved.PrivacyProfileVersion != 0 {
		t.Errorf("legacy contextual privacy profile = %#v, want empty", resolved)
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

type resolverSequenceQuerier struct {
	queries []string
	rows    []pgx.Row
}

func (q *resolverSequenceQuerier) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	q.queries = append(q.queries, query)
	row := q.rows[0]
	q.rows = q.rows[1:]
	return row
}

type resolverRow struct {
	scan func(...any) error
}

func (r resolverRow) Scan(destinations ...any) error {
	return r.scan(destinations...)
}
