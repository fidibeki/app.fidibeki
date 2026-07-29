package retentionhealth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Operation string

const (
	OperationTenantMagicLinks         Operation = "tenant_magic_links"
	OperationPlatformAdminMagicLinks  Operation = "platform_admin_magic_links"
	OperationAttachments              Operation = "attachments"
	OperationPrivacyExportArchives    Operation = "privacy_export_archives"
	OperationReporterPrivacyRequests  Operation = "reporter_privacy_requests"
	OperationReplaySessions           Operation = "replay_sessions"
	OperationWidgetFunnelEvents       Operation = "widget_funnel_events"
	OperationWidgetFunnelDaily        Operation = "widget_funnel_daily"
	OperationAbuseDiagnostics         Operation = "abuse_diagnostics"
	OperationAIBudgetLedgers          Operation = "ai_budget_ledgers"
	OperationIdentityAssetDeletions   Operation = "identity_asset_deletions"
	OperationSAMLAuthArtifacts        Operation = "saml_auth_artifacts"
	OperationMobileCaptureSessions    Operation = "mobile_capture_sessions"
	OperationEmailFeedbackInvitations Operation = "email_feedback_invitations"
)

// Migrations seed exactly one retention_maintenance_status row
// per entry; Read fails when the table and this map disagree.
var operationCategories = map[Operation]string{
	OperationTenantMagicLinks:         "auth_magic_links",
	OperationPlatformAdminMagicLinks:  "auth_magic_links",
	OperationAttachments:              "attachments",
	OperationPrivacyExportArchives:    "export_archives",
	OperationReporterPrivacyRequests:  "privacy_rights",
	OperationReplaySessions:           "session_replays",
	OperationWidgetFunnelEvents:       "widget_events",
	OperationWidgetFunnelDaily:        "widget_daily",
	OperationAbuseDiagnostics:         "abuse_diagnostics",
	OperationAIBudgetLedgers:          "ai_processing",
	OperationIdentityAssetDeletions:   "identity_assets",
	OperationSAMLAuthArtifacts:        "auth_magic_links",
	OperationMobileCaptureSessions:    "mobile_capture_credentials",
	OperationEmailFeedbackInvitations: "email_feedback_delivery",
}

type Result string

const (
	ResultSucceeded Result = "succeeded"
	ResultFailed    Result = "failed"
)

type State string

const (
	StateHealthy     State = "healthy"
	StateAttention   State = "attention"
	StateFailed      State = "failed"
	StateOverdue     State = "overdue"
	StateNotObserved State = "not_observed"
)

type AlertReason string

const (
	AlertNone            AlertReason = "none"
	AlertBudgetExhausted AlertReason = "budget_exhausted"
	AlertLastRunFailed   AlertReason = "last_run_failed"
	AlertCleanupOverdue  AlertReason = "cleanup_overdue"
	AlertNotObserved     AlertReason = "not_observed"
)

type Run struct {
	Operation       Operation
	AttemptedAt     time.Time
	Result          Result
	Processed       int64
	BudgetExhausted bool
}

type Recorder interface {
	RecordRun(context.Context, Run) error
}

type OperationHealth struct {
	Operation           Operation   `json:"operation"`
	Category            string      `json:"category"`
	State               State       `json:"state"`
	AlertReason         AlertReason `json:"alert_reason"`
	RegisteredAt        time.Time   `json:"registered_at"`
	ExpectedIntervalSec int         `json:"expected_interval_seconds"`
	AlertGraceSec       int         `json:"alert_grace_seconds"`
	LastAttemptAt       *time.Time  `json:"last_attempt_at,omitempty"`
	LastSuccessAt       *time.Time  `json:"last_success_at,omitempty"`
	NextExpectedAt      time.Time   `json:"next_expected_at"`
	LastResult          *Result     `json:"last_result,omitempty"`
	LastProcessed       int64       `json:"last_processed"`
	TotalProcessed      int64       `json:"total_processed"`
	TotalRuns           int64       `json:"total_runs"`
	TotalFailures       int64       `json:"total_failures"`
	ConsecutiveFailures int         `json:"consecutive_failures"`
	BudgetExhausted     bool        `json:"budget_exhausted"`
	FailureCode         string      `json:"failure_code,omitempty"`
}

type Summary struct {
	Tracked     int `json:"tracked"`
	Healthy     int `json:"healthy"`
	Attention   int `json:"attention"`
	Failed      int `json:"failed"`
	Overdue     int `json:"overdue"`
	NotObserved int `json:"not_observed"`
}

type Report struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Summary     Summary           `json:"summary"`
	Operations  []OperationHealth `json:"operations"`
}

type operationRecord struct {
	Operation           Operation
	Category            string
	RegisteredAt        time.Time
	ExpectedIntervalSec int
	AlertGraceSec       int
	LastAttemptAt       *time.Time
	LastSuccessAt       *time.Time
	LastResult          *Result
	LastProcessed       int64
	BudgetExhausted     bool
	ConsecutiveFailures int
	TotalRuns           int64
	TotalFailures       int64
	TotalProcessed      int64
	FailureCode         string
}

func ValidateRun(run Run) error {
	if _, known := operationCategories[run.Operation]; !known {
		return fmt.Errorf("unknown retention operation %q", run.Operation)
	}
	if run.AttemptedAt.IsZero() {
		return errors.New("retention run attempt time must not be zero")
	}
	switch run.Result {
	case ResultSucceeded, ResultFailed:
	default:
		return fmt.Errorf("unknown retention run result %q", run.Result)
	}
	if run.Processed < 0 {
		return errors.New("retention run processed count must not be negative")
	}
	return nil
}

func buildReport(records []operationRecord, now time.Time) (Report, error) {
	if now.IsZero() {
		return Report{}, errors.New("retention health time must not be zero")
	}
	report := Report{
		GeneratedAt: now.UTC(),
		Operations:  make([]OperationHealth, 0, len(records)),
	}
	for _, record := range records {
		health, err := deriveHealth(record, now.UTC())
		if err != nil {
			return Report{}, err
		}
		report.Operations = append(report.Operations, health)
		report.Summary.Tracked++
		switch health.State {
		case StateHealthy:
			report.Summary.Healthy++
		case StateAttention:
			report.Summary.Attention++
		case StateFailed:
			report.Summary.Failed++
		case StateOverdue:
			report.Summary.Overdue++
		case StateNotObserved:
			report.Summary.NotObserved++
		}
	}
	return report, nil
}

func deriveHealth(record operationRecord, now time.Time) (OperationHealth, error) {
	if record.RegisteredAt.IsZero() || record.ExpectedIntervalSec <= 0 || record.AlertGraceSec < 0 {
		return OperationHealth{}, fmt.Errorf(
			"retention operation %q has invalid schedule metadata", record.Operation,
		)
	}
	category, known := operationCategories[record.Operation]
	if !known {
		return OperationHealth{}, fmt.Errorf("unknown retention operation %q", record.Operation)
	}
	if record.Category != category {
		return OperationHealth{}, fmt.Errorf(
			"retention operation %q has unknown category %q", record.Operation, record.Category,
		)
	}
	if record.LastResult != nil {
		switch *record.LastResult {
		case ResultSucceeded, ResultFailed:
		default:
			return OperationHealth{}, fmt.Errorf(
				"retention operation %q has unknown result %q", record.Operation, *record.LastResult,
			)
		}
	}
	if record.LastProcessed < 0 || record.TotalProcessed < 0 || record.TotalRuns < 0 ||
		record.TotalFailures < 0 || record.ConsecutiveFailures < 0 {
		return OperationHealth{}, fmt.Errorf(
			"retention operation %q has invalid counters", record.Operation,
		)
	}

	interval := time.Duration(record.ExpectedIntervalSec) * time.Second
	grace := time.Duration(record.AlertGraceSec) * time.Second
	scheduleAnchor := record.RegisteredAt
	if record.LastAttemptAt != nil {
		scheduleAnchor = *record.LastAttemptAt
	}
	successAnchor := record.RegisteredAt
	if record.LastSuccessAt != nil {
		successAnchor = *record.LastSuccessAt
	}
	health := OperationHealth{
		Operation: record.Operation, Category: record.Category,
		RegisteredAt:        record.RegisteredAt.UTC(),
		ExpectedIntervalSec: record.ExpectedIntervalSec, AlertGraceSec: record.AlertGraceSec,
		LastAttemptAt:  utcTimePointer(record.LastAttemptAt),
		LastSuccessAt:  utcTimePointer(record.LastSuccessAt),
		NextExpectedAt: scheduleAnchor.UTC().Add(interval),
		LastResult:     record.LastResult, LastProcessed: record.LastProcessed,
		TotalProcessed: record.TotalProcessed, TotalRuns: record.TotalRuns,
		TotalFailures: record.TotalFailures, ConsecutiveFailures: record.ConsecutiveFailures,
		BudgetExhausted: record.BudgetExhausted, FailureCode: record.FailureCode,
	}

	deadline := successAnchor.UTC().Add(interval + grace)
	if !now.Before(deadline) {
		health.State = StateOverdue
		health.AlertReason = AlertCleanupOverdue
		return health, nil
	}
	if record.LastResult == nil {
		health.State = StateNotObserved
		health.AlertReason = AlertNotObserved
		return health, nil
	}
	if *record.LastResult == ResultFailed {
		health.State = StateFailed
		health.AlertReason = AlertLastRunFailed
		return health, nil
	}
	if record.BudgetExhausted {
		health.State = StateAttention
		health.AlertReason = AlertBudgetExhausted
		return health, nil
	}
	health.State = StateHealthy
	health.AlertReason = AlertNone
	return health, nil
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
