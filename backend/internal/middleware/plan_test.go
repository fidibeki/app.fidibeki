package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestRequirePlanLimitReturnsDistinctFeatureAndUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		plan       tenant.Plan
		options    appmiddleware.PlanLimitOptions
		wantStatus int
		wantCode   string
	}{
		{
			name: "feature unavailable", plan: tenant.PlanGrowth,
			options:    appmiddleware.PlanLimitOptions{Feature: config.FeatureSlack, UpgradeURL: "/billing"},
			wantStatus: http.StatusPaymentRequired, wantCode: "plan_feature_unavailable",
		},
		{
			name: "usage reached", plan: tenant.PlanStarter,
			options: appmiddleware.PlanLimitOptions{
				Limit: config.LimitProjects, UpgradeURL: "/billing",
				CountUsage: func(context.Context, tenant.Tenant) (int, error) { return 1, nil },
			},
			wantStatus: http.StatusPaymentRequired, wantCode: "plan_limit_exceeded",
		},
		{
			name: "below usage", plan: tenant.PlanGrowth,
			options: appmiddleware.PlanLimitOptions{
				Limit:      config.LimitProjects,
				CountUsage: func(context.Context, tenant.Tenant) (int, error) { return 3, nil },
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "usage unavailable", plan: tenant.PlanStarter,
			options: appmiddleware.PlanLimitOptions{
				Limit: config.LimitProjects,
				CountUsage: func(context.Context, tenant.Tenant) (int, error) {
					return 0, errors.New("database unavailable")
				},
			},
			wantStatus: http.StatusServiceUnavailable, wantCode: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeTransaction{}
			resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
				BaseDomain: "localhost",
				Resolver: tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
					return tenant.Tenant{ID: "tenant-id", Slug: slug, Plan: tt.plan, Status: tenant.StatusActive}, nil
				}),
				Begin: func(context.Context) (db.Transaction, error) { return tx, nil },
			})
			handler := resolve(appmiddleware.RequirePlanLimit(tt.options)(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			)))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://acme.localhost/v1/test", nil))
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if tt.wantCode != "" && !strings.Contains(recorder.Body.String(), `"code":"`+tt.wantCode+`"`) {
				t.Errorf("body = %s, want code %s", recorder.Body.String(), tt.wantCode)
			}
		})
	}
}
