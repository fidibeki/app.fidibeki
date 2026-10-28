package breachregister

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestActionRevisionsSucceedUnderAdminRoleGrants runs the action lifecycle
// through a connection restricted to the fidibeki_admin role — the exact
// privilege set the deployed API uses. The other integration tests connect as
// the table owner, which masked that SELECT ... FOR UPDATE on the append-only
// breach_action_events ledger requires an UPDATE privilege fidibeki_admin
// deliberately does not have (staging returned 503 for every revision append
// during the issue #64 tabletop exercise on 2026-07-24).
func TestActionRevisionsSucceedUnderAdminRoleGrants(t *testing.T) {
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

	owner, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL as owner: %v", err)
	}
	t.Cleanup(owner.Close)

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL config: %v", err)
	}
	adminConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE fidibeki_admin")
		return err
	}
	admin, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("connect PostgreSQL as fidibeki_admin: %v", err)
	}
	t.Cleanup(admin.Close)

	var platformAdminID, tenantID string
	if err := owner.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ('grants-admin@example.test', '{}') RETURNING id
	`).Scan(&platformAdminID); err != nil {
		t.Fatalf("create platform admin fixture: %v", err)
	}
	if err := owner.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Grants', 'grants-breach', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant fixture: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	store := Store{Database: admin, Now: func() time.Time { return now }}

	incident, err := store.CreateIncident(ctx, platformAdminID, "grants-create-1", CreateIncidentInput{
		AwarenessAt: now.Add(-time.Hour), DetectionSource: DetectionInternalMonitoring,
		ControllerRole: RoleProcessor, AffectedEnvironments: []string{"staging"},
		AffectedSystemCategories:  []string{"integration", "api"},
		AffectedDataCategories:    []string{"technical_identifiers"},
		AffectedSubjectCategories: []string{"tenant_members"},
		ConfidentialityImpact:     true,
		ContainmentSummary:        "Rotated the exposed signing secret.",
		AffectedTenantIDs:         []string{tenantID},
	})
	if err != nil {
		t.Fatalf("CreateIncident() under fidibeki_admin error = %v", err)
	}

	_, _, _, actions, _, _, _, err := store.GetIncident(ctx, incident.ID)
	if err != nil {
		t.Fatalf("GetIncident() under fidibeki_admin error = %v", err)
	}
	var containmentActionID string
	for _, action := range actions {
		if action.ContainmentArea != nil && *action.ContainmentArea == ContainmentIntegrations {
			containmentActionID = action.ActionID
		}
	}
	if containmentActionID == "" {
		t.Fatalf("seeded integrations containment action not found in %#v", actions)
	}

	outcome := "Signing secret rotated and delivery signatures verified."
	progress, err := store.AppendActionRevision(
		ctx, incident.ID, containmentActionID, platformAdminID, "grants-revision-1",
		AppendActionRevisionInput{
			ExpectedRevision: 1, Status: ActionInProgress,
			OwnerPlatformAdminID: &platformAdminID,
		},
	)
	if err != nil {
		t.Fatalf("AppendActionRevision(in_progress) under fidibeki_admin error = %v", err)
	}
	if progress.Revision != 2 || progress.Status != ActionInProgress {
		t.Fatalf("in_progress revision = %#v", progress)
	}

	completed, err := store.AppendActionRevision(
		ctx, incident.ID, containmentActionID, platformAdminID, "grants-revision-2",
		AppendActionRevisionInput{
			ExpectedRevision: 2, Status: ActionCompleted,
			OwnerPlatformAdminID: &platformAdminID, OutcomeSummary: &outcome,
			EvidenceReferences: []string{"tabletop:grants:rotation"},
		},
	)
	if err != nil {
		t.Fatalf("AppendActionRevision(completed) under fidibeki_admin error = %v", err)
	}
	if completed.Revision != 3 || completed.Status != ActionCompleted {
		t.Fatalf("completed revision = %#v", completed)
	}

	// The optimistic-concurrency contract must survive without the row lock:
	// a stale expected revision still maps to ErrRevisionConflict, not a
	// generic database error.
	if _, err := store.AppendActionRevision(
		ctx, incident.ID, containmentActionID, platformAdminID, "grants-revision-stale",
		AppendActionRevisionInput{
			ExpectedRevision: 2, Status: ActionInProgress,
			OwnerPlatformAdminID: &platformAdminID,
		},
	); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale AppendActionRevision() error = %v, want ErrRevisionConflict", err)
	}

	dueAt := now.Add(14 * 24 * time.Hour)
	action, err := store.CreatePostIncidentAction(
		ctx, incident.ID, platformAdminID, "grants-action-1",
		CreatePostIncidentActionInput{
			Title:                "Add secret scanning to artifact pipeline",
			OwnerPlatformAdminID: platformAdminID, DueAt: dueAt,
		},
	)
	if err != nil {
		t.Fatalf("CreatePostIncidentAction() under fidibeki_admin error = %v", err)
	}
	postOutcome := "Scanning gate enabled and exercised."
	if _, err := store.AppendActionRevision(
		ctx, incident.ID, action.ActionID, platformAdminID, "grants-action-revision-1",
		AppendActionRevisionInput{
			ExpectedRevision: 1, Status: ActionCompleted,
			OwnerPlatformAdminID: &platformAdminID, DueAt: &dueAt,
			OutcomeSummary:     &postOutcome,
			EvidenceReferences: []string{"tabletop:grants:gate"},
		},
	); err != nil {
		t.Fatalf("post-incident AppendActionRevision(completed) under fidibeki_admin error = %v", err)
	}
}
