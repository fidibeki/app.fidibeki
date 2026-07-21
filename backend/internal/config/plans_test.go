package config_test

import (
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestPlanDefinitionsMatchProductLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		plan             tenant.Plan
		projects         int
		feedbackPerMonth int
		users            int
		storageDays      int
	}{
		{tenant.PlanStarter, 1, 500, 1, 30},
		{tenant.PlanGrowth, 4, 2500, 3, 90},
		{tenant.PlanScale, 10, 10000, 10, 365},
		{tenant.PlanEnterprise, -1, -1, -1, -1},
	}
	for _, tt := range tests {
		definition, exists := config.PlanFor(tt.plan)
		if !exists {
			t.Fatalf("PlanFor(%q) does not exist", tt.plan)
		}
		if definition.Limits.Projects != tt.projects || definition.Limits.FeedbackPerMonth != tt.feedbackPerMonth ||
			definition.Limits.Users != tt.users || definition.Limits.StorageDays != tt.storageDays {
			t.Errorf("PlanFor(%q).Limits = %#v", tt.plan, definition.Limits)
		}
	}
}

func TestCanUseFeatureRejectsUnavailableAndUnknownFeatures(t *testing.T) {
	t.Parallel()

	starter := tenant.Tenant{Plan: tenant.PlanStarter}
	growth := tenant.Tenant{Plan: tenant.PlanGrowth}
	scale := tenant.Tenant{Plan: tenant.PlanScale}
	enterprise := tenant.Tenant{Plan: tenant.PlanEnterprise}

	if config.CanUseFeature(starter, config.FeatureAI) {
		t.Error("starter AI = true, want false")
	}
	if !config.CanUseFeature(growth, config.FeatureAudio) {
		t.Error("growth audio = false, want true")
	}
	if config.CanUseFeature(growth, config.FeatureSlack) {
		t.Error("growth Slack = true, want false")
	}
	if !config.CanUseFeature(scale, config.FeatureVideo) {
		t.Error("scale video = false, want true")
	}
	if !config.CanUseFeature(enterprise, config.FeatureCustomDomain) {
		t.Error("enterprise custom domain = false, want true")
	}
	if config.CanUseFeature(tenant.Tenant{Plan: "unknown"}, config.FeatureAI) {
		t.Error("unknown plan feature = true, want false")
	}
}

func TestPlanLimitValueRejectsUnknownLimit(t *testing.T) {
	t.Parallel()
	definition, _ := config.PlanFor(tenant.PlanStarter)
	if value, ok := definition.Limits.Value(config.LimitProjects); !ok || value != 1 {
		t.Errorf("projects = %d, %t", value, ok)
	}
	for limit, expected := range map[config.Limit]int{
		config.LimitFeedbackPerMonth: 500,
		config.LimitUsers:            1,
		config.LimitStorageDays:      30,
	} {
		if value, ok := definition.Limits.Value(limit); !ok || value != expected {
			t.Errorf("limit %q = %d, %t, want %d", limit, value, ok, expected)
		}
	}
	if _, ok := definition.Limits.Value("unknown"); ok {
		t.Error("unknown limit was accepted")
	}
}
