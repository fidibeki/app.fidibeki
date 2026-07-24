package breachregister

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
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
		"breach_incidents", "breach_incident_tenants", "breach_chronology_entries",
		"breach_assessments", "breach_action_events",
		"breach_provider_incident_intakes", "breach_provider_incident_tenants",
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
	for _, role := range []string{"fidibeki_app"} {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var allowed bool
			if err := connection.QueryRow(ctx,
				`SELECT has_table_privilege($1, 'breach_action_events', $2)`,
				role, privilege,
			).Scan(&allowed); err != nil {
				t.Fatalf("check %s %s privilege on breach_action_events: %v", role, privilege, err)
			}
			if allowed {
				t.Errorf("%s has %s on breach_action_events, want no grant", role, privilege)
			}
		}
	}
	var publicPrivilegeCount int
	if err := connection.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.table_privileges
		WHERE table_schema = 'public'
		  AND table_name = 'breach_action_events'
		  AND grantee = 'PUBLIC'
	`).Scan(&publicPrivilegeCount); err != nil {
		t.Fatalf("check PUBLIC privileges on breach_action_events: %v", err)
	}
	if publicPrivilegeCount != 0 {
		t.Errorf("PUBLIC breach_action_events privilege count = %d, want 0", publicPrivilegeCount)
	}
	for _, privilege := range []string{"UPDATE", "DELETE"} {
		var allowed bool
		if err := connection.QueryRow(ctx,
			`SELECT has_table_privilege('fidibeki_admin', 'breach_action_events', $1)`,
			privilege,
		).Scan(&allowed); err != nil {
			t.Fatalf("check fidibeki_admin %s privilege on breach_action_events: %v", privilege, err)
		}
		if allowed {
			t.Errorf("fidibeki_admin has %s on breach_action_events, want no grant", privilege)
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

	fetched, entries, assessments, actions, actionEvents, notifications, providerIntakes, err :=
		store.GetIncident(ctx, incident.ID)
	if err != nil || fetched.ID != incident.ID || len(entries) != 0 || len(assessments) != 0 ||
		len(actions) != 9 || len(actionEvents) != 9 || len(notifications) != 0 ||
		len(providerIntakes) != 0 {
		t.Fatalf(
			"GetIncident() = %#v/%#v/%#v/%#v/%#v/%#v/%#v, error = %v",
			fetched, entries, assessments, actions, actionEvents, notifications,
			providerIntakes, err,
		)
	}
	if !fetched.AuthorityNotificationDeadlineAt.Equal(wantDeadline) {
		t.Fatalf(
			"fetched authority notification deadline = %v, want %v",
			fetched.AuthorityNotificationDeadlineAt, wantDeadline,
		)
	}
	if _, _, _, _, _, _, _, err := store.GetIncident(
		ctx, "00000000-0000-4000-8000-000000000000",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetIncident(missing) error = %v, want ErrNotFound", err)
	}

	// External provider notices freeze the current code-owned governance
	// snapshot and can only map tenants already affected by the incident.
	providerInput := CreateProviderIncidentIntakeInput{
		ProviderID:                 "resend_transactional_email",
		ProviderNoticeReference:    "provider-case-123",
		ProviderReportedAt:         now.Add(-2 * time.Hour),
		Summary:                    "Provider reported an incident affecting message delivery.",
		EvidenceReferences:         []string{"provider-case:123"},
		AffectedDataFlowCategories: []string{"external_communications"},
		AffectedTenantIDs:          []string{tenantAID},
	}
	providerIntake, err := store.CreateProviderIncidentIntake(
		ctx, incident.ID, platformAdminID, "provider-intake-key-1", providerInput,
	)
	if err != nil {
		t.Fatalf("CreateProviderIncidentIntake() error = %v", err)
	}
	if providerIntake.ProviderID != providerInput.ProviderID ||
		providerIntake.ProviderDisplayName != "Resend transactional email" ||
		providerIntake.GovernanceRegistryVersion != 1 ||
		providerIntake.PrimaryRetentionCategory != "external_communications" ||
		!providerIntake.ReceivedAt.Equal(now) ||
		!providerIntake.CreatedAt.Equal(now) ||
		providerIntake.ActorPlatformAdminID != platformAdminID ||
		len(providerIntake.AffectedTenantIDs) != 1 ||
		providerIntake.AffectedTenantIDs[0] != tenantAID {
		t.Fatalf("provider incident intake = %#v", providerIntake)
	}
	replayedProviderIntake, err := store.CreateProviderIncidentIntake(
		ctx, incident.ID, platformAdminID, "provider-intake-key-1", providerInput,
	)
	if err != nil || replayedProviderIntake.ID != providerIntake.ID {
		t.Fatalf(
			"idempotent CreateProviderIncidentIntake() = %#v, error = %v",
			replayedProviderIntake, err,
		)
	}
	conflictingProviderInput := providerInput
	conflictingProviderInput.Summary = "A different external-provider incident summary."
	if _, err := store.CreateProviderIncidentIntake(
		ctx, incident.ID, platformAdminID, "provider-intake-key-1",
		conflictingProviderInput,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf(
			"conflicting CreateProviderIncidentIntake() error = %v, want ErrIdempotencyConflict",
			err,
		)
	}
	unrelatedProviderInput := providerInput
	unrelatedProviderInput.AffectedTenantIDs =
		[]string{"00000000-0000-4000-8000-000000000000"}
	if _, err := store.CreateProviderIncidentIntake(
		ctx, incident.ID, platformAdminID, "provider-intake-unrelated",
		unrelatedProviderInput,
	); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf(
			"unrelated-tenant CreateProviderIncidentIntake() error = %v, want ErrTenantNotFound",
			err,
		)
	}
	internalProviderInput := providerInput
	internalProviderInput.ProviderID = "email_capture_filesystem"
	if _, err := store.CreateProviderIncidentIntake(
		ctx, incident.ID, platformAdminID, "provider-intake-internal",
		internalProviderInput,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf(
			"internal-runtime CreateProviderIncidentIntake() error = %v, want ErrInvalidRequest",
			err,
		)
	}
	if _, err := store.CreateProviderIncidentIntake(
		ctx, "00000000-0000-4000-8000-000000000000", platformAdminID,
		"provider-intake-missing", providerInput,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf(
			"missing-incident CreateProviderIncidentIntake() error = %v, want ErrNotFound",
			err,
		)
	}
	_, _, _, _, _, _, providerIntakesAfterCreate, err :=
		store.GetIncident(ctx, incident.ID)
	if err != nil || len(providerIntakesAfterCreate) != 1 ||
		providerIntakesAfterCreate[0].ID != providerIntake.ID {
		t.Fatalf(
			"GetIncident() provider intakes = %#v, error = %v",
			providerIntakesAfterCreate, err,
		)
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

	_, entriesAfterAppend, _, _, _, _, _, err := store.GetIncident(ctx, incident.ID)
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

	_, _, assessmentsAfterCreate, _, _, _, _, err := store.GetIncident(ctx, incident.ID)
	if err != nil || len(assessmentsAfterCreate) != 1 || assessmentsAfterCreate[0].ID != assessment.ID {
		t.Fatalf("GetIncident() assessments = %#v, error = %v", assessmentsAfterCreate, err)
	}

	// Every new incident receives the complete code-owned containment
	// checklist in the same transaction.
	if len(actions) != len(containmentControls) {
		t.Fatalf("initial containment actions = %d, want %d", len(actions), len(containmentControls))
	}
	containmentAction := actions[0]
	for _, action := range actions {
		if action.ActionKind != ActionContainment || action.ContainmentArea == nil ||
			action.Status != ActionPending || action.Revision != 1 ||
			action.ActorPlatformAdminID != platformAdminID ||
			!action.CreatedAt.Equal(now) || !action.OccurredAt.Equal(now) {
			t.Fatalf("invalid initial containment action = %#v", action)
		}
	}

	dueAt := now.Add(48 * time.Hour)
	correctiveReference := "issue:FID-64"
	postInput := CreatePostIncidentActionInput{
		Title:                "Complete the credential rotation review",
		OwnerPlatformAdminID: platformAdminID, DueAt: dueAt,
		CorrectiveReference: &correctiveReference,
	}
	postAction, err := store.CreatePostIncidentAction(
		ctx, incident.ID, platformAdminID, "post-action-key-1", postInput,
	)
	if err != nil {
		t.Fatalf("CreatePostIncidentAction() error = %v", err)
	}
	if postAction.ActionKind != ActionPostIncident || postAction.Status != ActionPending ||
		postAction.OwnerPlatformAdminID == nil || *postAction.OwnerPlatformAdminID != platformAdminID ||
		postAction.DueAt == nil || !postAction.DueAt.Equal(dueAt) {
		t.Fatalf("created post-incident action = %#v", postAction)
	}
	replayedPostAction, err := store.CreatePostIncidentAction(
		ctx, incident.ID, platformAdminID, "post-action-key-1", postInput,
	)
	if err != nil || replayedPostAction.ID != postAction.ID {
		t.Fatalf("idempotent CreatePostIncidentAction() = %#v, error = %v", replayedPostAction, err)
	}
	afterDeadlineStore := Store{
		Database: connection, Now: func() time.Time { return dueAt.Add(time.Hour) },
	}
	replayedAfterDeadline, err := afterDeadlineStore.CreatePostIncidentAction(
		ctx, incident.ID, platformAdminID, "post-action-key-1", postInput,
	)
	if err != nil || replayedAfterDeadline.ID != postAction.ID {
		t.Fatalf(
			"post-deadline idempotent CreatePostIncidentAction() = %#v, error = %v",
			replayedAfterDeadline, err,
		)
	}
	pastDueInput := postInput
	pastDueInput.DueAt = now
	if _, err := store.CreatePostIncidentAction(
		ctx, incident.ID, platformAdminID, "post-action-past-due", pastDueInput,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("past-due CreatePostIncidentAction() error = %v, want ErrInvalidRequest", err)
	}
	conflictingPostInput := postInput
	conflictingPostInput.Title = "Different corrective work"
	if _, err := store.CreatePostIncidentAction(
		ctx, incident.ID, platformAdminID, "post-action-key-1", conflictingPostInput,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting CreatePostIncidentAction() error = %v", err)
	}
	unknownOwnerInput := postInput
	unknownOwnerInput.OwnerPlatformAdminID = "00000000-0000-4000-8000-000000000000"
	if _, err := store.CreatePostIncidentAction(
		ctx, incident.ID, platformAdminID, "post-action-unknown-owner", unknownOwnerInput,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown-owner CreatePostIncidentAction() error = %v", err)
	}

	ownerID := platformAdminID
	inProgressInput := AppendActionRevisionInput{
		ExpectedRevision: 1, Status: ActionInProgress, OwnerPlatformAdminID: &ownerID,
	}
	inProgress, err := store.AppendActionRevision(
		ctx, incident.ID, containmentAction.ActionID, platformAdminID,
		"containment-progress-key", inProgressInput,
	)
	if err != nil {
		t.Fatalf("AppendActionRevision(in_progress) error = %v", err)
	}
	if inProgress.Revision != 2 || inProgress.Title != containmentAction.Title ||
		inProgress.ContainmentArea == nil ||
		*inProgress.ContainmentArea != *containmentAction.ContainmentArea {
		t.Fatalf("in-progress containment action = %#v", inProgress)
	}
	replayedProgress, err := store.AppendActionRevision(
		ctx, incident.ID, containmentAction.ActionID, platformAdminID,
		"containment-progress-key", inProgressInput,
	)
	if err != nil || replayedProgress.ID != inProgress.ID {
		t.Fatalf("idempotent AppendActionRevision() = %#v, error = %v", replayedProgress, err)
	}
	changedProgress := inProgressInput
	changedProgress.Status = ActionPending
	if _, err := store.AppendActionRevision(
		ctx, incident.ID, containmentAction.ActionID, platformAdminID,
		"containment-progress-key", changedProgress,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting AppendActionRevision() error = %v", err)
	}
	if _, err := store.AppendActionRevision(
		ctx, incident.ID, containmentAction.ActionID, platformAdminID,
		"containment-stale-key", inProgressInput,
	); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale AppendActionRevision() error = %v", err)
	}

	completionOutcome := "Credential rotation was independently verified."
	completedInput := AppendActionRevisionInput{
		ExpectedRevision: 2, Status: ActionCompleted, OwnerPlatformAdminID: &ownerID,
		OutcomeSummary: &completionOutcome, EvidenceReferences: []string{"ticket:INC-42"},
	}
	completed, err := store.AppendActionRevision(
		ctx, incident.ID, containmentAction.ActionID, platformAdminID,
		"containment-complete-key", completedInput,
	)
	if err != nil || completed.Revision != 3 {
		t.Fatalf("AppendActionRevision(completed) = %#v, error = %v", completed, err)
	}
	invalidTerminalUpdate := completedInput
	invalidTerminalUpdate.ExpectedRevision = 3
	invalidTerminalUpdate.Status = ActionPending
	if _, err := store.AppendActionRevision(
		ctx, incident.ID, containmentAction.ActionID, platformAdminID,
		"containment-terminal-invalid", invalidTerminalUpdate,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("terminal action update error = %v, want ErrInvalidRequest", err)
	}
	reopenedInput := AppendActionRevisionInput{
		ExpectedRevision: 3, Status: ActionInProgress, OwnerPlatformAdminID: &ownerID,
	}
	reopened, err := store.AppendActionRevision(
		ctx, incident.ID, containmentAction.ActionID, platformAdminID,
		"containment-reopen-key", reopenedInput,
	)
	if err != nil || reopened.Revision != 4 {
		t.Fatalf("AppendActionRevision(reopen) = %#v, error = %v", reopened, err)
	}

	// Two callers may observe revision 4, but only one can commit revision
	// 5. The parent/action locks make the second return a clean conflict.
	var waitGroup sync.WaitGroup
	concurrentErrors := make([]error, 2)
	for index := range concurrentErrors {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			_, concurrentErrors[index] = store.AppendActionRevision(
				ctx, incident.ID, containmentAction.ActionID, platformAdminID,
				"containment-concurrent-"+string(rune('a'+index)),
				AppendActionRevisionInput{
					ExpectedRevision: 4, Status: ActionInProgress,
					OwnerPlatformAdminID: &ownerID,
				},
			)
		}(index)
	}
	waitGroup.Wait()
	successes, revisionConflicts := 0, 0
	for _, concurrentErr := range concurrentErrors {
		if concurrentErr == nil {
			successes++
		} else if errors.Is(concurrentErr, ErrRevisionConflict) {
			revisionConflicts++
		} else {
			t.Fatalf("concurrent AppendActionRevision() error = %v", concurrentErr)
		}
	}
	if successes != 1 || revisionConflicts != 1 {
		t.Fatalf(
			"concurrent AppendActionRevision() successes/conflicts = %d/%d, want 1/1",
			successes, revisionConflicts,
		)
	}

	_, _, _, actionsAfterUpdates, eventsAfterUpdates, _, _, err :=
		store.GetIncident(ctx, incident.ID)
	if err != nil || len(actionsAfterUpdates) != 10 || len(eventsAfterUpdates) != 14 {
		t.Fatalf(
			"GetIncident() actions/events = %d/%d, error = %v, want 10/14",
			len(actionsAfterUpdates), len(eventsAfterUpdates), err,
		)
	}

	// Incident detail is one repeatable-read snapshot. Hold the chronology
	// table after the incident row has been read, commit a new action
	// revision, then let the read continue: the in-flight response must
	// remain on revision 5 while a later response can see revision 6.
	blocker, err := connection.Begin(ctx)
	if err != nil {
		t.Fatalf("begin incident-read blocker: %v", err)
	}
	blockerReleased := false
	defer func() {
		if !blockerReleased {
			_ = blocker.Rollback(ctx)
		}
	}()
	if _, err := blocker.Exec(ctx,
		`LOCK TABLE breach_chronology_entries IN ACCESS EXCLUSIVE MODE`,
	); err != nil {
		t.Fatalf("lock chronology table: %v", err)
	}
	type incidentReadResult struct {
		actions []ActionEvent
		events  []ActionEvent
		err     error
	}
	readResult := make(chan incidentReadResult, 1)
	go func() {
		_, _, _, readActions, readEvents, _, _, readErr := store.GetIncident(ctx, incident.ID)
		readResult <- incidentReadResult{actions: readActions, events: readEvents, err: readErr}
	}()
	snapshotObserved := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var lockCount int
		if err := connection.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_locks
			WHERE relation = 'breach_incidents'::regclass
			  AND mode = 'AccessShareLock'
			  AND granted
		`).Scan(&lockCount); err != nil {
			t.Fatalf("inspect incident-read snapshot lock: %v", err)
		}
		if lockCount > 0 {
			snapshotObserved = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !snapshotObserved {
		t.Fatal("incident detail read did not establish its repeatable-read snapshot")
	}
	snapshotRevision, err := store.AppendActionRevision(
		ctx, incident.ID, containmentAction.ActionID, platformAdminID,
		"containment-snapshot-write", AppendActionRevisionInput{
			ExpectedRevision: 5, Status: ActionInProgress,
			OwnerPlatformAdminID: &ownerID,
		},
	)
	if err != nil || snapshotRevision.Revision != 6 {
		t.Fatalf("snapshot concurrent action revision = %#v, error = %v", snapshotRevision, err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release incident-read blocker: %v", err)
	}
	blockerReleased = true
	var snapshotRead incidentReadResult
	select {
	case snapshotRead = <-readResult:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for incident detail snapshot")
	}
	if snapshotRead.err != nil {
		t.Fatalf("GetIncident() snapshot error = %v", snapshotRead.err)
	}
	snapshotCurrentRevision := 0
	snapshotHistoryRevision := 0
	for _, action := range snapshotRead.actions {
		if action.ActionID == containmentAction.ActionID {
			snapshotCurrentRevision = action.Revision
		}
	}
	for _, event := range snapshotRead.events {
		if event.ActionID == containmentAction.ActionID &&
			event.Revision > snapshotHistoryRevision {
			snapshotHistoryRevision = event.Revision
		}
	}
	if snapshotCurrentRevision != 5 || snapshotHistoryRevision != 5 {
		t.Fatalf(
			"incident snapshot current/history revision = %d/%d, want 5/5",
			snapshotCurrentRevision, snapshotHistoryRevision,
		)
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
	if _, err := connection.Exec(ctx,
		`UPDATE breach_action_events SET title = 'rewritten' WHERE id = $1`, containmentAction.ID,
	); err == nil {
		t.Fatal("UPDATE breach_action_events succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx,
		`DELETE FROM breach_action_events WHERE id = $1`, containmentAction.ID,
	); err == nil {
		t.Fatal("DELETE breach_action_events succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx, `
		UPDATE breach_provider_incident_intakes
		SET summary = 'rewritten'
		WHERE id = $1
	`, providerIntake.ID); err == nil {
		t.Fatal("UPDATE breach_provider_incident_intakes succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx, `
		DELETE FROM breach_provider_incident_intakes WHERE id = $1
	`, providerIntake.ID); err == nil {
		t.Fatal("DELETE breach_provider_incident_intakes succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx, `
		UPDATE breach_provider_incident_tenants
		SET created_at = created_at + interval '1 second'
		WHERE provider_incident_intake_id = $1
	`, providerIntake.ID); err == nil {
		t.Fatal("UPDATE breach_provider_incident_tenants succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx, `
		DELETE FROM breach_provider_incident_tenants
		WHERE provider_incident_intake_id = $1
	`, providerIntake.ID); err == nil {
		t.Fatal("DELETE breach_provider_incident_tenants succeeded, want append-only rejection")
	}
	if _, err := connection.Exec(ctx, `
		INSERT INTO breach_provider_incident_intakes (
			id, breach_incident_id, provider_id, governance_registry_version,
			provider_display_name, boundary_kind, approval_status, contract_status,
			primary_retention_category, provider_notice_reference, provider_reported_at,
			received_at, summary, evidence_references, affected_data_flow_categories,
			actor_platform_admin_id, idempotency_key, request_fingerprint, created_at
		)
		SELECT
			gen_random_uuid(), intake.breach_incident_id, intake.provider_id,
			intake.governance_registry_version, intake.provider_display_name,
			intake.boundary_kind, intake.approval_status, intake.contract_status,
			intake.primary_retention_category, 'direct-pre-awareness',
			incident.awareness_at - interval '1 second',
			incident.awareness_at - interval '1 second',
			'Receipt must not predate incident awareness.', '{}',
			intake.affected_data_flow_categories, intake.actor_platform_admin_id,
			'direct-pre-awareness', digest('direct-pre-awareness', 'sha256'),
			incident.awareness_at - interval '1 second'
		FROM breach_provider_incident_intakes AS intake
		JOIN breach_incidents AS incident ON incident.id = intake.breach_incident_id
		WHERE intake.id = $1
	`, providerIntake.ID); err == nil {
		t.Fatal("provider intake receipt before incident awareness succeeded")
	}
	if _, err := connection.Exec(ctx, `
		INSERT INTO breach_action_events (
			breach_incident_id, action_id, revision, action_kind, containment_area,
			title, status, owner_platform_admin_id, evidence_references,
			actor_platform_admin_id, idempotency_key, request_fingerprint,
			occurred_at, created_at
		) VALUES (
			$1, $2, 7, 'containment', $3, 'Changed immutable title', 'in_progress',
			$4, '{}', $4, 'direct-identity-change',
			digest('direct-identity-change', 'sha256'), $5, $5
		)
	`, incident.ID, containmentAction.ActionID, *containmentAction.ContainmentArea,
		platformAdminID, now); err == nil {
		t.Fatal("INSERT changed breach action identity succeeded, want revision guard rejection")
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

func TestMigration101BackfillsAndReappliesActionLedger(t *testing.T) {
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
	if err := migrator.Migrate(100); err != nil {
		t.Fatalf("migrate to pre-101 version: %v", err)
	}

	connection, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(connection.Close)

	var platformAdminID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ('legacy-actions@example.test', '{}') RETURNING id
	`).Scan(&platformAdminID); err != nil {
		t.Fatalf("create legacy platform admin fixture: %v", err)
	}
	createdAt := time.Date(2026, time.July, 20, 9, 30, 0, 0, time.UTC)
	awarenessAt := createdAt.Add(-time.Hour)
	var incidentID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO breach_incidents (
			public_id, awareness_at, detection_source, controller_role,
			affected_environments, affected_system_categories, affected_data_categories,
			affected_subject_categories, confidentiality_impact, integrity_impact,
			availability_impact, containment_summary, created_by_platform_admin_id,
			idempotency_key, request_fingerprint, created_at,
			authority_notification_deadline_at
		) VALUES (
			'BR-LEGACY101', $1, 'internal_monitoring', 'controller',
			'{staging}', '{database}', '{contact_info}', '{tenant_members}',
			true, false, false, 'Contained before migration 101.', $2,
			'legacy-migration-101', decode(repeat('01', 32), 'hex'), $3,
			$1::timestamptz + interval '72 hours'
		) RETURNING id
	`, awarenessAt, platformAdminID, createdAt).Scan(&incidentID); err != nil {
		t.Fatalf("create pre-migration-101 breach incident: %v", err)
	}

	assertMigratedChecklist := func(stage string) {
		t.Helper()
		var count, distinctAreas, matchingActorAndTime int
		if err := connection.QueryRow(ctx, `
			SELECT count(*), count(DISTINCT containment_area),
			       count(*) FILTER (
			           WHERE actor_platform_admin_id = $2
			             AND occurred_at = $3
			             AND created_at = $3
			             AND action_kind = 'containment'
			             AND status = 'pending'
			             AND revision = 1
			             AND idempotency_key LIKE 'seed:containment:%'
			             AND octet_length(request_fingerprint) = 32
			       )
			FROM breach_action_events
			WHERE breach_incident_id = $1
		`, incidentID, platformAdminID, createdAt).Scan(
			&count, &distinctAreas, &matchingActorAndTime,
		); err != nil {
			t.Fatalf("%s: read migrated containment checklist: %v", stage, err)
		}
		if count != len(containmentControls) || distinctAreas != len(containmentControls) ||
			matchingActorAndTime != len(containmentControls) {
			t.Fatalf(
				"%s: migrated checklist count/areas/provenance = %d/%d/%d, want %d",
				stage, count, distinctAreas, matchingActorAndTime, len(containmentControls),
			)
		}
		rows, err := connection.Query(ctx, `
			SELECT containment_area::text, title
			FROM breach_action_events
			WHERE breach_incident_id = $1
			ORDER BY containment_area
		`, incidentID)
		if err != nil {
			t.Fatalf("%s: list migrated containment catalog: %v", stage, err)
		}
		defer rows.Close()
		got := make(map[ContainmentArea]string, len(containmentControls))
		for rows.Next() {
			var area ContainmentArea
			var title string
			if err := rows.Scan(&area, &title); err != nil {
				t.Fatalf("%s: scan migrated containment catalog: %v", stage, err)
			}
			got[area] = title
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s: iterate migrated containment catalog: %v", stage, err)
		}
		for _, control := range containmentControls {
			if got[control.Area] != control.Title {
				t.Errorf(
					"%s: containment %q title = %q, want %q",
					stage, control.Area, got[control.Area], control.Title,
				)
			}
		}
	}

	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate legacy incident to 101: %v", err)
	}
	assertMigratedChecklist("first up")

	var actionID, eventID, title, area string
	if err := connection.QueryRow(ctx, `
		SELECT action_id, id, title, containment_area
		FROM breach_action_events
		WHERE breach_incident_id = $1
		ORDER BY sequence
		LIMIT 1
	`, incidentID).Scan(&actionID, &eventID, &title, &area); err != nil {
		t.Fatalf("read backfilled action: %v", err)
	}
	if _, err := connection.Exec(ctx, `
		INSERT INTO breach_action_events (
			breach_incident_id, action_id, revision, action_kind, containment_area,
			title, status, owner_platform_admin_id, evidence_references,
			actor_platform_admin_id, idempotency_key, request_fingerprint,
			occurred_at, created_at
		) VALUES (
			$1, $2, 2, 'containment', $3, $4, 'completed', $5, '{}', $5,
			'migration-completed-without-evidence',
			digest('migration-completed-without-evidence', 'sha256'), $6, $6
		)
	`, incidentID, actionID, area, title, platformAdminID, createdAt.Add(time.Hour)); err == nil {
		t.Fatal("completed action without outcome/evidence succeeded")
	}
	if _, err := connection.Exec(ctx, `
		INSERT INTO breach_action_events (
			breach_incident_id, action_id, revision, action_kind, containment_area,
			title, status, evidence_references, actor_platform_admin_id,
			occurred_at, created_at
		) VALUES (
			$1, $2, 2, 'containment', $3, $4, 'pending', '{}', $5, $6, $6
		)
	`, incidentID, actionID, area, title, platformAdminID, createdAt.Add(time.Hour)); err == nil {
		t.Fatal("non-seed action revision without idempotency evidence succeeded")
	}
	if _, err := connection.Exec(ctx, `
		INSERT INTO breach_action_events (
			breach_incident_id, action_id, revision, action_kind, containment_area,
			title, status, owner_platform_admin_id, outcome_summary,
			evidence_references, actor_platform_admin_id, idempotency_key,
			request_fingerprint, occurred_at, created_at
		) VALUES (
			$1, $2, 2, 'containment', $3, $4, 'completed', $5,
			'Purportedly completed.', ARRAY[NULL]::text[], $5,
			'migration-null-evidence', digest('migration-null-evidence', 'sha256'), $6, $6
		)
	`, incidentID, actionID, area, title, platformAdminID, createdAt.Add(time.Hour)); err == nil {
		t.Fatal("completed action with NULL evidence reference succeeded")
	}
	if _, err := connection.Exec(ctx, `
		INSERT INTO breach_action_events (
			breach_incident_id, action_id, revision, action_kind, title, status,
			owner_platform_admin_id, due_at, evidence_references,
			actor_platform_admin_id, idempotency_key, request_fingerprint,
			occurred_at, created_at
		) VALUES (
			$1, gen_random_uuid(), 1, 'post_incident', 'Past-due corrective work',
			'pending', $2, $3, '{}', $2, 'migration-past-due',
			digest('migration-past-due', 'sha256'), $3, $3
		)
	`, incidentID, platformAdminID, createdAt); err == nil {
		t.Fatal("post-incident action with non-future initial due date succeeded")
	}
	if _, err := connection.Exec(ctx,
		`UPDATE breach_action_events SET title = 'rewritten' WHERE id = $1`, eventID,
	); err == nil {
		t.Fatal("UPDATE breach_action_events succeeded")
	}

	if err := migrator.Migrate(100); err != nil {
		t.Fatalf("roll migration 101 back: %v", err)
	}
	var tableExists bool
	if err := connection.QueryRow(ctx,
		`SELECT to_regclass('public.breach_action_events') IS NOT NULL`,
	).Scan(&tableExists); err != nil {
		t.Fatalf("check rolled-back action table: %v", err)
	}
	if tableExists {
		t.Fatal("breach_action_events still exists after rollback")
	}
	if err := migrator.Up(); err != nil {
		t.Fatalf("reapply migration 101: %v", err)
	}
	assertMigratedChecklist("second up")
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
