package breachregister

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/go-chi/chi/v5"
)

type fakeStore struct {
	created           Incident
	createErr         error
	createdInput      CreateIncidentInput
	createdActor      string
	createdKey        string
	incidents         []Incident
	listErr           error
	getIncident       Incident
	getEntries        []ChronologyEntry
	getAssessments    []Assessment
	getErr            error
	appendedEntry     ChronologyEntry
	appendErr         error
	appendedActor     string
	appendedKey       string
	appendedInput     AppendChronologyEntryInput
	createdAssessment Assessment
	assessErr         error
	assessedActor     string
	assessedKey       string
	assessedInput     CreateAssessmentInput
}

func (f *fakeStore) CreateIncident(
	_ context.Context, actorPlatformAdminID string, idempotencyKey string, input CreateIncidentInput,
) (Incident, error) {
	f.createdActor = actorPlatformAdminID
	f.createdKey = idempotencyKey
	f.createdInput = input
	return f.created, f.createErr
}

func (f *fakeStore) ListIncidents(context.Context, ListFilter) ([]Incident, error) {
	return f.incidents, f.listErr
}

func (f *fakeStore) GetIncident(context.Context, string) (Incident, []ChronologyEntry, []Assessment, error) {
	return f.getIncident, f.getEntries, f.getAssessments, f.getErr
}

func (f *fakeStore) AppendChronologyEntry(
	_ context.Context, _ string, actorPlatformAdminID string, idempotencyKey string,
	input AppendChronologyEntryInput,
) (ChronologyEntry, error) {
	f.appendedActor = actorPlatformAdminID
	f.appendedKey = idempotencyKey
	f.appendedInput = input
	return f.appendedEntry, f.appendErr
}

func (f *fakeStore) CreateAssessment(
	_ context.Context, _ string, actorPlatformAdminID string, idempotencyKey string,
	input CreateAssessmentInput,
) (Assessment, error) {
	f.assessedActor = actorPlatformAdminID
	f.assessedKey = idempotencyKey
	f.assessedInput = input
	return f.createdAssessment, f.assessErr
}

func newTestHandler(t *testing.T, store *fakeStore) (http.Handler, *auth.JWTManager) {
	t.Helper()
	jwtManager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	router := chi.NewRouter()
	RegisterPlatformRoutes(router, HTTPOptions{Store: store, JWT: jwtManager})
	return router, jwtManager
}

func validCreateBody() string {
	return `{
		"awareness_at": "2026-07-01T10:00:00Z",
		"detection_source": "internal_monitoring",
		"controller_role": "processor",
		"affected_environments": ["staging"],
		"affected_system_categories": ["database"],
		"affected_data_categories": ["contact_info"],
		"affected_subject_categories": ["tenant_members"],
		"confidentiality_impact": true,
		"integrity_impact": false,
		"availability_impact": false,
		"containment_summary": "Rotated the affected credential and revoked active sessions.",
		"affected_tenant_ids": ["11111111-1111-1111-1111-111111111111"]
	}`
}

func TestCreateIncidentRequiresPlatformAdminAudienceAndPermission(t *testing.T) {
	t.Parallel()

	store := &fakeStore{created: Incident{ID: "incident-id", PublicID: "BR-ABCDEF12"}}
	handler, jwtManager := newTestHandler(t, store)
	body := []byte(validCreateBody())

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(
		http.MethodPost, "/admin/breach-incidents", bytes.NewReader(body),
	))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", unauthenticated.Code)
	}

	// A tenant-audience token (what a tenant member or tenant API key would
	// carry) must never reach the handler, even if it happens to include a
	// matching permission string.
	tenantToken, _, err := jwtManager.Issue(
		"member-id", auth.AudienceTenant, []string{string(auth.PermissionPlatformBreachCreate)},
	)
	if err != nil {
		t.Fatalf("issue tenant token: %v", err)
	}
	crossAudience := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/breach-incidents", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+tenantToken)
	handler.ServeHTTP(crossAudience, request)
	if crossAudience.Code != http.StatusUnauthorized {
		t.Errorf("tenant-audience token status = %d", crossAudience.Code)
	}

	readOnlyToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)
	forbidden := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/admin/breach-incidents", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+readOnlyToken)
	handler.ServeHTTP(forbidden, request)
	if forbidden.Code != http.StatusForbidden {
		t.Errorf("forbidden status = %d", forbidden.Code)
	}

	createToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachCreate)},
	)
	created := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/admin/breach-incidents", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+createToken)
	request.Header.Set("Idempotency-Key", "create-key-1")
	handler.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	if store.createdActor != "admin-id" || store.createdKey != "create-key-1" {
		t.Errorf("create actor/key = %q/%q", store.createdActor, store.createdKey)
	}
	if store.createdInput.DetectionSource != DetectionInternalMonitoring ||
		store.createdInput.ControllerRole != RoleProcessor {
		t.Errorf("create input = %#v", store.createdInput)
	}
}

func TestCreateIncidentRejectsInvalidEnumsAndBounds(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler, jwtManager := newTestHandler(t, store)
	createToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachCreate)},
	)

	for name, body := range map[string]string{
		"invalid detection source": `{"detection_source":"not_real","controller_role":"processor",
			"awareness_at":"2026-07-01T10:00:00Z","affected_environments":["staging"],
			"affected_system_categories":["database"],"affected_data_categories":["contact_info"],
			"affected_subject_categories":["tenant_members"],"confidentiality_impact":true,
			"containment_summary":"contained","affected_tenant_ids":["11111111-1111-1111-1111-111111111111"]}`,
		"invalid controller role": `{"detection_source":"internal_monitoring","controller_role":"vendor",
			"awareness_at":"2026-07-01T10:00:00Z","affected_environments":["staging"],
			"affected_system_categories":["database"],"affected_data_categories":["contact_info"],
			"affected_subject_categories":["tenant_members"],"confidentiality_impact":true,
			"containment_summary":"contained","affected_tenant_ids":["11111111-1111-1111-1111-111111111111"]}`,
		"unknown field": `{"detection_source":"internal_monitoring","controller_role":"processor",
			"awareness_at":"2026-07-01T10:00:00Z","affected_environments":["staging"],
			"affected_system_categories":["database"],"affected_data_categories":["contact_info"],
			"affected_subject_categories":["tenant_members"],"confidentiality_impact":true,
			"containment_summary":"contained","affected_tenant_ids":["11111111-1111-1111-1111-111111111111"],
			"unexpected":"field"}`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/admin/breach-incidents", strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+createToken)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnprocessableEntity && recorder.Code != http.StatusBadRequest {
				t.Errorf("%s status = %d, body = %s", name, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestCreateIncidentMapsStoreErrors(t *testing.T) {
	t.Parallel()

	for name, storeErr := range map[string]error{
		"invalid request":      ErrInvalidRequest,
		"tenant not found":     ErrTenantNotFound,
		"idempotency conflict": ErrIdempotencyConflict,
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{createErr: storeErr}
			handler, jwtManager := newTestHandler(t, store)
			createToken, _, _ := jwtManager.Issue(
				"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachCreate)},
			)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost, "/admin/breach-incidents", strings.NewReader(validCreateBody()),
			)
			request.Header.Set("Authorization", "Bearer "+createToken)
			handler.ServeHTTP(recorder, request)
			if storeErr == ErrIdempotencyConflict && recorder.Code != http.StatusConflict {
				t.Errorf("%s status = %d", name, recorder.Code)
			}
			if storeErr != ErrIdempotencyConflict && recorder.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s status = %d", name, recorder.Code)
			}
		})
	}
}

func TestListAndGetIncidentRequireReadPermission(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		incidents:   []Incident{{ID: "incident-id", PublicID: "BR-ABCDEF12", AffectedTenantIDs: []string{"tenant-1"}}},
		getIncident: Incident{ID: "incident-id", PublicID: "BR-ABCDEF12"},
		getEntries:  []ChronologyEntry{{ID: "entry-id", EntryKind: EntryDetected}},
	}
	handler, jwtManager := newTestHandler(t, store)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/admin/breach-incidents", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated list status = %d", unauthenticated.Code)
	}

	readToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)

	list := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/admin/breach-incidents", nil)
	listRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var listResponse map[string]any
	if err := json.NewDecoder(list.Body).Decode(&listResponse); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if _, ok := listResponse["incidents"]; !ok {
		t.Errorf("list response missing incidents: %#v", listResponse)
	}

	get := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/admin/breach-incidents/incident-id", nil)
	getRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(get, getRequest)
	if get.Code != http.StatusNotFound {
		t.Errorf("non-UUID incident path status = %d, want 404", get.Code)
	}

	validID := "11111111-1111-1111-1111-111111111111"
	store.getIncident = Incident{ID: validID, PublicID: "BR-ABCDEF12"}
	getValid := httptest.NewRecorder()
	getValidRequest := httptest.NewRequest(http.MethodGet, "/admin/breach-incidents/"+validID, nil)
	getValidRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(getValid, getValidRequest)
	if getValid.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getValid.Code, getValid.Body.String())
	}
}

func TestAppendChronologyEntryRequiresAppendPermissionAndValidatesKind(t *testing.T) {
	t.Parallel()

	validID := "11111111-1111-1111-1111-111111111111"
	store := &fakeStore{appendedEntry: ChronologyEntry{ID: "entry-id", EntryKind: EntryContained}}
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + validID + "/chronology"

	readOnlyToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)
	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(`{"entry_kind":"contained","summary":"Contained."}`),
	)
	forbiddenRequest.Header.Set("Authorization", "Bearer "+readOnlyToken)
	handler.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Errorf("forbidden status = %d", forbidden.Code)
	}

	appendToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachAppend)},
	)

	invalidKind := httptest.NewRecorder()
	invalidKindRequest := httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(`{"entry_kind":"not_real","summary":"Contained."}`),
	)
	invalidKindRequest.Header.Set("Authorization", "Bearer "+appendToken)
	handler.ServeHTTP(invalidKind, invalidKindRequest)
	if invalidKind.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid entry_kind status = %d", invalidKind.Code)
	}

	ok := httptest.NewRecorder()
	okRequest := httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(
			`{"entry_kind":"contained","summary":"Rotated the credential.","evidence_references":["ticket:INC-1"]}`,
		),
	)
	okRequest.Header.Set("Authorization", "Bearer "+appendToken)
	okRequest.Header.Set("Idempotency-Key", "append-key-1")
	handler.ServeHTTP(ok, okRequest)
	if ok.Code != http.StatusCreated {
		t.Fatalf("append status = %d, body = %s", ok.Code, ok.Body.String())
	}
	if store.appendedActor != "admin-id" || store.appendedKey != "append-key-1" {
		t.Errorf("append actor/key = %q/%q", store.appendedActor, store.appendedKey)
	}
	if store.appendedInput.EntryKind != EntryContained {
		t.Errorf("append input = %#v", store.appendedInput)
	}
}

func validCreateAssessmentBody() string {
	return `{
		"authority_notification_decision": "assessment_pending",
		"authority_notification_reasoning": "Awaiting confirmation of exfiltration scope.",
		"high_risk_to_individuals": "assessment_pending",
		"high_risk_reasoning": "Risk to data subjects not yet determined.",
		"data_subject_communication_decision": "assessment_pending",
		"data_subject_communication_reasoning": "Depends on the risk assessment above.",
		"estimated_affected_subjects_min": 50,
		"estimated_affected_subjects_max": 200,
		"likely_consequences": "Possible unauthorized read access to contact details.",
		"evidence_references": ["ticket:INC-42"]
	}`
}

func TestCreateAssessmentRequiresAppendPermissionAndValidatesDecisions(t *testing.T) {
	t.Parallel()

	validID := "11111111-1111-1111-1111-111111111111"
	store := &fakeStore{createdAssessment: Assessment{
		ID: "assessment-id", AuthorityNotificationDecision: AuthorityNotificationAssessmentPending,
	}}
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + validID + "/assessments"

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(validCreateAssessmentBody()),
	))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", unauthenticated.Code)
	}

	readOnlyToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)
	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(http.MethodPost, path, strings.NewReader(validCreateAssessmentBody()))
	forbiddenRequest.Header.Set("Authorization", "Bearer "+readOnlyToken)
	handler.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Errorf("forbidden status = %d", forbidden.Code)
	}

	appendToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachAppend)},
	)

	for name, body := range map[string]string{
		"invalid authority decision": `{"authority_notification_decision":"not_real",
			"authority_notification_reasoning":"reason","high_risk_to_individuals":"assessment_pending",
			"high_risk_reasoning":"reason","data_subject_communication_decision":"assessment_pending",
			"data_subject_communication_reasoning":"reason"}`,
		"invalid risk decision": `{"authority_notification_decision":"assessment_pending",
			"authority_notification_reasoning":"reason","high_risk_to_individuals":"not_real",
			"high_risk_reasoning":"reason","data_subject_communication_decision":"assessment_pending",
			"data_subject_communication_reasoning":"reason"}`,
		"invalid communication decision": `{"authority_notification_decision":"assessment_pending",
			"authority_notification_reasoning":"reason","high_risk_to_individuals":"assessment_pending",
			"high_risk_reasoning":"reason","data_subject_communication_decision":"not_real",
			"data_subject_communication_reasoning":"reason"}`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+appendToken)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s status = %d, body = %s", name, recorder.Code, recorder.Body.String())
			}
		})
	}

	ok := httptest.NewRecorder()
	okRequest := httptest.NewRequest(http.MethodPost, path, strings.NewReader(validCreateAssessmentBody()))
	okRequest.Header.Set("Authorization", "Bearer "+appendToken)
	okRequest.Header.Set("Idempotency-Key", "assess-key-1")
	handler.ServeHTTP(ok, okRequest)
	if ok.Code != http.StatusCreated {
		t.Fatalf("create assessment status = %d, body = %s", ok.Code, ok.Body.String())
	}
	if store.assessedActor != "admin-id" || store.assessedKey != "assess-key-1" {
		t.Errorf("assessment actor/key = %q/%q", store.assessedActor, store.assessedKey)
	}
	if store.assessedInput.AuthorityNotificationDecision != AuthorityNotificationAssessmentPending {
		t.Errorf("assessment input = %#v", store.assessedInput)
	}
	if store.assessedInput.EstimatedAffectedSubjectsMin == nil || *store.assessedInput.EstimatedAffectedSubjectsMin != 50 {
		t.Errorf("assessment estimate = %#v", store.assessedInput)
	}

	nonUUID := httptest.NewRecorder()
	nonUUIDRequest := httptest.NewRequest(
		http.MethodPost, "/admin/breach-incidents/not-a-uuid/assessments", strings.NewReader(validCreateAssessmentBody()),
	)
	nonUUIDRequest.Header.Set("Authorization", "Bearer "+appendToken)
	handler.ServeHTTP(nonUUID, nonUUIDRequest)
	if nonUUID.Code != http.StatusNotFound {
		t.Errorf("non-UUID incident path status = %d, want 404", nonUUID.Code)
	}
}

func TestGetIncidentIncludesAssessments(t *testing.T) {
	t.Parallel()

	validID := "11111111-1111-1111-1111-111111111111"
	store := &fakeStore{
		getIncident:    Incident{ID: validID, PublicID: "BR-ABCDEF12"},
		getAssessments: []Assessment{{ID: "assessment-id", AuthorityNotificationDecision: AuthorityNotificationRequired}},
	}
	handler, jwtManager := newTestHandler(t, store)
	readToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/breach-incidents/"+validID, nil)
	request.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assessments, ok := response["assessments"].([]any)
	if !ok || len(assessments) != 1 {
		t.Fatalf("response assessments = %#v", response["assessments"])
	}
}
