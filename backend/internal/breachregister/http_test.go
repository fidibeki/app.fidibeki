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

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/go-chi/chi/v5"
)

type fakeStore struct {
	created                    Incident
	createErr                  error
	createdInput               CreateIncidentInput
	createdActor               string
	createdKey                 string
	incidents                  []Incident
	listErr                    error
	listCalls                  int
	getIncident                Incident
	getEntries                 []ChronologyEntry
	getAssessments             []Assessment
	getActions                 []ActionEvent
	getActionEvents            []ActionEvent
	getNotifications           []ControllerNotification
	getProviderIntakes         []ProviderIncidentIntake
	getErr                     error
	getCalls                   int
	createdProviderIntake      ProviderIncidentIntake
	createProviderIntakeErr    error
	providerIntakeActor        string
	providerIntakeKey          string
	providerIntakeInput        CreateProviderIncidentIntakeInput
	appendedEntry              ChronologyEntry
	appendErr                  error
	appendedActor              string
	appendedKey                string
	appendedInput              AppendChronologyEntryInput
	createdAssessment          Assessment
	assessErr                  error
	assessedActor              string
	assessedKey                string
	assessedInput              CreateAssessmentInput
	createdAction              ActionEvent
	createActionErr            error
	createActionActor          string
	createActionKey            string
	createActionInput          CreatePostIncidentActionInput
	revisedAction              ActionEvent
	revisionErr                error
	revisionActor              string
	revisionKey                string
	revisionActionID           string
	revisionInput              AppendActionRevisionInput
	stagedNotification         ControllerNotification
	stageNotificationErr       error
	stagedNotificationCreated  bool
	queuedNotification         ControllerNotification
	queueNotificationErr       error
	queuedNotificationChanged  bool
	retriedNotification        ControllerNotification
	retryNotificationErr       error
	retriedNotificationChanged bool
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
	f.listCalls++
	return f.incidents, f.listErr
}

func (f *fakeStore) GetIncident(
	context.Context, string,
) (
	Incident, []ChronologyEntry, []Assessment, []ActionEvent, []ActionEvent,
	[]ControllerNotification, []ProviderIncidentIntake, error,
) {
	f.getCalls++
	return f.getIncident, f.getEntries, f.getAssessments, f.getActions, f.getActionEvents,
		f.getNotifications, f.getProviderIntakes, f.getErr
}

func (f *fakeStore) CreateProviderIncidentIntake(
	_ context.Context, _ string, actorPlatformAdminID string, idempotencyKey string,
	input CreateProviderIncidentIntakeInput,
) (ProviderIncidentIntake, error) {
	f.providerIntakeActor = actorPlatformAdminID
	f.providerIntakeKey = idempotencyKey
	f.providerIntakeInput = input
	return f.createdProviderIntake, f.createProviderIntakeErr
}

func (f *fakeStore) StageControllerNotification(
	_ context.Context, _ string, _ string, _ string, _ StageControllerNotificationInput,
) (ControllerNotification, bool, error) {
	return f.stagedNotification, f.stagedNotificationCreated, f.stageNotificationErr
}

func (f *fakeStore) QueueControllerNotification(
	_ context.Context, _ string, _ string, _ string, _ string,
) (ControllerNotification, bool, error) {
	return f.queuedNotification, f.queuedNotificationChanged, f.queueNotificationErr
}

func (f *fakeStore) RetryControllerNotification(
	_ context.Context, _ string, _ string, _ string, _ string,
) (ControllerNotification, bool, error) {
	return f.retriedNotification, f.retriedNotificationChanged, f.retryNotificationErr
}

func (f *fakeStore) CreatePostIncidentAction(
	_ context.Context, _ string, actorPlatformAdminID string, idempotencyKey string,
	input CreatePostIncidentActionInput,
) (ActionEvent, error) {
	f.createActionActor = actorPlatformAdminID
	f.createActionKey = idempotencyKey
	f.createActionInput = input
	return f.createdAction, f.createActionErr
}

func (f *fakeStore) AppendActionRevision(
	_ context.Context, _ string, actionID string, actorPlatformAdminID string,
	idempotencyKey string, input AppendActionRevisionInput,
) (ActionEvent, error) {
	f.revisionActionID = actionID
	f.revisionActor = actorPlatformAdminID
	f.revisionKey = idempotencyKey
	f.revisionInput = input
	return f.revisedAction, f.revisionErr
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

func validProviderIncidentIntakeBody() string {
	return `{
		"provider_id":"resend_transactional_email",
		"provider_notice_reference":"provider-case-123",
		"provider_reported_at":"2026-07-24T10:00:00Z",
		"summary":"Provider reported an incident affecting message delivery.",
		"evidence_references":["case:123"],
		"affected_data_flow_categories":["external_communications"],
		"affected_tenant_ids":["22222222-2222-4222-8222-222222222222"]
	}`
}

func TestCreateProviderIncidentIntakeRequiresAppendPermissionAndPassesInput(t *testing.T) {
	t.Parallel()
	incidentID := "11111111-1111-4111-8111-111111111111"
	store := &fakeStore{
		createdProviderIntake: ProviderIncidentIntake{
			ID: "33333333-3333-4333-8333-333333333333",
		},
	}
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + incidentID + "/provider-intakes"

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(validProviderIncidentIntakeBody()),
	))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated provider intake status = %d, want 401", unauthenticated.Code)
	}

	tenantToken, _, _ := jwtManager.Issue(
		"member-id", auth.AudienceTenant,
		[]string{string(auth.PermissionPlatformBreachAppend)},
	)
	crossAudience := httptest.NewRecorder()
	crossAudienceRequest := httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(validProviderIncidentIntakeBody()),
	)
	crossAudienceRequest.Header.Set("Authorization", "Bearer "+tenantToken)
	handler.ServeHTTP(crossAudience, crossAudienceRequest)
	if crossAudience.Code != http.StatusUnauthorized {
		t.Fatalf("tenant-audience provider intake status = %d, want 401", crossAudience.Code)
	}

	readToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)
	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(validProviderIncidentIntakeBody()),
	)
	forbiddenRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("read-only provider intake status = %d, want 403", forbidden.Code)
	}

	appendToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachAppend)},
	)
	created := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(validProviderIncidentIntakeBody()),
	)
	request.Header.Set("Authorization", "Bearer "+appendToken)
	request.Header.Set("Idempotency-Key", "provider-intake-1")
	handler.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("provider intake status = %d, body = %s", created.Code, created.Body.String())
	}
	if store.providerIntakeActor != "admin-id" ||
		store.providerIntakeKey != "provider-intake-1" ||
		store.providerIntakeInput.ProviderID != "resend_transactional_email" ||
		len(store.providerIntakeInput.AffectedTenantIDs) != 1 {
		t.Fatalf(
			"provider intake actor/key/input = %q/%q/%#v",
			store.providerIntakeActor, store.providerIntakeKey, store.providerIntakeInput,
		)
	}

	invalidID := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(
		http.MethodPost, "/admin/breach-incidents/not-a-uuid/provider-intakes",
		strings.NewReader(validProviderIncidentIntakeBody()),
	)
	invalidRequest.Header.Set("Authorization", "Bearer "+appendToken)
	handler.ServeHTTP(invalidID, invalidRequest)
	if invalidID.Code != http.StatusNotFound {
		t.Fatalf("invalid incident ID status = %d, want 404", invalidID.Code)
	}
}

func TestCreateProviderIncidentIntakeMapsStoreErrors(t *testing.T) {
	t.Parallel()
	incidentID := "11111111-1111-4111-8111-111111111111"
	tests := []struct {
		err  error
		want int
	}{
		{err: ErrInvalidRequest, want: http.StatusUnprocessableEntity},
		{err: ErrTenantNotFound, want: http.StatusUnprocessableEntity},
		{err: ErrNotFound, want: http.StatusNotFound},
		{err: ErrIdempotencyConflict, want: http.StatusConflict},
	}
	for _, test := range tests {
		test := test
		t.Run(test.err.Error(), func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{createProviderIntakeErr: test.err}
			handler, jwtManager := newTestHandler(t, store)
			token, _, _ := jwtManager.Issue(
				"admin-id", auth.AudiencePlatform,
				[]string{string(auth.PermissionPlatformBreachAppend)},
			)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost,
				"/admin/breach-incidents/"+incidentID+"/provider-intakes",
				strings.NewReader(validProviderIncidentIntakeBody()),
			)
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Idempotency-Key", "provider-intake-1")
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
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
		getNotifications: []ControllerNotification{{
			ID: "notification-id", NotificationKind: ControllerNotificationInitial,
			Status: ControllerNotificationDraft,
		}},
		getProviderIntakes: []ProviderIncidentIntake{{
			ID: "provider-intake-id", ProviderID: "resend_transactional_email",
		}},
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
	notifications, ok := response["controller_notifications"].([]any)
	if !ok || len(notifications) != 1 {
		t.Fatalf("response controller notifications = %#v", response["controller_notifications"])
	}
	providerIntakes, ok := response["provider_incident_intakes"].([]any)
	if !ok || len(providerIntakes) != 1 {
		t.Fatalf("response provider intakes = %#v", response["provider_incident_intakes"])
	}
}

func TestBreachActionsRequireAppendPermissionAndExposeAdditiveDetail(t *testing.T) {
	t.Parallel()

	incidentID := "11111111-1111-4111-8111-111111111111"
	actionID := "22222222-2222-4222-8222-222222222222"
	ownerID := "33333333-3333-4333-8333-333333333333"
	store := &fakeStore{
		createdAction: ActionEvent{ActionID: actionID, Revision: 1, Status: ActionPending},
		revisedAction: ActionEvent{ActionID: actionID, Revision: 2, Status: ActionInProgress},
		getIncident:   Incident{ID: incidentID, PublicID: "BR-ACTIONS1"},
		getActions: []ActionEvent{{
			ActionID: actionID, Revision: 2, Status: ActionInProgress,
		}},
		getActionEvents: []ActionEvent{
			{ActionID: actionID, Revision: 1, Status: ActionPending},
			{ActionID: actionID, Revision: 2, Status: ActionInProgress},
		},
	}
	handler, jwtManager := newTestHandler(t, store)
	createPath := "/admin/breach-incidents/" + incidentID + "/actions"

	readToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)
	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(
		http.MethodPost, createPath, strings.NewReader(`{
			"title":"Complete review",
			"owner_platform_admin_id":"`+ownerID+`",
			"due_at":"2026-07-30T10:00:00Z"
		}`),
	)
	forbiddenRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Errorf("read-only create action status = %d, want 403", forbidden.Code)
	}

	appendToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachAppend)},
	)
	created := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost, createPath, strings.NewReader(`{
			"title":"Complete review",
			"owner_platform_admin_id":"`+ownerID+`",
			"due_at":"2026-07-30T10:00:00Z",
			"corrective_reference":"issue:FID-42"
		}`),
	)
	createRequest.Header.Set("Authorization", "Bearer "+appendToken)
	createRequest.Header.Set("Idempotency-Key", "action-create-key")
	handler.ServeHTTP(created, createRequest)
	if created.Code != http.StatusCreated {
		t.Fatalf("create action status = %d, body = %s", created.Code, created.Body.String())
	}
	if store.createActionActor != "admin-id" || store.createActionKey != "action-create-key" ||
		store.createActionInput.OwnerPlatformAdminID != ownerID {
		t.Errorf(
			"create action actor/key/input = %q/%q/%#v",
			store.createActionActor, store.createActionKey, store.createActionInput,
		)
	}

	revisionPath := createPath + "/" + actionID + "/revisions"
	revised := httptest.NewRecorder()
	revisionRequest := httptest.NewRequest(
		http.MethodPost, revisionPath, strings.NewReader(`{
			"expected_revision":1,
			"status":"in_progress",
			"owner_platform_admin_id":"`+ownerID+`",
			"due_at":"2026-07-30T10:00:00Z"
		}`),
	)
	revisionRequest.Header.Set("Authorization", "Bearer "+appendToken)
	revisionRequest.Header.Set("Idempotency-Key", "action-revision-key")
	handler.ServeHTTP(revised, revisionRequest)
	if revised.Code != http.StatusCreated {
		t.Fatalf("revise action status = %d, body = %s", revised.Code, revised.Body.String())
	}
	if store.revisionActionID != actionID || store.revisionActor != "admin-id" ||
		store.revisionKey != "action-revision-key" ||
		store.revisionInput.Status != ActionInProgress {
		t.Errorf(
			"revision action/actor/key/input = %q/%q/%q/%#v",
			store.revisionActionID, store.revisionActor, store.revisionKey, store.revisionInput,
		)
	}

	get := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/admin/breach-incidents/"+incidentID, nil)
	getRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(get, getRequest)
	if get.Code != http.StatusOK {
		t.Fatalf("get incident status = %d, body = %s", get.Code, get.Body.String())
	}
	var detail map[string]any
	if err := json.NewDecoder(get.Body).Decode(&detail); err != nil {
		t.Fatalf("decode incident detail: %v", err)
	}
	if actions, ok := detail["actions"].([]any); !ok || len(actions) != 1 {
		t.Errorf("detail actions = %#v", detail["actions"])
	}
	if events, ok := detail["action_events"].([]any); !ok || len(events) != 2 {
		t.Errorf("detail action_events = %#v", detail["action_events"])
	}
}

func TestBreachActionRoutesValidateIDsStatusAndRevisionConflict(t *testing.T) {
	t.Parallel()

	incidentID := "11111111-1111-4111-8111-111111111111"
	actionID := "22222222-2222-4222-8222-222222222222"
	store := &fakeStore{}
	handler, jwtManager := newTestHandler(t, store)
	appendToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachAppend)},
	)

	for name, path := range map[string]string{
		"invalid incident": "/admin/breach-incidents/not-a-uuid/actions",
		"invalid action": "/admin/breach-incidents/" + incidentID +
			"/actions/not-a-uuid/revisions",
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer "+appendToken)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", recorder.Code)
			}
		})
	}

	invalidStatus := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(
		http.MethodPost,
		"/admin/breach-incidents/"+incidentID+"/actions/"+actionID+"/revisions",
		strings.NewReader(`{"expected_revision":1,"status":"not_real"}`),
	)
	invalidRequest.Header.Set("Authorization", "Bearer "+appendToken)
	handler.ServeHTTP(invalidStatus, invalidRequest)
	if invalidStatus.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid status code = %d, want 422", invalidStatus.Code)
	}

	store.revisionErr = ErrRevisionConflict
	stale := httptest.NewRecorder()
	staleRequest := httptest.NewRequest(
		http.MethodPost,
		"/admin/breach-incidents/"+incidentID+"/actions/"+actionID+"/revisions",
		strings.NewReader(`{"expected_revision":1,"status":"pending"}`),
	)
	staleRequest.Header.Set("Authorization", "Bearer "+appendToken)
	handler.ServeHTTP(stale, staleRequest)
	if stale.Code != http.StatusConflict {
		t.Errorf("stale revision code = %d, want 409", stale.Code)
	}
}

func TestControllerNotificationRoutesRequireAppendPermissionAndExposeState(t *testing.T) {
	t.Parallel()

	incidentID := "11111111-1111-4111-8111-111111111111"
	notificationID := "22222222-2222-4222-8222-222222222222"
	tenantID := "33333333-3333-4333-8333-333333333333"
	store := &fakeStore{
		stagedNotification:         ControllerNotification{ID: notificationID, Status: ControllerNotificationDraft},
		stagedNotificationCreated:  true,
		queuedNotification:         ControllerNotification{ID: notificationID, Status: ControllerNotificationPending},
		queuedNotificationChanged:  true,
		retriedNotification:        ControllerNotification{ID: notificationID, Status: ControllerNotificationPending},
		retriedNotificationChanged: true,
	}
	handler, jwtManager := newTestHandler(t, store)
	readToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)
	appendToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachAppend)},
	)
	stagePath := "/admin/breach-incidents/" + incidentID + "/controller-notifications"
	stageBody := `{
		"tenant_id":"` + tenantID + `",
		"notification_kind":"initial",
		"summary":"A bounded incident summary.",
		"measures_taken":"The affected path was disabled."
	}`

	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(http.MethodPost, stagePath, strings.NewReader(stageBody))
	forbiddenRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("read-only stage status = %d", forbidden.Code)
	}

	staged := httptest.NewRecorder()
	stageRequest := httptest.NewRequest(http.MethodPost, stagePath, strings.NewReader(stageBody))
	stageRequest.Header.Set("Authorization", "Bearer "+appendToken)
	stageRequest.Header.Set("Idempotency-Key", "stage-notification")
	handler.ServeHTTP(staged, stageRequest)
	if staged.Code != http.StatusCreated {
		t.Fatalf("stage status = %d, body = %s", staged.Code, staged.Body.String())
	}

	queuePath := stagePath + "/" + notificationID + "/queue"
	queued := httptest.NewRecorder()
	queueRequest := httptest.NewRequest(http.MethodPost, queuePath, nil)
	queueRequest.Header.Set("Authorization", "Bearer "+appendToken)
	queueRequest.Header.Set("Idempotency-Key", "queue-notification")
	handler.ServeHTTP(queued, queueRequest)
	if queued.Code != http.StatusAccepted {
		t.Fatalf("queue status = %d, body = %s", queued.Code, queued.Body.String())
	}

	retryPath := stagePath + "/" + notificationID + "/retry"
	retried := httptest.NewRecorder()
	retryRequest := httptest.NewRequest(http.MethodPost, retryPath, nil)
	retryRequest.Header.Set("Authorization", "Bearer "+appendToken)
	retryRequest.Header.Set("Idempotency-Key", "retry-notification")
	handler.ServeHTTP(retried, retryRequest)
	if retried.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d, body = %s", retried.Code, retried.Body.String())
	}
}

func TestControllerNotificationRoutesMapValidationAndStateErrors(t *testing.T) {
	t.Parallel()

	incidentID := "11111111-1111-4111-8111-111111111111"
	notificationID := "22222222-2222-4222-8222-222222222222"
	tenantID := "33333333-3333-4333-8333-333333333333"
	store := &fakeStore{stageNotificationErr: ErrControllerContactMissing}
	handler, jwtManager := newTestHandler(t, store)
	appendToken, _, _ := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachAppend)},
	)
	stage := httptest.NewRecorder()
	stageRequest := httptest.NewRequest(
		http.MethodPost,
		"/admin/breach-incidents/"+incidentID+"/controller-notifications",
		strings.NewReader(`{
			"tenant_id":"`+tenantID+`",
			"notification_kind":"initial",
			"summary":"Summary",
			"measures_taken":"Measures"
		}`),
	)
	stageRequest.Header.Set("Authorization", "Bearer "+appendToken)
	stageRequest.Header.Set("Idempotency-Key", "stage")
	handler.ServeHTTP(stage, stageRequest)
	if stage.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing contact status = %d, body = %s", stage.Code, stage.Body.String())
	}

	store.queueNotificationErr = ErrNotificationNotQueueable
	queue := httptest.NewRecorder()
	queueRequest := httptest.NewRequest(
		http.MethodPost,
		"/admin/breach-incidents/"+incidentID+"/controller-notifications/"+notificationID+"/queue",
		nil,
	)
	queueRequest.Header.Set("Authorization", "Bearer "+appendToken)
	queueRequest.Header.Set("Idempotency-Key", "queue")
	handler.ServeHTTP(queue, queueRequest)
	if queue.Code != http.StatusConflict {
		t.Fatalf("not queueable status = %d, body = %s", queue.Code, queue.Body.String())
	}
}
