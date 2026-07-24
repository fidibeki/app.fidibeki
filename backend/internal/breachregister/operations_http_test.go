package breachregister

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/privacy"
	"github.com/go-chi/chi/v5"
)

const operationsTestCookie = "fidibeki_test_operations_access"

func newOperationsTestHandler(
	t *testing.T,
	store *fakeStore,
	now time.Time,
) (http.Handler, *auth.JWTManager) {
	t.Helper()
	jwtManager, err := auth.NewJWTManager(strings.Repeat("o", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	router := chi.NewRouter()
	RegisterPlatformOperationsRoutes(router, HTTPOptions{
		Store: store, JWT: jwtManager, Now: func() time.Time { return now },
	}, operationsTestCookie)
	return router, jwtManager
}

func operationsRequest(
	t *testing.T,
	handler http.Handler,
	jwtManager *auth.JWTManager,
	audience string,
	permissions []string,
	method string,
	path string,
) *httptest.ResponseRecorder {
	t.Helper()
	token, _, err := jwtManager.Issue("platform-admin-id", audience, permissions)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(&http.Cookie{Name: operationsTestCookie, Value: token})
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestOperationsRegisterRequiresPlatformCookieAudienceAndReadPermission(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	handler, jwtManager := newOperationsTestHandler(t, store, now)

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, OperationsRootPath, nil))
	if missing.Code != http.StatusUnauthorized || store.listCalls != 0 {
		t.Fatalf("missing cookie status = %d, list calls = %d", missing.Code, store.listCalls)
	}

	tenant := operationsRequest(t, handler, jwtManager, auth.AudienceTenant,
		[]string{string(auth.PermissionPlatformBreachRead)}, http.MethodGet, OperationsRootPath)
	if tenant.Code != http.StatusUnauthorized || store.listCalls != 0 {
		t.Fatalf("tenant cookie status = %d, list calls = %d", tenant.Code, store.listCalls)
	}

	denied := operationsRequest(t, handler, jwtManager, auth.AudiencePlatform,
		[]string{string(auth.PermissionPlatformTenantRead)}, http.MethodGet, OperationsRootPath)
	if denied.Code != http.StatusForbidden || store.listCalls != 0 {
		t.Fatalf("missing permission status = %d, list calls = %d", denied.Code, store.listCalls)
	}

	allowed := operationsRequest(t, handler, jwtManager, auth.AudiencePlatform,
		[]string{string(auth.PermissionPlatformBreachRead)}, http.MethodGet, OperationsRootPath)
	if allowed.Code != http.StatusOK || store.listCalls != 1 {
		t.Fatalf("valid cookie status = %d, list calls = %d", allowed.Code, store.listCalls)
	}
}

func TestOperationsIncidentListRendersSafeOperationalState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	incidentID := "11111111-1111-4111-8111-111111111111"
	store := &fakeStore{incidents: []Incident{{
		ID: incidentID, PublicID: `BR-<script>alert("public")</script>`,
		AwarenessAt:                     now.Add(-60 * time.Hour),
		AuthorityNotificationDeadlineAt: now.Add(12 * time.Hour),
		ControllerRole:                  RoleProcessor, AffectedEnvironments: []string{"staging"},
		ConfidentialityImpact: true,
		AffectedTenantIDs:     []string{"22222222-2222-4222-8222-222222222222"},
	}}}
	handler, jwtManager := newOperationsTestHandler(t, store, now)
	response := operationsRequest(t, handler, jwtManager, auth.AudiencePlatform,
		[]string{string(auth.PermissionPlatformBreachRead)}, http.MethodGet, OperationsRootPath)

	body := response.Body.String()
	for _, expected := range []string{
		"Privacy incidents", "12h remaining", "Confidentiality", "processor",
		"/admin/operations/privacy-incidents/" + incidentID,
		`BR-&lt;script&gt;alert(&#34;public&#34;)&lt;/script&gt;`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("body missing %q", expected)
		}
	}
	if strings.Contains(body, `<script>alert("public")</script>`) {
		t.Error("operator-controlled public ID rendered as executable HTML")
	}
	if response.Header().Get("Cache-Control") != "private, no-store" ||
		!strings.Contains(response.Header().Get("Content-Security-Policy"), "default-src 'none'") ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("security headers = %#v", response.Header())
	}
}

func TestOperationsIncidentDetailRendersCompleteReadOnlyEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	incidentID := "11111111-1111-4111-8111-111111111111"
	notificationID := "33333333-3333-4333-8333-333333333333"
	ownerID := "44444444-4444-4444-8444-444444444444"
	outcome := "Credential rotation confirmed."
	likely := "Unauthorized access may have occurred."
	recovery := "Affected sessions were revoked."
	providerMessageID := "provider-message-1"
	store := &fakeStore{
		getIncident: Incident{
			ID: incidentID, PublicID: "BR-TEST01", AwarenessAt: now.Add(-80 * time.Hour),
			AuthorityNotificationDeadlineAt: now.Add(-8 * time.Hour),
			DetectionSource:                 DetectionSubprocessorNotice, ControllerRole: RoleProcessor,
			AffectedEnvironments:      []string{"staging"},
			AffectedSystemCategories:  []string{"object_storage"},
			AffectedDataCategories:    []string{"attachments_media"},
			AffectedSubjectCategories: []string{"reporters_end_users"},
			ConfidentialityImpact:     true, ContainmentSummary: `<script>alert("summary")</script>`,
			CreatedByPlatformAdminID: "creator-id", CreatedAt: now.Add(-79 * time.Hour),
			AffectedTenantIDs: []string{"22222222-2222-4222-8222-222222222222"},
		},
		getEntries: []ChronologyEntry{{
			ID: "entry-1", BreachIncidentID: incidentID, EntryKind: EntryContained,
			Summary: "Access path disabled.", EvidenceReferences: []string{"evidence://case/1"},
			ActorPlatformAdminID: "actor-id", OccurredAt: now.Add(-70 * time.Hour),
		}},
		getAssessments: []Assessment{{
			ID: "assessment-1", BreachIncidentID: incidentID,
			AuthorityNotificationDecision:  AuthorityNotificationAssessmentPending,
			AuthorityNotificationReasoning: "Controller review is in progress.",
			HighRiskToIndividuals:          RiskAssessmentPending, HighRiskReasoning: "Impact is still being bounded.",
			DataSubjectCommunicationDecision:  CommunicationAssessmentPending,
			DataSubjectCommunicationReasoning: "No conclusion has been recorded.",
			LikelyConsequences:                &likely, RecoveryMeasures: &recovery,
			EvidenceReferences:        []string{"evidence://case/2"},
			AssessedByPlatformAdminID: "assessor-id", AssessedAt: now.Add(-60 * time.Hour),
		}},
		getActions: []ActionEvent{{
			ID: "action-event-2", BreachIncidentID: incidentID, ActionID: "action-1", Revision: 2,
			ActionKind: ActionContainment, Title: "Review object storage", Status: ActionCompleted,
			OwnerPlatformAdminID: &ownerID, OutcomeSummary: &outcome,
			EvidenceReferences: []string{"evidence://case/3"}, ActorPlatformAdminID: ownerID,
			OccurredAt: now.Add(-50 * time.Hour),
		}},
		getActionEvents: []ActionEvent{{
			ID: "action-event-1", BreachIncidentID: incidentID, ActionID: "action-1", Revision: 1,
			ActionKind: ActionContainment, Title: "Review object storage", Status: ActionPending,
			ActorPlatformAdminID: "creator-id", OccurredAt: now.Add(-79 * time.Hour),
		}},
		getNotifications: []ControllerNotification{{
			ID: notificationID, BreachIncidentID: incidentID,
			TenantID:         "22222222-2222-4222-8222-222222222222",
			NotificationKind: ControllerNotificationInitial, Sequence: 1, TemplateVersion: 1,
			WorkspaceName: "Workspace One", RecipientEmail: "privacy@example.test",
			Summary: "Initial factual notice.", MeasuresTaken: "Access path disabled.",
			Status: ControllerNotificationSent, Attempts: 1, ProviderMessageID: &providerMessageID,
			CreatedAt: now.Add(-58 * time.Hour),
			DeliveryAttempts: []ControllerNotificationAttempt{{
				ID: "attempt-1", NotificationID: notificationID, AttemptNumber: 1,
				Status: ControllerNotificationAttemptSent, ProviderMessageID: &providerMessageID,
				AttemptedAt: now.Add(-57 * time.Hour),
			}},
		}},
		getProviderIntakes: []ProviderIncidentIntake{{
			ID: "intake-1", BreachIncidentID: incidentID, ProviderID: "object-storage",
			ProviderDisplayName:       "Object storage provider",
			BoundaryKind:              privacy.GovernanceBoundaryInfrastructureProvider,
			ApprovalStatus:            privacy.GovernanceApprovalPending,
			ContractStatus:            privacy.GovernanceContractUnknown,
			GovernanceRegistryVersion: 1, ProviderNoticeReference: "notice-1",
			ProviderReportedAt: now.Add(-82 * time.Hour), ReceivedAt: now.Add(-80 * time.Hour),
			Summary:                    "Provider reported an access-control event.",
			AffectedDataFlowCategories: []string{"attachment_objects"},
			AffectedTenantIDs:          []string{"22222222-2222-4222-8222-222222222222"},
			PrimaryRetentionCategory:   "attachment_objects", ActorPlatformAdminID: "actor-id",
		}},
	}
	handler, jwtManager := newOperationsTestHandler(t, store, now)
	response := operationsRequest(t, handler, jwtManager, auth.AudiencePlatform,
		[]string{string(auth.PermissionPlatformBreachRead)}, http.MethodGet,
		OperationsRootPath+"/"+incidentID)

	body := response.Body.String()
	for _, expected := range []string{
		"Clock elapsed 8h ago", "Incident facts", "Assessment history", "Current actions",
		"Action revision ledger", "Controller notifications", "External provider intakes",
		"Immutable chronology", "Controller review is in progress.", "Review object storage",
		"privacy@example.test", "Provider reported an access-control event.",
		"evidence://case/1", "Download JSON", "Download Markdown",
		OperationsRootPath + "/" + incidentID + "/post-incident-report?format=json",
		operationsNotificationExportURL(incidentID, notificationID, ExportFormatMarkdown),
		`&lt;script&gt;alert(&#34;summary&#34;)&lt;/script&gt;`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("body missing %q", expected)
		}
	}
	if strings.Contains(body, `<script>alert("summary")</script>`) {
		t.Error("incident narrative rendered as executable HTML")
	}
}

func TestOperationsIncidentEmptyAndNeutralNotFoundStates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{getErr: ErrNotFound}
	handler, jwtManager := newOperationsTestHandler(t, store, now)
	permissions := []string{string(auth.PermissionPlatformBreachRead)}

	list := operationsRequest(t, handler, jwtManager, auth.AudiencePlatform, permissions,
		http.MethodGet, OperationsRootPath)
	if list.Code != http.StatusOK ||
		!strings.Contains(list.Body.String(), "No privacy incidents have been recorded.") {
		t.Fatalf("empty list status = %d, body = %s", list.Code, list.Body.String())
	}

	unknown := operationsRequest(t, handler, jwtManager, auth.AudiencePlatform, permissions,
		http.MethodGet, OperationsRootPath+"/11111111-1111-4111-8111-111111111111")
	if unknown.Code != http.StatusNotFound ||
		!strings.Contains(unknown.Body.String(), "The requested privacy incident could not be found.") {
		t.Fatalf("unknown status = %d, body = %s", unknown.Code, unknown.Body.String())
	}

	invalid := operationsRequest(t, handler, jwtManager, auth.AudiencePlatform, permissions,
		http.MethodGet, OperationsRootPath+"/not-a-uuid")
	if invalid.Code != http.StatusNotFound || store.getCalls != 1 {
		t.Fatalf("invalid status = %d, get calls = %d", invalid.Code, store.getCalls)
	}
}

func TestOperationsExportAliasesMatchBearerAPI(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	incidentID := "11111111-1111-4111-8111-111111111111"
	notificationID := "33333333-3333-4333-8333-333333333333"
	store := &fakeStore{
		getIncident: Incident{ID: incidentID, PublicID: "BR-TEST01"},
		getNotifications: []ControllerNotification{{
			ID: notificationID, BreachIncidentID: incidentID,
			NotificationKind: ControllerNotificationInitial, Sequence: 1,
		}},
	}
	operationsHandler, jwtManager := newOperationsTestHandler(t, store, now)
	bearerRouter := chi.NewRouter()
	RegisterPlatformRoutes(bearerRouter, HTTPOptions{
		Store: store, JWT: jwtManager, Now: func() time.Time { return now },
	})
	permissions := []string{string(auth.PermissionPlatformBreachRead)}
	accessToken, _, err := jwtManager.Issue("platform-admin-id", auth.AudiencePlatform, permissions)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	tests := []struct {
		name           string
		operationsPath string
		bearerPath     string
	}{
		{
			name: "post-incident JSON",
			operationsPath: OperationsRootPath + "/" + incidentID +
				"/post-incident-report?format=json",
			bearerPath: "/admin/breach-incidents/" + incidentID +
				"/post-incident-report?format=json",
		},
		{
			name: "notification Markdown",
			operationsPath: operationsNotificationExportURL(
				incidentID, notificationID, ExportFormatMarkdown,
			),
			bearerPath: "/admin/breach-incidents/" + incidentID +
				"/controller-notifications/" + notificationID + "/export?format=markdown",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			operationsResponse := operationsRequest(
				t, operationsHandler, jwtManager, auth.AudiencePlatform, permissions,
				http.MethodGet, test.operationsPath,
			)
			bearerResponse := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.bearerPath, nil)
			request.Header.Set("Authorization", "Bearer "+accessToken)
			bearerRouter.ServeHTTP(bearerResponse, request)

			if operationsResponse.Code != http.StatusOK ||
				bearerResponse.Code != http.StatusOK ||
				operationsResponse.Body.String() != bearerResponse.Body.String() ||
				operationsResponse.Header().Get("Content-Disposition") !=
					bearerResponse.Header().Get("Content-Disposition") ||
				operationsResponse.Header().Get("Content-Length") !=
					bearerResponse.Header().Get("Content-Length") {
				t.Errorf(
					"operations status/body/headers = %d/%q/%v; bearer = %d/%q/%v",
					operationsResponse.Code, operationsResponse.Body.String(), operationsResponse.Header(),
					bearerResponse.Code, bearerResponse.Body.String(), bearerResponse.Header(),
				)
			}

			head := operationsRequest(
				t, operationsHandler, jwtManager, auth.AudiencePlatform, permissions,
				http.MethodHead, test.operationsPath,
			)
			if head.Code != http.StatusOK || head.Body.Len() != 0 ||
				head.Header().Get("Content-Length") != operationsResponse.Header().Get("Content-Length") {
				t.Errorf("HEAD status/body/headers = %d/%q/%v", head.Code, head.Body.String(), head.Header())
			}
		})
	}
}
