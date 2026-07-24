package breachregister

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/go-chi/chi/v5"
)

const (
	exportIncidentID     = "11111111-1111-4111-8111-111111111111"
	exportNotificationID = "22222222-2222-4222-8222-222222222222"
)

func exportFixtureStore() *fakeStore {
	providerID := "provider-msg-1"
	return &fakeStore{
		getIncident: Incident{ID: exportIncidentID, PublicID: "BR-ABCDEF12"},
		getEntries:  []ChronologyEntry{{ID: "entry-1", EntryKind: EntryDetected, Summary: "Detected."}},
		getProviderIntakes: []ProviderIncidentIntake{{
			ID: "provider-intake-1", Sequence: 1, BreachIncidentID: exportIncidentID,
			ProviderID: "resend_transactional_email", GovernanceRegistryVersion: 1,
			ProviderDisplayName: "Resend transactional email", BoundaryKind: "service_provider",
			ApprovalStatus: "pending", ContractStatus: "unknown",
			PrimaryRetentionCategory:   "external_communications",
			ProviderNoticeReference:    "provider-case-123",
			ProviderReportedAt:         time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC),
			ReceivedAt:                 time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC),
			CreatedAt:                  time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC),
			Summary:                    "Provider reported a delivery incident.",
			EvidenceReferences:         []string{"case:123"},
			AffectedDataFlowCategories: []string{"external_communications"},
			AffectedTenantIDs:          []string{"tenant-1"},
			ActorPlatformAdminID:       "admin-id",
		}},
		getNotifications: []ControllerNotification{
			{
				ID: exportNotificationID, BreachIncidentID: exportIncidentID,
				NotificationKind: ControllerNotificationInitial, Sequence: 1,
				RecipientEmail: "privacy@acme.example", WorkspaceName: "Acme",
				Summary: "A bounded incident summary.", MeasuresTaken: "Access was revoked.",
				Status: ControllerNotificationSent, Attempts: 1,
				DeliveryAttempts: []ControllerNotificationAttempt{
					{ID: "attempt-1", NotificationID: exportNotificationID, AttemptNumber: 1,
						Status: ControllerNotificationAttemptSent, ProviderMessageID: &providerID},
				},
			},
		},
	}
}

// newExportTestHandlerWithClock is newTestHandler (http_test.go) plus a fixed
// HTTPOptions.Now. GET and HEAD requests to the same export route must
// render byte-identical bodies (so Content-Length matches between them), but
// generated_at is a live clock read on every request and Go's
// time.Time.MarshalJSON keeps a variable-precision fractional-second
// component -- two real-clock renders taken at different instants can
// legitimately differ in byte length. Freezing the clock removes that
// confound so a Content-Length mismatch in these tests reflects a real bug.
func newExportTestHandlerWithClock(
	t *testing.T, store *fakeStore, now time.Time,
) (http.Handler, *auth.JWTManager) {
	t.Helper()
	jwtManager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	router := chi.NewRouter()
	RegisterPlatformRoutes(router, HTTPOptions{
		Store: store, JWT: jwtManager, Now: func() time.Time { return now },
	})
	return router, jwtManager
}

func readTokenFor(t *testing.T, jwtManager *auth.JWTManager) string {
	t.Helper()
	token, _, err := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachRead)},
	)
	if err != nil {
		t.Fatalf("issue read token: %v", err)
	}
	return token
}

func appendOnlyTokenFor(t *testing.T, jwtManager *auth.JWTManager) string {
	t.Helper()
	token, _, err := jwtManager.Issue(
		"admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformBreachAppend)},
	)
	if err != nil {
		t.Fatalf("issue append token: %v", err)
	}
	return token
}

func TestExportControllerNotificationRequiresReadPermission(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + exportIncidentID +
		"/controller-notifications/" + exportNotificationID + "/export?format=json"

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, path, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", unauthenticated.Code)
	}

	tenantToken, _, err := jwtManager.Issue(
		"member-id", auth.AudienceTenant, []string{string(auth.PermissionPlatformBreachRead)},
	)
	if err != nil {
		t.Fatalf("issue tenant token: %v", err)
	}
	crossAudience := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+tenantToken)
	handler.ServeHTTP(crossAudience, request)
	if crossAudience.Code != http.StatusUnauthorized {
		t.Errorf("tenant-audience token status = %d", crossAudience.Code)
	}

	forbidden := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+appendOnlyTokenFor(t, jwtManager))
	handler.ServeHTTP(forbidden, request)
	if forbidden.Code != http.StatusForbidden {
		t.Errorf("append-only token status = %d", forbidden.Code)
	}

	ok := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+readTokenFor(t, jwtManager))
	handler.ServeHTTP(ok, request)
	if ok.Code != http.StatusOK {
		t.Fatalf("read token status = %d, body = %s", ok.Code, ok.Body.String())
	}
}

func TestExportControllerNotificationReturnsExactFrozenContentAndSafeHeaders(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newTestHandler(t, store)
	readToken := readTokenFor(t, jwtManager)
	path := "/admin/breach-incidents/" + exportIncidentID +
		"/controller-notifications/" + exportNotificationID + "/export?format=json"

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := recorder.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	wantLength := strconv.Itoa(recorder.Body.Len())
	if got := recorder.Header().Get("Content-Length"); got != wantLength {
		t.Errorf("Content-Length = %q, want %q (actual body length)", got, wantLength)
	}
	disposition := recorder.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment;") || !strings.Contains(disposition, "BR-ABCDEF12") ||
		!strings.HasSuffix(disposition, ".json\"") {
		t.Errorf("Content-Disposition = %q", disposition)
	}

	var decoded struct {
		SchemaVersion    int    `json:"schema_version"`
		IncidentPublicID string `json:"incident_public_id"`
		Notification     struct {
			ID               string `json:"id"`
			NotificationKind string `json:"notification_kind"`
			Sequence         int    `json:"sequence"`
			Summary          string `json:"summary"`
			MeasuresTaken    string `json:"measures_taken"`
			Status           string `json:"status"`
			DeliveryAttempts []struct {
				Status            string  `json:"status"`
				ProviderMessageID *string `json:"provider_message_id"`
			} `json:"delivery_attempts"`
		} `json:"notification"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode export body: %v", err)
	}
	if decoded.SchemaVersion != 1 || decoded.IncidentPublicID != "BR-ABCDEF12" {
		t.Errorf("wrapper = %#v", decoded)
	}
	notification := store.getNotifications[0]
	if decoded.Notification.ID != notification.ID ||
		decoded.Notification.NotificationKind != string(notification.NotificationKind) ||
		decoded.Notification.Sequence != notification.Sequence ||
		decoded.Notification.Summary != notification.Summary ||
		decoded.Notification.MeasuresTaken != notification.MeasuresTaken ||
		decoded.Notification.Status != string(notification.Status) {
		t.Fatalf("exported notification does not match frozen snapshot: %#v", decoded.Notification)
	}
	if len(decoded.Notification.DeliveryAttempts) != 1 ||
		decoded.Notification.DeliveryAttempts[0].Status != "sent" ||
		decoded.Notification.DeliveryAttempts[0].ProviderMessageID == nil ||
		*decoded.Notification.DeliveryAttempts[0].ProviderMessageID != "provider-msg-1" {
		t.Fatalf("exported delivery attempts = %#v", decoded.Notification.DeliveryAttempts)
	}
}

func TestExportControllerNotificationMarkdownFormat(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + exportIncidentID +
		"/controller-notifications/" + exportNotificationID + "/export?format=markdown"

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+readTokenFor(t, jwtManager))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "A bounded incident summary.") {
		t.Errorf("markdown body = %s", recorder.Body.String())
	}
	if !strings.HasSuffix(recorder.Header().Get("Content-Disposition"), ".md\"") {
		t.Errorf("Content-Disposition = %q", recorder.Header().Get("Content-Disposition"))
	}
}

func TestExportControllerNotificationValidatesFormatAndIDs(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newTestHandler(t, store)
	readToken := readTokenFor(t, jwtManager)

	cases := map[string]struct {
		path       string
		wantStatus int
	}{
		"missing format": {
			path:       "/admin/breach-incidents/" + exportIncidentID + "/controller-notifications/" + exportNotificationID + "/export",
			wantStatus: http.StatusUnprocessableEntity,
		},
		"invalid format": {
			path:       "/admin/breach-incidents/" + exportIncidentID + "/controller-notifications/" + exportNotificationID + "/export?format=pdf",
			wantStatus: http.StatusUnprocessableEntity,
		},
		"non-uuid incident": {
			path:       "/admin/breach-incidents/not-a-uuid/controller-notifications/" + exportNotificationID + "/export?format=json",
			wantStatus: http.StatusNotFound,
		},
		"non-uuid notification": {
			path:       "/admin/breach-incidents/" + exportIncidentID + "/controller-notifications/not-a-uuid/export?format=json",
			wantStatus: http.StatusNotFound,
		},
		"notification not in incident": {
			path:       "/admin/breach-incidents/" + exportIncidentID + "/controller-notifications/33333333-3333-4333-8333-333333333333/export?format=json",
			wantStatus: http.StatusNotFound,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			request.Header.Set("Authorization", "Bearer "+readToken)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != testCase.wantStatus {
				t.Errorf("status = %d, want %d, body = %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestExportControllerNotificationUnknownIncidentMapsStoreError(t *testing.T) {
	t.Parallel()
	store := &fakeStore{getErr: ErrNotFound}
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + exportIncidentID +
		"/controller-notifications/" + exportNotificationID + "/export?format=json"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+readTokenFor(t, jwtManager))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestExportControllerNotificationHeadMatchesGetHeadersWithEmptyBody(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newExportTestHandlerWithClock(
		t, store, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
	)
	readToken := readTokenFor(t, jwtManager)
	path := "/admin/breach-incidents/" + exportIncidentID +
		"/controller-notifications/" + exportNotificationID + "/export?format=json"

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, path, nil)
	getRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(getRecorder, getRequest)

	headRecorder := httptest.NewRecorder()
	headRequest := httptest.NewRequest(http.MethodHead, path, nil)
	headRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(headRecorder, headRequest)

	if headRecorder.Code != getRecorder.Code {
		t.Fatalf("HEAD status = %d, GET status = %d", headRecorder.Code, getRecorder.Code)
	}
	for _, header := range []string{
		"Content-Type", "Content-Disposition", "Content-Length", "Cache-Control",
		"X-Content-Type-Options", "Referrer-Policy",
	} {
		if headRecorder.Header().Get(header) != getRecorder.Header().Get(header) {
			t.Errorf("HEAD %s = %q, GET %s = %q", header, headRecorder.Header().Get(header), header, getRecorder.Header().Get(header))
		}
	}
	if headRecorder.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", headRecorder.Body.String())
	}
	if getRecorder.Body.Len() == 0 {
		t.Fatalf("GET body unexpectedly empty")
	}
	wantLength := strconv.Itoa(getRecorder.Body.Len())
	if getRecorder.Header().Get("Content-Length") != wantLength {
		t.Errorf("GET Content-Length = %q, want %q (actual body length)",
			getRecorder.Header().Get("Content-Length"), wantLength)
	}
	if headRecorder.Header().Get("Content-Length") != wantLength {
		t.Errorf("HEAD Content-Length = %q, want %q (matching GET's body length even though HEAD sends no body)",
			headRecorder.Header().Get("Content-Length"), wantLength)
	}
}

func TestExportPostIncidentReportRequiresReadPermission(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + exportIncidentID + "/post-incident-report?format=json"

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, path, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", unauthenticated.Code)
	}

	forbidden := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+appendOnlyTokenFor(t, jwtManager))
	handler.ServeHTTP(forbidden, request)
	if forbidden.Code != http.StatusForbidden {
		t.Errorf("append-only token status = %d", forbidden.Code)
	}

	ok := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+readTokenFor(t, jwtManager))
	handler.ServeHTTP(ok, request)
	if ok.Code != http.StatusOK {
		t.Fatalf("read token status = %d, body = %s", ok.Code, ok.Body.String())
	}
}

func TestExportPostIncidentReportIncludesPendingValuesAndLedger(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + exportIncidentID + "/post-incident-report?format=json"

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+readTokenFor(t, jwtManager))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	rawLatestAssessment, present := decoded["latest_assessment"]
	if !present {
		t.Fatalf("latest_assessment key omitted: %s", recorder.Body.String())
	}
	if rawLatestAssessment != nil {
		t.Errorf("latest_assessment = %#v, want explicit null (no assessment staged)", rawLatestAssessment)
	}
	notifications, ok := decoded["controller_notifications"].([]any)
	if !ok || len(notifications) != 1 {
		t.Fatalf("controller_notifications = %#v", decoded["controller_notifications"])
	}
	chronology, ok := decoded["chronology"].([]any)
	if !ok || len(chronology) != 1 {
		t.Fatalf("chronology = %#v", decoded["chronology"])
	}
	providerIntakes, ok := decoded["provider_incident_intakes"].([]any)
	if !ok || len(providerIntakes) != 1 {
		t.Fatalf("provider_incident_intakes = %#v", decoded["provider_incident_intakes"])
	}
	intake, ok := providerIntakes[0].(map[string]any)
	if !ok || intake["provider_notice_reference"] != "provider-case-123" ||
		intake["provider_id"] != "resend_transactional_email" {
		t.Fatalf("provider incident intake = %#v", providerIntakes[0])
	}
}

func TestExportPostIncidentReportMarkdownFormat(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newTestHandler(t, store)
	path := "/admin/breach-incidents/" + exportIncidentID + "/post-incident-report?format=markdown"

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+readTokenFor(t, jwtManager))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "assessment_pending") {
		t.Errorf("markdown missing pending assessment marker: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "provider-case-123") {
		t.Errorf("markdown missing provider intake: %s", recorder.Body.String())
	}
}

func TestExportPostIncidentReportValidatesFormatAndIncidentID(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newTestHandler(t, store)
	readToken := readTokenFor(t, jwtManager)

	invalidFormat := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet, "/admin/breach-incidents/"+exportIncidentID+"/post-incident-report?format=pdf", nil,
	)
	request.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(invalidFormat, request)
	if invalidFormat.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid format status = %d", invalidFormat.Code)
	}

	nonUUID := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/admin/breach-incidents/not-a-uuid/post-incident-report?format=json", nil)
	request.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(nonUUID, request)
	if nonUUID.Code != http.StatusNotFound {
		t.Errorf("non-uuid incident status = %d", nonUUID.Code)
	}

	store.getErr = ErrNotFound
	unknown := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/admin/breach-incidents/"+exportIncidentID+"/post-incident-report?format=json", nil)
	request.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(unknown, request)
	if unknown.Code != http.StatusNotFound {
		t.Errorf("unknown incident status = %d", unknown.Code)
	}
}

func TestExportPostIncidentReportHeadMatchesGetHeadersWithEmptyBody(t *testing.T) {
	t.Parallel()
	store := exportFixtureStore()
	handler, jwtManager := newExportTestHandlerWithClock(
		t, store, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
	)
	readToken := readTokenFor(t, jwtManager)
	path := "/admin/breach-incidents/" + exportIncidentID + "/post-incident-report?format=json"

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, path, nil)
	getRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(getRecorder, getRequest)

	headRecorder := httptest.NewRecorder()
	headRequest := httptest.NewRequest(http.MethodHead, path, nil)
	headRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(headRecorder, headRequest)

	if headRecorder.Code != getRecorder.Code {
		t.Fatalf("HEAD status = %d, GET status = %d", headRecorder.Code, getRecorder.Code)
	}
	for _, header := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
		if headRecorder.Header().Get(header) != getRecorder.Header().Get(header) {
			t.Errorf("HEAD %s = %q, GET %s = %q", header, headRecorder.Header().Get(header), header, getRecorder.Header().Get(header))
		}
	}
	if headRecorder.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", headRecorder.Body.String())
	}
	if getRecorder.Body.Len() == 0 {
		t.Fatalf("GET body unexpectedly empty")
	}
	wantLength := strconv.Itoa(getRecorder.Body.Len())
	if getRecorder.Header().Get("Content-Length") != wantLength {
		t.Errorf("GET Content-Length = %q, want %q (actual body length)",
			getRecorder.Header().Get("Content-Length"), wantLength)
	}
}
