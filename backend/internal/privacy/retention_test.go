package privacy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestEffectiveRetentionForPlan(t *testing.T) {
	t.Parallel()
	starter, err := EffectiveRetentionForPlan(tenant.PlanStarter)
	if err != nil {
		t.Fatalf("EffectiveRetentionForPlan(starter) error = %v", err)
	}
	if starter.Plan != tenant.PlanStarter ||
		starter.RegistryVersion != retentionRegistryVersion ||
		len(starter.Policies) != 32 {
		t.Fatalf("starter retention = %#v", starter)
	}
	if starter.Summary != (RetentionSummary{
		Enforced: 11, Configurable: 3, ExternalControl: 4, DecisionRequired: 14,
	}) {
		t.Fatalf("starter retention summary = %#v", starter.Summary)
	}
	attachment := retentionPolicyByCategory(t, starter, "attachments")
	if attachment.Status != RetentionConfigurable ||
		attachment.DurationDays == nil || *attachment.DurationDays != 30 {
		t.Fatalf("starter attachment policy = %#v", attachment)
	}
	magicLinks := retentionPolicyByCategory(t, starter, "auth_magic_links")
	if magicLinks.Status != RetentionEnforced || magicLinks.DurationDays != nil {
		t.Fatalf("starter auth magic link policy = %#v", magicLinks)
	}
	emailInvitations := retentionPolicyByCategory(t, starter, "email_feedback_delivery")
	if emailInvitations.Status != RetentionEnforced ||
		emailInvitations.DurationDays == nil || *emailInvitations.DurationDays != 90 ||
		emailInvitations.Implementation != "privacy.RetentionMaintainer" {
		t.Fatalf("email invitation retention policy = %#v", emailInvitations)
	}
	emailSuppressions := retentionPolicyByCategory(t, starter, "email_feedback_suppressions")
	if emailSuppressions.Status != RetentionEnforced ||
		emailSuppressions.Duration != "Until the customer removes the workspace or an approved policy changes" ||
		emailSuppressions.DurationDays != nil {
		t.Fatalf("email suppression retention policy = %#v", emailSuppressions)
	}
	for _, policy := range starter.Policies {
		if strings.Contains(strings.ToLower(policy.Duration), "workspace lifetime") {
			t.Fatalf("policy %q retains the old unbounded duration", policy.Category)
		}
	}
	enterprise, err := EffectiveRetentionForPlan(tenant.PlanEnterprise)
	enterpriseAttachment := retentionPolicyByCategory(t, enterprise, "attachments")
	if err != nil || enterpriseAttachment.Duration != "Custom" ||
		enterpriseAttachment.DurationDays != nil {
		t.Fatalf("enterprise retention = %#v, error=%v", enterprise, err)
	}
	if _, err := EffectiveRetentionForPlan(tenant.Plan("unknown")); err == nil {
		t.Fatal("EffectiveRetentionForPlan(unknown) error = nil")
	}
}

func TestRetentionRegistryCoversEveryMigratedTableExactlyOnce(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate retention test")
	}
	migrationFiles, err := filepath.Glob(filepath.Join(
		filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations", "*.up.sql",
	))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	createTable := regexp.MustCompile(
		`(?m)CREATE TABLE(?: IF NOT EXISTS)?\s+([a-zA-Z_][a-zA-Z0-9_]*)`,
	)
	migrated := make(map[string]int)
	for _, migrationFile := range migrationFiles {
		content, readErr := os.ReadFile(migrationFile)
		if readErr != nil {
			t.Fatalf("read migration %s: %v", migrationFile, readErr)
		}
		for _, match := range createTable.FindAllSubmatch(content, -1) {
			migrated[string(match[1])]++
		}
	}
	registered := make(map[string]int)
	for _, definition := range retentionRegistry() {
		for _, resource := range definition.Resources {
			if resource.Kind == RetentionPostgresTable {
				registered[resource.Name]++
			}
		}
	}
	if len(migrated) != 99 {
		t.Fatalf("migration table count = %d, want 99", len(migrated))
	}
	if !reflect.DeepEqual(registered, migrated) {
		t.Fatalf(
			"registered PostgreSQL tables differ from migrations\nregistered=%v\nmigrated=%v",
			sortedResourceCounts(registered), sortedResourceCounts(migrated),
		)
	}
}

func TestRetentionRegistryCoversKnownNonDatabaseBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind RetentionResourceKind
		want []string
	}{
		{
			kind: RetentionObjectPrefix,
			want: []string{
				"tenants/{tenant}/exports/",
				"tenants/{tenant}/feedbacks/{feedback}/attachments/",
				"tenants/{tenant}/identity/logos/",
				"tenants/{tenant}/identity/members/",
				"tenants/{tenant}/sessions/{session}/batches/",
				"tenants/{tenant}/sessions/{session}/replay.json",
			},
		},
		{
			kind: RetentionQueue,
			want: []string{
				"ai-pool", "attachment-verification-pool", "domain-pool",
				"email-pool", "privacy-pool", "replay-pool",
			},
		},
		{
			kind: RetentionBrowserKey,
			want: []string{
				"fidibeki:analytics-consent:v1:*", "fidibeki:analytics-outbox:v2:*",
				"fidibeki:policy-visit:v1:*", "fidibeki:replay-consent:v1:*",
				"fidibeki:session:v1", "fidibeki:state:v1:*", "fidibeki:state:v2:*",
				"fidibeki:visitor:v1",
			},
		},
		{
			kind: RetentionProvider,
			want: []string{
				"cloudflare_turnstile", "customer_generic_webhook",
				"customer_saml_identity_provider", "email_capture_filesystem",
				"google_gemini_api", "jira_cloud_issue_tracker", "linear_issue_tracker",
				"microsoft_teams_incoming_webhook", "postgresql_runtime", "redis_asynq",
				"resend_transactional_email", "s3_compatible_object_storage",
				"sentry_error_telemetry", "slack_incoming_webhook", "slack_oauth_install",
				"stripe_billing_api",
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.kind), func(t *testing.T) {
			t.Parallel()
			got := retentionResourceNames(test.kind)
			sort.Strings(test.want)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("%s resources = %v, want %v", test.kind, got, test.want)
			}
		})
	}
}

func TestRetentionRegistryDoesNotOverstateQueueProviderOrBrowserControl(t *testing.T) {
	t.Parallel()
	for _, definition := range retentionRegistry() {
		for _, resource := range definition.Resources {
			switch resource.Kind {
			case RetentionQueue:
				if definition.Status != RetentionExternalControl {
					t.Errorf(
						"queue %q status = %q, want external_control",
						resource.Name, definition.Status,
					)
				}
			case RetentionProvider:
				want := RetentionExternalControl
				if resource.Name == "google_gemini_api" {
					want = RetentionEnforced
				}
				if definition.Status != want {
					t.Errorf(
						"provider %q status = %q, want %q",
						resource.Name, definition.Status, want,
					)
				}
			case RetentionBrowserKey:
				if (resource.Name == "fidibeki:visitor:v1" ||
					resource.Name == "fidibeki:analytics-outbox:v2:*") &&
					definition.Status != RetentionDecisionRequired {
					t.Errorf(
						"analytics browser key %q status = %q, want decision_required",
						resource.Name, definition.Status,
					)
				}
			}
		}
	}
}

func TestRetentionRegistryRejectsUnsupportedClaimsAndDuplicates(t *testing.T) {
	t.Parallel()
	valid := func(category, resourceName string) retentionDefinition {
		return retentionDefinition{
			Category: category, Label: "Label", Purpose: "Purpose", Role: "processor",
			Status: RetentionEnforced, DurationKind: retentionDurationFixed,
			Duration: "1 day", DurationDays: days(1), Source: "Platform default",
			Trigger: "Created at", Action: "Delete", Cadence: "Daily",
			HoldBehavior: RetentionHoldNotApplicable, Implementation: "test.Maintainer",
			Resources: resource(RetentionPostgresTable, resourceName),
		}
	}
	tests := []struct {
		name        string
		definitions []retentionDefinition
		want        string
	}{
		{
			name: "duplicate category",
			definitions: []retentionDefinition{
				valid("one", "one"), valid("one", "two"),
			},
			want: "duplicate retention category",
		},
		{
			name: "duplicate resource",
			definitions: []retentionDefinition{
				valid("one", "same"), valid("two", "same"),
			},
			want: "belongs to both",
		},
		{
			name: "enforced without implementation",
			definitions: func() []retentionDefinition {
				definition := valid("one", "one")
				definition.Implementation = ""
				return []retentionDefinition{definition}
			}(),
			want: "unsupported enforcement claim",
		},
		{
			name: "decision with bounded claim",
			definitions: func() []retentionDefinition {
				definition := valid("one", "one")
				definition.Status = RetentionDecisionRequired
				return []retentionDefinition{definition}
			}(),
			want: "invalid decision state",
		},
		{
			name: "unknown status",
			definitions: func() []retentionDefinition {
				definition := valid("one", "one")
				definition.Status = "unknown"
				return []retentionDefinition{definition}
			}(),
			want: "unknown status",
		},
		{
			name: "unknown hold behavior",
			definitions: func() []retentionDefinition {
				definition := valid("one", "one")
				definition.HoldBehavior = "unknown"
				return []retentionDefinition{definition}
			}(),
			want: "unknown hold behavior",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateRetentionRegistry(test.definitions)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateRetentionRegistry() error = %v, want %q", err, test.want)
			}
		})
	}
}

func retentionPolicyByCategory(
	t *testing.T,
	effective EffectiveRetention,
	category string,
) RetentionPolicy {
	t.Helper()
	for _, policy := range effective.Policies {
		if policy.Category == category {
			return policy
		}
	}
	t.Fatalf("retention category %q not found", category)
	return RetentionPolicy{}
}

func retentionResourceNames(kind RetentionResourceKind) []string {
	var names []string
	for _, definition := range retentionRegistry() {
		for _, resource := range definition.Resources {
			if resource.Kind == kind {
				names = append(names, resource.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func sortedResourceCounts(values map[string]int) []string {
	result := make([]string, 0, len(values))
	for name, count := range values {
		result = append(result, fmt.Sprintf("%s=%d", name, count))
	}
	sort.Strings(result)
	return result
}

func TestRetentionMaintainerKeepsFailedObjectsRetryableAndContinues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	store := &retentionOperationsStub{
		attachmentBatches: [][]RetentionObjectCandidate{{
			{TenantID: "one", ID: "attachment-one", ObjectKey: "attachment-one"},
			{TenantID: "one", ID: "attachment-two", ObjectKey: "attachment-two"},
		}},
		archiveBatches: [][]RetentionObjectCandidate{{
			{TenantID: "one", ID: "archive-one", ObjectKey: "archive-one"},
		}},
		requestResults:    []int64{2},
		invitationResults: []int64{4},
	}
	storage := &retentionStorageStub{
		failures: map[string]error{"attachment-two": errors.New("storage unavailable")},
	}
	health := &retentionHealthRecorderStub{}
	var logs bytes.Buffer
	maintainer := RetentionMaintainer{
		Store: store, Storage: storage, Health: health,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Now:    func() time.Time { return now }, BatchSize: 10,
	}
	err := maintainer.MaintainOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "storage unavailable") {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	if len(health.runs) != 4 ||
		health.runs[0].Operation != retentionhealth.OperationAttachments ||
		health.runs[0].Result != retentionhealth.ResultFailed ||
		health.runs[0].Processed != 1 ||
		health.runs[1].Operation != retentionhealth.OperationPrivacyExportArchives ||
		health.runs[1].Result != retentionhealth.ResultSucceeded ||
		health.runs[1].Processed != 1 ||
		health.runs[2].Operation != retentionhealth.OperationReporterPrivacyRequests ||
		health.runs[2].Result != retentionhealth.ResultSucceeded ||
		health.runs[2].Processed != 2 ||
		health.runs[3].Operation != retentionhealth.OperationEmailFeedbackInvitations ||
		health.runs[3].Result != retentionhealth.ResultSucceeded ||
		health.runs[3].Processed != 4 ||
		!health.runs[0].AttemptedAt.Equal(now) {
		t.Fatalf("health runs = %#v", health.runs)
	}
	if !reflect.DeepEqual(storage.removed, []string{
		"attachment-one", "attachment-two", "archive-one",
	}) {
		t.Fatalf("removed objects = %#v", storage.removed)
	}
	if !reflect.DeepEqual(store.markedAttachments, []string{"attachment-one"}) ||
		!reflect.DeepEqual(store.markedArchives, []string{"archive-one"}) {
		t.Fatalf("marked attachments=%#v archives=%#v",
			store.markedAttachments, store.markedArchives)
	}
	if store.requestCalls != 1 || !store.lastNow.Equal(now) {
		t.Fatalf("request calls=%d now=%v", store.requestCalls, store.lastNow)
	}
	if !strings.Contains(logs.String(), `"attachment_cleanup_failed":true`) ||
		!strings.Contains(logs.String(), `"requests_expired":2`) {
		t.Fatalf("retention logs = %s", logs.String())
	}
}

func TestRetentionMaintainerBoundsWorkAndSupportsShutdown(t *testing.T) {
	t.Parallel()
	store := &retentionOperationsStub{
		attachmentBatches: [][]RetentionObjectCandidate{
			{{ID: "a1", ObjectKey: "a1"}},
			{{ID: "a2", ObjectKey: "a2"}},
		},
		archiveBatches: [][]RetentionObjectCandidate{
			{{ID: "e1", ObjectKey: "e1"}},
			{{ID: "e2", ObjectKey: "e2"}},
		},
		requestResults:    []int64{1, 1},
		invitationResults: []int64{1, 1},
	}
	health := &retentionHealthRecorderStub{}
	var logs bytes.Buffer
	maintainer := RetentionMaintainer{
		Store: store, Storage: &retentionStorageStub{}, Health: health,
		Logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
		BatchSize: 1, MaxBatches: 2,
	}
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	if !strings.Contains(logs.String(), `"msg":"privacy retention budget exhausted"`) ||
		!strings.Contains(logs.String(), `"request_budget_exhausted":true`) {
		t.Fatalf("budget logs = %s", logs.String())
	}
	if len(health.runs) != 4 || !health.runs[0].BudgetExhausted ||
		!health.runs[1].BudgetExhausted || !health.runs[2].BudgetExhausted ||
		!health.runs[3].BudgetExhausted {
		t.Fatalf("health runs = %#v", health.runs)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := maintainer.Run(ctx, time.Hour); err != nil {
		t.Fatalf("Run(cancelled) error = %v", err)
	}
}

func TestRetentionMaintainerValidationAndPhaseErrors(t *testing.T) {
	t.Parallel()
	if err := (RetentionMaintainer{}).MaintainOnce(context.Background()); err == nil {
		t.Fatal("MaintainOnce(no store) error = nil")
	}
	if err := (RetentionMaintainer{Store: &retentionOperationsStub{}}).
		MaintainOnce(context.Background()); err == nil {
		t.Fatal("MaintainOnce(no storage) error = nil")
	}
	if err := (RetentionMaintainer{
		Store: &retentionOperationsStub{}, Storage: &retentionStorageStub{},
	}).MaintainOnce(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "health recorder is unavailable") {
		t.Fatalf("MaintainOnce(no health recorder) error = %v", err)
	}
	want := errors.New("database unavailable")
	store := &retentionOperationsStub{
		attachmentErr: want, archiveErr: want, requestErr: want, invitationErr: want,
	}
	health := &retentionHealthRecorderStub{}
	err := (RetentionMaintainer{
		Store: store, Storage: &retentionStorageStub{}, Health: health,
	}).MaintainOnce(context.Background())
	if !errors.Is(err, want) || store.archiveCalls != 1 || store.requestCalls != 1 ||
		store.invitationCalls != 1 {
		t.Fatalf("MaintainOnce(independent failures) error=%v calls=%d/%d/%d",
			err, store.archiveCalls, store.requestCalls, store.invitationCalls)
	}
	if len(health.runs) != 4 ||
		health.runs[0].Result != retentionhealth.ResultFailed ||
		health.runs[1].Result != retentionhealth.ResultFailed ||
		health.runs[2].Result != retentionhealth.ResultFailed ||
		health.runs[3].Result != retentionhealth.ResultFailed {
		t.Fatalf("failed health runs = %#v", health.runs)
	}
	recordFailure := errors.New("health database unavailable")
	err = (RetentionMaintainer{
		Store: &retentionOperationsStub{}, Storage: &retentionStorageStub{},
		Health: &retentionHealthRecorderStub{err: recordFailure},
	}).MaintainOnce(context.Background())
	if !errors.Is(err, recordFailure) {
		t.Fatalf("MaintainOnce(record failure) error = %v", err)
	}
	if (RetentionMaintainer{}).now().Location() != time.UTC {
		t.Fatal("default retention clock is not UTC")
	}
}

type retentionHealthRecorderStub struct {
	mu   sync.Mutex
	runs []retentionhealth.Run
	err  error
}

func (recorder *retentionHealthRecorderStub) RecordRun(
	_ context.Context,
	run retentionhealth.Run,
) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.runs = append(recorder.runs, run)
	return recorder.err
}

type retentionOperationsStub struct {
	mu                sync.Mutex
	attachmentBatches [][]RetentionObjectCandidate
	archiveBatches    [][]RetentionObjectCandidate
	requestResults    []int64
	invitationResults []int64
	attachmentErr     error
	archiveErr        error
	requestErr        error
	invitationErr     error
	markedAttachments []string
	markedArchives    []string
	archiveCalls      int
	requestCalls      int
	invitationCalls   int
	lastNow           time.Time
}

func (store *retentionOperationsStub) ExpiredAttachments(
	_ context.Context,
	now time.Time,
	_ int,
) ([]RetentionObjectCandidate, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.lastNow = now
	if store.attachmentErr != nil {
		return nil, store.attachmentErr
	}
	if len(store.attachmentBatches) == 0 {
		return nil, nil
	}
	result := store.attachmentBatches[0]
	store.attachmentBatches = store.attachmentBatches[1:]
	return result, nil
}

func (store *retentionOperationsStub) MarkAttachmentRetentionDeleted(
	_ context.Context,
	candidate RetentionObjectCandidate,
	_ time.Time,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.markedAttachments = append(store.markedAttachments, candidate.ID)
	return nil
}

func (store *retentionOperationsStub) ExpiredExportArchives(
	_ context.Context,
	now time.Time,
	_ int,
) ([]RetentionObjectCandidate, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.archiveCalls++
	store.lastNow = now
	if store.archiveErr != nil {
		return nil, store.archiveErr
	}
	if len(store.archiveBatches) == 0 {
		return nil, nil
	}
	result := store.archiveBatches[0]
	store.archiveBatches = store.archiveBatches[1:]
	return result, nil
}

func (store *retentionOperationsStub) MarkExportArchiveExpired(
	_ context.Context,
	candidate RetentionObjectCandidate,
	_ time.Time,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.markedArchives = append(store.markedArchives, candidate.ID)
	return nil
}

func (store *retentionOperationsStub) ExpireReporterRequests(
	_ context.Context,
	now time.Time,
	_ int,
) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.requestCalls++
	store.lastNow = now
	if store.requestErr != nil {
		return 0, store.requestErr
	}
	if len(store.requestResults) == 0 {
		return 0, nil
	}
	result := store.requestResults[0]
	store.requestResults = store.requestResults[1:]
	return result, nil
}

func (store *retentionOperationsStub) ExpireEmailFeedbackInvitations(
	_ context.Context,
	now time.Time,
	_ int,
) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.invitationCalls++
	store.lastNow = now
	if store.invitationErr != nil {
		return 0, store.invitationErr
	}
	if len(store.invitationResults) == 0 {
		return 0, nil
	}
	result := store.invitationResults[0]
	store.invitationResults = store.invitationResults[1:]
	return result, nil
}

type retentionStorageStub struct {
	mu       sync.Mutex
	removed  []string
	failures map[string]error
}

func (storage *retentionStorageStub) RemoveObject(_ context.Context, key string) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.removed = append(storage.removed, key)
	return storage.failures[key]
}
