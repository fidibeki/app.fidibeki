package privacy

import (
	"fmt"

	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

type RetentionLifecycleStatus string

const (
	RetentionEnforced         RetentionLifecycleStatus = "enforced"
	RetentionConfigurable     RetentionLifecycleStatus = "configurable"
	RetentionExternalControl  RetentionLifecycleStatus = "external_control"
	RetentionDecisionRequired RetentionLifecycleStatus = "decision_required"
)

type RetentionHoldBehavior string

const (
	RetentionHoldCaseScoped       RetentionHoldBehavior = "case_scoped"
	RetentionHoldSupported        RetentionHoldBehavior = "supported"
	RetentionHoldNotApplicable    RetentionHoldBehavior = "not_applicable"
	RetentionHoldDecisionRequired RetentionHoldBehavior = "decision_required"
)

type RetentionRole string

const (
	RetentionRoleController          RetentionRole = "controller"
	RetentionRoleProcessor           RetentionRole = "processor"
	RetentionRoleControllerProcessor RetentionRole = "controller_and_processor"
)

type RetentionResourceKind string

const (
	RetentionPostgresTable RetentionResourceKind = "postgres_table"
	RetentionDatabaseScope RetentionResourceKind = "database_scope"
	RetentionObjectPrefix  RetentionResourceKind = "object_prefix"
	RetentionQueue         RetentionResourceKind = "queue"
	RetentionBrowserKey    RetentionResourceKind = "browser_key"
	RetentionRedisKey      RetentionResourceKind = "redis_key"
	RetentionProvider      RetentionResourceKind = "provider"
)

type RetentionResourceCount struct {
	Kind  RetentionResourceKind `json:"kind"`
	Count int                   `json:"count"`
}

type RetentionPolicy struct {
	Category       string                   `json:"category"`
	Label          string                   `json:"label"`
	Duration       string                   `json:"duration"`
	DurationDays   *int                     `json:"duration_days,omitempty"`
	Source         string                   `json:"source"`
	Purpose        string                   `json:"purpose"`
	Role           RetentionRole            `json:"role"`
	Status         RetentionLifecycleStatus `json:"status"`
	Trigger        string                   `json:"trigger"`
	Action         string                   `json:"action"`
	Cadence        string                   `json:"cadence"`
	HoldBehavior   RetentionHoldBehavior    `json:"hold_behavior"`
	Implementation string                   `json:"implementation,omitempty"`
	ResourceCounts []RetentionResourceCount `json:"resource_counts"`
}

type RetentionSummary struct {
	Enforced         int `json:"enforced"`
	Configurable     int `json:"configurable"`
	ExternalControl  int `json:"external_control"`
	DecisionRequired int `json:"decision_required"`
}

type EffectiveRetention struct {
	Plan            tenant.Plan       `json:"plan"`
	RegistryVersion int               `json:"registry_version"`
	Summary         RetentionSummary  `json:"summary"`
	Policies        []RetentionPolicy `json:"policies"`
}

func EffectiveRetentionForPlan(plan tenant.Plan) (EffectiveRetention, error) {
	effective, err := buildEffectiveRetention(plan, retentionRegistry())
	if err != nil {
		return EffectiveRetention{}, fmt.Errorf(
			"retention policy is unavailable for plan %q: %w", plan, err,
		)
	}
	return effective, nil
}
