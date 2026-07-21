package middleware_test

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

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTenantTransactionFlushesOnlyAfterCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		commitErr  error
		wantStatus int
		wantBody   string
	}{
		{name: "commit succeeds", wantStatus: http.StatusCreated, wantBody: "created"},
		{
			name: "commit fails", commitErr: errors.New("connection lost"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"error":{"code":"service_unavailable","message":"service unavailable"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := &fakeTransaction{commitErr: tt.commitErr}
			resolver := tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
				return tenant.Tenant{
					ID: "0762af3d-01b4-452f-81ad-dabf7652395f", Name: "Acme", Slug: slug,
					Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
				}, nil
			})
			middleware := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
				BaseDomain: "localhost",
				Resolver:   resolver,
				Begin: func(context.Context) (db.Transaction, error) {
					return tx, nil
				},
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resolved, ok := appmiddleware.TenantFromContext(r.Context())
				if !ok || resolved.Slug != "acme" {
					t.Errorf("tenant context = %#v, %t", resolved, ok)
				}
				if _, ok := appmiddleware.QuerierFromContext(r.Context()); !ok {
					t.Error("transaction querier missing from context")
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("created"))
			}))

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "http://acme.localhost/v1/projects", nil)
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if got := strings.TrimSpace(recorder.Body.String()); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
			if tx.configuredTenant != "0762af3d-01b4-452f-81ad-dabf7652395f" {
				t.Errorf("configured tenant = %q", tx.configuredTenant)
			}
			if tx.commitCalls != 1 {
				t.Errorf("commit calls = %d, want 1", tx.commitCalls)
			}
		})
	}
}

func TestRequireActiveBlocksSuspendedCancelledAndExpiredTrials(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name       string
		status     tenant.Status
		trialEnd   *time.Time
		wantStatus int
	}{
		{name: "active", status: tenant.StatusActive, wantStatus: http.StatusNoContent},
		{name: "valid trial", status: tenant.StatusTrial, trialEnd: timePointer(now.Add(time.Hour)), wantStatus: http.StatusNoContent},
		{name: "expired trial", status: tenant.StatusTrial, trialEnd: timePointer(now.Add(-time.Hour)), wantStatus: http.StatusForbidden},
		{name: "suspended", status: tenant.StatusSuspended, wantStatus: http.StatusForbidden},
		{name: "cancelled", status: tenant.StatusCancelled, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeTransaction{}
			resolver := tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
				return tenant.Tenant{
					ID: "0762af3d-01b4-452f-81ad-dabf7652395f", Slug: slug,
					Status: tt.status, TrialEndsAt: tt.trialEnd,
				}, nil
			})
			resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
				BaseDomain: "localhost", Resolver: resolver,
				Begin:  func(context.Context) (db.Transaction, error) { return tx, nil },
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			handler := resolve(appmiddleware.RequireActive(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			)))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://acme.localhost/v1/projects", nil))

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusForbidden && !strings.Contains(recorder.Body.String(), `"code":"tenant_suspended"`) {
				t.Errorf("body = %q, want tenant_suspended error", recorder.Body.String())
			}
		})
	}
}

func TestRequireWritableBlocksDeletionGrace(t *testing.T) {
	now := time.Now()
	resolver := tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
		return tenant.Tenant{
			ID: "0762af3d-01b4-452f-81ad-dabf7652395f", Slug: "acme",
			Status: tenant.StatusActive, DeletionRequestedAt: &now,
		}, nil
	})
	resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: resolver,
		Begin:  func(context.Context) (db.Transaction, error) { return &fakeTransaction{}, nil },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	handler := resolve(appmiddleware.RequireWritable(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://acme.localhost/v1/projects", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestTenantTransactionFailsClosedBeforeHandlerExecution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		host        string
		resolverErr error
		beginErr    error
		contextErr  error
		wantStatus  int
	}{
		{name: "invalid host", host: "api.localhost", wantStatus: http.StatusNotFound},
		{name: "unknown tenant", host: "acme.localhost", resolverErr: tenant.ErrUnknownTenant, wantStatus: http.StatusNotFound},
		{name: "resolver unavailable", host: "acme.localhost", resolverErr: errors.New("database unavailable"), wantStatus: http.StatusServiceUnavailable},
		{name: "transaction unavailable", host: "acme.localhost", beginErr: errors.New("pool exhausted"), wantStatus: http.StatusServiceUnavailable},
		{name: "tenant context rejected", host: "acme.localhost", contextErr: errors.New("set_config failed"), wantStatus: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handlerCalled := false
			tx := &fakeTransaction{queryRowErr: tt.contextErr}
			middleware := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
				BaseDomain: "localhost",
				Resolver: tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
					if tt.resolverErr != nil {
						return tenant.Tenant{}, tt.resolverErr
					}
					return tenant.Tenant{ID: "0762af3d-01b4-452f-81ad-dabf7652395f", Slug: slug}, nil
				}),
				Begin: func(context.Context) (db.Transaction, error) {
					if tt.beginErr != nil {
						return nil, tt.beginErr
					}
					return tx, nil
				},
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusNoContent)
			}))
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/v1/test", nil)
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if handlerCalled {
				t.Error("handler executed after tenant setup failure")
			}
		})
	}
}

func TestTenantTransactionRollsBackErrorResponsesAndPanics(t *testing.T) {
	t.Parallel()

	newMiddleware := func(tx *fakeTransaction) func(http.Handler) http.Handler {
		return appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
			BaseDomain: "localhost",
			Resolver: tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
				return tenant.Tenant{ID: "0762af3d-01b4-452f-81ad-dabf7652395f", Slug: slug}, nil
			}),
			Begin:  func(context.Context) (db.Transaction, error) { return tx, nil },
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}

	t.Run("error response", func(t *testing.T) {
		tx := &fakeTransaction{}
		handler := newMiddleware(tx)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "invalid", http.StatusUnprocessableEntity)
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://acme.localhost/v1/test", nil))
		if tx.rollbackCalls != 1 || tx.commitCalls != 0 || recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("rollback=%d commit=%d status=%d", tx.rollbackCalls, tx.commitCalls, recorder.Code)
		}
	})

	t.Run("panic", func(t *testing.T) {
		tx := &fakeTransaction{}
		handler := newMiddleware(tx)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("handler panic")
		}))
		defer func() {
			if recover() == nil {
				t.Error("panic was not propagated to recovery middleware")
			}
			if tx.rollbackCalls != 1 || tx.commitCalls != 0 {
				t.Errorf("rollback=%d commit=%d", tx.rollbackCalls, tx.commitCalls)
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "http://acme.localhost/v1/test", nil))
	})
}

func TestRequestLoggerIncludesResolvedTenant(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	tx := &fakeTransaction{}
	resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost",
		Resolver: tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
			return tenant.Tenant{ID: "0762af3d-01b4-452f-81ad-dabf7652395f", Slug: slug}, nil
		}),
		Begin:  func(context.Context) (db.Transaction, error) { return tx, nil },
		Logger: logger,
	})
	handler := appmiddleware.RequestID(appmiddleware.RequestLogger(logger)(resolve(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
	))))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://acme.localhost/v1/test", nil))

	if !strings.Contains(output.String(), `"tenant_id":"0762af3d-01b4-452f-81ad-dabf7652395f"`) {
		t.Errorf("log = %s, want tenant_id", output.String())
	}
	if !strings.Contains(output.String(), `"tenant_slug":"acme"`) {
		t.Errorf("log = %s, want tenant_slug", output.String())
	}
}

func TestTenantPostCommitHooksRunOnlyAfterCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hookErr    error
		status     int
		wantStatus int
		wantCalls  int
	}{
		{name: "success", status: http.StatusNoContent, wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "hook failure", hookErr: errors.New("queue unavailable"), status: http.StatusNoContent, wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "handler failure", status: http.StatusBadRequest, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeTransaction{}
			hookCalls := 0
			resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
				BaseDomain: "localhost",
				Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
					return tenant.Tenant{ID: "tenant-id", Slug: "acme"}, nil
				}),
				Begin: func(context.Context) (db.Transaction, error) { return tx, nil },
			})
			handler := resolve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !appmiddleware.AfterTenantCommit(r.Context(), func(context.Context) error {
					hookCalls++
					if tx.commitCalls != 1 {
						t.Errorf("hook ran before commit; commit calls = %d", tx.commitCalls)
					}
					return tt.hookErr
				}) {
					t.Error("AfterTenantCommit() was not registered")
				}
				w.WriteHeader(tt.status)
			}))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://acme.localhost/auth/login", nil))
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if hookCalls != tt.wantCalls {
				t.Errorf("hook calls = %d, want %d", hookCalls, tt.wantCalls)
			}
		})
	}
	if appmiddleware.AfterTenantCommit(context.Background(), func(context.Context) error { return nil }) {
		t.Error("AfterTenantCommit() registered without tenant transaction context")
	}
}

func TestTenantTransactionCanCommitSecuritySideEffectWithErrorResponse(t *testing.T) {
	t.Parallel()
	tx := &fakeTransaction{}
	resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost",
		Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
			return tenant.Tenant{ID: "tenant-id", Slug: "acme"}, nil
		}),
		Begin: func(context.Context) (db.Transaction, error) { return tx, nil },
	})
	handler := resolve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !appmiddleware.CommitTenantTransactionOnError(r.Context()) {
			t.Error("CommitTenantTransactionOnError() returned false")
		}
		http.Error(w, "authentication failed", http.StatusUnauthorized)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://acme.localhost/auth/refresh", nil))
	if recorder.Code != http.StatusUnauthorized || tx.commitCalls != 1 || tx.rollbackCalls != 0 {
		t.Errorf("status=%d commits=%d rollbacks=%d", recorder.Code, tx.commitCalls, tx.rollbackCalls)
	}
	if appmiddleware.CommitTenantTransactionOnError(context.Background()) {
		t.Error("commit-on-error enabled outside tenant transaction")
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

type fakeTransaction struct {
	commitErr        error
	queryRowErr      error
	commitCalls      int
	rollbackCalls    int
	configuredTenant string
}

func (f *fakeTransaction) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeTransaction) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (f *fakeTransaction) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if !strings.Contains(query, "set_config") || len(args) != 1 {
		return fakeRow{err: errors.New("unexpected QueryRow call")}
	}
	if f.queryRowErr != nil {
		return fakeRow{err: f.queryRowErr}
	}
	f.configuredTenant, _ = args[0].(string)
	return fakeRow{value: f.configuredTenant}
}

func (f *fakeTransaction) Commit(context.Context) error {
	f.commitCalls++
	return f.commitErr
}

func (f *fakeTransaction) Rollback(context.Context) error {
	f.rollbackCalls++
	return nil
}

type fakeRow struct {
	value string
	err   error
}

func (r fakeRow) Scan(destinations ...any) error {
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
