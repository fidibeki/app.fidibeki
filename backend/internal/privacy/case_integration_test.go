package privacy

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestPrivacyCaseVerificationRestrictionAndLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	tenantID, memberID := seedPrivacyWorkspace(
		t, ctx, database, "case-rights", "Restricted reporter feedback",
	)
	store := Store{Database: database}

	created, token, shouldEmail, err := store.CreateCase(
		ctx, database, tenantID, memberID, "case-create-1",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightRestriction,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   " Reporter@Example.Test ",
			OwnerMemberID:  memberID,
			Scope:          map[string]any{"source": "email"},
		}, now,
	)
	if err != nil || !shouldEmail || token == "" ||
		created.Status != CasePendingVerification ||
		created.SubjectEmail == nil || *created.SubjectEmail != "reporter@example.test" {
		t.Fatalf("CreateCase() = %#v token=%q email=%t error=%v",
			created, token, shouldEmail, err)
	}
	tokenHash, ok := parseReporterRequestToken(token)
	if !ok {
		t.Fatalf("case verification token is invalid: %q", token)
	}
	var storedHash, storedDigest []byte
	if err := database.QueryRow(ctx, `
		SELECT verification_token_hash, subject_email_digest
		FROM privacy_cases WHERE tenant_id = $1 AND id = $2
	`, tenantID, created.ID).Scan(&storedHash, &storedDigest); err != nil {
		t.Fatalf("read privacy case evidence: %v", err)
	}
	wantDigest := reporterEmailDigest("reporter@example.test")
	if !bytes.Equal(storedHash, tokenHash[:]) || !bytes.Equal(storedDigest, wantDigest[:]) ||
		bytes.Contains(storedHash, []byte(token)) {
		t.Fatal("privacy case did not store the expected token/email hashes")
	}
	var restrictionCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM privacy_processing_restrictions
		WHERE tenant_id = $1 AND privacy_case_id = $2
	`, tenantID, created.ID).Scan(&restrictionCount); err != nil || restrictionCount != 0 {
		t.Fatalf("unverified restriction count = %d, error=%v", restrictionCount, err)
	}

	duplicate, duplicateToken, duplicateEmail, err := store.CreateCase(
		ctx, database, tenantID, memberID, "case-create-1",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightRestriction,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   "reporter@example.test", OwnerMemberID: memberID,
			Scope: map[string]any{"source": "email"},
		}, now.Add(time.Minute),
	)
	if err != nil || duplicate.ID != created.ID || duplicateToken != "" || duplicateEmail {
		t.Fatalf("idempotent CreateCase() = %#v token=%q email=%t error=%v",
			duplicate, duplicateToken, duplicateEmail, err)
	}
	_, _, _, err = store.CreateCase(
		ctx, database, tenantID, memberID, "case-create-1",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightErasure,
			ControllerRole: RoleController, SubjectEmail: "reporter@example.test",
		}, now,
	)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting CreateCase() error = %v", err)
	}

	view, err := store.VerifyCase(ctx, database, tokenHash, now.Add(time.Hour))
	if err != nil || view.Status != CaseInProgress || view.PublicID != created.PublicID {
		t.Fatalf("VerifyCase() = %#v error=%v", view, err)
	}
	if _, err := store.VerifyCase(ctx, database, tokenHash, now.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reused verification token error = %v", err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM privacy_processing_restrictions
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND active
		  AND purposes @> ARRAY['ai', 'integration', 'reporter_email', 'replay',
		                        'analytics', 'background_processing']::text[]
	`, tenantID, created.ID).Scan(&restrictionCount); err != nil || restrictionCount != 1 {
		t.Fatalf("active restriction count = %d, error=%v", restrictionCount, err)
	}

	var feedbackID, issueID, projectID string
	if err := database.QueryRow(ctx, `
		SELECT feedback.id, feedback.issue_id, feedback.project_id
		FROM feedbacks AS feedback WHERE feedback.tenant_id = $1
	`, tenantID).Scan(&feedbackID, &issueID, &projectID); err != nil {
		t.Fatalf("read restricted fixture: %v", err)
	}
	var sessionID string
	if err := database.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, feedback_id, session_id, started_at,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, $3, gen_random_uuid(), $4, '2.0.0', 300, 1000)
		RETURNING id
	`, tenantID, projectID, feedbackID, now).Scan(&sessionID); err != nil {
		t.Fatalf("create restricted replay session: %v", err)
	}
	for name, check := range map[string]func() error{
		"feedback AI": func() error {
			return EnsureFeedbackProcessingAllowed(
				ctx, database, tenantID, feedbackID, PurposeAI,
			)
		},
		"issue integration": func() error {
			return EnsureIssueProcessingAllowed(
				ctx, database, tenantID, issueID, PurposeIntegration,
			)
		},
		"reporter email": func() error {
			return EnsureEmailProcessingAllowed(
				ctx, database, tenantID, "reporter@example.test", PurposeReporterEmail,
			)
		},
		"session replay": func() error {
			return EnsureSessionProcessingAllowed(
				ctx, database, tenantID, sessionID, PurposeReplay,
			)
		},
	} {
		if err := check(); !errors.Is(err, ErrProcessingRestricted) {
			t.Errorf("%s restriction error = %v", name, err)
		}
	}
	if err := EnsureEmailProcessingAllowed(
		ctx, database, tenantID, "different@example.test", PurposeReporterEmail,
	); err != nil {
		t.Errorf("unrelated reporter restriction error = %v", err)
	}
	deactivated, err := store.SetCaseRestriction(
		ctx, database, tenantID, created.ID, memberID, "restriction-off-1", false,
		"Restriction lifted after an explicit reviewed decision.", now.Add(90*time.Minute),
	)
	if err != nil || deactivated.Active || deactivated.DeactivatedAt == nil {
		t.Fatalf("deactivate restriction = %#v error=%v", deactivated, err)
	}
	if err := EnsureFeedbackProcessingAllowed(
		ctx, database, tenantID, feedbackID, PurposeAI,
	); err != nil {
		t.Fatalf("processing remained restricted after explicit deactivation: %v", err)
	}
	reactivated, err := store.SetCaseRestriction(
		ctx, database, tenantID, created.ID, memberID, "restriction-on-1", true,
		"Restriction restored while the request is reassessed.", now.Add(100*time.Minute),
	)
	if err != nil || !reactivated.Active || reactivated.DeactivatedAt != nil {
		t.Fatalf("reactivate restriction = %#v error=%v", reactivated, err)
	}
	listed, err := store.ListCases(ctx, database, tenantID, 100)
	if err != nil {
		t.Fatalf("list privacy cases with restrictions: %v", err)
	}
	var listedCase *PrivacyCase
	for index := range listed {
		if listed[index].ID == created.ID {
			listedCase = &listed[index]
			break
		}
	}
	if listedCase == nil || listedCase.Restriction == nil ||
		!listedCase.Restriction.Active || len(listedCase.Restriction.Purposes) == 0 {
		t.Fatalf("listed privacy case restriction = %#v", listedCase)
	}

	paused, err := store.TransitionCase(
		ctx, database, tenantID, created.ID, memberID, "pause-1",
		TransitionCaseInput{Status: CasePaused}, now.Add(2*time.Hour),
	)
	if err != nil || paused.Status != CasePaused || paused.PauseStartedAt == nil {
		t.Fatalf("pause case = %#v error=%v", paused, err)
	}
	resumed, err := store.TransitionCase(
		ctx, database, tenantID, created.ID, memberID, "resume-1",
		TransitionCaseInput{Status: CaseInProgress}, now.Add(3*time.Hour),
	)
	if err != nil || resumed.Status != CaseInProgress || resumed.PausedSeconds != 3600 {
		t.Fatalf("resume case = %#v error=%v", resumed, err)
	}
	if _, err := store.TransitionCase(
		ctx, database, tenantID, created.ID, memberID, "refuse-1",
		TransitionCaseInput{Status: CaseRefused}, now.Add(4*time.Hour),
	); !errors.Is(err, ErrCaseReasonRequired) {
		t.Fatalf("reasonless refusal error = %v", err)
	}
	refused, err := store.TransitionCase(
		ctx, database, tenantID, created.ID, memberID, "refuse-2",
		TransitionCaseInput{
			Status:           CaseRefused,
			DecisionSummary:  "Request refused after documented legal review.",
			LegalReviewNotes: "Operator-entered review outcome; not an automatic legal conclusion.",
		}, now.Add(4*time.Hour),
	)
	if err != nil || refused.Status != CaseRefused || refused.CompletedAt == nil {
		t.Fatalf("refuse case = %#v error=%v", refused, err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE privacy_case_events SET event_type = 'tampered'
		WHERE tenant_id = $1 AND privacy_case_id = $2
	`, tenantID, created.ID); err == nil {
		t.Fatal("append-only privacy case event accepted an update")
	}
	events, err := store.ListCaseEvents(ctx, database, tenantID, created.ID)
	if err != nil || len(events) != 7 {
		t.Fatalf("ListCaseEvents() = %#v error=%v", events, err)
	}
}

func TestPrivacyCaseMemberVerificationAndTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tenantID, memberID := seedPrivacyWorkspace(t, ctx, database, "case-member", "Member case")
	otherTenantID, _ := seedPrivacyWorkspace(t, ctx, database, "case-other", "Other case")
	store := Store{Database: database}

	created, token, shouldEmail, err := store.CreateCase(
		ctx, database, tenantID, memberID, "member-case-1",
		CreateCaseInput{
			SubjectType: SubjectMember, RightType: RightConsentWithdrawal,
			ControllerRole: RoleController, SubjectMemberID: memberID,
		}, now,
	)
	if err != nil || token != "" || shouldEmail || created.Status != CaseInProgress ||
		created.VerifiedAt == nil || created.VerificationMethod == nil ||
		*created.VerificationMethod != "workspace_membership" ||
		created.Restriction == nil || !created.Restriction.Active {
		t.Fatalf("member CreateCase() = %#v token=%q email=%t error=%v",
			created, token, shouldEmail, err)
	}
	if _, err := store.GetCase(ctx, database, otherTenantID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant GetCase() error = %v", err)
	}
	dashboard, err := store.CaseDashboard(ctx, database, tenantID, now.Add(25*24*time.Hour))
	if err != nil || dashboard.DueSoon != 1 || dashboard.Overdue != 0 {
		t.Fatalf("CaseDashboard(due soon) = %#v error=%v", dashboard, err)
	}
	dashboard, err = store.CaseDashboard(ctx, database, tenantID, now.Add(31*24*time.Hour))
	if err != nil || dashboard.Overdue != 1 {
		t.Fatalf("CaseDashboard(overdue) = %#v error=%v", dashboard, err)
	}
}

func TestPrivacyCaseReporterExecutionRespectsLegalHolds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 14, 0, 0, 0, time.UTC)
	tenantID, memberID := seedPrivacyWorkspace(
		t, ctx, database, "case-execution", "Reporter erasure fixture",
	)
	otherTenantID, otherMemberID := seedPrivacyWorkspace(
		t, ctx, database, "case-execution-other", "Other tenant fixture",
	)
	store := Store{Database: database}

	erasureCase, token, _, err := store.CreateCase(
		ctx, database, tenantID, memberID, "erasure-case",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightErasure,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   "reporter@example.test",
		}, now,
	)
	if err != nil {
		t.Fatalf("create erasure case: %v", err)
	}
	tokenHash, ok := parseReporterRequestToken(token)
	if !ok {
		t.Fatal("parse erasure verification token")
	}
	if _, err := store.VerifyCase(ctx, database, tokenHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("verify erasure case: %v", err)
	}

	hold, err := store.PlaceLegalHold(
		ctx, database, tenantID, erasureCase.ID, memberID, "hold-1",
		"Preserve records while the contractual dispute is reviewed.", "CASE-2026-17",
		now.Add(2*time.Minute),
	)
	if err != nil || !hold.Active || hold.LegalReference == nil ||
		*hold.LegalReference != "CASE-2026-17" {
		t.Fatalf("PlaceLegalHold() = %#v error=%v", hold, err)
	}
	duplicateHold, err := store.PlaceLegalHold(
		ctx, database, tenantID, erasureCase.ID, memberID, "hold-1",
		"Preserve records while the contractual dispute is reviewed.", "CASE-2026-17",
		now.Add(3*time.Minute),
	)
	if err != nil || duplicateHold.ID != hold.ID {
		t.Fatalf("idempotent PlaceLegalHold() = %#v error=%v", duplicateHold, err)
	}
	if _, err := store.PlaceLegalHold(
		ctx, database, tenantID, erasureCase.ID, memberID, "hold-1",
		"Different reason", "CASE-2026-17", now.Add(3*time.Minute),
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting PlaceLegalHold() error = %v", err)
	}
	if _, _, err := store.StartCaseExecution(
		ctx, database, tenantID, erasureCase.ID, memberID, "execute-1",
		now.Add(4*time.Minute),
	); !errors.Is(err, ErrLegalHoldActive) {
		t.Fatalf("erasure with active hold error = %v", err)
	}
	if _, err := store.ReleaseLegalHold(
		ctx, database, otherTenantID, erasureCase.ID, hold.ID, otherMemberID,
		"release-cross-tenant", "Must not release.", now.Add(5*time.Minute),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant ReleaseLegalHold() error = %v", err)
	}
	released, err := store.ReleaseLegalHold(
		ctx, database, tenantID, erasureCase.ID, hold.ID, memberID, "release-1",
		"The dispute ended and the preservation duty was reviewed.", now.Add(5*time.Minute),
	)
	if err != nil || released.Active || released.ReleasedAt == nil {
		t.Fatalf("ReleaseLegalHold() = %#v error=%v", released, err)
	}

	execution, created, err := store.StartCaseExecution(
		ctx, database, tenantID, erasureCase.ID, memberID, "execute-1",
		now.Add(6*time.Minute),
	)
	if err != nil || !created || execution.RightType != RightErasure ||
		execution.Primitive != ReporterAnonymize ||
		execution.RequestStatus != ReporterProcessing ||
		execution.JobID == nil || execution.JobStatus == nil ||
		*execution.JobStatus != JobPending {
		t.Fatalf("StartCaseExecution(erasure) = %#v created=%t error=%v",
			execution, created, err)
	}
	replayed, created, err := store.StartCaseExecution(
		ctx, database, tenantID, erasureCase.ID, memberID, "execute-1",
		now.Add(7*time.Minute),
	)
	if err != nil || created || replayed.JobID == nil ||
		*replayed.JobID != *execution.JobID {
		t.Fatalf("idempotent StartCaseExecution() = %#v created=%t error=%v",
			replayed, created, err)
	}
	lateHold, err := store.PlaceLegalHold(
		ctx, database, tenantID, erasureCase.ID, memberID, "hold-after-queue",
		"Preserve records before the queued worker begins.", "", now.Add(7*time.Minute),
	)
	if err != nil {
		t.Fatalf("place hold after queue: %v", err)
	}
	queuedJob, err := store.GetJob(ctx, database, tenantID, *execution.JobID)
	if err != nil {
		t.Fatalf("load queued erasure job: %v", err)
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		t.Fatalf("begin erasure hold check: %v", err)
	}
	if err := ensureCaseErasureAllowed(ctx, tx, queuedJob); !errors.Is(err, ErrLegalHoldActive) {
		_ = tx.Rollback(ctx)
		t.Fatalf("queued worker legal hold error = %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("roll back erasure hold check: %v", err)
	}
	if _, err := store.ReleaseLegalHold(
		ctx, database, tenantID, erasureCase.ID, lateHold.ID, memberID,
		"release-after-queue", "Worker may proceed after review.", now.Add(8*time.Minute),
	); err != nil {
		t.Fatalf("release queued execution hold: %v", err)
	}

	accessCase, accessToken, _, err := store.CreateCase(
		ctx, database, tenantID, memberID, "access-case",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightAccess,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   "reporter@example.test",
		}, now.Add(9*time.Minute),
	)
	if err != nil {
		t.Fatalf("create access case: %v", err)
	}
	accessHash, ok := parseReporterRequestToken(accessToken)
	if !ok {
		t.Fatal("parse access verification token")
	}
	if _, err := store.VerifyCase(ctx, database, accessHash, now.Add(10*time.Minute)); err != nil {
		t.Fatalf("verify access case: %v", err)
	}
	accessExecution, created, err := store.StartCaseExecution(
		ctx, database, tenantID, accessCase.ID, memberID, "access-execute-1",
		now.Add(11*time.Minute),
	)
	if err != nil || !created || accessExecution.RightType != RightAccess ||
		accessExecution.Primitive != ReporterExport {
		t.Fatalf("StartCaseExecution(access) = %#v created=%t error=%v",
			accessExecution, created, err)
	}

	listed, err := store.ListCases(ctx, database, tenantID, 100)
	if err != nil {
		t.Fatalf("ListCases() after execution: %v", err)
	}
	for _, privacyCase := range listed {
		switch privacyCase.ID {
		case erasureCase.ID:
			if privacyCase.Execution == nil || len(privacyCase.LegalHolds) != 2 ||
				privacyCase.LegalHolds[0].Active || privacyCase.LegalHolds[1].Active {
				t.Fatalf("listed erasure case state = %#v", privacyCase)
			}
		case accessCase.ID:
			if privacyCase.Execution == nil ||
				privacyCase.Execution.RightType != RightAccess {
				t.Fatalf("listed access case state = %#v", privacyCase)
			}
		}
	}
}

func TestPrivacyCasePortabilityAssessmentGatesExport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 16, 0, 0, 0, time.UTC)
	tenantID, memberID := seedPrivacyWorkspace(
		t, ctx, database, "case-portability", "Portable reporter feedback",
	)
	otherTenantID, otherMemberID := seedPrivacyWorkspace(
		t, ctx, database, "case-portability-other", "Other reporter feedback",
	)
	store := Store{Database: database}

	portabilityCase, token, _, err := store.CreateCase(
		ctx, database, tenantID, memberID, "portability-case",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightPortabilityAssessment,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   "reporter@example.test",
		}, now,
	)
	if err != nil {
		t.Fatalf("create portability case: %v", err)
	}
	tokenHash, ok := parseReporterRequestToken(token)
	if !ok {
		t.Fatal("parse portability verification token")
	}
	if _, err := store.VerifyCase(ctx, database, tokenHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("verify portability case: %v", err)
	}
	if _, _, err := store.StartCaseExecution(
		ctx, database, tenantID, portabilityCase.ID, memberID,
		"execute-before-assessment", now.Add(2*time.Minute),
	); !errors.Is(err, ErrCaseExecutionUnsupported) {
		t.Fatalf("execution before portability approval error = %v", err)
	}
	if _, _, err := store.RecordPortabilityAssessment(
		ctx, database, tenantID, portabilityCase.ID, memberID, "assessment-invalid",
		RecordPortabilityAssessmentInput{
			Decision: PortabilityApproved, ExportFormat: portabilityExportFormat,
			Reason: "No categories were approved.",
		}, now.Add(3*time.Minute),
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("categoryless portability approval error = %v", err)
	}

	input := RecordPortabilityAssessmentInput{
		Decision: PortabilityPartiallyApproved,
		DataCategories: []PortabilityDataCategory{
			PortabilityReplay, PortabilityFeedback, PortabilityReplay,
		},
		ExportFormat: portabilityExportFormat,
		Reason:       "Reporter-provided feedback and linked replay data are portable.",
	}
	assessment, created, err := store.RecordPortabilityAssessment(
		ctx, database, tenantID, portabilityCase.ID, memberID, "assessment-approved",
		input, now.Add(4*time.Minute),
	)
	if err != nil || !created || assessment.Decision != PortabilityPartiallyApproved ||
		len(assessment.DataCategories) != 2 ||
		assessment.DataCategories[0] != PortabilityFeedback ||
		assessment.DataCategories[1] != PortabilityReplay ||
		assessment.ExportFormat == nil || *assessment.ExportFormat != portabilityExportFormat {
		t.Fatalf("RecordPortabilityAssessment() = %#v created=%t error=%v",
			assessment, created, err)
	}
	replayed, created, err := store.RecordPortabilityAssessment(
		ctx, database, tenantID, portabilityCase.ID, memberID, "assessment-approved",
		input, now.Add(5*time.Minute),
	)
	if err != nil || created || replayed.ID != assessment.ID {
		t.Fatalf("idempotent portability assessment = %#v created=%t error=%v",
			replayed, created, err)
	}
	conflicting := input
	conflicting.Reason = "A conflicting reason must not reuse the key."
	if _, _, err := store.RecordPortabilityAssessment(
		ctx, database, tenantID, portabilityCase.ID, memberID, "assessment-approved",
		conflicting, now.Add(5*time.Minute),
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting portability assessment error = %v", err)
	}
	if _, _, err := store.RecordPortabilityAssessment(
		ctx, database, otherTenantID, portabilityCase.ID, otherMemberID,
		"assessment-cross-tenant", input, now.Add(5*time.Minute),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant portability assessment error = %v", err)
	}

	execution, created, err := store.StartCaseExecution(
		ctx, database, tenantID, portabilityCase.ID, memberID,
		"execute-after-assessment", now.Add(6*time.Minute),
	)
	if err != nil || !created || execution.RightType != RightPortabilityAssessment ||
		execution.Primitive != ReporterExport || execution.JobID == nil {
		t.Fatalf("StartCaseExecution(portability) = %#v created=%t error=%v",
			execution, created, err)
	}
	job, err := store.GetJob(ctx, database, tenantID, *execution.JobID)
	if err != nil {
		t.Fatalf("load portability export job: %v", err)
	}
	exportStorage := &privacyStorageCapture{}
	exporter := Exporter{
		Database: database, Store: store, Storage: exportStorage,
		Now: func() time.Time { return now.Add(6 * time.Minute) },
	}
	if err := exporter.Handler()(ctx, reporterJobTask(job)); err != nil {
		t.Fatalf("run portability export: %v", err)
	}
	files := readPrivacyArchive(t, exportStorage.body)
	for _, filename := range []string{
		"feedback.jsonl", "replays.jsonl", "replay-consents.jsonl",
		"request.json", "manifest.json",
	} {
		if _, exists := files[filename]; !exists {
			t.Errorf("portable archive is missing approved file %q", filename)
		}
	}
	for _, filename := range []string{
		"issues.jsonl", "attachments.jsonl", "ai-processing-runs.jsonl",
		"delivery-logs.jsonl", "widget-submission-events.jsonl",
		"feedback-consents.jsonl", "privacy-request-history.jsonl",
	} {
		if _, exists := files[filename]; exists {
			t.Errorf("portable archive included unapproved file %q", filename)
		}
	}
	if !bytes.Contains(files["manifest.json"], []byte(`"partially_approved"`)) ||
		!bytes.Contains(files["manifest.json"], []byte(`"feedback"`)) ||
		!bytes.Contains(files["manifest.json"], []byte(`"replay"`)) {
		t.Errorf("portable archive manifest = %s", files["manifest.json"])
	}
	if _, _, err := store.RecordPortabilityAssessment(
		ctx, database, tenantID, portabilityCase.ID, memberID,
		"assessment-after-execution", input, now.Add(7*time.Minute),
	); !errors.Is(err, ErrCaseExecutionUnsupported) {
		t.Fatalf("assessment after export started error = %v", err)
	}

	loaded, err := store.GetCase(ctx, database, tenantID, portabilityCase.ID)
	if err != nil || loaded.PortabilityAssessment == nil ||
		loaded.PortabilityAssessment.ID != assessment.ID ||
		loaded.Execution == nil || loaded.Execution.RightType != RightPortabilityAssessment {
		t.Fatalf("GetCase(portability) = %#v error=%v", loaded, err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE privacy_case_portability_assessments SET reason = 'tampered'
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, assessment.ID); err == nil {
		t.Fatal("append-only portability assessment accepted an update")
	}
}
