package billing

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTenantBillingRoutesDuringDeletionGrace(t *testing.T) {
	t.Parallel()

	deletionRequestedAt := time.Now()
	resolved := tenant.Tenant{
		ID: "0762af3d-01b4-452f-81ad-dabf7652395f", Name: "Acme", Slug: "acme",
		Plan: tenant.PlanGrowth, Status: tenant.StatusActive, DeletionRequestedAt: &deletionRequestedAt,
	}
	jwt, err := auth.NewJWTManager(strings.Repeat("x", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	manageToken, _, err := jwt.IssueTenant("member-id", resolved.ID, []string{
		string(auth.PermissionBillingManage),
	})
	if err != nil {
		t.Fatalf("issue manage-only JWT: %v", err)
	}

	newHandler := func(store *serviceStoreStub, provider *providerStub) http.Handler {
		service := Service{
			Store: store, Provider: provider,
			Prices:        PriceCatalog{tenant.PlanGrowth: "price_growth"},
			PublicBaseURL: "http://api.localhost:8088", BaseDomain: "localhost",
		}
		router := chi.NewRouter()
		RegisterTenantRoutes(router, HTTPOptions{Service: service, JWT: jwt})
		return appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
			BaseDomain: "localhost",
			Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
				return resolved, nil
			}),
			Begin:  func(context.Context) (db.Transaction, error) { return &billingHTTPTransaction{}, nil },
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})(router)
	}
	request := func(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, "http://acme.localhost"+path, bytes.NewBufferString(body))
		req.Host = "acme.localhost"
		req.Header.Set("Authorization", "Bearer "+manageToken)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{name: "checkout", path: "/v1/billing/checkout", body: `{"plan":"growth"}`},
		{name: "portal", path: "/v1/billing/portal"},
		{name: "delete account", path: "/v1/billing/delete-account", body: `{"confirmation":"acme"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceStoreStub{tenant: Tenant{
				ID: resolved.ID, Name: resolved.Name, Slug: resolved.Slug, Plan: resolved.Plan,
				Status: resolved.Status, DeletionRequestedAt: &deletionRequestedAt,
			}}
			provider := &providerStub{}
			response := request(newHandler(store, provider), http.MethodPost, test.path, test.body)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
			}
			if store.getCalls != 0 || store.saveCustomerCalls != 0 || store.requestDeletionCalls != 0 || store.cancelDeletionCalls != 0 {
				t.Errorf("store calls: get=%d save_customer=%d request_deletion=%d cancel_deletion=%d", store.getCalls, store.saveCustomerCalls, store.requestDeletionCalls, store.cancelDeletionCalls)
			}
			if provider.createCustomerCalls != 0 || provider.createCheckoutCalls != 0 || provider.createPortalCalls != 0 {
				t.Errorf("provider calls: customer=%d checkout=%d portal=%d", provider.createCustomerCalls, provider.createCheckoutCalls, provider.createPortalCalls)
			}
		})
	}

	t.Run("manage-only member can read billing and cancel deletion", func(t *testing.T) {
		store := &serviceStoreStub{tenant: Tenant{
			ID: resolved.ID, Name: resolved.Name, Slug: resolved.Slug, Plan: resolved.Plan,
			Status: resolved.Status, DeletionRequestedAt: &deletionRequestedAt,
		}}
		handler := newHandler(store, &providerStub{})
		if response := request(handler, http.MethodGet, "/v1/billing", ""); response.Code != http.StatusOK {
			t.Fatalf("GET billing status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
		}
		if response := request(handler, http.MethodPost, "/v1/billing/cancel-deletion", ""); response.Code != http.StatusNoContent {
			t.Fatalf("cancel deletion status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
		}
		if store.cancelDeletionCalls != 1 {
			t.Fatalf("cancel deletion calls = %d, want 1", store.cancelDeletionCalls)
		}
	})
}

type billingHTTPTransaction struct{}

func (*billingHTTPTransaction) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (*billingHTTPTransaction) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (*billingHTTPTransaction) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if !strings.Contains(query, "set_config") || len(args) != 1 {
		return billingHTTPRow{err: errors.New("unexpected QueryRow call")}
	}
	configuredTenant, _ := args[0].(string)
	return billingHTTPRow{value: configuredTenant}
}

func (*billingHTTPTransaction) Commit(context.Context) error   { return nil }
func (*billingHTTPTransaction) Rollback(context.Context) error { return nil }

type billingHTTPRow struct {
	value string
	err   error
}

func (r billingHTTPRow) Scan(destinations ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected scan destination count")
	}
	destination, ok := destinations[0].(*string)
	if !ok {
		return errors.New("unexpected scan destination type")
	}
	*destination = r.value
	return nil
}
