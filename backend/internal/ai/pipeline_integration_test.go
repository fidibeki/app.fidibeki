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

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
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
	testProjectID           = "10000000-0000-4000-8000-000000000002"
	testIssueID             = "10000000-0000-4000-8000-000000000003"
	testFeedbackID          = "10000000-0000-4000-8000-000000000004"
	testAudioID             = "10000000-0000-4000-8000-000000000005"
	testScreenshotID        = "10000000-0000-4000-8000-000000000006"
	testReplayID            = "10000000-0000-4000-8000-000000000007"
	testStarterTenantID     = "30000000-0000-4000-8000-000000000001"
	testStarterProjectID    = "30000000-0000-4000-8000-000000000002"
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
	store := Store{Database: pool}
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

	analyzer := &fakeAnalyzer{}
	processor := Processor{
		Store: store, Analyzer: analyzer, Storage: stringObjectSource{}, Limiter: allowLimiter{}, RPM: 15,
		Now: func() time.Time { return dayOne.Add(time.Minute) },
	}
	for _, task := range prepared {
		if err := runPreparedTask(ctx, processor, task); err != nil {
			t.Fatalf("process day-one task %#v: %v", task, err)
		}
	}
	assertAIState(t, ctx, pool, testFeedbackID, "completed", nil)
	assertAttachmentState(t, ctx, pool, testAudioID, "completed", 1, nil)
	assertAttachmentState(t, ctx, pool, testScreenshotID, "pending", 0, nil)

	dayTwo := dayOne.Add(24 * time.Hour)
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
	analyzer.textErr = permanent(errors.New("text policy blocked"))
	if err := runPreparedTask(ctx, processor, textTask); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("permanent text error = %v, want SkipRetry", err)
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
			id, tenant_id, project_id, public_id, issue_id, client_feedback_id,
			text, page_url, text_process_status
		) VALUES (
			$1, $2, $3, 'fb_Stale001', $4, '40000000-0000-4000-8000-000000000003',
			'Feedback generation one', 'https://example.com/stale', 'pending'
		)
	`, staleFeedbackID, testTenantID, testProjectID, staleIssueID); err != nil {
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

	eligibleTenantID := "50000000-0000-4000-8000-000000000001"
	eligibleProjectID := "50000000-0000-4000-8000-000000000002"
	eligibleFeedbackID := "50000000-0000-4000-8000-000000000003"
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
		INSERT INTO feedbacks (
			id, tenant_id, project_id, public_id, client_feedback_id, text, page_url, text_process_status
		) VALUES (
			$1, $2, $3, 'fb_Elig0001', '50000000-0000-4000-8000-000000000004',
			'Eligible work', 'https://eligible.example.com', 'pending'
		)
	`, eligibleFeedbackID, eligibleTenantID, eligibleProjectID); err != nil {
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
		INSERT INTO projects (id, tenant_id, name, public_key, allowed_domains)
		VALUES ($1, $2, 'Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.com}')`, args: []any{testProjectID, testTenantID}},
		{query: `
		INSERT INTO issues (id, tenant_id, project_id, public_id, title)
		VALUES ($1, $2, $3, 'AI-TENANT-2026-0001', 'Checkout feedback')`, args: []any{testIssueID, testTenantID, testProjectID}},
		{query: `
		INSERT INTO feedbacks (
			id, tenant_id, project_id, public_id, issue_id, client_feedback_id,
			text, page_url, text_process_status
		) VALUES (
			$1, $2, $3, 'fb_AbCd1234', $4, '10000000-0000-4000-8000-000000000009',
			'Um, the checkout is damn confusing', 'https://example.com/checkout', 'pending'
		)`, args: []any{testFeedbackID, testTenantID, testProjectID, testIssueID}},
		{query: `
		INSERT INTO attachments (
			id, tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at
		) VALUES
			($1, $2, $3, 'audio', '20000000-0000-4000-8000-000000000001', 'audio.webm',
			 'audio/webm', 10, 'pending', now()),
			($4, $2, $3, 'screenshot', '20000000-0000-4000-8000-000000000002', 'screen.png',
			 'image/png', 10, 'pending', now()),
			($5, $2, $3, 'replay', '20000000-0000-4000-8000-000000000003', 'replay.json',
			 'application/json', 10, 'pending', now())`, args: []any{
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
		INSERT INTO issues (id, tenant_id, project_id, public_id, title)
		VALUES ($1, $2, $3, 'STARTER-TENANT-2026-0001', 'Starter feedback')`,
			args: []any{testStarterIssueID, testStarterTenantID, testStarterProjectID}},
		{query: `
		INSERT INTO feedbacks (
			id, tenant_id, project_id, public_id, issue_id, client_feedback_id,
			text, page_url, text_process_status
		) VALUES (
			$1, $2, $3, 'fb_ZyXw9876', $4, '30000000-0000-4000-8000-000000000009',
			'Starter feedback', 'https://starter.example.com', 'pending'
		)`, args: []any{testStarterFeedbackID, testStarterTenantID, testStarterProjectID, testStarterIssueID}},
		{query: `
		INSERT INTO attachments (
			id, tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at
		) VALUES (
			$1, $2, $3, 'screenshot', '30000000-0000-4000-8000-000000000006',
			'starter.png', 'image/png', 10, 'pending', now()
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
		ModerationFlags: []string{"profanity"}}, nil
}

func (a *fakeAnalyzer) AnalyzeAttachment(_ context.Context, work AttachmentWork, _ io.Reader) (Result, error) {
	if a.attachmentErr != nil {
		return Result{}, a.attachmentErr
	}
	if work.Kind == "screenshot" {
		return Result{Transcription: "Payment failed", CleanedText: "Payment failed", SentimentScore: -0.4}, nil
	}
	return Result{Transcription: "The checkout is confusing", CleanedText: "checkout is confusing",
		SentimentScore: -0.7, ModerationFlags: []string{"profanity"}}, nil
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
	token, _, err := jwtManager.IssueTenant("member-id", testTenantID, []string{"feedback:update"})
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
