package privacy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

type privacyHTTPContractStore struct {
	HTTPStore
	now               time.Time
	caseID            string
	caseJobID         string
	workspaceJobID    string
	failure           error
	duplicate         bool
	jobUnavailable    bool
	noExecution       bool
	downloadError     error
	queueCaseEmail    bool
	queueRequestEmail bool
	missingEmail      bool
}

func (store privacyHTTPContractStore) workspaceJob() Job {
	objectKey := "privacy/workspace.zip"
	expiresAt := store.now.Add(time.Hour)
	status := JobCompleted
	if store.jobUnavailable {
		status = JobPending
	}
	return Job{
		ID: store.workspaceJobID, Kind: JobWorkspaceExport, Status: status,
		ObjectKey: &objectKey, ExpiresAt: &expiresAt,
	}
}

func (store privacyHTTPContractStore) memberJob() Job {
	objectKey := "privacy/member.zip"
	expiresAt := store.now.Add(time.Hour)
	status := JobCompleted
	if store.jobUnavailable {
		status = JobPending
	}
	return Job{
		ID: store.caseJobID, Kind: JobMemberExport, Status: status,
		ObjectKey: &objectKey, ExpiresAt: &expiresAt,
	}
}

func (store privacyHTTPContractStore) privacyCase() PrivacyCase {
	privacyCase := PrivacyCase{
		ID: store.caseID, PublicID: "PRIV-HTTP-1", SubjectType: SubjectMember,
		RightType: RightAccess, Status: CaseInProgress,
		Execution: &CaseExecution{JobID: &store.caseJobID},
	}
	if store.noExecution {
		privacyCase.Execution = nil
	}
	return privacyCase
}

func (store privacyHTTPContractStore) CreateWorkspaceExport(
	context.Context, db.Querier, string, string, time.Time,
) (Job, bool, error) {
	if store.failure != nil {
		return Job{}, false, store.failure
	}
	return store.workspaceJob(), !store.duplicate, nil
}

func (store privacyHTTPContractStore) ListJobs(
	context.Context, db.Querier, string, int,
) ([]Job, error) {
	if store.failure != nil {
		return nil, store.failure
	}
	return []Job{store.workspaceJob()}, nil
}

func (store privacyHTTPContractStore) GetJob(
	_ context.Context, _ db.Querier, _, jobID string,
) (Job, error) {
	if store.failure != nil {
		return Job{}, store.failure
	}
	if jobID == store.caseJobID {
		return store.memberJob(), nil
	}
	return store.workspaceJob(), nil
}

func (store privacyHTTPContractStore) RetryJob(
	context.Context, db.Querier, string, string, string, time.Time,
) (Job, error) {
	if store.failure != nil {
		return Job{}, store.failure
	}
	return store.workspaceJob(), nil
}

func (store privacyHTTPContractStore) RecordDownload(
	context.Context, db.Querier, string, string, string, time.Time,
) error {
	if store.failure != nil {
		return store.failure
	}
	if store.downloadError != nil {
		return store.downloadError
	}
	return nil
}

func (store privacyHTTPContractStore) CreateReporterRequest(
	context.Context, db.Querier, string, string, string, ReporterAction, time.Time,
) (ReporterRequest, string, bool, error) {
	if store.failure != nil {
		return ReporterRequest{}, "", false, store.failure
	}
	request := ReporterRequest{
		ID:     "10000000-0000-4000-8000-000000000002",
		Action: ReporterExport,
	}
	if !store.missingEmail {
		email := "reporter@example.test"
		request.ReporterEmail = &email
	}
	return request, strings.Repeat("r", 43), store.queueRequestEmail, nil
}

func (store privacyHTTPContractStore) ListReporterRequests(
	context.Context, db.Querier, string, int,
) ([]ReporterRequest, error) {
	if store.failure != nil {
		return nil, store.failure
	}
	return []ReporterRequest{{ID: "10000000-0000-4000-8000-000000000002"}}, nil
}

func (store privacyHTTPContractStore) ListReviewItems(
	context.Context, db.Querier, string, string,
) ([]ReviewItem, error) {
	if store.failure != nil {
		return nil, store.failure
	}
	return []ReviewItem{{ID: "10000000-0000-4000-8000-000000000003"}}, nil
}

func (store privacyHTTPContractStore) DecideReviewItem(
	context.Context, db.Querier, string, string, string, string, ReviewDecision, time.Time,
) (ReviewItem, error) {
	if store.failure != nil {
		return ReviewItem{}, store.failure
	}
	return ReviewItem{ID: "10000000-0000-4000-8000-000000000003"}, nil
}

func (store privacyHTTPContractStore) FinalizeReporterAnonymization(
	context.Context, db.Querier, string, string, time.Time,
) (bool, int, error) {
	if store.failure != nil {
		return false, 0, store.failure
	}
	return true, 0, nil
}

func (store privacyHTTPContractStore) CreateCase(
	context.Context, db.Querier, string, string, string, CreateCaseInput, time.Time,
) (PrivacyCase, string, bool, error) {
	if store.failure != nil {
		return PrivacyCase{}, "", false, store.failure
	}
	privacyCase := store.privacyCase()
	if !store.missingEmail {
		email := "reporter@example.test"
		privacyCase.SubjectEmail = &email
	}
	return privacyCase, strings.Repeat("c", 43), store.queueCaseEmail, nil
}

func (store privacyHTTPContractStore) ListCases(
	context.Context, db.Querier, string, int,
) ([]PrivacyCase, error) {
	if store.failure != nil {
		return nil, store.failure
	}
	return []PrivacyCase{store.privacyCase()}, nil
}

func (store privacyHTTPContractStore) GetCase(
	context.Context, db.Querier, string, string,
) (PrivacyCase, error) {
	if store.failure != nil {
		return PrivacyCase{}, store.failure
	}
	return store.privacyCase(), nil
}

func (store privacyHTTPContractStore) ListCaseEvents(
	context.Context, db.Querier, string, string,
) ([]CaseEvent, error) {
	if store.failure != nil {
		return nil, store.failure
	}
	return []CaseEvent{}, nil
}

func (store privacyHTTPContractStore) TransitionCase(
	context.Context, db.Querier, string, string, string, string, TransitionCaseInput, time.Time,
) (PrivacyCase, error) {
	if store.failure != nil {
		return PrivacyCase{}, store.failure
	}
	return store.privacyCase(), nil
}

func (store privacyHTTPContractStore) CaseDashboard(
	context.Context, db.Querier, string, time.Time,
) (CaseDashboard, error) {
	if store.failure != nil {
		return CaseDashboard{}, store.failure
	}
	return CaseDashboard{}, nil
}

func (store privacyHTTPContractStore) SetCaseRestriction(
	context.Context, db.Querier, string, string, string, string, bool, string, time.Time,
) (Restriction, error) {
	if store.failure != nil {
		return Restriction{}, store.failure
	}
	return Restriction{Active: true}, nil
}

func (store privacyHTTPContractStore) PlaceLegalHold(
	context.Context, db.Querier, string, string, string, string, string, string, time.Time,
) (LegalHold, error) {
	if store.failure != nil {
		return LegalHold{}, store.failure
	}
	return LegalHold{ID: "10000000-0000-4000-8000-000000000004"}, nil
}

func (store privacyHTTPContractStore) ReleaseLegalHold(
	context.Context, db.Querier, string, string, string, string, string, string, time.Time,
) (LegalHold, error) {
	if store.failure != nil {
		return LegalHold{}, store.failure
	}
	return LegalHold{ID: "10000000-0000-4000-8000-000000000004"}, nil
}

func (store privacyHTTPContractStore) RecordPortabilityAssessment(
	context.Context, db.Querier, string, string, string, string,
	RecordPortabilityAssessmentInput, time.Time,
) (PortabilityAssessment, bool, error) {
	if store.failure != nil {
		return PortabilityAssessment{}, false, store.failure
	}
	return PortabilityAssessment{}, !store.duplicate, nil
}

func (store privacyHTTPContractStore) ApplyCaseRectification(
	context.Context, db.Querier, string, string, string, string,
	RecordCaseRectificationInput, time.Time,
) (CaseRectification, bool, error) {
	if store.failure != nil {
		return CaseRectification{}, false, store.failure
	}
	return CaseRectification{}, !store.duplicate, nil
}

func (store privacyHTTPContractStore) ListRectifiableFeedbacks(
	context.Context, db.Querier, string, string,
) ([]RectifiableFeedback, error) {
	if store.failure != nil {
		return nil, store.failure
	}
	return []RectifiableFeedback{}, nil
}

func (store privacyHTTPContractStore) ApplyFeedbackRectification(
	context.Context, db.Querier, string, string, string, string,
	RecordFeedbackRectificationInput, time.Time,
) (FeedbackRectification, bool, error) {
	if store.failure != nil {
		return FeedbackRectification{}, false, store.failure
	}
	return FeedbackRectification{}, !store.duplicate, nil
}

func (store privacyHTTPContractStore) RefreshIntegrationActions(
	context.Context, db.Querier, string, string, string, time.Time,
) ([]IntegrationAction, int, int, error) {
	if store.failure != nil {
		return nil, 0, 0, store.failure
	}
	return []IntegrationAction{}, 1, 0, nil
}

func (store privacyHTTPContractStore) CompleteIntegrationAction(
	context.Context, db.Querier, string, string, string, string, string,
	CompleteIntegrationActionInput, time.Time,
) (IntegrationAction, bool, error) {
	if store.failure != nil {
		return IntegrationAction{}, false, store.failure
	}
	return IntegrationAction{}, !store.duplicate, nil
}

func (store privacyHTTPContractStore) CreateRecipientAction(
	context.Context, db.Querier, string, string, string, string,
	CreateRecipientActionInput, time.Time,
) (RecipientAction, bool, error) {
	if store.failure != nil {
		return RecipientAction{}, false, store.failure
	}
	return RecipientAction{}, !store.duplicate, nil
}

func (store privacyHTTPContractStore) RetryRecipientAction(
	context.Context, db.Querier, string, string, string, string, string, time.Time,
) (RecipientAction, bool, error) {
	if store.failure != nil {
		return RecipientAction{}, false, store.failure
	}
	return RecipientAction{}, true, nil
}

func (store privacyHTTPContractStore) CreateCaseCommunication(
	context.Context, db.Querier, string, string, string, string,
	CreateCaseCommunicationInput, time.Time,
) (CaseCommunication, bool, error) {
	if store.failure != nil {
		return CaseCommunication{}, false, store.failure
	}
	return CaseCommunication{}, !store.duplicate, nil
}

func (store privacyHTTPContractStore) RetryCaseCommunication(
	context.Context, db.Querier, string, string, string, string, string, time.Time,
) (CaseCommunication, bool, error) {
	if store.failure != nil {
		return CaseCommunication{}, false, store.failure
	}
	return CaseCommunication{}, true, nil
}

func (store privacyHTTPContractStore) StartCaseExecution(
	context.Context, db.Querier, string, string, string, string, time.Time,
) (CaseExecution, bool, error) {
	if store.failure != nil {
		return CaseExecution{}, false, store.failure
	}
	return CaseExecution{}, !store.duplicate, nil
}

func TestAuthenticatedPrivacyHTTPContract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	tenantID, ownerID := seedPrivacyWorkspace(
		t, ctx, database, "privacy-http", "Privacy HTTP contract",
	)

	jwtManager, err := auth.NewJWTManager(
		"privacy-http-contract-secret-000000", "fidibeki", 10*time.Minute,
	)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	ownerToken, _, err := jwtManager.IssueTenantMember(
		ownerID, tenantID, auth.TenantPermissionStrings(), 0,
	)
	if err != nil {
		t.Fatalf("issue owner token: %v", err)
	}

	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	const (
		caseID              = "10000000-0000-4000-8000-000000000001"
		requestID           = "10000000-0000-4000-8000-000000000002"
		reviewItemID        = "10000000-0000-4000-8000-000000000003"
		holdID              = "10000000-0000-4000-8000-000000000004"
		recipientActionID   = "10000000-0000-4000-8000-000000000005"
		integrationActionID = "10000000-0000-4000-8000-000000000006"
		communicationID     = "10000000-0000-4000-8000-000000000007"
		workspaceJobID      = "10000000-0000-4000-8000-000000000008"
		caseJobID           = "10000000-0000-4000-8000-000000000009"
	)
	contractStore := privacyHTTPContractStore{
		now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
	}
	storage := &privacyStorageCapture{downloadURL: "https://downloads.example.test/privacy.zip"}
	tenantOptions := HTTPOptions{
		Store: contractStore, JWT: jwtManager, Storage: storage,
		PublicBaseURL: "https://api.fidibeki.test", BaseDomain: "fidibeki.test",
		From: "Fidibeki <auth@fidibeki.com>", Now: func() time.Time { return now },
	}
	publicStore := Store{Database: database}
	publicOptions := PublicHTTPOptions{
		Store: publicStore, Begin: db.BeginFromPool(database), Storage: storage,
		Now: func() time.Time { return now },
	}
	resolver := tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
		if slug != "privacy-http" {
			return tenant.Tenant{}, tenant.ErrUnknownTenant
		}
		return tenant.Tenant{
			ID: tenantID, Slug: slug, Name: "Privacy HTTP",
			Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
		}, nil
	})
	handler := dispatchedPrivacyRouters(
		t, resolver, "fidibeki.test", tenantOptions, publicOptions,
	)
	baseURL := "https://privacy-http.fidibeki.test"

	successes := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "retention", method: http.MethodGet, path: "/v1/privacy/retention", status: http.StatusOK},
		{name: "list exports", method: http.MethodGet, path: "/v1/privacy/exports", status: http.StatusOK},
		{name: "create export", method: http.MethodPost, path: "/v1/privacy/exports", status: http.StatusAccepted},
		{name: "retry export", method: http.MethodPost, path: "/v1/privacy/exports/" + workspaceJobID + "/retry", status: http.StatusAccepted},
		{name: "download export", method: http.MethodPost, path: "/v1/privacy/exports/" + workspaceJobID + "/download", status: http.StatusOK},
		{name: "list requests", method: http.MethodGet, path: "/v1/privacy/requests", status: http.StatusOK},
		{name: "create request", method: http.MethodPost, path: "/v1/privacy/requests", body: `{"email":"reporter@example.test","action":"export"}`, status: http.StatusAccepted},
		{name: "list review items", method: http.MethodGet, path: "/v1/privacy/requests/" + requestID + "/review-items", status: http.StatusOK},
		{name: "decide review item", method: http.MethodPost, path: "/v1/privacy/requests/" + requestID + "/review-items/" + reviewItemID + "/decide", body: `{"status":"redacted","reason":"Approved review evidence."}`, status: http.StatusOK},
		{name: "finalize request", method: http.MethodPost, path: "/v1/privacy/requests/" + requestID + "/finalize", status: http.StatusOK},
		{name: "list cases", method: http.MethodGet, path: "/v1/privacy/cases", status: http.StatusOK},
		{name: "case dashboard", method: http.MethodGet, path: "/v1/privacy/cases/dashboard", status: http.StatusOK},
		{name: "get case", method: http.MethodGet, path: "/v1/privacy/cases/" + caseID, status: http.StatusOK},
		{name: "create case", method: http.MethodPost, path: "/v1/privacy/cases", body: `{"subject_type":"member","right_type":"access","controller_role":"controller","subject_member_id":"` + ownerID + `","owner_member_id":"` + ownerID + `","scope":{"source":"support"}}`, status: http.StatusAccepted},
		{name: "transition case", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/transition", body: `{"status":"paused"}`, status: http.StatusOK},
		{name: "set restriction", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/restriction", body: `{"active":true,"reason":"Verified request"}`, status: http.StatusOK},
		{name: "place hold", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/legal-holds", body: `{"reason":"Preserve scoped evidence","legal_reference":"CASE-24"}`, status: http.StatusCreated},
		{name: "release hold", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/legal-holds/" + holdID + "/release", body: `{"reason":"Review completed"}`, status: http.StatusOK},
		{name: "portability assessment", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/portability-assessments", body: `{"decision":"approved","data_categories":["feedback"],"export_format":"json","reason":"Scope verified"}`, status: http.StatusCreated},
		{name: "case rectification", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/rectifications", body: `{"changes":{"name":"Correct name"},"reason":"Verified correction"}`, status: http.StatusCreated},
		{name: "list rectifiable feedbacks", method: http.MethodGet, path: "/v1/privacy/cases/" + caseID + "/rectifiable-feedbacks", status: http.StatusOK},
		{name: "feedback rectification", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/feedback-rectifications", body: `{"feedback_id":"` + caseID + `","description":"Corrected feedback","reason":"Verified correction"}`, status: http.StatusCreated},
		{name: "refresh integration actions", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/integration-actions/refresh", status: http.StatusOK},
		{name: "complete integration action", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/integration-actions/" + integrationActionID + "/complete", body: `{"completion_note":"Completed at destination"}`, status: http.StatusOK},
		{name: "create recipient action", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/recipient-actions", body: `{"action_kind":"rectification","recipient_name":"Processor","recipient_email":"privacy@example.test","instruction_summary":"Correct the scoped record"}`, status: http.StatusCreated},
		{name: "retry recipient action", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/recipient-actions/" + recipientActionID + "/retry", status: http.StatusAccepted},
		{name: "create communication", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/communications", body: `{"communication_kind":"progress_update","subject_line":"Request update","message_body":"The review is in progress."}`, status: http.StatusCreated},
		{name: "retry communication", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/communications/" + communicationID + "/retry", status: http.StatusAccepted},
		{name: "start execution", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/execute", status: http.StatusAccepted},
		{name: "download execution", method: http.MethodPost, path: "/v1/privacy/cases/" + caseID + "/execution/download", status: http.StatusOK},
	}
	for _, test := range successes {
		t.Run(test.name, func(t *testing.T) {
			response := dispatchedRequest(
				handler, test.method, baseURL+test.path, ownerToken, test.body,
			)
			if response.Code != test.status {
				t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), test.status)
			}
		})
	}

	failedOptions := tenantOptions
	failedOptions.Store = privacyHTTPContractStore{
		now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
		failure: errors.New("database unavailable"),
	}
	failedHandler := dispatchedPrivacyRouters(
		t, resolver, "fidibeki.test", failedOptions, publicOptions,
	)
	for _, test := range successes {
		if test.name == "retention" {
			continue
		}
		t.Run("store failure "+test.name, func(t *testing.T) {
			response := dispatchedRequest(
				failedHandler, test.method, baseURL+test.path, ownerToken, test.body,
			)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf(
					"status = %d body=%s, want %d",
					response.Code, response.Body.String(), http.StatusServiceUnavailable,
				)
			}
		})
	}

	duplicateOptions := tenantOptions
	duplicateOptions.Store = privacyHTTPContractStore{
		now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
		duplicate: true,
	}
	duplicateHandler := dispatchedPrivacyRouters(
		t, resolver, "fidibeki.test", duplicateOptions, publicOptions,
	)
	duplicates := []struct {
		name string
		path string
		body string
	}{
		{name: "workspace export", path: "/v1/privacy/exports"},
		{name: "portability assessment", path: "/v1/privacy/cases/" + caseID + "/portability-assessments", body: `{"decision":"approved","data_categories":["feedback"],"export_format":"json","reason":"Scope verified"}`},
		{name: "case rectification", path: "/v1/privacy/cases/" + caseID + "/rectifications", body: `{"changes":{"name":"Correct name"},"reason":"Verified correction"}`},
		{name: "feedback rectification", path: "/v1/privacy/cases/" + caseID + "/feedback-rectifications", body: `{"feedback_id":"` + caseID + `","description":"Corrected feedback","reason":"Verified correction"}`},
		{name: "recipient action", path: "/v1/privacy/cases/" + caseID + "/recipient-actions", body: `{"action_kind":"rectification","recipient_name":"Processor","recipient_email":"privacy@example.test","instruction_summary":"Correct the scoped record"}`},
		{name: "case communication", path: "/v1/privacy/cases/" + caseID + "/communications", body: `{"communication_kind":"progress_update","subject_line":"Request update","message_body":"The review is in progress."}`},
		{name: "case execution", path: "/v1/privacy/cases/" + caseID + "/execute"},
	}
	for _, test := range duplicates {
		t.Run("idempotent "+test.name, func(t *testing.T) {
			response := dispatchedRequest(
				duplicateHandler, http.MethodPost, baseURL+test.path, ownerToken, test.body,
			)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}

	emailCapture := &privacyEmailCapture{}
	emailOptions := tenantOptions
	emailOptions.Store = privacyHTTPContractStore{
		now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
		queueCaseEmail: true, queueRequestEmail: true,
	}
	emailOptions.Email = emailCapture
	emailHandler := dispatchedPrivacyRouters(
		t, resolver, "fidibeki.test", emailOptions, publicOptions,
	)
	emailRequests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "reporter request", path: "/v1/privacy/requests",
			body: `{"email":"reporter@example.test","action":"export"}`,
		},
		{
			name: "rights case", path: "/v1/privacy/cases",
			body: `{"subject_type":"reporter","right_type":"access","controller_role":"customer_controller_assistance","subject_email":"reporter@example.test","owner_member_id":"` + ownerID + `"}`,
		},
	}
	for _, test := range emailRequests {
		response := dispatchedRequest(
			emailHandler, http.MethodPost, baseURL+test.path, ownerToken, test.body,
		)
		if response.Code != http.StatusAccepted {
			t.Fatalf(
				"queue %s email status = %d body=%s",
				test.name, response.Code, response.Body.String(),
			)
		}
	}
	if len(emailCapture.messages) != len(emailRequests) {
		t.Fatalf("queued verification emails = %d, want %d", len(emailCapture.messages), len(emailRequests))
	}

	for _, missing := range []struct {
		name         string
		missingEmail bool
		emailService bool
	}{
		{name: "missing recipient", missingEmail: true, emailService: true},
		{name: "missing service", emailService: false},
	} {
		options := tenantOptions
		options.Store = privacyHTTPContractStore{
			now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
			queueCaseEmail: true, queueRequestEmail: true, missingEmail: missing.missingEmail,
		}
		if missing.emailService {
			options.Email = &privacyEmailCapture{}
		} else {
			options.Email = nil
		}
		unavailableHandler := dispatchedPrivacyRouters(
			t, resolver, "fidibeki.test", options, publicOptions,
		)
		for _, test := range emailRequests {
			response := dispatchedRequest(
				unavailableHandler, http.MethodPost, baseURL+test.path, ownerToken, test.body,
			)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf(
					"%s %s status = %d body=%s",
					missing.name, test.name, response.Code, response.Body.String(),
				)
			}
		}
	}

	downloadCases := []struct {
		name    string
		store   privacyHTTPContractStore
		storage DownloadStorage
		paths   []string
		status  int
	}{
		{
			name: "pending jobs",
			store: privacyHTTPContractStore{
				now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
				jobUnavailable: true,
			},
			storage: storage,
			paths: []string{
				"/v1/privacy/exports/" + workspaceJobID + "/download",
				"/v1/privacy/cases/" + caseID + "/execution/download",
			},
			status: http.StatusConflict,
		},
		{
			name: "execution not started",
			store: privacyHTTPContractStore{
				now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
				noExecution: true,
			},
			storage: storage,
			paths:   []string{"/v1/privacy/cases/" + caseID + "/execution/download"},
			status:  http.StatusConflict,
		},
		{
			name: "storage unavailable",
			store: privacyHTTPContractStore{
				now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
			},
			paths: []string{
				"/v1/privacy/exports/" + workspaceJobID + "/download",
				"/v1/privacy/cases/" + caseID + "/execution/download",
			},
			status: http.StatusServiceUnavailable,
		},
		{
			name: "presign failure",
			store: privacyHTTPContractStore{
				now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
			},
			storage: &privacyStorageCapture{err: errors.New("storage unavailable")},
			paths: []string{
				"/v1/privacy/exports/" + workspaceJobID + "/download",
				"/v1/privacy/cases/" + caseID + "/execution/download",
			},
			status: http.StatusServiceUnavailable,
		},
		{
			name: "audit failure",
			store: privacyHTTPContractStore{
				now: now, caseID: caseID, workspaceJobID: workspaceJobID, caseJobID: caseJobID,
				downloadError: errors.New("audit unavailable"),
			},
			storage: storage,
			paths: []string{
				"/v1/privacy/exports/" + workspaceJobID + "/download",
				"/v1/privacy/cases/" + caseID + "/execution/download",
			},
			status: http.StatusServiceUnavailable,
		},
	}
	for _, test := range downloadCases {
		options := tenantOptions
		options.Store = test.store
		options.Storage = test.storage
		downloadHandler := dispatchedPrivacyRouters(
			t, resolver, "fidibeki.test", options, publicOptions,
		)
		for _, path := range test.paths {
			response := dispatchedRequest(
				downloadHandler, http.MethodPost, baseURL+path, ownerToken, "",
			)
			if response.Code != test.status {
				t.Fatalf(
					"%s %s status = %d body=%s, want %d",
					test.name, path, response.Code, response.Body.String(), test.status,
				)
			}
		}
	}

	invalidRequests := []struct {
		name string
		path string
		body string
	}{
		{name: "case enum", path: "/v1/privacy/cases", body: `{"subject_type":"unknown"}`},
		{name: "case trailing JSON", path: "/v1/privacy/cases", body: `{} {}`},
		{name: "transition enum", path: "/v1/privacy/cases/" + caseID + "/transition", body: `{"status":"unknown"}`},
		{name: "portability decision", path: "/v1/privacy/cases/" + caseID + "/portability-assessments", body: `{"decision":"unknown"}`},
		{name: "portability category", path: "/v1/privacy/cases/" + caseID + "/portability-assessments", body: `{"decision":"approved","data_categories":["unknown"]}`},
		{name: "recipient kind", path: "/v1/privacy/cases/" + caseID + "/recipient-actions", body: `{"action_kind":"unknown"}`},
		{name: "communication kind", path: "/v1/privacy/cases/" + caseID + "/communications", body: `{"communication_kind":"unknown"}`},
		{name: "review status", path: "/v1/privacy/requests/" + requestID + "/review-items/" + reviewItemID + "/decide", body: `{"status":"unknown"}`},
	}
	for _, test := range invalidRequests {
		t.Run("reject "+test.name, func(t *testing.T) {
			response := dispatchedRequest(
				handler, http.MethodPost, baseURL+test.path, ownerToken, test.body,
			)
			if response.Code != http.StatusBadRequest &&
				response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}

	malformedBodies := []string{
		"/v1/privacy/requests",
		"/v1/privacy/requests/" + requestID + "/review-items/" + reviewItemID + "/decide",
		"/v1/privacy/cases",
		"/v1/privacy/cases/" + caseID + "/transition",
		"/v1/privacy/cases/" + caseID + "/restriction",
		"/v1/privacy/cases/" + caseID + "/legal-holds",
		"/v1/privacy/cases/" + caseID + "/legal-holds/" + holdID + "/release",
		"/v1/privacy/cases/" + caseID + "/portability-assessments",
		"/v1/privacy/cases/" + caseID + "/rectifications",
		"/v1/privacy/cases/" + caseID + "/feedback-rectifications",
		"/v1/privacy/cases/" + caseID + "/integration-actions/" + integrationActionID + "/complete",
		"/v1/privacy/cases/" + caseID + "/recipient-actions",
		"/v1/privacy/cases/" + caseID + "/communications",
	}
	for _, path := range malformedBodies {
		response := dispatchedRequest(handler, http.MethodPost, baseURL+path, ownerToken, `{`)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d body=%s", path, response.Code, response.Body.String())
		}
	}

	invalidIDs := []string{
		"/v1/privacy/exports/not-a-uuid/retry",
		"/v1/privacy/requests/not-a-uuid/finalize",
		"/v1/privacy/requests/" + requestID + "/review-items/not-a-uuid/decide",
		"/v1/privacy/cases/not-a-uuid/transition",
		"/v1/privacy/cases/" + caseID + "/legal-holds/not-a-uuid/release",
		"/v1/privacy/cases/" + caseID + "/recipient-actions/not-a-uuid/retry",
		"/v1/privacy/cases/" + caseID + "/integration-actions/not-a-uuid/complete",
		"/v1/privacy/cases/" + caseID + "/communications/not-a-uuid/retry",
	}
	for _, path := range invalidIDs {
		response := dispatchedRequest(handler, http.MethodPost, baseURL+path, ownerToken, `{}`)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d body=%s", path, response.Code, response.Body.String())
		}
	}

	reporterCase, verificationToken, shouldEmail, err := publicStore.CreateCase(
		ctx, database, tenantID, ownerID, "http-public-case",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightAccess,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   "reporter-http@example.test", OwnerMemberID: ownerID,
		},
		now,
	)
	if err != nil || !shouldEmail || reporterCase.ID == "" || verificationToken == "" {
		t.Fatalf(
			"create reporter case = %#v token=%q email=%t error=%v",
			reporterCase, verificationToken, shouldEmail, err,
		)
	}
	verified := dispatchedRequest(
		handler, http.MethodPost,
		baseURL+"/v1/privacy/cases/"+verificationToken+"/verify", "", "",
	)
	if verified.Code != http.StatusOK {
		t.Fatalf("verify reporter case = %d body=%s", verified.Code, verified.Body.String())
	}

	invalidCommands := []struct {
		name string
		call func() error
	}{
		{name: "create case", call: func() error {
			_, _, _, err := publicStore.CreateCase(
				ctx, database, tenantID, ownerID, "",
				CreateCaseInput{}, now,
			)
			return err
		}},
		{name: "transition case", call: func() error {
			_, err := publicStore.TransitionCase(
				ctx, database, tenantID, reporterCase.ID, ownerID, "",
				TransitionCaseInput{}, now,
			)
			return err
		}},
		{name: "set restriction", call: func() error {
			_, err := publicStore.SetCaseRestriction(
				ctx, database, tenantID, reporterCase.ID, ownerID, "",
				true, "", now,
			)
			return err
		}},
		{name: "place legal hold", call: func() error {
			_, err := publicStore.PlaceLegalHold(
				ctx, database, tenantID, reporterCase.ID, ownerID,
				"", "", "", now,
			)
			return err
		}},
		{name: "release legal hold", call: func() error {
			_, err := publicStore.ReleaseLegalHold(
				ctx, database, tenantID, reporterCase.ID, holdID, ownerID,
				"", "", now,
			)
			return err
		}},
		{name: "portability assessment", call: func() error {
			_, _, err := publicStore.RecordPortabilityAssessment(
				ctx, database, tenantID, reporterCase.ID, ownerID, "",
				RecordPortabilityAssessmentInput{}, now,
			)
			return err
		}},
		{name: "case rectification", call: func() error {
			_, _, err := publicStore.ApplyCaseRectification(
				ctx, database, tenantID, reporterCase.ID, ownerID, "",
				RecordCaseRectificationInput{}, now,
			)
			return err
		}},
		{name: "feedback rectification", call: func() error {
			_, _, err := publicStore.ApplyFeedbackRectification(
				ctx, database, tenantID, reporterCase.ID, ownerID, "",
				RecordFeedbackRectificationInput{}, now,
			)
			return err
		}},
		{name: "complete integration action", call: func() error {
			_, _, err := publicStore.CompleteIntegrationAction(
				ctx, database, tenantID, reporterCase.ID, integrationActionID,
				ownerID, "", CompleteIntegrationActionInput{}, now,
			)
			return err
		}},
		{name: "create recipient action", call: func() error {
			_, _, err := publicStore.CreateRecipientAction(
				ctx, database, tenantID, reporterCase.ID, ownerID, "",
				CreateRecipientActionInput{}, now,
			)
			return err
		}},
		{name: "retry recipient action", call: func() error {
			_, _, err := publicStore.RetryRecipientAction(
				ctx, database, tenantID, reporterCase.ID, recipientActionID,
				ownerID, "", now,
			)
			return err
		}},
		{name: "create communication", call: func() error {
			_, _, err := publicStore.CreateCaseCommunication(
				ctx, database, tenantID, reporterCase.ID, ownerID, "",
				CreateCaseCommunicationInput{}, now,
			)
			return err
		}},
		{name: "retry communication", call: func() error {
			_, _, err := publicStore.RetryCaseCommunication(
				ctx, database, tenantID, reporterCase.ID, communicationID,
				ownerID, "", now,
			)
			return err
		}},
		{name: "start execution", call: func() error {
			_, _, err := publicStore.StartCaseExecution(
				ctx, database, tenantID, reporterCase.ID, ownerID, "", now,
			)
			return err
		}},
	}
	for _, test := range invalidCommands {
		t.Run("invalid command "+test.name, func(t *testing.T) {
			if err := test.call(); err == nil {
				t.Fatal("command succeeded")
			}
		})
	}
}

func TestPrivacyHTTPErrorAndVerificationContracts(t *testing.T) {
	handler := &HTTPHandler{
		options: HTTPOptions{},
		logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	errorCases := []struct {
		name   string
		err    error
		status int
	}{
		{name: "not found", err: ErrNotFound, status: http.StatusNotFound},
		{name: "job unavailable", err: ErrJobUnavailable, status: http.StatusConflict},
		{name: "human required", err: ErrHumanRequired, status: http.StatusForbidden},
		{name: "invalid request", err: ErrInvalidRequest, status: http.StatusUnprocessableEntity},
		{name: "review reason", err: ErrReviewReasonMissing, status: http.StatusUnprocessableEntity},
		{name: "review decided", err: ErrReviewItemDecided, status: http.StatusConflict},
		{name: "idempotency conflict", err: ErrIdempotencyConflict, status: http.StatusConflict},
		{name: "case transition", err: ErrCaseTransition, status: http.StatusConflict},
		{name: "case reason", err: ErrCaseReasonRequired, status: http.StatusUnprocessableEntity},
		{name: "legal hold", err: ErrLegalHoldActive, status: http.StatusConflict},
		{name: "execution unsupported", err: ErrCaseExecutionUnsupported, status: http.StatusUnprocessableEntity},
		{name: "recipient retry", err: ErrRecipientActionNotRetryable, status: http.StatusConflict},
		{name: "recipient evidence", err: ErrRecipientActionEvidence, status: http.StatusConflict},
		{name: "integration manual", err: ErrIntegrationActionNotManual, status: http.StatusConflict},
		{name: "communication unavailable", err: ErrCaseCommunicationUnavailable, status: http.StatusConflict},
		{name: "communication retry", err: ErrCaseCommunicationNotRetryable, status: http.StatusConflict},
		{name: "infrastructure", err: errors.New("database unavailable"), status: http.StatusServiceUnavailable},
	}
	for _, test := range errorCases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/privacy/cases", nil)
			handler.writeError(recorder, request, test.err)
			if recorder.Code != test.status {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), test.status)
			}
		})
	}

	recorder := httptest.NewRecorder()
	handler.listCases(recorder, httptest.NewRequest(http.MethodGet, "/v1/privacy/cases", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("missing request context status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if handler.now().IsZero() {
		t.Fatal("default handler clock returned a zero time")
	}

	reporterEmail := "reporter@example.test"
	message, err := privacyCaseVerificationMessage(
		PrivacyCase{
			ID: "case-1", PublicID: "PRIV-1", SubjectEmail: &reporterEmail,
		},
		"Workspace", "workspace", strings.Repeat("a", 43),
		HTTPOptions{
			PublicBaseURL: "https://api.fidibeki.test",
			BaseDomain:    "fidibeki.test",
			From:          "Fidibeki <auth@fidibeki.com>",
		},
	)
	if err != nil || message.To != reporterEmail ||
		!strings.Contains(message.Text, "https://workspace.fidibeki.test/privacy/") {
		t.Fatalf("verification message = %#v error=%v", message, err)
	}
	if _, err := privacyCaseVerificationMessage(
		PrivacyCase{ID: "case-2"}, "Workspace", "workspace",
		strings.Repeat("b", 43),
		HTTPOptions{
			PublicBaseURL: "https://api.fidibeki.test",
			BaseDomain:    "fidibeki.test",
			From:          "Fidibeki <auth@fidibeki.com>",
		},
	); err == nil {
		t.Fatal("verification message without reporter email succeeded")
	}
	if _, err := privacyCaseVerificationMessage(
		PrivacyCase{ID: "case-3", SubjectEmail: &reporterEmail},
		"Workspace", "workspace", strings.Repeat("c", 43),
		HTTPOptions{PublicBaseURL: "://invalid", From: "Fidibeki <auth@fidibeki.com>"},
	); err == nil {
		t.Fatal("verification message with invalid base URL succeeded")
	}
}
