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
	for _, table := range []string{
		"breach_incidents", "breach_incident_tenants", "breach_chronology_entries", "breach_assessments",
	} {
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

	// The 72-hour authority-notification deadline is server-computed from
	// awareness_at, not from created_at or client input.
	wantDeadline := baseInput.AwarenessAt.Add(72 * time.Hour)
	if !incident.AuthorityNotificationDeadlineAt.Equal(wantDeadline) {
		t.Fatalf(
			"authority notification deadline = %v, want %v",
			incident.AuthorityNotificationDeadlineAt, wantDeadline,
		)
	}

	fetched, entries, assessments, err := store.GetIncident(ctx, incident.ID)
	if err != nil || fetched.ID != incident.ID || len(entries) != 0 || len(assessments) != 0 {
		t.Fatalf(
			"GetIncident() = %#v/%#v/%#v, error = %v", fetched, entries, assessments, err,
		)
	}
	if !fetched.AuthorityNotificationDeadlineAt.Equal(wantDeadline) {
		t.Fatalf(
			"fetched authority notification deadline = %v, want %v",
			fetched.AuthorityNotificationDeadlineAt, wantDeadline,
		)
	}
	if _, _, _, err := store.GetIncident(
		ctx, "00000000-0000-4000-8000-000000000000",
	); !errors.Is(err, ErrNotFound) {
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

	_, entriesAfterAppend, _, err := store.GetIncident(ctx, incident.ID)
	if err != nil || len(entriesAfterAppend) != 1 || entriesAfterAppend[0].ID != entry.ID {
		t.Fatalf("GetIncident() chronology = %#v, error = %v", entriesAfterAppend, err)
	}

	// Assessment creation: reasoned, operator-entered decisions, bounded
	// estimates, server-authoritative timestamps, actor identity,
	// idempotency, and immutability. The first assessment leaves the
	// authority-notification decision pending -- nothing here is inferred
	// automatically from the incident's facts.
	subjectsMin, subjectsMax := int64(50), int64(200)
	recordsMin, recordsMax := int64(50), int64(400)
	likelyConsequences := "Possible unauthorized read access to contact details."
	recoveryMeasures := "Forced re-authentication and rotated the leaked credential."
	assessmentInput := CreateAssessmentInput{
		AuthorityNotificationDecision:     AuthorityNotificationAssessmentPending,
		AuthorityNotificationReasoning:    "Awaiting confirmation of exfiltration scope before deciding.",
		HighRiskToIndividuals:             RiskAssessmentPending,
		HighRiskReasoning:                 "Risk to data subjects not yet determined.",
		DataSubjectCommunicationDecision:  CommunicationAssessmentPending,
		DataSubjectCommunicationReasoning: "Communication decision depends on the risk assessment above.",
		EstimatedAffectedSubjectsMin:      &subjectsMin,
		EstimatedAffectedSubjectsMax:      &subjectsMax,
		EstimatedAffectedRecordsMin:       &recordsMin,
		EstimatedAffectedRecordsMax:       &recordsMax,
		LikelyConsequences:                &likelyConsequences,
		RecoveryMeasures:                  &recoveryMeasures,
		EvidenceReferences:                []string{"ticket:INC-42"},
	}
	assessment, err := store.CreateAssessment(ctx, incident.ID, platformAdminID, "assess-key-1", assessmentInput)
	if err != nil {
		t.Fatalf("CreateAssessment() error = %v", err)
	}
	if !assessment.AssessedAt.Equal(now) || !assessment.CreatedAt.Equal(now) {
		t.Fatalf(
			"assessment timestamps = %v/%v, want both %v", assessment.AssessedAt, assessment.CreatedAt, now,
		)
	}
	if assessment.AssessedByPlatformAdminID != platformAdminID {
		t.Errorf("assessment actor = %q, want %q", assessment.AssessedByPlatformAdminID, platformAdminID)
	}
	if assessment.AuthorityNotificationDecision != AuthorityNotificationAssessmentPending {
		t.Errorf(
			"assessment authority decision = %q, want %q",
			assessment.AuthorityNotificationDecision, AuthorityNotificationAssessmentPending,
		)
	}
	if assessment.EstimatedAffectedSubjectsMin == nil || *assessment.EstimatedAffectedSubjectsMin != subjectsMin ||
		assessment.EstimatedAffectedSubjectsMax == nil || *assessment.EstimatedAffectedSubjectsMax != subjectsMax {
		t.Fatalf("assessment subject estimate = %#v", assessment)
	}

	replayedAssessment, err := store.CreateAssessment(
		ctx, incident.ID, platformAdminID, "assess-key-1", assessmentInput,
	)
	if err != nil || replayedAssessment.ID != assessment.ID {
		t.Fatalf("idempotent CreateAssessment() = %#v, error = %v", replayedAssessment, err)
	}
	var assessmentCount int
	if err := connection.QueryRow(ctx,
		`SELECT count(*) FROM breach_assessments WHERE breach_incident_id = $1`, incident.ID,
	).Scan(&assessmentCount); err != nil || assessmentCount != 1 {
		t.Fatalf("breach_assessments row count = %d, error = %v, want 1", assessmentCount, err)
	}

	conflictingAssessment := assessmentInput
	conflictingAssessment.AuthorityNotificationReasoning = "A materially different reasoning narrative."
	if _, err := store.CreateAssessment(
		ctx, incident.ID, platformAdminID, "assess-key-1", conflictingAssessment,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting CreateAssessment() error = %v, want ErrIdempotencyConflict", err)
	}

	if _, err := store.CreateAssessment(
		ctx, "00000000-0000-4000-8000-000000000000", platformAdminID, "assess-key-2", assessmentInput,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateAssessment(missing incident) error = %v, want ErrNotFound", err)
	}

	_, _, assessmentsAfterCreate, err := store.GetIncident(ctx, incident.ID)
	if err != nil || len(assessmentsAfterCreate) != 1 || assessmentsAfterCreate[0].ID != assessment.ID {
		t.Fatalf("GetIncident() assessments = %#v, error = %v", assessmentsAfterCreate, err)
	}

	// Facts that change are new rows, never a rewrite of history: every
	// breach register table rejects UPDATE and DELETE outright.
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
	if _, err := connection.Exec(ctx,
		`UPDATE breach_assessments SET authority_notification_reasoning = 'rewritten' WHERE id = $1`, assessment.ID,
	); err == nil {
		t.Fatal("UPDATE breach_assessments succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx,
		`DELETE FROM breach_assessments WHERE id = $1`, assessment.ID,
	); err == nil {
		t.Fatal("DELETE breach_assessments succeeded, want append-only rejection")
	}
}

// TestMigration096RollsBackAndReappliesCleanly exercises migration 000096's
// down.sql directly (up -> down -> up), since the tracer bullet in migration
// 000092 only ever exercised a single Up. This is the only test that would
// catch a wrong DROP order or a leftover object in the down migration.
func TestMigration096RollsBackAndReappliesCleanly(t *testing.T) {
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
	if err := migrator.Migrate(95); err != nil {
		t.Fatalf("migrate to pre-96 version: %v", err)
	}

	connection, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(connection.Close)

	// Migration 92 made incidents append-only. Exercise a real 95 -> 96
	// upgrade with an existing incident so migration 96's controlled
	// deadline backfill cannot regress into being blocked by that trigger.
	var legacyPlatformAdminID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ('legacy-migration-check@example.test', '{}') RETURNING id
	`).Scan(&legacyPlatformAdminID); err != nil {
		t.Fatalf("create legacy platform admin fixture: %v", err)
	}
	legacyAwarenessAt := time.Date(2026, time.July, 18, 8, 30, 0, 0, time.UTC)
	var legacyIncidentID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO breach_incidents (
			public_id, awareness_at, detection_source, controller_role,
			affected_environments, affected_system_categories, affected_data_categories,
			affected_subject_categories, confidentiality_impact, integrity_impact,
			availability_impact, containment_summary, created_by_platform_admin_id,
			idempotency_key, request_fingerprint, created_at
		) VALUES (
			'BR-LEGACY96', $1::timestamptz, 'internal_monitoring', 'controller',
			'{staging}', '{database}', '{contact_info}', '{tenant_members}',
			true, false, false, 'Contained before migration 96.', $2,
			'legacy-migration-96', decode(repeat('00', 32), 'hex'),
			$1::timestamptz + interval '1 hour'
		) RETURNING id
	`, legacyAwarenessAt, legacyPlatformAdminID).Scan(&legacyIncidentID); err != nil {
		t.Fatalf("create pre-migration-96 breach incident: %v", err)
	}
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate existing incident up to 96: %v", err)
	}
	var legacyDeadline time.Time
	if err := connection.QueryRow(ctx, `
		SELECT authority_notification_deadline_at
		FROM breach_incidents
		WHERE id = $1
	`, legacyIncidentID).Scan(&legacyDeadline); err != nil {
		t.Fatalf("read backfilled authority deadline: %v", err)
	}
	if want := legacyAwarenessAt.Add(72 * time.Hour); !legacyDeadline.Equal(want) {
		t.Fatalf("backfilled authority deadline = %v, want %v", legacyDeadline, want)
	}
	if _, err := connection.Exec(ctx, `
		UPDATE breach_incidents SET containment_summary = 'must remain immutable' WHERE id = $1
	`, legacyIncidentID); err == nil {
		t.Fatal("post-migration breach incident UPDATE succeeded, want append-only rejection")
	}
	if err := migrator.Migrate(95); err != nil {
		t.Fatalf("migrate to pre-96 version after existing-row upgrade: %v", err)
	}
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate 96 up after rollback: %v", err)
	}

	var deadlineColumnExists bool
	if err := connection.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'breach_incidents' AND column_name = 'authority_notification_deadline_at'
		)
	`).Scan(&deadlineColumnExists); err != nil || !deadlineColumnExists {
		t.Fatalf("authority_notification_deadline_at column exists = %t, error = %v", deadlineColumnExists, err)
	}

	var assessmentsGranted bool
	if err := connection.QueryRow(ctx,
		`SELECT has_table_privilege('fidibeki_admin', 'breach_assessments', 'INSERT')`,
	).Scan(&assessmentsGranted); err != nil || !assessmentsGranted {
		t.Fatalf("fidibeki_admin breach_assessments INSERT privilege = %t, error = %v", assessmentsGranted, err)
	}

	// The reapplied 72-hour deadline and reasoned-assessment insert path
	// must both still work after the rollback/reapply cycle.
	var platformAdminID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ('migration-check@example.test', '{}') RETURNING id
	`).Scan(&platformAdminID); err != nil {
		t.Fatalf("create platform admin fixture: %v", err)
	}
	var tenantID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme-migration-check', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant fixture: %v", err)
	}

	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	store := Store{Database: connection, Now: func() time.Time { return now }}
	incident, err := store.CreateIncident(ctx, platformAdminID, "migration-check-key", CreateIncidentInput{
		AwarenessAt: now.Add(-time.Hour), DetectionSource: DetectionInternalMonitoring,
		ControllerRole: RoleProcessor, AffectedEnvironments: []string{"staging"},
		AffectedSystemCategories: []string{"database"}, AffectedDataCategories: []string{"contact_info"},
		AffectedSubjectCategories: []string{"tenant_members"}, ConfidentialityImpact: true,
		ContainmentSummary: "Rotated the affected credential.", AffectedTenantIDs: []string{tenantID},
	})
	if err != nil {
		t.Fatalf("CreateIncident() after migration cycle error = %v", err)
	}
	wantDeadline := now.Add(-time.Hour).Add(72 * time.Hour)
	if !incident.AuthorityNotificationDeadlineAt.Equal(wantDeadline) {
		t.Fatalf(
			"authority notification deadline = %v, want %v",
			incident.AuthorityNotificationDeadlineAt, wantDeadline,
		)
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
