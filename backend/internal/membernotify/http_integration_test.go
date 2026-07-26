package membernotify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/membernotify"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type preferencesEnvelope struct {
	Preferences membernotify.Preferences `json:"notification_preferences"`
	Categories  []string                 `json:"categories"`
	Modes       map[string][]string      `json:"supported_modes"`
	Deliveries  []membernotify.Delivery  `json:"deliveries"`
}

// The preference routes are self-scoped: a member reads and writes their own row
// and there is no way to name another member, so this exercises the whole
// authenticated path including the tenant transaction and RLS.
func TestPreferenceRoutesAreSelfScoped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root, directURL := preferenceHTTPDatabase(t, ctx)

	tenantID := seedPreferenceTenant(t, ctx, root, "prefs")
	otherTenantID := seedPreferenceTenant(t, ctx, root, "prefs-other")
	memberID := seedPreferenceMember(t, ctx, root, tenantID, "member@prefs.test")
	colleagueID := seedPreferenceMember(t, ctx, root, tenantID, "colleague@prefs.test")
	outsiderID := seedPreferenceMember(t, ctx, root, otherTenantID, "outsider@prefs.test")

	appURL := preferenceAppRoleURL(t, directURL)
	pools, err := db.OpenPools(ctx, appURL, directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)

	jwtManager, err := auth.NewJWTManager(strings.Repeat("a", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	token := preferenceToken(t, jwtManager, memberID, tenantID)
	colleagueToken := preferenceToken(t, jwtManager, colleagueID, tenantID)
	outsiderToken := preferenceToken(t, jwtManager, outsiderID, otherTenantID)

	routes := chi.NewRouter()
	membernotify.RegisterRoutes(routes, membernotify.HTTPOptions{
		Store: membernotify.Store{}, JWT: jwtManager,
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)

	// A member who never opened the settings still gets the documented defaults.
	response := performPreferenceRequest(handler, http.MethodGet, "prefs", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("read preferences status = %d body=%s", response.Code, response.Body.String())
	}
	envelope := decodePreferences(t, response)
	if envelope.Preferences != withTimezone(membernotify.Defaults(), "UTC", "default") {
		t.Fatalf("default preferences = %+v", envelope.Preferences)
	}
	if len(envelope.Categories) != len(membernotify.Categories) {
		t.Fatalf("categories = %v", envelope.Categories)
	}
	for _, mode := range envelope.Modes[membernotify.CategoryFeedbackSummary] {
		if mode == membernotify.ModeImmediate {
			t.Fatal("the API offered immediate delivery for new-feedback summaries")
		}
	}

	saved := performPreferenceRequest(handler, http.MethodPatch, "prefs", token, `{
		"comments": "daily", "digest_hour": 19, "timezone": "Europe/Lisbon"
	}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("save preferences status = %d body=%s", saved.Code, saved.Body.String())
	}
	savedEnvelope := decodePreferences(t, saved)
	if savedEnvelope.Preferences.Comments != membernotify.ModeDaily ||
		savedEnvelope.Preferences.DigestHour != 19 ||
		savedEnvelope.Preferences.Timezone != "Europe/Lisbon" {
		t.Fatalf("saved preferences = %+v", savedEnvelope.Preferences)
	}

	// The route carries no member parameter, so a colleague's request can only
	// ever return their own untouched defaults.
	colleague := decodePreferences(
		t, performPreferenceRequest(handler, http.MethodGet, "prefs", colleagueToken, ""),
	)
	if colleague.Preferences.Comments != membernotify.Defaults().Comments {
		t.Fatalf("a save leaked into another member's preferences: %+v", colleague.Preferences)
	}

	// A member of another workspace resolves against their own tenant only.
	outsider := performPreferenceRequest(handler, http.MethodGet, "prefs-other", outsiderToken, "")
	if outsider.Code != http.StatusOK {
		t.Fatalf("other-tenant read status = %d body=%s", outsider.Code, outsider.Body.String())
	}
	if decodePreferences(t, outsider).Preferences.Comments != membernotify.Defaults().Comments {
		t.Fatal("a preference crossed a workspace boundary")
	}
	crossTenant := performPreferenceRequest(handler, http.MethodGet, "prefs", outsiderToken, "")
	if crossTenant.Code == http.StatusOK {
		t.Fatalf("a token from another workspace was accepted: %s", crossTenant.Body.String())
	}
}

func TestPreferenceRoutesRejectInvalidRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root, directURL := preferenceHTTPDatabase(t, ctx)
	tenantID := seedPreferenceTenant(t, ctx, root, "prefs-validation")
	memberID := seedPreferenceMember(t, ctx, root, tenantID, "member@validation.test")

	pools, err := db.OpenPools(ctx, preferenceAppRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwtManager, err := auth.NewJWTManager(strings.Repeat("b", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	token := preferenceToken(t, jwtManager, memberID, tenantID)

	routes := chi.NewRouter()
	membernotify.RegisterRoutes(routes, membernotify.HTTPOptions{
		Store: membernotify.Store{}, JWT: jwtManager,
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)

	cases := map[string]struct {
		body string
		want int
	}{
		// A mandatory message class is not a field, so naming one is a bad
		// request rather than a silently ignored key.
		"mandatory class":   {body: `{"billing":"off"}`, want: http.StatusBadRequest},
		"unknown field":     {body: `{"nonsense":true}`, want: http.StatusBadRequest},
		"malformed body":    {body: `{`, want: http.StatusBadRequest},
		"immediate summary": {body: `{"feedback_summary":"immediate"}`, want: http.StatusUnprocessableEntity},
		"invalid mode":      {body: `{"mentions":"hourly"}`, want: http.StatusUnprocessableEntity},
		"invalid hour":      {body: `{"digest_hour":24}`, want: http.StatusUnprocessableEntity},
		"invalid weekday":   {body: `{"digest_weekday":0}`, want: http.StatusUnprocessableEntity},
		"invalid timezone":  {body: `{"timezone":"Mars/Olympus"}`, want: http.StatusUnprocessableEntity},
	}
	for name, testCase := range cases {
		response := performPreferenceRequest(handler, http.MethodPatch, "prefs-validation", token, testCase.body)
		if response.Code != testCase.want {
			t.Fatalf("%s status = %d, want %d (body=%s)", name, response.Code, testCase.want, response.Body.String())
		}
	}

	// Nothing above may have been persisted.
	var rows int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM member_notification_preferences WHERE tenant_id = $1
	`, tenantID).Scan(&rows); err != nil {
		t.Fatalf("count preference rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("rejected requests persisted %d preference rows", rows)
	}
	unauthenticated := performPreferenceRequest(handler, http.MethodGet, "prefs-validation", "", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthenticated.Code)
	}
}

func performPreferenceRequest(
	handler http.Handler,
	method string,
	hostSlug string,
	token string,
	body string,
) *httptest.ResponseRecorder {
	target := "http://" + hostSlug + ".localhost/v1/notification-preferences"
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodePreferences(t *testing.T, response *httptest.ResponseRecorder) preferencesEnvelope {
	t.Helper()
	var envelope preferencesEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode preferences response: %v", err)
	}
	return envelope
}

func preferenceToken(
	t *testing.T,
	jwtManager *auth.JWTManager,
	memberID string,
	tenantID string,
) string {
	t.Helper()
	token, _, err := jwtManager.IssueTenant(memberID, tenantID, []string{"issue:read"})
	if err != nil {
		t.Fatalf("issue tenant token: %v", err)
	}
	return token
}

func seedPreferenceTenant(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	slug string,
) string {
	t.Helper()
	var tenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ($1, $2, 'growth', 'active') RETURNING id
	`, slug, slug).Scan(&tenantID); err != nil {
		t.Fatalf("create preference tenant: %v", err)
	}
	return tenantID
}

func seedPreferenceMember(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	tenantID string,
	address string,
) string {
	t.Helper()
	var memberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status)
		VALUES ($1, $2, ARRAY['issue:read'], 'active') RETURNING id
	`, tenantID, address).Scan(&memberID); err != nil {
		t.Fatalf("create preference member: %v", err)
	}
	return memberID
}

func preferenceAppRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}

// preferenceHTTPDatabase migrates a container and returns a root connection
// together with the direct URL, with the app role able to log in so the tenant
// transaction middleware runs under row-level security.
func preferenceHTTPDatabase(t *testing.T, ctx context.Context) (*pgx.Conn, string) {
	t.Helper()
	pool := digestTestDatabase(t, ctx)
	directURL := pool.Config().ConnString()
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(context.WithoutCancel(ctx)) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}
	return root, directURL
}
