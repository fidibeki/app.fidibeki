package config

import "github.com/acidiney/app.fidibeki/backend/internal/tenant"

type Feature string

const (
	FeatureAI Feature = "ai"
	// FeatureTestimonials rides with AI rather than standing alone: the Suggested
	// queue refuses to rank text that never completed moderation, and moderation
	// is the AI pass. Without it the queue is empty for good, with nothing to
	// explain why.
	FeatureTestimonials Feature = "testimonials"
	FeatureReplay       Feature = "session_replay"
	FeatureSlack        Feature = "slack"
	FeatureVideo        Feature = "video"
	FeatureAudio        Feature = "audio"
	FeatureDOMCapture   Feature = "dom_capture"
	FeatureCustomDomain Feature = "custom_domain"
)

type Limit string

const (
	LimitProjects         Limit = "projects"
	LimitFeedbackPerMonth Limit = "feedback_per_month"
	LimitUsers            Limit = "users"
	LimitStorageDays      Limit = "storage_days"
)

type PlanLimits struct {
	Projects         int `json:"projects"`
	FeedbackPerMonth int `json:"feedback_per_month"`
	Users            int `json:"users"`
	StorageDays      int `json:"storage_days"`
}

func (l PlanLimits) Value(limit Limit) (int, bool) {
	switch limit {
	case LimitProjects:
		return l.Projects, true
	case LimitFeedbackPerMonth:
		return l.FeedbackPerMonth, true
	case LimitUsers:
		return l.Users, true
	case LimitStorageDays:
		return l.StorageDays, true
	default:
		return 0, false
	}
}

type PlanFeatures struct {
	AI           bool `json:"ai"`
	Testimonials bool `json:"testimonials"`
	Replay       bool `json:"session_replay"`
	Slack        bool `json:"slack"`
	Video        bool `json:"video"`
	Audio        bool `json:"audio"`
	DOMCapture   bool `json:"dom_capture"`
	CustomDomain bool `json:"custom_domain"`
}

func (f PlanFeatures) Enabled(feature Feature) bool {
	switch feature {
	case FeatureAI:
		return f.AI
	case FeatureTestimonials:
		return f.Testimonials
	case FeatureReplay:
		return f.Replay
	case FeatureSlack:
		return f.Slack
	case FeatureVideo:
		return f.Video
	case FeatureAudio:
		return f.Audio
	case FeatureDOMCapture:
		return f.DOMCapture
	case FeatureCustomDomain:
		return f.CustomDomain
	default:
		return false
	}
}

type PlanDefinition struct {
	Plan                  tenant.Plan  `json:"plan"`
	MonthlyPriceCents     *int         `json:"monthly_price_cents"`
	YearlyPriceCents      *int         `json:"yearly_price_cents"`
	YearlyDiscountPercent *int         `json:"yearly_discount_percent"`
	Limits                PlanLimits   `json:"limits"`
	Features              PlanFeatures `json:"features"`
}

// Yearly prices must match the Stripe yearly price objects exactly. They are
// multiples of 12 so clients can show an even per-month amount, and each keeps
// the effective discount at or above the advertised YearlyDiscountPercent.
var planDefinitions = map[tenant.Plan]PlanDefinition{
	tenant.PlanStarter: {
		Plan: tenant.PlanStarter, MonthlyPriceCents: intPointer(499),
		YearlyPriceCents: intPointer(5568), YearlyDiscountPercent: intPointer(7),
		Limits: PlanLimits{Projects: 1, FeedbackPerMonth: 500, Users: 1, StorageDays: 30},
	},
	tenant.PlanGrowth: {
		Plan: tenant.PlanGrowth, MonthlyPriceCents: intPointer(999),
		YearlyPriceCents: intPointer(10308), YearlyDiscountPercent: intPointer(14),
		Limits: PlanLimits{Projects: 4, FeedbackPerMonth: 2500, Users: 3, StorageDays: 90},
		Features: PlanFeatures{
			AI: true, Testimonials: true, Replay: true, Audio: true, DOMCapture: true,
		},
	},
	tenant.PlanScale: {
		Plan: tenant.PlanScale, MonthlyPriceCents: intPointer(2799),
		YearlyPriceCents: intPointer(28884), YearlyDiscountPercent: intPointer(14),
		Limits: PlanLimits{Projects: 10, FeedbackPerMonth: 10000, Users: 10, StorageDays: 365},
		Features: PlanFeatures{
			AI: true, Testimonials: true, Replay: true, Slack: true, Video: true, Audio: true,
			DOMCapture: true,
		},
	},
	tenant.PlanEnterprise: {
		Plan:   tenant.PlanEnterprise,
		Limits: PlanLimits{Projects: -1, FeedbackPerMonth: -1, Users: -1, StorageDays: -1},
		Features: PlanFeatures{
			AI: true, Testimonials: true, Replay: true, Slack: true, Video: true, Audio: true, DOMCapture: true, CustomDomain: true,
		},
	},
}

func PlanFor(plan tenant.Plan) (PlanDefinition, bool) {
	definition, exists := planDefinitions[plan]
	return definition, exists
}

func SelfServicePlans() []PlanDefinition {
	plans := []tenant.Plan{tenant.PlanStarter, tenant.PlanGrowth, tenant.PlanScale}
	definitions := make([]PlanDefinition, 0, len(plans))
	for _, plan := range plans {
		definitions = append(definitions, planDefinitions[plan])
	}
	return definitions
}

func CanUseFeature(resolved tenant.Tenant, feature Feature) bool {
	definition, exists := PlanFor(resolved.Plan)
	return exists && definition.Features.Enabled(feature)
}

func intPointer(value int) *int {
	return &value
}
