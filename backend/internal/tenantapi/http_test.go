package tenantapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestConfigReturnsTenantPlanLimitsAndFeatures(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(func(context.Context) (tenant.Tenant, bool) {
		return tenant.Tenant{
			ID: "tenant-id", Name: "Acme", Slug: "acme",
			Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
		}, true
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Plan struct {
			Limits struct {
				Projects int `json:"projects"`
			} `json:"limits"`
			Features struct {
				AI    bool `json:"ai"`
				Slack bool `json:"slack"`
			} `json:"features"`
		} `json:"plan"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Plan.Limits.Projects != 4 || !response.Plan.Features.AI || response.Plan.Features.Slack {
		t.Errorf("plan response = %#v", response.Plan)
	}
}

func TestConfigFailsClosedWithoutTenantOrKnownPlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resolved tenant.Tenant
		ok       bool
	}{
		{name: "missing context"},
		{name: "unknown plan", resolved: tenant.Tenant{Plan: "unknown"}, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHTTPHandler(func(context.Context) (tenant.Tenant, bool) {
				return tt.resolved, tt.ok
			})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
			if recorder.Code != http.StatusInternalServerError {
				t.Errorf("status = %d", recorder.Code)
			}
		})
	}
}
