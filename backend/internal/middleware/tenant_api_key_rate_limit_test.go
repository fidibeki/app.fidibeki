package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

func TestTenantAPIKeyRateLimitIsSharedAndChargedOncePerRequest(t *testing.T) {
	t.Parallel()

	manager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Minute)
	tx := &fakeTransaction{
		apiKeyID: "3b06d9d3-86c2-4439-9085-5172064b51ca", apiKeyRateLimit: 1,
	}
	resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost",
		Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
			return tenant.Tenant{
				ID:   "0762af3d-01b4-452f-81ad-dabf7652395f",
				Slug: "acme", Status: tenant.StatusActive,
			}, nil
		}),
		Begin: func(context.Context) (db.Transaction, error) { return tx, nil },
	})
	limiter := &tenantCredentialLimiter{counts: map[string]int{}}
	protected := appmiddleware.RequireTenantMember(manager)(
		// A duplicate auth layer must reuse the request-scoped decision.
		appmiddleware.RequireTenantMember(manager)(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
		)),
	)
	handler := appmiddleware.TenantAPIKeyRateLimit(
		appmiddleware.TenantAPIKeyRateLimitOptions{Limiter: limiter},
	)(resolve(protected))
	raw := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUV"

	first := performTenantAPIKeyRequest(handler, raw)
	if first.Code != http.StatusNoContent ||
		first.Header().Get("X-RateLimit-Limit") != "1" ||
		first.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("first response = %d headers=%v body=%s", first.Code, first.Header(), first.Body)
	}
	wantLimiterKey := "tenant-api-key:" + tx.apiKeyID
	if limiter.callCount(wantLimiterKey) != 1 || limiter.callCount(raw) != 0 {
		t.Fatalf("limiter calls = %#v, want one immutable-ID charge", limiter.snapshot())
	}

	second := performTenantAPIKeyRequest(handler, raw)
	if second.Code != http.StatusTooManyRequests ||
		second.Header().Get("Retry-After") == "" ||
		second.Header().Get("X-RateLimit-Limit") != "1" ||
		second.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("second response = %d headers=%v body=%s", second.Code, second.Header(), second.Body)
	}
	if limiter.callCount(wantLimiterKey) != 2 {
		t.Fatalf("limiter calls = %#v, want one charge per HTTP request", limiter.snapshot())
	}
}

func TestTenantAPIKeyRateLimitFailsClosed(t *testing.T) {
	t.Parallel()

	manager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Minute)
	tx := &fakeTransaction{apiKeyID: "key-id", apiKeyRateLimit: 10}
	resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost",
		Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
			return tenant.Tenant{ID: "tenant-id", Slug: "acme", Status: tenant.StatusActive}, nil
		}),
		Begin: func(context.Context) (db.Transaction, error) { return tx, nil },
	})
	protected := resolve(appmiddleware.RequireTenantMember(manager)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
	)))
	raw := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUV"

	for _, testCase := range []struct {
		name    string
		handler http.Handler
	}{
		{name: "missing limiter", handler: protected},
		{
			name: "limiter error",
			handler: appmiddleware.TenantAPIKeyRateLimit(
				appmiddleware.TenantAPIKeyRateLimitOptions{
					Limiter: &tenantCredentialLimiter{err: errors.New("redis unavailable")},
				},
			)(protected),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := performTenantAPIKeyRequest(testCase.handler, raw)
			if response.Code != http.StatusServiceUnavailable ||
				response.Header().Get("Retry-After") != "1" {
				t.Fatalf("response = %d headers=%v body=%s",
					response.Code, response.Header(), response.Body)
			}
		})
	}
}

func performTenantAPIKeyRequest(handler http.Handler, raw string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "http://acme.localhost/v1/probe", nil)
	request.Header.Set("Authorization", "Bearer "+raw)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type tenantCredentialLimiter struct {
	mu     sync.Mutex
	counts map[string]int
	err    error
}

func (l *tenantCredentialLimiter) Allow(
	_ context.Context,
	key string,
	window ratelimit.Window,
) (ratelimit.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return ratelimit.Decision{}, l.err
	}
	if l.counts == nil {
		l.counts = map[string]int{}
	}
	l.counts[key]++
	remaining := max(window.Limit-l.counts[key], 0)
	return ratelimit.Decision{
		Allowed: l.counts[key] <= window.Limit, Limit: window.Limit,
		Remaining: remaining, RetryAfter: 30 * time.Second,
	}, nil
}

func (l *tenantCredentialLimiter) callCount(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[key]
}

func (l *tenantCredentialLimiter) snapshot() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := make(map[string]int, len(l.counts))
	for key, value := range l.counts {
		snapshot[key] = value
	}
	return snapshot
}
