package abuse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestParseDiagnosticsQueryDefaultsAndBoundsFilters(t *testing.T) {
	t.Parallel()

	defaults, fields := parseDiagnosticsQuery(url.Values{})
	if len(fields) != 0 || defaults.Range != "7d" || defaults.RangeHours != 168 {
		t.Fatalf("default query = %#v, fields = %#v", defaults, fields)
	}
	request := httptest.NewRequest("GET",
		"/v1/abuse/diagnostics?range=24h&project_id=%7B00000000-0000-4000-8000-000000000001%7D",
		nil,
	)
	filtered, fields := parseDiagnosticsQuery(request.URL.Query())
	if len(fields) != 0 || filtered.RangeHours != 24 ||
		filtered.ProjectID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("filtered query = %#v, fields = %#v", filtered, fields)
	}
}

func TestParseDiagnosticsQueryRejectsUnknownRangeAndProject(t *testing.T) {
	t.Parallel()

	query, fields := parseDiagnosticsQuery(url.Values{
		"range": {"90d"}, "project_id": {"not-a-project"},
	})
	if query.RangeHours != 168 || fields["range"] == "" || fields["project_id"] == "" {
		t.Fatalf("query = %#v, fields = %#v", query, fields)
	}
}

func TestSummarizeDiagnosticsReturnsEveryOperationalCategory(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	workspace := summarizeDiagnostics(
		DiagnosticsQuery{Range: "24h", RangeHours: 24, ProjectID: "project"},
		start,
		end,
		[]DiagnosticPoint{
			{Category: CategoryRateLimit, Count: 3},
			{Category: CategoryProvider, Count: 2},
		},
	)
	if workspace.Total != 5 || len(workspace.Totals) != 6 || len(workspace.Trend) != 2 ||
		workspace.Filters.ProjectID != "project" {
		t.Fatalf("workspace = %#v", workspace)
	}
	if workspace.Totals[0].Category != CategoryRateLimit || workspace.Totals[0].Count != 3 ||
		workspace.Totals[5].Category != CategoryProvider || workspace.Totals[5].Count != 2 {
		t.Errorf("totals = %#v", workspace.Totals)
	}
}

func TestTenantDiagnosticsHTTPAuthorizesAndBoundsOperationalData(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 23, 12, 34, 0, 0, time.UTC)
	projectID := "00000000-0000-4000-8000-000000000001"
	store := &diagnosticsHTTPStoreStub{points: []DiagnosticPoint{{
		ProjectID: projectID, BucketStart: now.Truncate(time.Hour),
		Category: CategoryRateLimit, Reason: ReasonSubmissionRateLimited, Count: 4,
	}}}
	handler, token := diagnosticsHTTPHandler(t, store, []string{
		string(auth.PermissionTenantRead),
	}, now)

	response := diagnosticsRequest(handler, token,
		"/v1/abuse/diagnostics?range=24h&project_id="+projectID)
	if response.Code != http.StatusOK {
		t.Fatalf("diagnostics status = %d, body = %s", response.Code, response.Body.String())
	}
	var workspace DiagnosticsWorkspace
	if err := json.NewDecoder(response.Body).Decode(&workspace); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	expectedEnd := now.Truncate(time.Hour).Add(time.Hour)
	if workspace.Range != "24h" || workspace.Total != 4 ||
		workspace.Filters.ProjectID != projectID || len(workspace.Totals) != 6 ||
		store.calls != 1 || store.tenantID != diagnosticsTenantID ||
		!store.start.Equal(expectedEnd.Add(-24*time.Hour)) || !store.end.Equal(expectedEnd) {
		t.Fatalf("workspace = %#v, store = %#v", workspace, store)
	}

	invalid := diagnosticsRequest(handler, token, "/v1/abuse/diagnostics?range=forever")
	if invalid.Code != http.StatusUnprocessableEntity || store.calls != 1 {
		t.Fatalf("invalid diagnostics status = %d, calls = %d", invalid.Code, store.calls)
	}
	unauthenticated := diagnosticsRequest(handler, "", "/v1/abuse/diagnostics")
	if unauthenticated.Code != http.StatusUnauthorized || store.calls != 1 {
		t.Fatalf("unauthenticated status = %d, calls = %d", unauthenticated.Code, store.calls)
	}
}

func TestTenantDiagnosticsHTTPHandlesPermissionAndStoreFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	forbiddenStore := &diagnosticsHTTPStoreStub{}
	forbiddenHandler, forbiddenToken := diagnosticsHTTPHandler(t, forbiddenStore, nil, now)
	forbidden := diagnosticsRequest(forbiddenHandler, forbiddenToken, "/v1/abuse/diagnostics")
	if forbidden.Code != http.StatusForbidden || forbiddenStore.calls != 0 {
		t.Fatalf("forbidden status = %d, calls = %d", forbidden.Code, forbiddenStore.calls)
	}

	failingStore := &diagnosticsHTTPStoreStub{err: errors.New("database unavailable")}
	failingHandler, token := diagnosticsHTTPHandler(t, failingStore, []string{
		string(auth.PermissionTenantRead),
	}, now)
	unavailable := diagnosticsRequest(failingHandler, token, "/v1/abuse/diagnostics")
	if unavailable.Code != http.StatusServiceUnavailable || failingStore.calls != 1 {
		t.Fatalf("unavailable status = %d, calls = %d", unavailable.Code, failingStore.calls)
	}

	missingContext := httptest.NewRecorder()
	(&tenantHTTPHandler{options: TenantHTTPOptions{Store: failingStore}}).list(
		missingContext,
		httptest.NewRequest(http.MethodGet, "/v1/abuse/diagnostics", nil),
	)
	if missingContext.Code != http.StatusInternalServerError {
		t.Fatalf("missing context status = %d", missingContext.Code)
	}
}

const diagnosticsTenantID = "00000000-0000-4000-8000-000000000010"

type diagnosticsHTTPStoreStub struct {
	points    []DiagnosticPoint
	err       error
	calls     int
	tenantID  string
	projectID string
	start     time.Time
	end       time.Time
}

func (store *diagnosticsHTTPStoreStub) List(
	_ context.Context,
	_ db.Querier,
	tenantID string,
	projectID string,
	start time.Time,
	end time.Time,
) ([]DiagnosticPoint, error) {
	store.calls++
	store.tenantID, store.projectID = tenantID, projectID
	store.start, store.end = start, end
	return store.points, store.err
}

func diagnosticsHTTPHandler(
	t *testing.T,
	store TenantHTTPStore,
	permissions []string,
	now time.Time,
) (http.Handler, string) {
	t.Helper()
	manager, err := auth.NewJWTManager(strings.Repeat("d", 32), "fidibeki", time.Hour)
	if err != nil {
		t.Fatalf("create diagnostics JWT: %v", err)
	}
	token, _, err := manager.IssueTenant("member-id", diagnosticsTenantID, permissions)
	if err != nil {
		t.Fatalf("issue diagnostics JWT: %v", err)
	}
	resolved := tenant.Tenant{
		ID: diagnosticsTenantID, Name: "Acme", Slug: "acme",
		Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
	}
	router := chi.NewRouter()
	RegisterTenantRoutes(router, TenantHTTPOptions{
		Store: store, JWT: manager, Now: func() time.Time { return now },
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost",
		Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
			return resolved, nil
		}),
		Begin: func(context.Context) (db.Transaction, error) {
			return &diagnosticsHTTPTransaction{
				tenantID: diagnosticsTenantID, permissions: permissions,
			}, nil
		},
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(router)
	return handler, token
}

func diagnosticsRequest(handler http.Handler, token, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://acme.localhost"+path, nil)
	request.Host = "acme.localhost"
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	handler.ServeHTTP(response, request)
	return response
}

type diagnosticsHTTPTransaction struct {
	tenantID    string
	permissions []string
}

func (*diagnosticsHTTPTransaction) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (*diagnosticsHTTPTransaction) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected diagnostics query")
}

func (transaction *diagnosticsHTTPTransaction) QueryRow(
	_ context.Context,
	query string,
	_ ...any,
) pgx.Row {
	if strings.Contains(query, "FROM tenant_members") {
		return diagnosticsHTTPRow{scan: func(destinations ...any) error {
			permissions, permissionsOK := destinations[0].(*[]string)
			status, statusOK := destinations[1].(*string)
			version, versionOK := destinations[2].(*int)
			if !permissionsOK || !statusOK || !versionOK {
				return errors.New("unexpected member destination")
			}
			*permissions = append([]string(nil), transaction.permissions...)
			*status, *version = "active", 0
			return nil
		}}
	}
	return diagnosticsHTTPRow{scan: func(destinations ...any) error {
		configured, ok := destinations[0].(*string)
		if !ok {
			return errors.New("unexpected tenant destination")
		}
		*configured = transaction.tenantID
		return nil
	}}
}

func (*diagnosticsHTTPTransaction) Commit(context.Context) error   { return nil }
func (*diagnosticsHTTPTransaction) Rollback(context.Context) error { return nil }

type diagnosticsHTTPRow struct {
	scan func(...any) error
}

func (row diagnosticsHTTPRow) Scan(destinations ...any) error {
	return row.scan(destinations...)
}
