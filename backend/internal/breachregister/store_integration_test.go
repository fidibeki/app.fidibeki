package breachregister

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStoreEnforcesLifecycleIdempotencyImmutabilityAndTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(databaseURL, migrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	connection, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(connection.Close)

	// Tenant credentials always connect through the fidibeki_app role. If
	// that role can read or write any breach register table, a tenant
	// member or tenant API key could reach restricted breach data even
	// without an application bug — this is the actual enforcement boundary,
	// not merely an application-layer permission check.
	for _, table := range []string{"breach_incidents", "breach_incident_tenants", "breach_chronology_entries"} {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var allowed bool
			if err := connection.QueryRow(ctx,
				`SELECT has_table_privilege('fidibeki_app', $1, $2)`, table, privilege,
			).Scan(&allowed); err != nil {
				t.Fatalf("check fidibeki_app privilege on %s: %v", table, err)
			}
			if allowed {
				t.Errorf("fidibeki_app has %s on %s, want no grant", privilege, table)
			}
		}
		for _, privilege := range []string{"SELECT", "INSERT"} {
			var allowed bool
			if err := connection.QueryRow(ctx,
				`SELECT has_table_privilege('fidibeki_admin', $1, $2)`, table, privilege,
			).Scan(&allowed); err != nil {
				t.Fatalf("check fidibeki_admin privilege on %s: %v", table, err)
			}
			if !allowed {
				t.Errorf("fidibeki_admin lacks %s on %s", privilege, table)
			}
		}
	}

	var platformAdminID, tenantAID, tenantBID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ('admin@example.test', '{}') RETURNING id
	`).Scan(&platformAdminID); err != nil {
		t.Fatalf("create platform admin fixture: %v", err)
	}
	if err := connection.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme-breach', 'growth', 'active') RETURNING id
	`).Scan(&tenantAID); err != nil {
		t.Fatalf("create tenant A fixture: %v", err)
	}
	if err := connection.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Beta', 'beta-breach', 'growth', 'active') RETURNING id
	`).Scan(&tenantBID); err != nil {
		t.Fatalf("create tenant B fixture: %v", err)
	}

	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	store := Store{Database: connection, Now: func() time.Time { return now }}

	baseInput := CreateIncidentInput{
		AwarenessAt: now.Add(-time.Hour), DetectionSource: DetectionCustomerReport,
		ControllerRole: RoleProcessor, AffectedEnvironments: []string{"production"},
		AffectedSystemCategories:  []string{"database", "api"},
		AffectedDataCategories:    []string{"contact_info"},
		AffectedSubjectCategories: []string{"tenant_members"},
		ConfidentialityImpact:     true,
		ContainmentSummary:        "Rotated the leaked API credential and forced re-authentication.",
		AffectedTenantIDs:         []string{tenantAID, tenantBID},
	}

	incident, err := store.CreateIncident(ctx, platformAdminID, "create-key-1", baseInput)
	if err != nil {
		t.Fatalf("CreateIncident() error = %v", err)
	}
	if incident.PublicID == "" || !incident.CreatedAt.Equal(now) || !incident.AwarenessAt.Equal(baseInput.AwarenessAt) {
		t.Fatalf("created incident = %#v", incident)
	}
	if incident.CreatedByPlatformAdminID != platformAdminID {
		t.Errorf("created incident actor = %q, want %q", incident.CreatedByPlatformAdminID, platformAdminID)
	}
	if len(incident.AffectedTenantIDs) != 2 {
		t.Fatalf("affected tenant ids = %#v", incident.AffectedTenantIDs)
	}

	// Idempotency: replaying the identical request returns the same record
	// without inserting a second row.
	replay, err := store.CreateIncident(ctx, platformAdminID, "create-key-1", baseInput)
	if err != nil || replay.ID != incident.ID {
		t.Fatalf("idempotent CreateIncident() = %#v, error = %v", replay, err)
	}
	var incidentCount int
	if err := connection.QueryRow(ctx,
		`SELECT count(*) FROM breach_incidents WHERE idempotency_key = 'create-key-1'`,
	).Scan(&incidentCount); err != nil || incidentCount != 1 {
		t.Fatalf("breach_incidents row count = %d, error = %v, want 1", incidentCount, err)
	}

	// Idempotency conflict: same key, different input.
	conflictingInput := baseInput
	conflictingInput.ContainmentSummary = "A materially different containment narrative."
	if _, err := store.CreateIncident(
		ctx, platformAdminID, "create-key-1", conflictingInput,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting CreateIncident() error = %v, want ErrIdempotencyConflict", err)
	}

	// Unknown affected tenant reference is rejected and creates nothing.
	unknownTenantInput := baseInput
	unknownTenantInput.AffectedTenantIDs = []string{"00000000-0000-4000-8000-000000000000"}
	if _, err := store.CreateIncident(
		ctx, platformAdminID, "create-key-unknown-tenant", unknownTenantInput,
	); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("unknown-tenant CreateIncident() error = %v, want ErrTenantNotFound", err)
	}
	if err := connection.QueryRow(ctx,
		`SELECT count(*) FROM breach_incidents WHERE idempotency_key = 'create-key-unknown-tenant'`,
	).Scan(&incidentCount); err != nil || incidentCount != 0 {
		t.Fatalf("rejected breach_incidents row count = %d, error = %v, want 0", incidentCount, err)
	}

	// List/get never leak tenant business data: the Go type only ever
	// carries opaque tenant ID references.
	listed, err := store.ListIncidents(ctx, ListFilter{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListIncidents() = %#v, error = %v", listed, err)
	}

	fetched, entries, err := store.GetIncident(ctx, incident.ID)
	if err != nil || fetched.ID != incident.ID || len(entries) != 0 {
		t.Fatalf("GetIncident() = %#v/%#v, error = %v", fetched, entries, err)
	}
	if _, _, err := store.GetIncident(ctx, "00000000-0000-4000-8000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetIncident(missing) error = %v, want ErrNotFound", err)
	}

	// Chronology append: server-authoritative timestamps, actor identity,
	// idempotency, and immutability.
	entryInput := AppendChronologyEntryInput{
		EntryKind: EntryContained, Summary: "Rotated the credential and revoked active sessions.",
		EvidenceReferences: []string{"ticket:INC-42"},
	}
	entry, err := store.AppendChronologyEntry(ctx, incident.ID, platformAdminID, "append-key-1", entryInput)
	if err != nil {
		t.Fatalf("AppendChronologyEntry() error = %v", err)
	}
	if !entry.OccurredAt.Equal(now) || !entry.CreatedAt.Equal(now) {
		t.Fatalf("chronology entry timestamps = %v/%v, want both %v", entry.OccurredAt, entry.CreatedAt, now)
	}
	if entry.ActorPlatformAdminID != platformAdminID {
		t.Errorf("chronology entry actor = %q, want %q", entry.ActorPlatformAdminID, platformAdminID)
	}

	replayedEntry, err := store.AppendChronologyEntry(ctx, incident.ID, platformAdminID, "append-key-1", entryInput)
	if err != nil || replayedEntry.ID != entry.ID {
		t.Fatalf("idempotent AppendChronologyEntry() = %#v, error = %v", replayedEntry, err)
	}
	var entryCount int
	if err := connection.QueryRow(ctx,
		`SELECT count(*) FROM breach_chronology_entries WHERE breach_incident_id = $1`, incident.ID,
	).Scan(&entryCount); err != nil || entryCount != 1 {
		t.Fatalf("breach_chronology_entries row count = %d, error = %v, want 1", entryCount, err)
	}

	conflictingEntry := entryInput
	conflictingEntry.Summary = "A materially different chronology narrative."
	if _, err := store.AppendChronologyEntry(
		ctx, incident.ID, platformAdminID, "append-key-1", conflictingEntry,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting AppendChronologyEntry() error = %v, want ErrIdempotencyConflict", err)
	}

	if _, err := store.AppendChronologyEntry(
		ctx, "00000000-0000-4000-8000-000000000000", platformAdminID, "append-key-2", entryInput,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AppendChronologyEntry(missing incident) error = %v, want ErrNotFound", err)
	}

	_, entriesAfterAppend, err := store.GetIncident(ctx, incident.ID)
	if err != nil || len(entriesAfterAppend) != 1 || entriesAfterAppend[0].ID != entry.ID {
		t.Fatalf("GetIncident() chronology = %#v, error = %v", entriesAfterAppend, err)
	}

	// Facts that change are new chronology rows, never a rewrite of history:
	// both tables reject UPDATE and DELETE outright.
	if _, err := connection.Exec(ctx,
		`UPDATE breach_incidents SET containment_summary = 'rewritten' WHERE id = $1`, incident.ID,
	); err == nil {
		t.Fatal("UPDATE breach_incidents succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx, `DELETE FROM breach_incidents WHERE id = $1`, incident.ID); err == nil {
		t.Fatal("DELETE breach_incidents succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx,
		`UPDATE breach_chronology_entries SET summary = 'rewritten' WHERE id = $1`, entry.ID,
	); err == nil {
		t.Fatal("UPDATE breach_chronology_entries succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx,
		`DELETE FROM breach_chronology_entries WHERE id = $1`, entry.ID,
	); err == nil {
		t.Fatal("DELETE breach_chronology_entries succeeded, want append-only rejection")
	}
}

func migrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	return path
}
