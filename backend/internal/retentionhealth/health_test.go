package retentionhealth

import (
	"strings"
	"testing"
	"time"
)

func TestValidateRun(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	valid := Run{
		Operation: OperationTenantMagicLinks, AttemptedAt: now,
		Result: ResultSucceeded, Processed: 3,
	}
	if err := ValidateRun(valid); err != nil {
		t.Fatalf("ValidateRun(valid) error = %v", err)
	}
	for operation := range operationCategories {
		run := valid
		run.Operation = operation
		if err := ValidateRun(run); err != nil {
			t.Fatalf("ValidateRun(%s) error = %v", operation, err)
		}
	}
	tests := []struct {
		name string
		run  Run
		want string
	}{
		{name: "unknown operation", run: Run{
			Operation: "unknown", AttemptedAt: now, Result: ResultSucceeded,
		}, want: "unknown retention operation"},
		{name: "zero time", run: Run{
			Operation: OperationTenantMagicLinks, Result: ResultSucceeded,
		}, want: "must not be zero"},
		{name: "unknown result", run: Run{
			Operation: OperationTenantMagicLinks, AttemptedAt: now, Result: "unknown",
		}, want: "unknown retention run result"},
		{name: "negative count", run: Run{
			Operation: OperationTenantMagicLinks, AttemptedAt: now,
			Result: ResultFailed, Processed: -1,
		}, want: "must not be negative"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRun(test.run)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateRun() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDeriveHealthStatesAndBoundaries(t *testing.T) {
	t.Parallel()
	registered := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	succeeded := ResultSucceeded
	failed := ResultFailed
	base := operationRecord{
		Operation: OperationTenantMagicLinks, Category: "auth_magic_links",
		RegisteredAt: registered, ExpectedIntervalSec: 3600, AlertGraceSec: 900,
	}
	tests := []struct {
		name   string
		record operationRecord
		now    time.Time
		state  State
		alert  AlertReason
	}{
		{
			name: "first run grace", record: base,
			now:   registered.Add(74 * time.Minute),
			state: StateNotObserved, alert: AlertNotObserved,
		},
		{
			name: "first run overdue at boundary", record: base,
			now:   registered.Add(75 * time.Minute),
			state: StateOverdue, alert: AlertCleanupOverdue,
		},
		{
			name: "recent failure", record: func() operationRecord {
				record := base
				attempt := registered.Add(30 * time.Minute)
				record.LastAttemptAt = &attempt
				record.LastResult = &failed
				record.ConsecutiveFailures = 1
				record.TotalRuns = 1
				record.TotalFailures = 1
				record.FailureCode = "cleanup_failed"
				return record
			}(),
			now:   registered.Add(40 * time.Minute),
			state: StateFailed, alert: AlertLastRunFailed,
		},
		{
			name: "stale success overrides historical failure", record: func() operationRecord {
				record := base
				success := registered.Add(10 * time.Minute)
				attempt := registered.Add(80 * time.Minute)
				record.LastSuccessAt = &success
				record.LastAttemptAt = &attempt
				record.LastResult = &failed
				record.ConsecutiveFailures = 1
				record.TotalRuns = 2
				record.TotalFailures = 1
				record.FailureCode = "cleanup_failed"
				return record
			}(),
			now:   registered.Add(85 * time.Minute),
			state: StateOverdue, alert: AlertCleanupOverdue,
		},
		{
			name: "budget attention", record: func() operationRecord {
				record := base
				attempt := registered.Add(30 * time.Minute)
				record.LastAttemptAt = &attempt
				record.LastSuccessAt = &attempt
				record.LastResult = &succeeded
				record.BudgetExhausted = true
				record.TotalRuns = 1
				return record
			}(),
			now:   registered.Add(40 * time.Minute),
			state: StateAttention, alert: AlertBudgetExhausted,
		},
		{
			name: "healthy", record: func() operationRecord {
				record := base
				attempt := registered.Add(30 * time.Minute)
				record.LastAttemptAt = &attempt
				record.LastSuccessAt = &attempt
				record.LastResult = &succeeded
				record.TotalRuns = 1
				return record
			}(),
			now:   registered.Add(40 * time.Minute),
			state: StateHealthy, alert: AlertNone,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			health, err := deriveHealth(test.record, test.now)
			if err != nil {
				t.Fatalf("deriveHealth() error = %v", err)
			}
			if health.State != test.state || health.AlertReason != test.alert {
				t.Fatalf("health = %#v, want state=%q alert=%q", health, test.state, test.alert)
			}
		})
	}
}

func TestDeriveHealthRejectsMismatchedCategory(t *testing.T) {
	t.Parallel()
	registered := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	if _, err := deriveHealth(operationRecord{
		Operation: OperationAttachments, Category: "auth_magic_links",
		RegisteredAt: registered, ExpectedIntervalSec: 86400, AlertGraceSec: 900,
	}, registered.Add(time.Minute)); err == nil ||
		!strings.Contains(err.Error(), "unknown category") {
		t.Fatalf("deriveHealth(mismatched category) error = %v", err)
	}
}

func TestBuildReportSummarizesStates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 10, 30, 0, 0, time.UTC)
	succeeded := ResultSucceeded
	successAt := now.Add(-10 * time.Minute)
	records := []operationRecord{
		{
			Operation: OperationTenantMagicLinks, Category: "auth_magic_links",
			RegisteredAt: now.Add(-time.Hour), ExpectedIntervalSec: 3600, AlertGraceSec: 900,
			LastAttemptAt: &successAt, LastSuccessAt: &successAt, LastResult: &succeeded,
			TotalRuns: 1,
		},
		{
			Operation: OperationPlatformAdminMagicLinks, Category: "auth_magic_links",
			RegisteredAt: now.Add(-10 * time.Minute), ExpectedIntervalSec: 3600, AlertGraceSec: 900,
		},
		{
			Operation: OperationReplaySessions, Category: "session_replays",
			RegisteredAt: now.Add(-10 * time.Minute), ExpectedIntervalSec: 86400, AlertGraceSec: 900,
		},
	}
	report, err := buildReport(records, now)
	if err != nil {
		t.Fatalf("buildReport() error = %v", err)
	}
	if report.Summary != (Summary{Tracked: 3, Healthy: 1, NotObserved: 2}) {
		t.Fatalf("Summary = %#v", report.Summary)
	}
	if !report.GeneratedAt.Equal(now) || len(report.Operations) != 3 {
		t.Fatalf("Report = %#v", report)
	}
}
