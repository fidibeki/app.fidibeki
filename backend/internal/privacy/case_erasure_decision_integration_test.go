package privacy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestErasureCaseTerminalDecisionRequiresExecutionEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 21, 0, 0, 0, time.UTC)
	tenantID, actorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "erasure-decision", "Erasure decision feedback",
	)
	store := Store{Database: database}

	partialCase := createVerifiedReporterCase(
		t, ctx, database, store, tenantID, actorMemberID,
		"erasure-partial", RightErasure, now,
	)
	if _, err := store.TransitionCase(
		ctx, database, tenantID, partialCase.ID, actorMemberID,
		"complete-before-execution", TransitionCaseInput{Status: CaseCompleted},
		now.Add(time.Minute),
	); !errors.Is(err, ErrCaseTransition) {
		t.Fatalf("erasure completion before execution error = %v", err)
	}
	partialExecution, _, err := store.StartCaseExecution(
		ctx, database, tenantID, partialCase.ID, actorMemberID,
		"start-partial-erasure", now.Add(2*time.Minute),
	)
	if err != nil || partialExecution.RequestID == nil || partialExecution.JobID == nil {
		t.Fatalf("start partial erasure = %#v error=%v", partialExecution, err)
	}
	completeErasurePrimitive(
		t, ctx, database, tenantID, *partialExecution.RequestID,
		*partialExecution.JobID, now.Add(3*time.Minute),
	)
	var feedbackID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM feedbacks WHERE tenant_id = $1 LIMIT 1
	`, tenantID).Scan(&feedbackID); err != nil {
		t.Fatalf("read erasure decision feedback: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO reporter_privacy_review_items (
			tenant_id, reporter_request_id, privacy_job_id, resource_type,
			resource_id, fields, status, decision_reason,
			decided_by_member_id, decided_at
		) VALUES (
			$1, $2, $3, 'feedback', $4, ARRAY['description'],
			'retained_exception', 'Required for a documented contractual dispute.',
			$5, $6
		)
	`, tenantID, *partialExecution.RequestID, *partialExecution.JobID,
		feedbackID, actorMemberID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("record retained erasure exception: %v", err)
	}

	loaded, err := store.GetCase(ctx, database, tenantID, partialCase.ID)
	if err != nil || loaded.Execution == nil || loaded.Execution.ErasureReview == nil ||
		loaded.Execution.ErasureReview.RetainedException != 1 ||
		len(loaded.Execution.ErasureReview.Items) != 1 ||
		loaded.Execution.ErasureReview.Items[0].DecisionReason == nil {
		t.Fatalf("erasure exception evidence = %#v error=%v", loaded.Execution, err)
	}
	if _, err := store.TransitionCase(
		ctx, database, tenantID, partialCase.ID, actorMemberID,
		"complete-with-exception", TransitionCaseInput{Status: CaseCompleted},
		now.Add(4*time.Minute),
	); !errors.Is(err, ErrCaseTransition) {
		t.Fatalf("erasure completion with retained exception error = %v", err)
	}
	if _, err := store.TransitionCase(
		ctx, database, tenantID, partialCase.ID, actorMemberID,
		"partial-without-legal-evidence",
		TransitionCaseInput{
			Status:          CasePartiallyFulfilled,
			DecisionSummary: "Automated erasure completed with one retained item.",
		},
		now.Add(4*time.Minute),
	); !errors.Is(err, ErrCaseReasonRequired) {
		t.Fatalf("partial erasure without legal evidence error = %v", err)
	}
	partiallyFulfilled, err := store.TransitionCase(
		ctx, database, tenantID, partialCase.ID, actorMemberID,
		"partial-with-evidence",
		TransitionCaseInput{
			Status:           CasePartiallyFulfilled,
			DecisionSummary:  "Automated erasure completed with one retained item.",
			LegalReviewNotes: "The operator reviewed the documented exception; no legal outcome was inferred.",
			ExceptionReason:  "One feedback description remains for the recorded contractual dispute.",
		},
		now.Add(5*time.Minute),
	)
	if err != nil || partiallyFulfilled.Status != CasePartiallyFulfilled ||
		partiallyFulfilled.ExceptionReason == nil ||
		partiallyFulfilled.LegalReviewNotes == nil {
		t.Fatalf("partial erasure decision = %#v error=%v", partiallyFulfilled, err)
	}

	completedCase := createVerifiedReporterCase(
		t, ctx, database, store, tenantID, actorMemberID,
		"erasure-complete", RightErasure, now.Add(6*time.Minute),
	)
	completedExecution, _, err := store.StartCaseExecution(
		ctx, database, tenantID, completedCase.ID, actorMemberID,
		"start-complete-erasure", now.Add(7*time.Minute),
	)
	if err != nil || completedExecution.RequestID == nil || completedExecution.JobID == nil {
		t.Fatalf("start complete erasure = %#v error=%v", completedExecution, err)
	}
	completeErasurePrimitive(
		t, ctx, database, tenantID, *completedExecution.RequestID,
		*completedExecution.JobID, now.Add(8*time.Minute),
	)
	completed, err := store.TransitionCase(
		ctx, database, tenantID, completedCase.ID, actorMemberID,
		"complete-with-evidence",
		TransitionCaseInput{
			Status:          CaseCompleted,
			DecisionSummary: "The assessed erasure completed without retained exceptions.",
		},
		now.Add(9*time.Minute),
	)
	if err != nil || completed.Status != CaseCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed erasure decision = %#v error=%v", completed, err)
	}
}

func TestErasureCaseRefusalRequiresPreExecutionLegalEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 22, 0, 0, 0, time.UTC)
	tenantID, actorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "erasure-refusal", "Erasure refusal feedback",
	)
	store := Store{Database: database}
	privacyCase := createVerifiedReporterCase(
		t, ctx, database, store, tenantID, actorMemberID,
		"erasure-refusal", RightErasure, now,
	)
	if _, err := store.TransitionCase(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"refuse-without-exception",
		TransitionCaseInput{
			Status:           CaseRefused,
			DecisionSummary:  "The request was refused after review.",
			LegalReviewNotes: "Operator-entered review record.",
		},
		now.Add(time.Minute),
	); !errors.Is(err, ErrCaseReasonRequired) {
		t.Fatalf("erasure refusal without exception error = %v", err)
	}
	refused, err := store.TransitionCase(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"refuse-with-evidence",
		TransitionCaseInput{
			Status:           CaseRefused,
			DecisionSummary:  "The request was refused before any erasure operation started.",
			LegalReviewNotes: "Operator-entered review record; the product made no legal conclusion.",
			ExceptionReason:  "The documented preservation obligation covers the entire assessed scope.",
		},
		now.Add(2*time.Minute),
	)
	if err != nil || refused.Status != CaseRefused || refused.CompletedAt == nil {
		t.Fatalf("erasure refusal = %#v error=%v", refused, err)
	}
}

func createVerifiedReporterCase(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	store Store,
	tenantID string,
	actorMemberID string,
	idempotencyKey string,
	right RightType,
	now time.Time,
) PrivacyCase {
	t.Helper()
	privacyCase, token, _, err := store.CreateCase(
		ctx, database, tenantID, actorMemberID, idempotencyKey,
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: right,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   "reporter@example.test", OwnerMemberID: actorMemberID,
		},
		now,
	)
	if err != nil {
		t.Fatalf("create verified reporter case: %v", err)
	}
	tokenHash, ok := parseReporterRequestToken(token)
	if !ok {
		t.Fatalf("parse reporter case token: %q", token)
	}
	if _, err := store.VerifyCase(
		ctx, database, tokenHash, now.Add(time.Minute),
	); err != nil {
		t.Fatalf("verify reporter case: %v", err)
	}
	return privacyCase
}

func completeErasurePrimitive(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	tenantID string,
	requestID string,
	jobID string,
	now time.Time,
) {
	t.Helper()
	if _, err := database.Exec(ctx, `
		UPDATE privacy_jobs
		SET status = 'completed', started_at = $4::timestamptz - interval '1 minute',
		    heartbeat_at = $4, completed_at = $4,
		    expires_at = $4::timestamptz + interval '30 days',
		    object_key = NULL, object_size_bytes = 0
		WHERE tenant_id = $1 AND reporter_request_id = $2 AND id = $3
	`, tenantID, requestID, jobID, now); err != nil {
		t.Fatalf("complete erasure privacy job: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE reporter_privacy_requests
		SET status = 'completed', reporter_email = NULL, completed_at = $3,
		    record_count = 1, manual_review_count = 0
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, requestID, now); err != nil {
		t.Fatalf("complete erasure reporter request: %v", err)
	}
}
