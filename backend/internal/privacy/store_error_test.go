package privacy

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPrivacyStoresPropagateInfrastructureFailures(t *testing.T) {
	t.Parallel()
	want := errors.New("database unavailable")
	query := privacyErrorQuerier{err: want}
	store := Store{Database: query}
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	candidate := RetentionObjectCandidate{TenantID: "tenant", ID: "resource", ObjectKey: "object"}

	checks := []struct {
		name string
		call func() error
	}{
		{name: "create export", call: func() error {
			_, _, err := store.CreateWorkspaceExport(
				context.Background(), query, "tenant", "member", now,
			)
			return err
		}},
		{name: "list exports", call: func() error {
			_, err := store.ListJobs(context.Background(), query, "tenant", 50)
			return err
		}},
		{name: "get export", call: func() error {
			_, err := store.GetJob(context.Background(), query, "tenant", "job")
			return err
		}},
		{name: "retry export", call: func() error {
			_, err := store.RetryJob(
				context.Background(), query, "tenant", "member", "job", now,
			)
			return err
		}},
		{name: "prepare exports", call: func() error {
			_, err := store.PrepareJobs(context.Background(), now, 25)
			return err
		}},
		{name: "undo prepared export", call: func() error {
			return store.UndoPrepared(context.Background(), PreparedJob{
				ID: "job", TenantID: "tenant", Generation: 1,
			})
		}},
		{name: "claim export", call: func() error {
			_, _, err := store.ClaimJob(
				context.Background(), "job", "tenant", 1, now,
			)
			return err
		}},
		{name: "mark export failed", call: func() error {
			return store.MarkJobFailed(context.Background(), Job{
				ID: "job", TenantID: "tenant", Generation: 1,
			}, "failure", now)
		}},
		{name: "complete export", call: func() error {
			return store.CompleteJob(
				context.Background(), Job{ID: "job", TenantID: "tenant"},
				"object", 1, now,
			)
		}},
		{name: "record download", call: func() error {
			return store.RecordDownload(
				context.Background(), query, "tenant", "member", "job", now,
			)
		}},
		{name: "list reporter requests", call: func() error {
			_, err := store.ListReporterRequests(context.Background(), query, "tenant", 50)
			return err
		}},
		{name: "create rights case", call: func() error {
			_, _, _, err := store.CreateCase(
				context.Background(), query, "tenant", "member", "case-create",
				CreateCaseInput{}, now,
			)
			return err
		}},
		{name: "list rights cases", call: func() error {
			_, err := store.ListCases(context.Background(), query, "tenant", 50)
			return err
		}},
		{name: "get rights case", call: func() error {
			_, err := store.GetCase(context.Background(), query, "tenant", "case")
			return err
		}},
		{name: "list rights case events", call: func() error {
			_, err := store.ListCaseEvents(context.Background(), query, "tenant", "case")
			return err
		}},
		{name: "transition rights case", call: func() error {
			_, err := store.TransitionCase(
				context.Background(), query, "tenant", "case", "member",
				"case-transition", TransitionCaseInput{}, now,
			)
			return err
		}},
		{name: "rights case dashboard", call: func() error {
			_, err := store.CaseDashboard(context.Background(), query, "tenant", now)
			return err
		}},
		{name: "set rights case restriction", call: func() error {
			_, err := store.SetCaseRestriction(
				context.Background(), query, "tenant", "case", "member",
				"case-restriction", true, "reason", now,
			)
			return err
		}},
		{name: "place rights case legal hold", call: func() error {
			_, err := store.PlaceLegalHold(
				context.Background(), query, "tenant", "case", "member",
				"hold-place", "reason", "reference", now,
			)
			return err
		}},
		{name: "release rights case legal hold", call: func() error {
			_, err := store.ReleaseLegalHold(
				context.Background(), query, "tenant", "case", "hold", "member",
				"hold-release", "reason", now,
			)
			return err
		}},
		{name: "record portability assessment", call: func() error {
			_, _, err := store.RecordPortabilityAssessment(
				context.Background(), query, "tenant", "case", "member",
				"portability", RecordPortabilityAssessmentInput{}, now,
			)
			return err
		}},
		{name: "apply case rectification", call: func() error {
			_, _, err := store.ApplyCaseRectification(
				context.Background(), query, "tenant", "case", "member",
				"rectification", RecordCaseRectificationInput{}, now,
			)
			return err
		}},
		{name: "list rectifiable feedbacks", call: func() error {
			_, err := store.ListRectifiableFeedbacks(
				context.Background(), query, "tenant", "case",
			)
			return err
		}},
		{name: "apply feedback rectification", call: func() error {
			_, _, err := store.ApplyFeedbackRectification(
				context.Background(), query, "tenant", "case", "member",
				"feedback-rectification", RecordFeedbackRectificationInput{}, now,
			)
			return err
		}},
		{name: "refresh integration actions", call: func() error {
			_, _, _, err := store.RefreshIntegrationActions(
				context.Background(), query, "tenant", "case", "member", now,
			)
			return err
		}},
		{name: "complete integration action", call: func() error {
			_, _, err := store.CompleteIntegrationAction(
				context.Background(), query, "tenant", "case", "action", "member",
				"integration-action", CompleteIntegrationActionInput{}, now,
			)
			return err
		}},
		{name: "create recipient action", call: func() error {
			_, _, err := store.CreateRecipientAction(
				context.Background(), query, "tenant", "case", "member",
				"recipient-action", CreateRecipientActionInput{}, now,
			)
			return err
		}},
		{name: "retry recipient action", call: func() error {
			_, _, err := store.RetryRecipientAction(
				context.Background(), query, "tenant", "case", "action", "member",
				"recipient-retry", now,
			)
			return err
		}},
		{name: "create case communication", call: func() error {
			_, _, err := store.CreateCaseCommunication(
				context.Background(), query, "tenant", "case", "member",
				"communication", CreateCaseCommunicationInput{}, now,
			)
			return err
		}},
		{name: "retry case communication", call: func() error {
			_, _, err := store.RetryCaseCommunication(
				context.Background(), query, "tenant", "case", "communication",
				"member", "communication-retry", now,
			)
			return err
		}},
		{name: "start case execution", call: func() error {
			_, _, err := store.StartCaseExecution(
				context.Background(), query, "tenant", "case", "member",
				"case-execution", now,
			)
			return err
		}},
		{name: "expired attachments", call: func() error {
			_, err := store.ExpiredAttachments(context.Background(), now, 10)
			return err
		}},
		{name: "mark attachment", call: func() error {
			return store.MarkAttachmentRetentionDeleted(context.Background(), candidate, now)
		}},
		{name: "expired archives", call: func() error {
			_, err := store.ExpiredExportArchives(context.Background(), now, 10)
			return err
		}},
		{name: "mark archive", call: func() error {
			return store.MarkExportArchiveExpired(context.Background(), candidate, now)
		}},
		{name: "expire reporter requests", call: func() error {
			_, err := store.ExpireReporterRequests(context.Background(), now, 10)
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, want) {
				t.Fatalf("error = %v, want wrapping %v", err, want)
			}
		})
	}
}

func TestPrivacySchedulerLogsFailureAndStops(t *testing.T) {
	t.Parallel()
	called := make(chan struct{}, 1)
	var logs lockedPrivacyBuffer
	scheduler := Scheduler{
		Store: Store{Database: privacyErrorQuerier{
			err: errors.New("database unavailable"), called: called,
		}},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx, time.Hour) }()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not attempt reconciliation")
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(logs.String(), "privacy reconciliation failed") &&
		time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(logs.String(), "privacy reconciliation failed") {
		t.Fatalf("scheduler logs = %s", logs.String())
	}
}

type privacyErrorQuerier struct {
	err    error
	called chan struct{}
}

func (query privacyErrorQuerier) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	if query.called != nil {
		select {
		case query.called <- struct{}{}:
		default:
		}
	}
	return pgconn.CommandTag{}, query.err
}

func (query privacyErrorQuerier) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	return nil, query.err
}

func (query privacyErrorQuerier) QueryRow(
	context.Context,
	string,
	...any,
) pgx.Row {
	return privacyErrorRow{err: query.err}
}

type privacyErrorRow struct {
	err error
}

func (row privacyErrorRow) Scan(...any) error {
	return row.err
}

type lockedPrivacyBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (buffer *lockedPrivacyBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	buffer.data = append(buffer.data, value...)
	return len(value), nil
}

func (buffer *lockedPrivacyBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(buffer.data)
}
