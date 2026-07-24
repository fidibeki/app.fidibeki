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
