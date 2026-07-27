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

func TestPlanDefinitionsMatchAdvertisedPrices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		plan     tenant.Plan
		monthly  int
		yearly   int
		discount int
	}{
		{tenant.PlanStarter, 999, 11148, 7},
		{tenant.PlanGrowth, 2999, 30948, 14},
		{tenant.PlanScale, 6799, 70164, 14},
	}

	for _, tt := range tests {
		definition, exists := config.PlanFor(tt.plan)
		if !exists {
			t.Fatalf("PlanFor(%q) does not exist", tt.plan)
		}
		if definition.MonthlyPriceCents == nil || *definition.MonthlyPriceCents != tt.monthly {
			t.Errorf("PlanFor(%q).MonthlyPriceCents = %v, want %d", tt.plan, definition.MonthlyPriceCents, tt.monthly)
		}
		if definition.YearlyPriceCents == nil || *definition.YearlyPriceCents != tt.yearly {
			t.Errorf("PlanFor(%q).YearlyPriceCents = %v, want %d", tt.plan, definition.YearlyPriceCents, tt.yearly)
		}
		if definition.YearlyDiscountPercent == nil || *definition.YearlyDiscountPercent != tt.discount {
			t.Errorf("PlanFor(%q).YearlyDiscountPercent = %v, want %d", tt.plan, definition.YearlyDiscountPercent, tt.discount)
		}
	}
}

func TestSelfServicePlansAreStableAndExcludeEnterprise(t *testing.T) {
	t.Parallel()

	plans := config.SelfServicePlans()
	want := []tenant.Plan{tenant.PlanStarter, tenant.PlanGrowth, tenant.PlanScale}
	if len(plans) != len(want) {
		t.Fatalf("SelfServicePlans() length = %d, want %d", len(plans), len(want))
	}
	for index, plan := range plans {
		if plan.Plan != want[index] {
			t.Errorf("SelfServicePlans()[%d].Plan = %q, want %q", index, plan.Plan, want[index])
		}
		if plan.MonthlyPriceCents == nil {
			t.Errorf("SelfServicePlans()[%d].MonthlyPriceCents = nil", index)
		}
		if plan.YearlyPriceCents == nil {
			t.Errorf("SelfServicePlans()[%d].YearlyPriceCents = nil", index)
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

// Testimonials cannot be sold on a plan without AI. The Suggested queue refuses
// to rank text that never completed moderation, moderation is the AI pass, and a
// queue that is empty for good is not a feature.
func TestTestimonialsAreAvailableExactlyWhereAIIs(t *testing.T) {
	t.Parallel()

	for _, plan := range []tenant.Plan{
		tenant.PlanStarter, tenant.PlanGrowth, tenant.PlanScale, tenant.PlanEnterprise,
	} {
		resolved := tenant.Tenant{Plan: plan}
		ai := config.CanUseFeature(resolved, config.FeatureAI)
		testimonials := config.CanUseFeature(resolved, config.FeatureTestimonials)
		if ai != testimonials {
			t.Errorf("%s: ai = %t but testimonials = %t; they have to move together",
				plan, ai, testimonials)
		}
	}
	if config.CanUseFeature(tenant.Tenant{Plan: tenant.PlanStarter}, config.FeatureTestimonials) {
		t.Error("starter testimonials = true, want false")
	}
	if !config.CanUseFeature(tenant.Tenant{Plan: tenant.PlanGrowth}, config.FeatureTestimonials) {
		t.Error("growth testimonials = false, want true")
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
