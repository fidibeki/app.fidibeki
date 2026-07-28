package ai

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQueuedAIStopsAfterTenantSuspension(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store := Store{Database: pool, Model: "gemini-test"}
	prepared, _, err := store.PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil {
		t.Fatalf("PrepareFeedback() error = %v", err)
	}
	textTask := preparedTaskOfKind(t, prepared, TaskKindText)
	attachmentTask := preparedTaskOfKind(t, prepared, TaskKindAttachment)
	if _, err := pool.Exec(ctx, `
		UPDATE tenants SET status = 'suspended' WHERE id = $1
	`, testTenantID); err != nil {
		t.Fatalf("suspend tenant after enqueue: %v", err)
	}

	analyzer := &lifecycleAnalyzer{}
	storage := &lifecycleObjectSource{}
	processor := Processor{
		Store: store, Analyzer: analyzer, Storage: storage,
		Limiter: allowLimiter{}, RPM: 15, Now: func() time.Time { return now.Add(time.Minute) },
	}
	if err := runPreparedTask(ctx, processor, textTask); err != nil {
		t.Fatalf("TextHandler() error = %v", err)
	}
	if analyzer.textCalls != 0 {
		t.Fatalf("AnalyzeText() calls = %d, want 0", analyzer.textCalls)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "skipped", nil)
	if err := runPreparedTask(ctx, processor, attachmentTask); err != nil {
		t.Fatalf("AttachmentHandler() error = %v", err)
	}
	if storage.openCalls != 0 {
		t.Fatalf("OpenObject() calls = %d, want 0", storage.openCalls)
	}
	if analyzer.attachmentCalls != 0 {
		t.Fatalf("AnalyzeAttachment() calls = %d, want 0", analyzer.attachmentCalls)
	}
	assertAttachmentState(t, ctx, pool, attachmentTask.ResourceID, "skipped", 1, nil)

	reprepared, exhausted, err := store.PrepareFeedback(
		ctx, testFeedbackID, 10, now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("PrepareFeedback(inactive in-flight) error = %v", err)
	}
	if len(reprepared) != 0 || exhausted {
		t.Fatalf(
			"PrepareFeedback(inactive in-flight) = %#v, exhausted = %t",
			reprepared, exhausted,
		)
	}
	assertAttachmentState(t, ctx, pool, testScreenshotID, "skipped", 1, nil)
	var reservations, tenantUsage, projectUsage int
	if err := pool.QueryRow(ctx, `
		SELECT
		    (SELECT count(*) FROM ai_work_reservations WHERE tenant_id = $1),
		    COALESCE((SELECT requests FROM ai_daily_usage
		              WHERE tenant_id = $1 AND usage_date = $2), 0),
		    COALESCE((SELECT requests FROM ai_project_daily_usage
		              WHERE project_id = $3 AND usage_date = $2), 0)
	`, testTenantID, midnightUTC(now), testProjectID).Scan(
		&reservations, &tenantUsage, &projectUsage,
	); err != nil {
		t.Fatalf("read inactive scheduling budgets: %v", err)
	}
	if reservations != 0 || tenantUsage != 0 || projectUsage != 0 {
		t.Fatalf(
			"inactive scheduling budgets = reservations %d tenant %d project %d, want zero",
			reservations, tenantUsage, projectUsage,
		)
	}
}

func TestClaimedAIReauthorizesBeforeProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)

	now := time.Date(2026, 7, 28, 13, 0, 0, 0, time.UTC)
	store := Store{Database: pool, Model: "gemini-test"}
	prepared, _, err := store.PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil {
		t.Fatalf("PrepareFeedback() error = %v", err)
	}
	textTask := preparedTaskOfKind(t, prepared, TaskKindText)
	analyzer := &lifecycleAnalyzer{}
	processor := Processor{
		Store: store, Analyzer: analyzer, Storage: &lifecycleObjectSource{},
		Limiter: lifecycleTransitionLimiter{transition: func(ctx context.Context) error {
			_, err := pool.Exec(ctx, `
				UPDATE tenants SET deletion_requested_at = $2 WHERE id = $1
			`, testTenantID, now.Add(30*time.Second))
			return err
		}},
		RPM: 15, Now: func() time.Time { return now.Add(time.Minute) },
	}
	if err := runPreparedTask(ctx, processor, textTask); err != nil {
		t.Fatalf("TextHandler() error = %v", err)
	}
	if analyzer.textCalls != 0 {
		t.Fatalf("AnalyzeText() calls = %d, want 0", analyzer.textCalls)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "skipped", nil)

	var status, reason string
	var completedAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT status, failure_reason, completed_at
		FROM ai_processing_runs
		WHERE tenant_id = $1 AND feedback_id = $2 AND generation = $3
		  AND attachment_id IS NULL
	`, testTenantID, testFeedbackID, textTask.Generation).Scan(
		&status, &reason, &completedAt,
	); err != nil {
		t.Fatalf("read lifecycle-cancelled text run: %v", err)
	}
	if status != "discarded" || reason != tenantLifecycleInactiveReason || completedAt == nil {
		t.Fatalf("lifecycle-cancelled text run = %s/%s/%v", status, reason, completedAt)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE tenants
		SET status = 'active', deletion_requested_at = NULL
		WHERE id = $1
	`, testTenantID); err != nil {
		t.Fatalf("restore active tenant before attachment claim: %v", err)
	}
	attachmentTask := preparedTaskOfKind(t, prepared, TaskKindAttachment)
	attachmentAnalyzer := &lifecycleAnalyzer{}
	storage := &lifecycleObjectSource{}
	processor = Processor{
		Store: store, Analyzer: attachmentAnalyzer, Storage: storage,
		Limiter: lifecycleTransitionLimiter{transition: func(ctx context.Context) error {
			_, err := pool.Exec(ctx, `
				UPDATE tenants SET status = 'suspended' WHERE id = $1
			`, testTenantID)
			return err
		}},
		RPM: 15, Now: func() time.Time { return now.Add(2 * time.Minute) },
	}
	if err := runPreparedTask(ctx, processor, attachmentTask); err != nil {
		t.Fatalf("AttachmentHandler() error = %v", err)
	}
	if storage.openCalls != 0 {
		t.Fatalf("OpenObject() calls = %d, want 0", storage.openCalls)
	}
	if attachmentAnalyzer.attachmentCalls != 0 {
		t.Fatalf("AnalyzeAttachment() calls = %d, want 0", attachmentAnalyzer.attachmentCalls)
	}
	assertAttachmentState(t, ctx, pool, attachmentTask.ResourceID, "skipped", 1, nil)

	status, reason, completedAt = "", "", nil
	if err := pool.QueryRow(ctx, `
		SELECT status, failure_reason, completed_at
		FROM ai_processing_runs
		WHERE tenant_id = $1 AND attachment_id = $2 AND attempt = $3
	`, testTenantID, attachmentTask.ResourceID, attachmentTask.Attempt).Scan(
		&status, &reason, &completedAt,
	); err != nil {
		t.Fatalf("read lifecycle-cancelled attachment run: %v", err)
	}
	if status != "discarded" || reason != tenantLifecycleInactiveReason || completedAt == nil {
		t.Fatalf("lifecycle-cancelled attachment run = %s/%s/%v", status, reason, completedAt)
	}
}

func TestProviderAuthorizationFailureIsFailClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)

	now := time.Date(2026, 7, 28, 13, 30, 0, 0, time.UTC)
	store := Store{Database: pool, Model: "gemini-test"}
	prepared, _, err := store.PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil {
		t.Fatalf("PrepareFeedback() error = %v", err)
	}
	analyzer := &lifecycleAnalyzer{}
	storage := &lifecycleObjectSource{}

	textCtx, cancelText := context.WithCancel(ctx)
	textProcessor := Processor{
		Store: store, Analyzer: analyzer, Storage: storage,
		Limiter: cancelingLimiter{cancel: cancelText},
		RPM:     15, Now: func() time.Time { return now.Add(time.Minute) },
	}
	if err := runPreparedTask(
		textCtx, textProcessor, preparedTaskOfKind(t, prepared, TaskKindText),
	); err == nil {
		t.Fatal("TextHandler() error = nil after authorization context cancellation")
	}
	if analyzer.textCalls != 0 {
		t.Fatalf("AnalyzeText() calls = %d, want 0", analyzer.textCalls)
	}

	attachmentCtx, cancelAttachment := context.WithCancel(ctx)
	attachmentProcessor := Processor{
		Store: store, Analyzer: analyzer, Storage: storage,
		Limiter: cancelingLimiter{cancel: cancelAttachment},
		RPM:     15, Now: func() time.Time { return now.Add(time.Minute) },
	}
	if err := runPreparedTask(
		attachmentCtx,
		attachmentProcessor,
		preparedTaskOfKind(t, prepared, TaskKindAttachment),
	); err == nil {
		t.Fatal("AttachmentHandler() error = nil after authorization context cancellation")
	}
	if storage.openCalls != 0 {
		t.Fatalf("OpenObject() calls = %d, want 0", storage.openCalls)
	}
	if analyzer.attachmentCalls != 0 {
		t.Fatalf("AnalyzeAttachment() calls = %d, want 0", analyzer.attachmentCalls)
	}
}

func TestTenantLifecyclePolicyStopsAIScheduling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	now := time.Date(2026, 7, 28, 14, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Minute)
	deletionRequested := now.Add(-time.Second)
	for _, test := range []struct {
		name       string
		status     string
		trialEnd   *time.Time
		deletionAt *time.Time
		want       bool
	}{
		{name: "active", status: "active", want: true},
		{name: "unexpired trial", status: "trial", trialEnd: &future, want: true},
		{name: "suspended", status: "suspended"},
		{name: "cancelled", status: "cancelled"},
		{name: "expired trial", status: "trial", trialEnd: &past},
		{
			name: "active pending deletion", status: "active",
			deletionAt: &deletionRequested,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var eligible bool
			if err := pool.QueryRow(ctx, `
				SELECT fidibeki_ai_tenant_processing_eligible(
				    $1::tenant_status, $2::timestamptz, $3::timestamptz, $4
				)
			`, test.status, test.trialEnd, test.deletionAt, now).Scan(&eligible); err != nil {
				t.Fatalf("evaluate lifecycle policy: %v", err)
			}
			if eligible != test.want {
				t.Fatalf("eligible = %t, want %t", eligible, test.want)
			}
		})
	}

	seedAIFeedback(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		UPDATE tenants SET status = 'suspended' WHERE id = $1
	`, testTenantID); err != nil {
		t.Fatalf("suspend tenant before scheduling: %v", err)
	}
	prepared, exhausted, err := (Store{Database: pool, Model: "gemini-test"}).
		PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil {
		t.Fatalf("PrepareFeedback() error = %v", err)
	}
	if len(prepared) != 0 || exhausted {
		t.Fatalf("PrepareFeedback() = %#v, exhausted = %t; want no work", prepared, exhausted)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "skipped", timePointer(now))
	assertAttachmentState(t, ctx, pool, testAudioID, "skipped", 0, nil)
	assertAttachmentState(t, ctx, pool, testScreenshotID, "skipped", 0, nil)

	var reservations int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM ai_work_reservations WHERE tenant_id = $1
	`, testTenantID).Scan(&reservations); err != nil {
		t.Fatalf("count inactive tenant reservations: %v", err)
	}
	if reservations != 0 {
		t.Fatalf("inactive tenant reservations = %d, want 0", reservations)
	}
}

func TestReconcilerCancelsInactiveAIAndPreservesProviderCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)

	now := time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC)
	store := Store{Database: pool, Model: "gemini-test"}
	prepared, _, err := store.PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil || len(prepared) != 3 {
		t.Fatalf("PrepareFeedback() = %#v, %v", prepared, err)
	}
	const (
		pendingFeedbackID = "71000000-0000-4000-8000-000000000001"
		geminiFileID      = "71000000-0000-4000-8000-000000000002"
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO feedbacks (
			id, tenant_id, project_id, widget_rule_id, public_id, issue_id,
			client_feedback_id, text, page_url, text_process_status
		) VALUES (
			$1, $2, $3, $4, 'fb_Life0001', $5,
			'71000000-0000-4000-8000-000000000003',
			'Pending lifecycle work', 'https://example.com/lifecycle', 'pending'
		)
	`, pendingFeedbackID, testTenantID, testProjectID, testWidgetRuleID, testIssueID); err != nil {
		t.Fatalf("seed pending inactive AI work: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO ai_gemini_files (
			id, tenant_id, attachment_id, file_name, delete_after
		) VALUES ($1, $2, $3, 'files/lifecycle-cleanup', $4)
	`, geminiFileID, testTenantID, testAudioID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("seed provider cleanup work: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE tenants SET status = 'suspended' WHERE id = $1
	`, testTenantID); err != nil {
		t.Fatalf("suspend tenant before reconciliation: %v", err)
	}

	pending, err := store.PendingFeedbackIDs(ctx, 100, 10, now)
	if err != nil {
		t.Fatalf("PendingFeedbackIDs() error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("PendingFeedbackIDs() = %#v, want none", pending)
	}
	inFlight, err := store.InFlightTasks(ctx, 100)
	if err != nil {
		t.Fatalf("InFlightTasks() error = %v", err)
	}
	if len(inFlight) != 0 {
		t.Fatalf("InFlightTasks() = %#v, want no analysis work", inFlight)
	}

	queue := &capturingTaskQueue{}
	scheduler := Scheduler{
		Store: store, Queue: queue, DailyLimit: 10, Now: func() time.Time { return now },
	}
	if err := scheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	if len(queue.tasks) != 1 || queue.tasks[0].Kind != TaskKindFileDelete ||
		queue.tasks[0].ResourceID != geminiFileID {
		t.Fatalf("reconciled tasks = %#v, want only Gemini cleanup", queue.tasks)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "skipped", timePointer(now))
	assertAIState(t, ctx, pool, pendingFeedbackID, "skipped", timePointer(now))
	assertAttachmentState(t, ctx, pool, testAudioID, "skipped", 1, nil)
	assertAttachmentState(t, ctx, pool, testScreenshotID, "skipped", 1, nil)

	var reservations, tenantUsage, projectUsage int
	if err := pool.QueryRow(ctx, `
		SELECT
		    (SELECT count(*) FROM ai_work_reservations WHERE tenant_id = $1),
		    COALESCE((SELECT requests FROM ai_daily_usage
		              WHERE tenant_id = $1 AND usage_date = $2), 0),
		    COALESCE((SELECT requests FROM ai_project_daily_usage
		              WHERE project_id = $3 AND usage_date = $2), 0)
	`, testTenantID, midnightUTC(now), testProjectID).Scan(
		&reservations, &tenantUsage, &projectUsage,
	); err != nil {
		t.Fatalf("read cancelled AI budgets: %v", err)
	}
	if reservations != 0 || tenantUsage != 0 || projectUsage != 0 {
		t.Fatalf(
			"cancelled AI budgets = reservations %d tenant %d project %d, want zero",
			reservations, tenantUsage, projectUsage,
		)
	}
	cancelledAgain, err := store.CancelIneligibleWork(ctx, 500, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("CancelIneligibleWork(idempotent) error = %v", err)
	}
	if cancelledAgain != 0 {
		t.Fatalf("CancelIneligibleWork(idempotent) = %d, want 0", cancelledAgain)
	}
}

func TestLifecycleCancellationPreservesCompletedAttachmentBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)

	now := time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		UPDATE widget_rules
		SET ai_processing = jsonb_set(
		    ai_processing,
		    '{input_categories}',
		    '["audio","screenshot"]'::jsonb
		)
		WHERE id = $1
	`, testWidgetRuleID); err != nil {
		t.Fatalf("restrict AI rule to attachments: %v", err)
	}
	store := Store{Database: pool, Model: "gemini-test"}
	prepared, _, err := store.PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil || len(prepared) != 2 {
		t.Fatalf("PrepareFeedback() = %#v, %v, want two attachments", prepared, err)
	}
	audioTask := preparedTaskForResource(t, prepared, testAudioID)
	processor := Processor{
		Store: store, Analyzer: &lifecycleAnalyzer{}, Storage: &lifecycleObjectSource{},
		Limiter: allowLimiter{}, RPM: 15, Now: func() time.Time { return now.Add(time.Minute) },
	}
	if err := runPreparedTask(ctx, processor, audioTask); err != nil {
		t.Fatalf("complete audio attachment: %v", err)
	}
	assertAttachmentState(t, ctx, pool, testAudioID, "completed", 1, nil)
	assertAttachmentState(t, ctx, pool, testScreenshotID, "processing", 1, nil)

	if _, err := pool.Exec(ctx, `
		UPDATE tenants SET status = 'suspended' WHERE id = $1
	`, testTenantID); err != nil {
		t.Fatalf("suspend tenant with mixed attachment states: %v", err)
	}
	if cancelled, err := store.CancelIneligibleWork(ctx, 500, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("CancelIneligibleWork() error = %v", err)
	} else if cancelled != 1 {
		t.Fatalf("CancelIneligibleWork() = %d, want 1 feedback", cancelled)
	}
	assertAttachmentState(t, ctx, pool, testAudioID, "completed", 1, nil)
	assertAttachmentState(t, ctx, pool, testScreenshotID, "skipped", 1, nil)
	assertLifecycleBudgetState(
		t, ctx, pool, now,
		"attachment", testAudioID, 1,
		1, 1,
	)
}

func TestLifecycleCancellationPreservesCompletedTextGenerationBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)

	now := time.Date(2026, 7, 28, 17, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		UPDATE widget_rules
		SET ai_processing = jsonb_set(
		    ai_processing,
		    '{input_categories}',
		    '["feedback_text"]'::jsonb
		)
		WHERE id = $1
	`, testWidgetRuleID); err != nil {
		t.Fatalf("restrict AI rule to feedback text: %v", err)
	}
	store := Store{Database: pool, Model: "gemini-test"}
	prepared, _, err := store.PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil || len(prepared) != 1 {
		t.Fatalf("PrepareFeedback() = %#v, %v, want one text task", prepared, err)
	}
	textTask := preparedTaskOfKind(t, prepared, TaskKindText)
	processor := Processor{
		Store: store, Analyzer: &lifecycleAnalyzer{}, Storage: &lifecycleObjectSource{},
		Limiter: allowLimiter{}, RPM: 15, Now: func() time.Time { return now.Add(time.Minute) },
	}
	if err := runPreparedTask(ctx, processor, textTask); err != nil {
		t.Fatalf("complete feedback text: %v", err)
	}
	if err := store.ReprocessFeedback(ctx, pool, testTenantID, testFeedbackID); err != nil {
		t.Fatalf("ReprocessFeedback() error = %v", err)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "pending", nil)

	if _, err := pool.Exec(ctx, `
		UPDATE tenants SET deletion_requested_at = $2 WHERE id = $1
	`, testTenantID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("request deletion with pending text reprocessing: %v", err)
	}
	if cancelled, err := store.CancelIneligibleWork(ctx, 500, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("CancelIneligibleWork() error = %v", err)
	} else if cancelled != 1 {
		t.Fatalf("CancelIneligibleWork() = %d, want 1 feedback", cancelled)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "skipped", timePointer(now.Add(3*time.Minute)))
	assertLifecycleBudgetState(
		t, ctx, pool, now,
		"text", testFeedbackID, textTask.Generation,
		1, 1,
	)
}

func preparedTaskOfKind(t *testing.T, tasks []PreparedTask, kind TaskKind) PreparedTask {
	t.Helper()
	for _, task := range tasks {
		if task.Kind == kind {
			return task
		}
	}
	t.Fatalf("prepared tasks %#v do not contain kind %q", tasks, kind)
	return PreparedTask{}
}

func preparedTaskForResource(t *testing.T, tasks []PreparedTask, resourceID string) PreparedTask {
	t.Helper()
	for _, task := range tasks {
		if task.ResourceID == resourceID {
			return task
		}
	}
	t.Fatalf("prepared tasks %#v do not contain resource %q", tasks, resourceID)
	return PreparedTask{}
}

func assertLifecycleBudgetState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
	kind string,
	resourceID string,
	generation int64,
	wantReservations, wantUsage int,
) {
	t.Helper()
	var reservations, preserved, tenantUsage, projectUsage int
	if err := pool.QueryRow(ctx, `
		SELECT
		    (SELECT count(*) FROM ai_work_reservations WHERE tenant_id = $1),
		    (SELECT count(*) FROM ai_work_reservations
		     WHERE tenant_id = $1 AND work_kind = $4
		       AND resource_id = $5 AND generation = $6),
		    COALESCE((SELECT requests FROM ai_daily_usage
		              WHERE tenant_id = $1 AND usage_date = $2), 0),
		    COALESCE((SELECT requests FROM ai_project_daily_usage
		              WHERE project_id = $3 AND usage_date = $2), 0)
	`, testTenantID, midnightUTC(now), testProjectID, kind, resourceID, generation).Scan(
		&reservations, &preserved, &tenantUsage, &projectUsage,
	); err != nil {
		t.Fatalf("read lifecycle budget state: %v", err)
	}
	if reservations != wantReservations || preserved != 1 ||
		tenantUsage != wantUsage || projectUsage != wantUsage {
		t.Fatalf(
			"lifecycle budgets = reservations %d preserved %d tenant %d project %d, want %d/1/%d/%d",
			reservations, preserved, tenantUsage, projectUsage,
			wantReservations, wantUsage, wantUsage,
		)
	}
}

type lifecycleAnalyzer struct {
	textCalls       int
	attachmentCalls int
}

func (analyzer *lifecycleAnalyzer) AnalyzeText(context.Context, string) (Result, error) {
	analyzer.textCalls++
	return Result{CleanedText: "must not leave Fidibeki"}, nil
}

func (analyzer *lifecycleAnalyzer) AnalyzeAttachment(
	context.Context,
	AttachmentWork,
	io.Reader,
) (Result, error) {
	analyzer.attachmentCalls++
	return Result{CleanedText: "must not leave Fidibeki"}, nil
}

type lifecycleObjectSource struct {
	openCalls int
}

func (source *lifecycleObjectSource) OpenObject(context.Context, string) (io.ReadCloser, error) {
	source.openCalls++
	return io.NopCloser(&emptyReader{}), nil
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

type lifecycleTransitionLimiter struct {
	transition func(context.Context) error
}

func (limiter lifecycleTransitionLimiter) Allow(
	ctx context.Context,
	_ string,
	_ ratelimit.Window,
) (ratelimit.Decision, error) {
	if err := limiter.transition(ctx); err != nil {
		return ratelimit.Decision{}, err
	}
	return ratelimit.Decision{Allowed: true}, nil
}

type cancelingLimiter struct {
	cancel context.CancelFunc
}

func (limiter cancelingLimiter) Allow(
	context.Context,
	string,
	ratelimit.Window,
) (ratelimit.Decision, error) {
	limiter.cancel()
	return ratelimit.Decision{Allowed: true}, nil
}
