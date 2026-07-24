package privacy

import (
	"fmt"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

type RetentionPolicy struct {
	Category     string `json:"category"`
	Label        string `json:"label"`
	Duration     string `json:"duration"`
	DurationDays *int   `json:"duration_days,omitempty"`
	Source       string `json:"source"`
}

type EffectiveRetention struct {
	Plan     tenant.Plan       `json:"plan"`
	Policies []RetentionPolicy `json:"policies"`
}

func EffectiveRetentionForPlan(plan tenant.Plan) (EffectiveRetention, error) {
	definition, ok := config.PlanFor(plan)
	if !ok {
		return EffectiveRetention{}, fmt.Errorf("retention policy is unavailable for plan %q", plan)
	}
	storageDays := definition.Limits.StorageDays
	storageDuration := "Custom"
	var storageDurationDays *int
	if storageDays >= 0 {
		storageDuration = fmt.Sprintf("%d days", storageDays)
		storageDurationDays = &storageDays
	}
	fixedDays := func(value int) *int { return &value }
	return EffectiveRetention{
		Plan: plan,
		Policies: []RetentionPolicy{
			{
				Category: "workflow_metadata", Label: "Feedback and issue workflow metadata",
				Duration: "Workspace lifetime", Source: "Workspace lifecycle",
			},
			{
				Category: "auth_magic_links", Label: "Login and invitation magic links",
				Duration: "Configured link expiry, deleted within 1 hour of expiring",
				Source:   "Platform default",
			},
			{
				Category: "attachments", Label: "Attachments", Duration: storageDuration,
				DurationDays: storageDurationDays, Source: "Plan storage limit",
			},
			{
				Category: "session_replays", Label: "Session replay objects", Duration: storageDuration,
				DurationDays: storageDurationDays, Source: "Plan storage limit",
			},
			{
				Category: "widget_events", Label: "Raw widget funnel events", Duration: "90 days",
				DurationDays: fixedDays(90), Source: "Platform default",
			},
			{
				Category: "widget_daily", Label: "Daily widget funnel aggregates", Duration: "730 days",
				DurationDays: fixedDays(730), Source: "Platform default",
			},
			{
				Category: "export_archives", Label: "Export archives", Duration: "7 days",
				DurationDays: fixedDays(7), Source: "Platform default",
			},
			{
				Category: "unverified_requests", Label: "Unverified reporter requests",
				Duration: "48 hours", DurationDays: fixedDays(2), Source: "Platform default",
			},
			{
				Category: "verified_requests", Label: "Verified reporter request access",
				Duration: "30 days", DurationDays: fixedDays(30), Source: "Platform default",
			},
		},
	}, nil
}
