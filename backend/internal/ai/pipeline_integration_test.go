package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	feedbackdomain "github.com/acidiney/app.fidibeki/backend/internal/feedback"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	testTenantID            = "10000000-0000-4000-8000-000000000001"
	testMemberID            = "10000000-0000-4000-8000-000000000010"
	testProjectID           = "10000000-0000-4000-8000-000000000002"
	testWidgetRuleID        = "10000000-0000-4000-8000-000000000012"
	testIssueID             = "10000000-0000-4000-8000-000000000003"
	testFeedbackID          = "10000000-0000-4000-8000-000000000004"
	testAudioID             = "10000000-0000-4000-8000-000000000005"
	testScreenshotID        = "10000000-0000-4000-8000-000000000006"
	testReplayID            = "10000000-0000-4000-8000-000000000007"
	testStarterTenantID     = "30000000-0000-4000-8000-000000000001"
	testStarterProjectID    = "30000000-0000-4000-8000-000000000002"
	testStarterWidgetRuleID = "30000000-0000-4000-8000-000000000012"
	testStarterIssueID      = "30000000-0000-4000-8000-000000000003"
	testStarterFeedbackID   = "30000000-0000-4000-8000-000000000004"
	testStarterAttachmentID = "30000000-0000-4000-8000-000000000005"
)

func TestAIPipelineStateMachine(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)
	diagnostics := &capturingDiagnosticRecorder{}
	store := Store{Database: pool, Diagnostics: diagnostics}
	dayOne := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if _, _, err := store.PrepareFeedback(
		ctx, "90000000-0000-4000-8000-000000000001", 2, dayOne,
	); !errors.Is(err, ErrNotFound) {
		t.Errorf("PrepareFeedback(missing) error = %v", err)
	}
	starterTasks, starterExhausted, err := store.PrepareFeedback(ctx, testStarterFeedbackID, 2, dayOne)
	if err != nil || starterExhausted || len(starterTasks) != 0 {
		t.Fatalf("PrepareFeedback(starter) = %#v, exhausted = %t, error = %v",
			starterTasks, starterExhausted, err)
	}
	var starterTextStatus, starterAttachmentStatus string
	var starterProcessedAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT feedback.text_process_status, feedback.ai_processed_at, attachment.processing_status
		FROM feedbacks AS feedback
		JOIN attachments AS attachment ON attachment.feedback_id = feedback.id
		WHERE feedback.id = $1
	`, testStarterFeedbackID).Scan(
		&starterTextStatus, &starterProcessedAt, &starterAttachmentStatus,
	); err != nil {
		t.Fatalf("read starter AI state: %v", err)
	}
	if starterTextStatus != "skipped" || starterAttachmentStatus != "skipped" || starterProcessedAt == nil {
		t.Errorf("starter AI state = %s/%s/%#v", starterTextStatus, starterAttachmentStatus, starterProcessedAt)
	}

	prepared, exhausted, err := store.PrepareFeedback(ctx, testFeedbackID, 2, dayOne)
	if err != nil {
		t.Fatalf("PrepareFeedback(day one) error = %v", err)
	}
	if !exhausted || len(prepared) != 2 || prepared[0].Kind != TaskKindText ||
		prepared[1].ResourceID != testAudioID {
		t.Fatalf("prepared day one = %#v, exhausted = %t", prepared, exhausted)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "processing", nil)
	assertAttachmentState(t, ctx, pool, testReplayID, "skipped", 0, nil)
	assertUsage(t, ctx, pool, dayOne, 2)
	if len(diagnostics.events) != 1 ||
		diagnostics.events[0].TenantID != testTenantID ||
		diagnostics.events[0].ProjectID != testProjectID ||
		diagnostics.events[0].Category != abuse.CategoryAIBudget ||
		diagnostics.events[0].Reason != budgetDeferredReason {
		t.Fatalf("AI budget diagnostics = %#v", diagnostics.events)
	}

	analyzer := &fakeAnalyzer{}
	processor := Processor{
		Store: store, Analyzer: analyzer, Storage: stringObjectSource{}, Limiter: allowLimiter{}, RPM: 15,
		Now: func() time.Time { return dayOne.Add(time.Minute) },
	}
	var dayOneTextTask, dayOneAttachmentTask PreparedTask
	for _, task := range prepared {
		if task.Kind == TaskKindText {
			dayOneTextTask = task
		} else {
			dayOneAttachmentTask = task
		}
	}
	if err := runPreparedTask(ctx, processor, dayOneAttachmentTask); err != nil {
		t.Fatalf("process day-one attachment %#v: %v", dayOneAttachmentTask, err)
	}
	assertRecommendations(t, ctx, pool, testFeedbackID,
		"Checkout audio is confusing", "actionable", "checkout", "high")
	if err := runPreparedTask(ctx, processor, dayOneTextTask); err != nil {
		t.Fatalf("process day-one text %#v: %v", dayOneTextTask, err)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "completed", nil)
	assertRecommendations(t, ctx, pool, testFeedbackID,
		"Checkout is confusing", "actionable", "checkout", "high")
	assertAttachmentState(t, ctx, pool, testAudioID, "completed", 1, nil)
	assertAttachmentState(
		t, ctx, pool, testScreenshotID, "pending", 0, stringPointer(budgetDeferredReason),
	)

	dayTwo := dayOne.Add(24 * time.Hour)
	if _, err := pool.Exec(ctx, `
		UPDATE attachments
		SET safety_status = 'quarantined', safety_checked_at = NULL,
		    detected_content_type = NULL
		WHERE id = $1
	`, testScreenshotID); err != nil {
		t.Fatalf("quarantine screenshot before AI scheduling: %v", err)
	}
	prepared, exhausted, err = store.PrepareFeedback(ctx, testFeedbackID, 2, dayTwo)
	if err != nil || exhausted || len(prepared) != 0 {
		t.Fatalf("PrepareFeedback(quarantined) = %#v, exhausted = %t, error = %v",
			prepared, exhausted, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE attachments
		SET safety_status = 'safe', safety_checked_at = $2,
		    detected_content_type = content_type
		WHERE id = $1
	`, testScreenshotID, dayTwo); err != nil {
		t.Fatalf("mark screenshot safe before AI scheduling: %v", err)
	}
	prepared, exhausted, err = store.PrepareFeedback(ctx, testFeedbackID, 2, dayTwo)
	if err != nil || exhausted || len(prepared) != 1 || prepared[0].ResourceID != testScreenshotID {
		t.Fatalf("PrepareFeedback(day two) = %#v, exhausted = %t, error = %v", prepared, exhausted, err)
	}
	processor.Now = func() time.Time { return dayTwo.Add(time.Minute) }
	if err := runPreparedTask(ctx, processor, prepared[0]); err != nil {
		t.Fatalf("process screenshot: %v", err)
	}
	assertAIState(t, ctx, pool, testFeedbackID, "completed", timePointer(dayTwo.Add(time.Minute)))
	assertUsage(t, ctx, pool, dayTwo, 1)

	feedbackID, err := store.RetryAttachment(ctx, pool, testTenantID, testAudioID)
	if err != nil || feedbackID != testFeedbackID {
		t.Fatalf("RetryAttachment() = %q, %v", feedbackID, err)
	}
	dayThree := dayTwo.Add(24 * time.Hour)
	prepared, _, err = store.PrepareFeedback(ctx, testFeedbackID, 10, dayThree)
	if err != nil || len(prepared) != 1 || prepared[0].Attempt != 2 {
		t.Fatalf("PrepareFeedback(retry) = %#v, %v", prepared, err)
	}
	if _, err := store.RetryAttachment(ctx, pool, testTenantID, testAudioID); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("RetryAttachment(processing) error = %v, want ErrAlreadyRunning", err)
	}
	analyzer.attachmentErr = errors.New("upstream timeout")
	processor.Now = func() time.Time { return dayThree.Add(time.Minute) }
	err = runPreparedTask(ctx, processor, prepared[0])
	if err == nil || errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("transient attachment error = %v", err)
	}
	assertAttachmentState(t, ctx, pool, testAudioID, "pending", 2, stringPointer("upstream timeout"))

	analyzer.attachmentErr = nil
	if _, err := store.RetryAttachment(ctx, pool, testTenantID, testAudioID); err != nil {
		t.Fatalf("reset transient attachment: %v", err)
	}
	prepared, _, err = store.PrepareFeedback(ctx, testFeedbackID, 10, dayThree.Add(time.Hour))
	if err != nil || len(prepared) != 1 || prepared[0].Attempt != 3 {
		t.Fatalf("PrepareFeedback(permanent retry) = %#v, %v", prepared, err)
	}
	analyzer.attachmentErr = permanent(errors.New("malformed media"))
	err = runPreparedTask(ctx, processor, prepared[0])
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("permanent attachment error = %v, want SkipRetry", err)
	}
	assertAttachmentState(t, ctx, pool, testAudioID, "failed", 3, stringPointer("malformed media"))

	if err := store.ReprocessFeedback(ctx, pool, testTenantID, testFeedbackID); err != nil {
		t.Fatalf("ReprocessFeedback() error = %v", err)
	}
	dayFour := dayThree.Add(24 * time.Hour)
	prepared, exhausted, err = store.PrepareFeedback(ctx, testFeedbackID, 10, dayFour)
	if err != nil || exhausted || len(prepared) != 3 {
		t.Fatalf("PrepareFeedback(reprocess) = %#v, exhausted = %t, error = %v", prepared, exhausted, err)
	}
	if err := store.ReprocessFeedback(ctx, pool, testTenantID, testFeedbackID); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("ReprocessFeedback(processing) error = %v, want ErrAlreadyRunning", err)
	}
	var textTask PreparedTask
	analyzer.textErr = errors.New("text service timeout")
	for _, task := range prepared {
		if task.Kind == TaskKindText {
			textTask = task
			break
		}
	}
	if err := runPreparedTask(ctx, processor, textTask); err == nil || errors.Is(err, asynq.SkipRetry) {
		t.Errorf("transient text error = %v", err)
	}
	analyzer.textErr = permanent(ErrUnsafeAIResult)
	if err := runPreparedTask(ctx, processor, textTask); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("permanent text error = %v, want SkipRetry", err)
	}
	var unsafeRunStatus, unsafeRunReason string
	if err := pool.QueryRow(ctx, `
		SELECT status, failure_reason
		FROM ai_processing_runs
		WHERE tenant_id = $1 AND feedback_id = $2 AND generation = $3
	`, testTenantID, testFeedbackID, textTask.Generation).Scan(
		&unsafeRunStatus, &unsafeRunReason,
	); err != nil {
		t.Fatalf("read quarantined AI run: %v", err)
	}
	if unsafeRunStatus != "quarantined" || unsafeRunReason != "redaction_policy_failed" {
		t.Fatalf("unsafe AI run = %s/%s", unsafeRunStatus, unsafeRunReason)
	}
	analyzer.textErr = nil
	for _, task := range prepared {
		if task.Kind == TaskKindText {
			continue
		}
		if err := store.UndoPrepared(ctx, task); err != nil {
			t.Fatalf("UndoPrepared(%#v) error = %v", task, err)
		}
	}
	assertUsage(t, ctx, pool, dayFour, 1)
	ids, err := store.PendingFeedbackIDs(ctx, 10, 10, dayFour)
	if err != nil || len(ids) != 1 || ids[0] != testFeedbackID {
		t.Errorf("PendingFeedbackIDs() = %#v, %v", ids, err)
	}

	queue := &capturingTaskQueue{}
	dayFive := dayFour.Add(24 * time.Hour)
	scheduler := Scheduler{
		Store: store, Queue: queue, DailyLimit: 10, Now: func() time.Time { return dayFive },
	}
	if err := scheduler.ReconcileOnce(ctx); err != nil || len(queue.tasks) != 2 {
		t.Fatalf("ReconcileOnce() queued %d tasks, error = %v", len(queue.tasks), err)
	}
	validPipeline := asynq.NewTask("feedback:pipeline", []byte(`{"resource_id":"`+testFeedbackID+`"}`))
	if err := scheduler.PipelineHandler()(ctx, validPipeline); err != nil {
		t.Errorf("PipelineHandler(valid) error = %v", err)
	}
	if err := scheduler.PipelineHandler()(ctx, asynq.NewTask("feedback:pipeline", []byte("{"))); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("PipelineHandler(malformed) error = %v", err)
	}
	missingPipeline := asynq.NewTask("feedback:pipeline", []byte(
		`{"resource_id":"90000000-0000-4000-8000-000000000001"}`,
	))
	if err := scheduler.PipelineHandler()(ctx, missingPipeline); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("PipelineHandler(missing) error = %v", err)
	}
	if err := scheduler.ScheduleAttachment(ctx, testAudioID, testFeedbackID); err != nil {
		t.Errorf("ScheduleAttachment() error = %v", err)
	}
	reconcileCtx, stopReconcile := context.WithCancel(ctx)
	stopReconcile()
	if err := scheduler.RunReconciler(reconcileCtx, time.Millisecond); err != nil {
		t.Errorf("RunReconciler(cancelled) error = %v", err)
	}
	for _, task := range queue.tasks {
		if err := store.UndoPrepared(ctx, task); err != nil {
			t.Fatalf("undo reconciled task: %v", err)
		}
	}
	queue.tasks = nil
	queue.err = errors.New("Redis unavailable")
	daySix := dayFive.Add(24 * time.Hour)
	scheduler.Now = func() time.Time { return daySix }
	if err := scheduler.ScheduleFeedback(ctx, testFeedbackID); err == nil {
		t.Error("ScheduleFeedback(queue failure) error = nil")
	}
	assertUsage(t, ctx, pool, daySix, 0)

	if _, err := store.ClaimText(ctx, testTenantID, testFeedbackID, textTask.Generation); !errors.Is(err, ErrNoWork) {
		t.Errorf("ClaimText(terminal) error = %v", err)
	}
	if _, err := store.ClaimAttachment(ctx, testTenantID, testAudioID, 999); !errors.Is(err, ErrNoWork) {
		t.Errorf("ClaimAttachment(wrong attempt) error = %v", err)
	}
	if _, err := store.RetryAttachment(
		ctx, pool, testTenantID, "90000000-0000-4000-8000-000000000002",
	); !errors.Is(err, ErrNotFound) {
		t.Errorf("RetryAttachment(missing) error = %v", err)
	}
	if err := store.ReprocessFeedback(
		ctx, pool, testTenantID, "90000000-0000-4000-8000-000000000003",
	); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReprocessFeedback(missing) error = %v", err)
	}

	staleIssueID := "40000000-0000-4000-8000-000000000001"
	staleFeedbackID := "40000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx, `
		INSERT INTO issues (id, tenant_id, project_id, public_id, title)
		VALUES ($1, $2, $3, 'AI-TENANT-2026-0099', 'Stale generation')
	`, staleIssueID, testTenantID, testProjectID); err != nil {
		t.Fatalf("seed stale-generation issue: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO feedbacks (
			id, tenant_id, project_id, widget_rule_id, public_id, issue_id, client_feedback_id,
			text, page_url, text_process_status
		) VALUES (
			$1, $2, $3, $4, 'fb_Stale001', $5, '40000000-0000-4000-8000-000000000003',
			'Feedback generation one', 'https://example.com/stale', 'pending'
		)
	`, staleFeedbackID, testTenantID, testProjectID, testWidgetRuleID, staleIssueID); err != nil {
		t.Fatalf("seed stale-generation feedback: %v", err)
	}
	daySeven := daySix.Add(24 * time.Hour)
	staleTasks, _, err := store.PrepareFeedback(ctx, staleFeedbackID, 10, daySeven)
	if err != nil || len(staleTasks) != 1 || staleTasks[0].Generation <= 0 {
		t.Fatalf("PrepareFeedback(stale generation) = %#v, %v", staleTasks, err)
	}
	staleTask := staleTasks[0]
	analyzer.textErr = errors.New("temporary text failure")
	processor.Now = func() time.Time { return daySeven.Add(time.Minute) }
	if err := runPreparedTask(ctx, processor, staleTask); err == nil || errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("run stale text task = %v", err)
	}
	if err := store.ReprocessFeedback(ctx, pool, testTenantID, staleFeedbackID); err != nil {
		t.Fatalf("ReprocessFeedback(pending generation) error = %v", err)
	}
	var generationAfterPending int64
	if err := pool.QueryRow(ctx, `
		SELECT text_processing_generation FROM feedbacks WHERE id = $1
	`, staleFeedbackID).Scan(&generationAfterPending); err != nil {
		t.Fatalf("read pending generation: %v", err)
	}
	if generationAfterPending != staleTask.Generation {
		t.Fatalf("pending reprocess changed generation from %d to %d", staleTask.Generation, generationAfterPending)
	}
	analyzer.textErr = permanent(errors.New("terminal text failure"))
	if err := runPreparedTask(ctx, processor, staleTask); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("terminalize stale-generation fixture = %v", err)
	}
	if err := store.ReprocessFeedback(ctx, pool, testTenantID, staleFeedbackID); err != nil {
		t.Fatalf("ReprocessFeedback(terminal generation) error = %v", err)
	}
	var resetGeneration int64
	if err := pool.QueryRow(ctx, `
		SELECT text_processing_generation FROM feedbacks WHERE id = $1
	`, staleFeedbackID).Scan(&resetGeneration); err != nil {
		t.Fatalf("read reset generation: %v", err)
	}
	if err := store.ReprocessFeedback(ctx, pool, testTenantID, staleFeedbackID); err != nil {
		t.Fatalf("ReprocessFeedback(concurrent-equivalent) error = %v", err)
	}
	var idempotentGeneration int64
	if err := pool.QueryRow(ctx, `
		SELECT text_processing_generation FROM feedbacks WHERE id = $1
	`, staleFeedbackID).Scan(&idempotentGeneration); err != nil {
		t.Fatalf("read idempotent generation: %v", err)
	}
	if idempotentGeneration != resetGeneration {
		t.Fatalf("idempotent reprocess changed generation from %d to %d", resetGeneration, idempotentGeneration)
	}
	newTasks, _, err := store.PrepareFeedback(ctx, staleFeedbackID, 10, daySeven.Add(time.Hour))
	if err != nil || len(newTasks) != 1 || newTasks[0].Generation <= staleTask.Generation {
		t.Fatalf("PrepareFeedback(new generation) = %#v, %v", newTasks, err)
	}
	if _, err := store.ClaimText(
		ctx, testTenantID, staleFeedbackID, staleTask.Generation,
	); !errors.Is(err, ErrNoWork) {
		t.Errorf("ClaimText(stale generation) error = %v", err)
	}
	newTask := newTasks[0]
	inFlight, err := store.InFlightTasks(ctx, 100)
	if err != nil {
		t.Fatalf("InFlightTasks(text) error = %v", err)
	}
	if !containsPreparedTask(inFlight, newTask) {
		t.Errorf("InFlightTasks() = %#v, missing %#v", inFlight, newTask)
	}
	recoveryQueue := &capturingTaskQueue{}
	recoveryScheduler := Scheduler{
		Store: store, Queue: recoveryQueue, DailyLimit: 10,
		Now: func() time.Time { return daySeven.Add(90 * time.Minute) },
	}
	if err := recoveryScheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce(in-flight recovery) error = %v", err)
	}
	if !containsPreparedTask(recoveryQueue.tasks, newTask) {
		t.Errorf("ReconcileOnce() queued %#v, missing in-flight %#v", recoveryQueue.tasks, newTask)
	}
	for _, recoveredTask := range recoveryQueue.tasks {
		if containsPreparedTask([]PreparedTask{recoveredTask}, newTask) {
			continue
		}
		if err := store.UndoPrepared(ctx, recoveredTask); err != nil {
			t.Fatalf("undo recovery side task %#v: %v", recoveredTask, err)
		}
	}
	if err := store.FinalizeExhaustedTask(ctx, TaskKindText, taskPayload{
		TenantID: testTenantID, ResourceID: staleFeedbackID, Generation: newTask.Generation,
	}, errors.New("retry limit reached"), daySeven.Add(2*time.Hour)); err != nil {
		t.Fatalf("FinalizeExhaustedTask(text) error = %v", err)
	}
	var exhaustedStatus string
	var exhaustedProcessedAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT text_process_status, ai_processed_at FROM feedbacks WHERE id = $1
	`, staleFeedbackID).Scan(&exhaustedStatus, &exhaustedProcessedAt); err != nil {
		t.Fatalf("read exhausted text state: %v", err)
	}
	if exhaustedStatus != "failed" || exhaustedProcessedAt == nil {
		t.Errorf("exhausted text state = %s/%#v", exhaustedStatus, exhaustedProcessedAt)
	}

	trackedID, err := store.TrackGeminiFile(ctx, AttachmentWork{
		TenantID: testTenantID, AttachmentID: testAudioID,
	}, "files/integration-cleanup", daySeven)
	if err != nil {
		t.Fatalf("TrackGeminiFile() error = %v", err)
	}
	cleanupTasks, err := store.PrepareGeminiFileDeletions(ctx, 10, daySeven.Add(time.Minute))
	if err != nil || len(cleanupTasks) != 1 || cleanupTasks[0].ResourceID != trackedID {
		t.Fatalf("PrepareGeminiFileDeletions() = %#v, %v", cleanupTasks, err)
	}
	cleanupWork, err := store.ClaimGeminiFile(
		ctx, testTenantID, trackedID, cleanupTasks[0].Attempt,
	)
	if err != nil || cleanupWork.FileName != "files/integration-cleanup" {
		t.Fatalf("ClaimGeminiFile() = %#v, %v", cleanupWork, err)
	}
	inFlight, err = store.InFlightTasks(ctx, 100)
	if err != nil || !containsPreparedTask(inFlight, cleanupTasks[0]) {
		t.Errorf("InFlightTasks(cleanup) = %#v, %v", inFlight, err)
	}
	if err := store.FailGeminiFileDeletion(
		ctx, cleanupWork, errors.New("provider unavailable"), true, false,
		daySeven.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("FailGeminiFileDeletion(requeue) error = %v", err)
	}
	cleanupTasks, err = store.PrepareGeminiFileDeletions(ctx, 10, daySeven.Add(3*time.Minute))
	if err != nil || len(cleanupTasks) != 1 || cleanupTasks[0].Attempt != 2 {
		t.Fatalf("PrepareGeminiFileDeletions(retry) = %#v, %v", cleanupTasks, err)
	}
	cleanupWork, err = store.ClaimGeminiFile(ctx, testTenantID, trackedID, 2)
	if err != nil {
		t.Fatalf("ClaimGeminiFile(retry) error = %v", err)
	}
	if err := store.CompleteGeminiFileDeletion(ctx, cleanupWork, daySeven.Add(4*time.Minute)); err != nil {
		t.Fatalf("CompleteGeminiFileDeletion() error = %v", err)
	}
	inlineTrackedID, err := store.TrackGeminiFile(ctx, AttachmentWork{
		TenantID: testTenantID, AttachmentID: testAudioID,
	}, "files/inline-cleanup", daySeven.Add(15*time.Minute))
	if err != nil {
		t.Fatalf("TrackGeminiFile(inline) error = %v", err)
	}
	if err := store.MarkGeminiFileReady(ctx, inlineTrackedID, daySeven); err != nil {
		t.Fatalf("MarkGeminiFileReady() error = %v", err)
	}
	if err := store.RecordGeminiFileDeleteFailure(
		ctx, inlineTrackedID, errors.New("inline delete failed"), false, daySeven,
	); err != nil {
		t.Fatalf("RecordGeminiFileDeleteFailure() error = %v", err)
	}
	if err := store.CompleteTrackedGeminiFileDeletion(
		ctx, inlineTrackedID, daySeven.Add(time.Minute),
	); err != nil {
		t.Fatalf("CompleteTrackedGeminiFileDeletion() error = %v", err)
	}
	permanentTrackedID, err := store.TrackGeminiFile(ctx, AttachmentWork{
		TenantID: testTenantID, AttachmentID: testAudioID,
	}, "files/permanent-cleanup", daySeven)
	if err != nil {
		t.Fatalf("TrackGeminiFile(permanent) error = %v", err)
	}
	if err := store.RecordGeminiFileDeleteFailure(
		ctx, permanentTrackedID, errors.New("delete forbidden"), true, daySeven,
	); err != nil {
		t.Fatalf("RecordGeminiFileDeleteFailure(permanent) error = %v", err)
	}

	expiredFeedbackID := "60000000-0000-4000-8000-000000000001"
	recentFeedbackID := "60000000-0000-4000-8000-000000000002"
	expiredAttachmentID := "60000000-0000-4000-8000-000000000003"
	recentAttachmentID := "60000000-0000-4000-8000-000000000004"
	if _, err := pool.Exec(ctx, `
		INSERT INTO feedbacks (
			id, tenant_id, project_id, public_id, client_feedback_id,
			text, page_url, text_process_status
		) VALUES
			($1, $3, $4, 'fb_Expire01', '60000000-0000-4000-8000-000000000005',
			 'Expired upload', 'https://example.com/expired', 'completed'),
			($2, $3, $4, 'fb_Recent01', '60000000-0000-4000-8000-000000000006',
			 'Recent upload', 'https://example.com/recent', 'completed')
	`, expiredFeedbackID, recentFeedbackID, testTenantID, testProjectID); err != nil {
		t.Fatalf("seed incomplete-upload feedback: %v", err)
	}
	if _, err := pool.Exec(ctx, `
			INSERT INTO attachments (
				id, tenant_id, feedback_id, kind, client_attachment_id, storage_key,
				content_type, size_bytes, processing_status, complete_token_hash,
				upload_refreshed_at, upload_expires_at, created_at
			) VALUES
				($1, $3, $4, 'screenshot', '60000000-0000-4000-8000-000000000007',
				 'expired.png', 'image/png', 10, 'pending', 'hash', $6, $6::timestamptz + interval '10 minutes', $6),
				($2, $3, $5, 'screenshot', '60000000-0000-4000-8000-000000000008',
				 'recent.png', 'image/png', 10, 'pending', 'hash', $7, $7::timestamptz + interval '10 minutes', $7)
	`, expiredAttachmentID, recentAttachmentID, testTenantID, expiredFeedbackID,
		recentFeedbackID, daySeven.Add(-20*time.Minute), daySeven.Add(-time.Minute)); err != nil {
		t.Fatalf("seed incomplete uploads: %v", err)
	}
	expired, err := store.ExpireIncompleteAttachments(
		ctx, daySeven.Add(-15*time.Minute), daySeven, 100,
	)
	if err != nil || expired != 1 {
		t.Fatalf("ExpireIncompleteAttachments() = %d, %v", expired, err)
	}
	var expiredStatus, expiredError string
	var expiredProcessedAt, expiredFeedbackProcessedAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT attachment.processing_status, attachment.error_message, attachment.processed_at,
		       feedback.ai_processed_at
		FROM attachments AS attachment
		JOIN feedbacks AS feedback ON feedback.id = attachment.feedback_id
		WHERE attachment.id = $1
	`, expiredAttachmentID).Scan(
		&expiredStatus, &expiredError, &expiredProcessedAt, &expiredFeedbackProcessedAt,
	); err != nil {
		t.Fatalf("read expired upload state: %v", err)
	}
	if expiredStatus != "failed" || expiredError != "attachment upload expired before completion" ||
		expiredProcessedAt == nil || expiredFeedbackProcessedAt == nil {
		t.Errorf("expired upload state = %s/%q/%v/%v", expiredStatus, expiredError,
			expiredProcessedAt, expiredFeedbackProcessedAt)
	}
	var recentStatus string
	var recentProcessedAt, recentFeedbackProcessedAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT attachment.processing_status, attachment.processed_at, feedback.ai_processed_at
		FROM attachments AS attachment
		JOIN feedbacks AS feedback ON feedback.id = attachment.feedback_id
		WHERE attachment.id = $1
	`, recentAttachmentID).Scan(&recentStatus, &recentProcessedAt, &recentFeedbackProcessedAt); err != nil {
		t.Fatalf("read recent upload state: %v", err)
	}
	if recentStatus != "pending" || recentProcessedAt != nil || recentFeedbackProcessedAt != nil {
		t.Errorf("recent upload state = %s/%v/%v", recentStatus, recentProcessedAt, recentFeedbackProcessedAt)
	}

	concurrentFeedbackID := "60000000-0000-4000-8000-000000000009"
	if _, err := pool.Exec(ctx, `
		INSERT INTO feedbacks (
			id, tenant_id, project_id, public_id, client_feedback_id,
			text, page_url, text_process_status
		) VALUES (
			$1, $2, $3, 'fb_Concur01', '60000000-0000-4000-8000-000000000010',
			'Concurrent uploads', 'https://example.com/concurrent', 'completed'
		)
	`, concurrentFeedbackID, testTenantID, testProjectID); err != nil {
		t.Fatalf("seed concurrent incomplete-upload feedback: %v", err)
	}
	if _, err := pool.Exec(ctx, `
			INSERT INTO attachments (
				id, tenant_id, feedback_id, kind, client_attachment_id, storage_key,
				content_type, size_bytes, processing_status, complete_token_hash,
				upload_refreshed_at, upload_expires_at, created_at
			) VALUES
				('60000000-0000-4000-8000-000000000011', $1, $2, 'screenshot',
				 '60000000-0000-4000-8000-000000000012', 'concurrent-a.png', 'image/png',
				 10, 'pending', 'hash-a', $3, $3::timestamptz + interval '10 minutes', $3),
				('60000000-0000-4000-8000-000000000013', $1, $2, 'screenshot',
				 '60000000-0000-4000-8000-000000000014', 'concurrent-b.png', 'image/png',
				 10, 'pending', 'hash-b', $3, $3::timestamptz + interval '10 minutes', $3)
	`, testTenantID, concurrentFeedbackID, daySeven.Add(-20*time.Minute)); err != nil {
		t.Fatalf("seed concurrent incomplete uploads: %v", err)
	}
	feedbackBlocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin feedback blocker: %v", err)
	}
	if _, err := feedbackBlocker.Exec(ctx,
		`SELECT id FROM feedbacks WHERE id = $1 FOR UPDATE`, concurrentFeedbackID,
	); err != nil {
		_ = feedbackBlocker.Rollback(ctx)
		t.Fatalf("lock feedback before concurrent expiration: %v", err)
	}
	type expirationResult struct {
		count int
		err   error
	}
	startExpirations := make(chan struct{})
	expirationResults := make(chan expirationResult, 2)
	for range 2 {
		go func() {
			<-startExpirations
			count, err := store.ExpireIncompleteAttachments(
				ctx, daySeven.Add(-15*time.Minute), daySeven, 1,
			)
			expirationResults <- expirationResult{count: count, err: err}
		}()
	}
	close(startExpirations)
	lockDeadline := time.Now().Add(5 * time.Second)
	blockedExpirations := 0
	for time.Now().Before(lockDeadline) {
		if err := pool.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%lock expired attachment feedbacks%'
		`).Scan(&blockedExpirations); err != nil {
			_ = feedbackBlocker.Rollback(ctx)
			t.Fatalf("inspect concurrent expiration locks: %v", err)
		}
		if blockedExpirations == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := feedbackBlocker.Commit(ctx); err != nil {
		t.Fatalf("release feedback blocker: %v", err)
	}
	for range 2 {
		result := <-expirationResults
		if result.err != nil || result.count != 1 {
			t.Errorf("concurrent ExpireIncompleteAttachments() = %d, %v", result.count, result.err)
		}
	}
	if blockedExpirations != 2 {
		t.Fatalf("blocked concurrent expirations = %d, want 2", blockedExpirations)
	}
	var concurrentProcessedAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT ai_processed_at FROM feedbacks WHERE id = $1`, concurrentFeedbackID,
	).Scan(&concurrentProcessedAt); err != nil {
		t.Fatalf("read concurrent feedback completion: %v", err)
	}
	if concurrentProcessedAt == nil {
		t.Error("concurrent feedback ai_processed_at = nil")
	}

	eligibleTenantID := "50000000-0000-4000-8000-000000000001"
	eligibleProjectID := "50000000-0000-4000-8000-000000000002"
	eligibleFeedbackID := "50000000-0000-4000-8000-000000000003"
	eligibleRuleID := "50000000-0000-4000-8000-000000000005"
	if _, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, name, slug, plan, status)
		VALUES ($1, 'Eligible Tenant', 'eligible-tenant', 'growth', 'active')
	`, eligibleTenantID); err != nil {
		t.Fatalf("seed eligible tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, tenant_id, name, public_key, allowed_domains)
		VALUES ($1, $2, 'Eligible Web', 'pk_live_1234567890ABCDEFGHIJKL', '{eligible.example.com}')
	`, eligibleProjectID, eligibleTenantID); err != nil {
		t.Fatalf("seed eligible project: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO widget_rules (
			id, project_id, name, matcher_type, trigger_type, trigger_delay_seconds, ai_processing
		) VALUES (
			$1, $2, 'Eligible AI feedback', 'all_pages', 'delay', 0,
			'{"enabled":true,"policy_version":"2026-07-24","provider":"google_gemini",
			  "purposes":["transcription","cleanup","moderation","sentiment","triage_recommendations"],
			  "input_categories":["feedback_text"],"privacy_policy_url":"https://eligible.example.com/privacy",
			  "sensitive_data_mode":"prohibit_and_quarantine","acknowledged":true,
			  "acknowledged_at":"2026-07-21T00:00:00Z",
			  "acknowledged_by_member_id":"50000000-0000-4000-8000-000000000006"}'
		)
	`, eligibleRuleID, eligibleProjectID); err != nil {
		t.Fatalf("seed eligible AI rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO feedbacks (
			id, tenant_id, project_id, widget_rule_id, public_id, client_feedback_id,
			text, page_url, text_process_status
		) VALUES (
			$1, $2, $3, $4, 'fb_Elig0001', '50000000-0000-4000-8000-000000000004',
			'Eligible work', 'https://eligible.example.com', 'pending'
		)
	`, eligibleFeedbackID, eligibleTenantID, eligibleProjectID, eligibleRuleID); err != nil {
		t.Fatalf("seed eligible tenant AI work: %v", err)
	}
	dayEight := daySeven.Add(24 * time.Hour)
	if _, err := pool.Exec(ctx, `
		INSERT INTO ai_daily_usage (tenant_id, usage_date, requests) VALUES ($1, $2, 1)
	`, testTenantID, midnightUTC(dayEight)); err != nil {
		t.Fatalf("exhaust tenant AI budget: %v", err)
	}
	eligibleIDs, err := store.PendingFeedbackIDs(ctx, 100, 1, dayEight)
	if err != nil || len(eligibleIDs) != 1 || eligibleIDs[0] != eligibleFeedbackID {
		t.Errorf("PendingFeedbackIDs(exhausted tenant) = %#v, %v", eligibleIDs, err)
	}
	exerciseAIHTTP(t, ctx, pool)
	exerciseIdempotentAIBudgets(t, ctx, pool, store, dayEight.Add(24*time.Hour))
}

func TestAIProcessingPolicyDisableDiscardsInFlightResult(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := Store{Database: pool, Model: "gemini-test"}

	prepared, _, err := store.PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil {
		t.Fatalf("PrepareFeedback() error = %v", err)
	}
	var textTask PreparedTask
	for _, task := range prepared {
		if task.Kind == TaskKindText {
			textTask = task
			break
		}
	}
	if textTask.ResourceID == "" {
		t.Fatalf("PrepareFeedback() did not return a text task: %#v", prepared)
	}
	work, err := store.ClaimText(
		ctx, textTask.TenantID, textTask.ResourceID, textTask.Generation,
	)
	if err != nil {
		t.Fatalf("ClaimText() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE widget_rules SET ai_processing = '{"enabled":false}' WHERE id = $1
	`, testWidgetRuleID); err != nil {
		t.Fatalf("disable AI policy during processing: %v", err)
	}
	result := Result{
		CleanedText:    "This result must not be saved",
		SuggestedTitle: "Discard this",
		Actionability:  "actionable",
		Category:       "checkout",
		Priority:       "high",
	}
	if err := store.CompleteText(ctx, work, result, now.Add(time.Minute)); !errors.Is(err, ErrNoWork) {
		t.Fatalf("CompleteText() error = %v, want ErrNoWork", err)
	}

	var status, reason string
	var completedAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT status, failure_reason, completed_at
		FROM ai_processing_runs WHERE id = $1
	`, work.RunID).Scan(&status, &reason, &completedAt); err != nil {
		t.Fatalf("read discarded AI evidence: %v", err)
	}
	if status != "discarded" || reason != "policy_disabled" || completedAt == nil {
		t.Fatalf("discarded AI evidence = %s/%s/%v", status, reason, completedAt)
	}
	var cleaned, title *string
	if err := pool.QueryRow(ctx, `
		SELECT text_cleaned, ai_suggested_title FROM feedbacks WHERE id = $1
	`, testFeedbackID).Scan(&cleaned, &title); err != nil {
		t.Fatalf("read feedback after policy disable: %v", err)
	}
	if cleaned != nil || title != nil {
		t.Fatalf("disabled policy persisted derived values: cleaned=%v title=%v", cleaned, title)
	}
}

func exerciseIdempotentAIBudgets(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store Store,
	now time.Time,
) {
	t.Helper()
	usageDate := midnightUTC(now)
	reservation := budgetReservation{
		TenantID: testTenantID, ProjectID: testProjectID, Kind: TaskKindText,
		ResourceID: "60000000-0000-4000-8000-000000000001",
		Generation: 1, UsageDate: usageDate,
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	for range 8 {
		go func() {
			<-start
			tx, err := pool.Begin(ctx)
			if err == nil {
				var reserved bool
				reserved, err = reserveBudget(ctx, tx, reservation, 10, 10)
				if err == nil && !reserved {
					err = errors.New("idempotent reservation was denied")
				}
			}
			if err == nil {
				err = tx.Commit(ctx)
			} else if tx != nil {
				_ = tx.Rollback(ctx)
			}
			results <- err
		}()
	}
	close(start)
	for range 8 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent idempotent AI reservation: %v", err)
		}
	}
	var tenantRequests, projectRequests, reservations int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT requests FROM ai_daily_usage
			 WHERE tenant_id = $1 AND usage_date = $3),
			(SELECT requests FROM ai_project_daily_usage
			 WHERE project_id = $2 AND usage_date = $3),
			(SELECT count(*) FROM ai_work_reservations
			 WHERE tenant_id = $1 AND resource_id = $4 AND generation = 1)
	`, testTenantID, testProjectID, usageDate, reservation.ResourceID).Scan(
		&tenantRequests, &projectRequests, &reservations,
	); err != nil {
		t.Fatalf("read idempotent AI budget state: %v", err)
	}
	if tenantRequests != 1 || projectRequests != 1 || reservations != 1 {
		t.Fatalf("idempotent AI budget = tenant %d project %d reservations %d",
			tenantRequests, projectRequests, reservations)
	}

	deniedReservation := reservation
	deniedReservation.ResourceID = "60000000-0000-4000-8000-000000000002"
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin project-limit AI reservation: %v", err)
	}
	reserved, err := reserveBudget(ctx, tx, deniedReservation, 10, 1)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("reserve exhausted project AI budget: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit exhausted project AI budget: %v", err)
	}
	if reserved {
		t.Fatal("exhausted project AI budget was reserved")
	}

	secondProjectID := "60000000-0000-4000-8000-000000000003"
	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, tenant_id, name, public_key, allowed_domains)
		VALUES (
			$1, $2, 'Second budget project',
			'pk_live_SECONDPROJECTABCDEFGHI', '{second.example.com}'
		)
	`, secondProjectID, testTenantID); err != nil {
		t.Fatalf("create second AI budget project: %v", err)
	}
	secondProjectReservation := reservation
	secondProjectReservation.ProjectID = secondProjectID
	secondProjectReservation.ResourceID = "60000000-0000-4000-8000-000000000004"
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin second-project AI reservation: %v", err)
	}
	reserved, err = reserveBudget(ctx, tx, secondProjectReservation, 10, 1)
	if err != nil || !reserved {
		_ = tx.Rollback(ctx)
		t.Fatalf("reserve second-project AI budget = %t, %v", reserved, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit second-project AI budget: %v", err)
	}

	deferredFeedbackID := "60000000-0000-4000-8000-000000000005"
	if _, err := pool.Exec(ctx, `
		INSERT INTO feedbacks (
			id, tenant_id, project_id, widget_rule_id, public_id, client_feedback_id,
			text, title, page_url, text_process_status
		) VALUES (
			$1, $2, $3, $4, 'fb_Budget01', gen_random_uuid(),
			'Please analyze this later', 'Please analyze this later',
			'https://example.com/budget', 'pending'
		)
	`, deferredFeedbackID, testTenantID, testProjectID, testWidgetRuleID); err != nil {
		t.Fatalf("create deferred AI feedback: %v", err)
	}
	queue := &capturingTaskQueue{}
	limitedStore := store
	limitedStore.ProjectDailyLimit = 1
	scheduler := Scheduler{
		Store: limitedStore, Queue: queue, DailyLimit: 10, Now: func() time.Time { return now },
	}
	if err := scheduler.ScheduleFeedback(ctx, deferredFeedbackID); err != nil {
		t.Fatalf("schedule project-exhausted AI feedback: %v", err)
	}
	if len(queue.tasks) != 0 {
		t.Fatalf("project-exhausted AI tasks = %#v", queue.tasks)
	}
	var deferredStatus string
	var deferredReason *string
	var deferredUntil *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT text_process_status::text, ai_budget_deferred_reason,
		       ai_budget_deferred_until
		FROM feedbacks WHERE id = $1
	`, deferredFeedbackID).Scan(
		&deferredStatus, &deferredReason, &deferredUntil,
	); err != nil {
		t.Fatalf("read deferred AI feedback: %v", err)
	}
	if deferredStatus != "pending" || deferredReason == nil ||
		*deferredReason != budgetDeferredReason || deferredUntil == nil {
		t.Fatalf("deferred AI state = %s/%v/%v",
			deferredStatus, deferredReason, deferredUntil)
	}
	visible, err := (feedbackdomain.Store{}).GetTenantFeedback(
		ctx, pool, testTenantID, deferredFeedbackID,
	)
	if err != nil {
		t.Fatalf("read deferred feedback through tenant store: %v", err)
	}
	if visible.AIStatus != "deferred" {
		t.Fatalf("tenant-visible AI status = %q, want deferred", visible.AIStatus)
	}
	filtered, err := (feedbackdomain.Store{}).ListTenantFeedback(
		ctx, pool, testTenantID, feedbackdomain.ListOptions{
			Processing: "deferred", Limit: 10,
		},
	)
	if err != nil {
		t.Fatalf("filter deferred feedback through tenant store: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != deferredFeedbackID {
		t.Fatalf("deferred tenant filter = %#v", filtered)
	}

	if err := limitedStore.ReprocessFeedback(
		ctx, pool, testTenantID, deferredFeedbackID,
	); err != nil {
		t.Fatalf("retry deferred AI feedback: %v", err)
	}
	limitedStore.ProjectDailyLimit = 2
	scheduler.Store = limitedStore
	if err := scheduler.ScheduleFeedback(ctx, deferredFeedbackID); err != nil {
		t.Fatalf("schedule recovered AI feedback: %v", err)
	}
	if len(queue.tasks) != 1 || queue.tasks[0].ResourceID != deferredFeedbackID {
		t.Fatalf("recovered AI tasks = %#v", queue.tasks)
	}
	if err := limitedStore.UndoPrepared(ctx, queue.tasks[0]); err != nil {
		t.Fatalf("undo recovered AI task: %v", err)
	}
}

func aiTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(databaseURL, aiMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate AI database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open AI database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedAIFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	statements := []struct {
		query string
		args  []any
	}{
		{query: `
		INSERT INTO tenants (id, name, slug, plan, status)
		VALUES ($1, 'AI Tenant', 'ai-tenant', 'growth', 'active')`, args: []any{testTenantID}},
		{query: `
		INSERT INTO tenant_members (id, tenant_id, email, permissions)
		VALUES ($1, $2, 'analyst@ai-tenant.test', ARRAY['feedback:update'])`, args: []any{testMemberID, testTenantID}},
		{query: `
			INSERT INTO projects (id, tenant_id, name, public_key, allowed_domains)
			VALUES ($1, $2, 'Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.com}')`, args: []any{testProjectID, testTenantID}},
		{query: `
			INSERT INTO widget_rules (
				id, project_id, name, matcher_type, trigger_type, trigger_delay_seconds, ai_processing
			) VALUES (
				$1, $2, 'AI feedback', 'all_pages', 'delay', 0,
				'{"enabled":true,"policy_version":"2026-07-24","provider":"google_gemini",
				  "purposes":["transcription","cleanup","moderation","sentiment","triage_recommendations"],
				  "input_categories":["feedback_text","screenshot","audio","video"],
				  "privacy_policy_url":"https://example.com/privacy",
				  "sensitive_data_mode":"prohibit_and_quarantine","acknowledged":true,
				  "acknowledged_at":"2026-07-21T00:00:00Z",
				  "acknowledged_by_member_id":"10000000-0000-4000-8000-000000000010"}'
			)`, args: []any{testWidgetRuleID, testProjectID}},
		{query: `
		INSERT INTO issues (id, tenant_id, project_id, public_id, title)
		VALUES ($1, $2, $3, 'AI-TENANT-2026-0001', 'Checkout feedback')`, args: []any{testIssueID, testTenantID, testProjectID}},
		{query: `
			INSERT INTO feedbacks (
				id, tenant_id, project_id, widget_rule_id, public_id, issue_id, client_feedback_id,
				text, page_url, text_process_status
			) VALUES (
				$1, $2, $3, $4, 'fb_AbCd1234', $5, '10000000-0000-4000-8000-000000000009',
				'Um, the checkout is damn confusing', 'https://example.com/checkout', 'pending'
			)`, args: []any{testFeedbackID, testTenantID, testProjectID, testWidgetRuleID, testIssueID}},
		{query: `
			INSERT INTO attachments (
				id, tenant_id, feedback_id, kind, client_attachment_id, storage_key,
				content_type, size_bytes, processing_status, completed_at,
				safety_status, safety_checked_at, detected_content_type
			) VALUES
				($1, $2, $3, 'audio', '20000000-0000-4000-8000-000000000001', 'audio.webm',
				 'audio/webm', 10, 'pending', now(), 'safe', now(), 'audio/webm'),
				($4, $2, $3, 'screenshot', '20000000-0000-4000-8000-000000000002', 'screen.png',
				 'image/png', 10, 'pending', now(), 'safe', now(), 'image/png'),
				($5, $2, $3, 'replay', '20000000-0000-4000-8000-000000000003', 'replay.json',
				 'application/json', 10, 'pending', now(), 'safe', now(), 'application/json')`, args: []any{
			testAudioID, testTenantID, testFeedbackID, testScreenshotID, testReplayID,
		}},
		{query: `
		INSERT INTO tenants (id, name, slug, plan, status)
		VALUES ($1, 'Starter Tenant', 'starter-tenant', 'starter', 'active')`, args: []any{testStarterTenantID}},
		{query: `
			INSERT INTO projects (id, tenant_id, name, public_key, allowed_domains)
			VALUES ($1, $2, 'Starter Web', 'pk_live_ZYXWVUTSRQPONMLKJIHGFE', '{starter.example.com}')`,
			args: []any{testStarterProjectID, testStarterTenantID}},
		{query: `
			INSERT INTO widget_rules (
				id, project_id, name, matcher_type, trigger_type, trigger_delay_seconds, ai_processing
			) VALUES (
				$1, $2, 'Starter AI feedback', 'all_pages', 'delay', 0,
				'{"enabled":true,"policy_version":"2026-07-24","provider":"google_gemini",
				  "purposes":["transcription","cleanup","moderation","sentiment","triage_recommendations"],
				  "input_categories":["feedback_text","screenshot"],
				  "privacy_policy_url":"https://starter.example.com/privacy",
				  "sensitive_data_mode":"prohibit_and_quarantine","acknowledged":true,
				  "acknowledged_at":"2026-07-21T00:00:00Z",
				  "acknowledged_by_member_id":"30000000-0000-4000-8000-000000000010"}'
			)`, args: []any{testStarterWidgetRuleID, testStarterProjectID}},
		{query: `
		INSERT INTO issues (id, tenant_id, project_id, public_id, title)
		VALUES ($1, $2, $3, 'STARTER-TENANT-2026-0001', 'Starter feedback')`,
			args: []any{testStarterIssueID, testStarterTenantID, testStarterProjectID}},
		{query: `
			INSERT INTO feedbacks (
				id, tenant_id, project_id, widget_rule_id, public_id, issue_id, client_feedback_id,
				text, page_url, text_process_status
			) VALUES (
				$1, $2, $3, $4, 'fb_ZyXw9876', $5, '30000000-0000-4000-8000-000000000009',
				'Starter feedback', 'https://starter.example.com', 'pending'
			)`, args: []any{
			testStarterFeedbackID, testStarterTenantID, testStarterProjectID,
			testStarterWidgetRuleID, testStarterIssueID,
		}},
		{query: `
			INSERT INTO attachments (
				id, tenant_id, feedback_id, kind, client_attachment_id, storage_key,
				content_type, size_bytes, processing_status, completed_at,
				safety_status, safety_checked_at, detected_content_type
			) VALUES (
				$1, $2, $3, 'screenshot', '30000000-0000-4000-8000-000000000006',
				'starter.png', 'image/png', 10, 'pending', now(), 'safe', now(), 'image/png'
			)`, args: []any{testStarterAttachmentID, testStarterTenantID, testStarterFeedbackID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed AI feedback: %v", err)
		}
	}
}

func runPreparedTask(ctx context.Context, processor Processor, prepared PreparedTask) error {
	payload := []byte(`{"tenant_id":"` + prepared.TenantID + `","resource_id":"` +
		prepared.ResourceID + `","attempt":` + fmtInt(prepared.Attempt) +
		`,"generation":` + strconv.FormatInt(prepared.Generation, 10) + `}`)
	if prepared.Kind == TaskKindText {
		return processor.TextHandler()(ctx, asynq.NewTask(TaskTextProcess, payload))
	}
	return processor.AttachmentHandler()(ctx, asynq.NewTask(TaskAttachmentProcess, payload))
}

func fmtInt(value int) string {
	if value == 0 {
		return "0"
	}
	return strings.TrimSpace(strings.Repeat(" ", 0) + strconv.Itoa(value))
}

func containsPreparedTask(tasks []PreparedTask, expected PreparedTask) bool {
	for _, task := range tasks {
		if task.Kind == expected.Kind && task.TenantID == expected.TenantID &&
			task.ResourceID == expected.ResourceID && task.Attempt == expected.Attempt &&
			task.Generation == expected.Generation {
			return true
		}
	}
	return false
}

func assertAIState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	feedbackID, textStatus string,
	wantProcessedAt *time.Time,
) {
	t.Helper()
	var status, cleaned string
	var processedAt *time.Time
	var score float64
	var flags []string
	if err := pool.QueryRow(ctx, `
		SELECT text_process_status, COALESCE(text_cleaned, ''), COALESCE(text_sentiment_score, 0),
		       text_moderation_flags, ai_processed_at
		FROM feedbacks WHERE id = $1
	`, feedbackID).Scan(&status, &cleaned, &score, &flags, &processedAt); err != nil {
		t.Fatalf("read feedback AI state: %v", err)
	}
	if status != textStatus {
		t.Errorf("text status = %q, want %q", status, textStatus)
	}
	if textStatus == "completed" && (cleaned != "checkout is confusing" || score != -0.7 ||
		len(flags) != 1 || flags[0] != "profanity") {
		t.Errorf("text AI output = cleaned %q score %.2f flags %#v", cleaned, score, flags)
	}
	if (processedAt == nil) != (wantProcessedAt == nil) {
		t.Errorf("ai_processed_at = %#v, want %#v", processedAt, wantProcessedAt)
	}
}

func assertAttachmentState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	attachmentID, wantStatus string,
	wantAttempts int,
	wantError *string,
) {
	t.Helper()
	var status string
	var attempts int
	var message *string
	if err := pool.QueryRow(ctx, `
		SELECT processing_status, processing_attempts, error_message
		FROM attachments WHERE id = $1
	`, attachmentID).Scan(&status, &attempts, &message); err != nil {
		t.Fatalf("read attachment AI state: %v", err)
	}
	if status != wantStatus || attempts != wantAttempts {
		t.Errorf("attachment %s state = %s/%d, want %s/%d", attachmentID, status, attempts, wantStatus, wantAttempts)
	}
	if wantError == nil && message != nil || wantError != nil && (message == nil || !strings.Contains(*message, *wantError)) {
		t.Errorf("attachment %s error = %#v, want %#v", attachmentID, message, wantError)
	}
}

func assertRecommendations(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	feedbackID, wantTitle, wantActionability, wantCategory, wantPriority string,
) {
	t.Helper()
	var title, actionability, category, priority string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(ai_suggested_title, ''), COALESCE(ai_actionability, ''),
		       COALESCE(ai_category, ''), COALESCE(ai_priority, '')
		FROM feedbacks WHERE id = $1
	`, feedbackID).Scan(&title, &actionability, &category, &priority); err != nil {
		t.Fatalf("read feedback AI recommendations: %v", err)
	}
	if title != wantTitle || actionability != wantActionability ||
		category != wantCategory || priority != wantPriority {
		t.Errorf("AI recommendations = %q/%q/%q/%q, want %q/%q/%q/%q",
			title, actionability, category, priority,
			wantTitle, wantActionability, wantCategory, wantPriority)
	}
}

func assertUsage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, day time.Time, want int) {
	t.Helper()
	var requests int
	if err := pool.QueryRow(ctx, `
		SELECT requests FROM ai_daily_usage WHERE tenant_id = $1 AND usage_date = $2
	`, testTenantID, midnightUTC(day)).Scan(&requests); err != nil {
		t.Fatalf("read AI daily usage: %v", err)
	}
	if requests != want {
		t.Errorf("AI daily requests = %d, want %d", requests, want)
	}
}

func aiMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve AI test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	return path
}

type fakeAnalyzer struct {
	attachmentErr error
	textErr       error
}

func (a *fakeAnalyzer) AnalyzeText(context.Context, string) (Result, error) {
	if a.textErr != nil {
		return Result{}, a.textErr
	}
	return Result{CleanedText: "checkout is confusing", SentimentScore: -0.7,
		ModerationFlags: []string{"profanity"}, SuggestedTitle: "Checkout is confusing",
		Actionability: "actionable", Category: "checkout", Priority: "high"}, nil
}

func (a *fakeAnalyzer) AnalyzeAttachment(_ context.Context, work AttachmentWork, _ io.Reader) (Result, error) {
	if a.attachmentErr != nil {
		return Result{}, a.attachmentErr
	}
	if work.Kind == "screenshot" {
		return Result{Transcription: "Payment failed", CleanedText: "Payment failed", SentimentScore: -0.4}, nil
	}
	return Result{Transcription: "The checkout is confusing", CleanedText: "checkout is confusing",
		SentimentScore: -0.7, ModerationFlags: []string{"profanity"},
		SuggestedTitle: "Checkout audio is confusing", Actionability: "actionable",
		Category: "checkout", Priority: "high"}, nil
}

type stringObjectSource struct{}

func (stringObjectSource) OpenObject(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("media-data")), nil
}

type allowLimiter struct{}

func (allowLimiter) Allow(context.Context, string, ratelimit.Window) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: true}, nil
}

func timePointer(value time.Time) *time.Time { return &value }
func stringPointer(value string) *string     { return &value }

type capturingTaskQueue struct {
	tasks []PreparedTask
	err   error
}

type capturingDiagnosticRecorder struct {
	events []abuse.DiagnosticEvent
}

func (recorder *capturingDiagnosticRecorder) Record(
	_ context.Context,
	event abuse.DiagnosticEvent,
) error {
	recorder.events = append(recorder.events, event)
	return nil
}

func (q *capturingTaskQueue) Enqueue(_ context.Context, task PreparedTask) error {
	if q.err != nil {
		return q.err
	}
	q.tasks = append(q.tasks, task)
	return nil
}

type capturingScheduler struct {
	feedbackIDs []string
}

func (s *capturingScheduler) ScheduleFeedback(_ context.Context, feedbackID string) error {
	s.feedbackIDs = append(s.feedbackIDs, feedbackID)
	return nil
}

func exerciseAIHTTP(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	jwtManager, err := auth.NewJWTManager(
		"0123456789abcdef0123456789abcdef", "fidibeki", time.Hour,
	)
	if err != nil {
		t.Fatalf("create tenant JWT manager: %v", err)
	}
	token, _, err := jwtManager.IssueTenant(testMemberID, testTenantID, []string{"feedback:update"})
	if err != nil {
		t.Fatalf("issue tenant token: %v", err)
	}
	scheduled := &capturingScheduler{}
	router := chi.NewRouter()
	RegisterRoutes(router, HTTPOptions{Store: Store{}, JWT: jwtManager, Scheduler: scheduled})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost",
		Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
			return tenant.Tenant{
				ID: testTenantID, Name: "AI Tenant", Slug: "ai-tenant",
				Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
			}, nil
		}),
		Begin: db.BeginFromPool(pool),
	})(router)

	request := func(path string, authorization bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Host = "ai-tenant.localhost"
		if authorization {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	unauthorized := request("/v1/attachments/"+testAudioID+"/retry", false)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized retry status = %d", unauthorized.Code)
	}
	retry := request("/v1/attachments/"+testAudioID+"/retry", true)
	if retry.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d, body = %s", retry.Code, retry.Body.String())
	}
	reprocess := request("/v1/feedbacks/"+testFeedbackID+"/reprocess", true)
	if reprocess.Code != http.StatusAccepted {
		t.Fatalf("reprocess status = %d, body = %s", reprocess.Code, reprocess.Body.String())
	}
	if len(scheduled.feedbackIDs) != 2 || scheduled.feedbackIDs[0] != testFeedbackID {
		t.Errorf("scheduled feedback IDs = %#v", scheduled.feedbackIDs)
	}
	invalid := request("/v1/attachments/not-a-uuid/retry", true)
	if invalid.Code != http.StatusNotFound {
		t.Errorf("invalid retry status = %d", invalid.Code)
	}

	starterResponse := httptest.NewRecorder()
	if requireAI(starterResponse, tenant.Tenant{Plan: tenant.PlanStarter}) ||
		starterResponse.Code != http.StatusPaymentRequired {
		t.Errorf("starter requireAI status = %d", starterResponse.Code)
	}
	for _, value := range []error{ErrNotFound, ErrAlreadyRunning, errors.New("database")} {
		response := httptest.NewRecorder()
		writeError(response, value)
		if response.Code < 400 {
			t.Errorf("writeError(%v) status = %d", value, response.Code)
		}
	}
}
