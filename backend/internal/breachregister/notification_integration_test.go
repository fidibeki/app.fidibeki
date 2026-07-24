package breachregister

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestControllerNotificationLifecycleIsRestrictedIdempotentAndRetryable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
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
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(databaseURL, migrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	connection, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(connection.Close)

	restrictedTables := []string{
		"breach_controller_notifications",
		"breach_controller_notification_attempts",
		"breach_controller_notification_outbox",
	}
	for _, table := range restrictedTables {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var allowed bool
			if err := connection.QueryRow(ctx,
				`SELECT has_table_privilege('fidibeki_app', $1, $2)`, table, privilege,
			).Scan(&allowed); err != nil {
				t.Fatalf("check fidibeki_app privilege on %s: %v", table, err)
			}
			if allowed {
				t.Errorf("fidibeki_app has %s on %s, want no grant", privilege, table)
			}
		}
		for _, privilege := range []string{"SELECT", "INSERT"} {
			var allowed bool
			if err := connection.QueryRow(ctx,
				`SELECT has_table_privilege('fidibeki_admin', $1, $2)`, table, privilege,
			).Scan(&allowed); err != nil {
				t.Fatalf("check fidibeki_admin privilege on %s: %v", table, err)
			}
			if !allowed {
				t.Errorf("fidibeki_admin lacks %s on %s", privilege, table)
			}
		}
	}
	var canUpdateContent, canUpdateStatus bool
	if err := connection.QueryRow(ctx, `
		SELECT has_column_privilege(
			'fidibeki_admin', 'breach_controller_notifications', 'summary', 'UPDATE'
		), has_column_privilege(
			'fidibeki_admin', 'breach_controller_notifications', 'status', 'UPDATE'
		)
	`).Scan(&canUpdateContent, &canUpdateStatus); err != nil {
		t.Fatalf("check controller notification column grants: %v", err)
	}
	if canUpdateContent || !canUpdateStatus {
		t.Fatalf("notification update grants content/status = %t/%t, want false/true", canUpdateContent, canUpdateStatus)
	}

	var platformAdminID, tenantID, missingContactTenantID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ('breach-admin@example.test', '{}') RETURNING id
	`).Scan(&platformAdminID); err != nil {
		t.Fatalf("create platform admin: %v", err)
	}
	if err := connection.QueryRow(ctx, `
		INSERT INTO tenants (
			name, slug, plan, status, privacy_contact_email, privacy_profile_version
		) VALUES (
			'Acme', 'acme-controller-notify', 'growth', 'active',
			'privacy@acme.test', 3
		) RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := connection.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('No Contact', 'no-controller-contact', 'growth', 'active')
		RETURNING id
	`).Scan(&missingContactTenantID); err != nil {
		t.Fatalf("create missing-contact tenant: %v", err)
	}

	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	store := Store{Database: connection, Now: func() time.Time { return now }}
	incident, err := store.CreateIncident(ctx, platformAdminID, "notify-incident", CreateIncidentInput{
		AwarenessAt: now.Add(-time.Hour), DetectionSource: DetectionInternalMonitoring,
		ControllerRole: RoleProcessor, AffectedEnvironments: []string{"staging"},
		AffectedSystemCategories:  []string{"database"},
		AffectedDataCategories:    []string{"contact_info"},
		AffectedSubjectCategories: []string{"reporters_end_users"},
		ConfidentialityImpact:     true,
		ContainmentSummary:        "The affected database access path was disabled.",
		AffectedTenantIDs:         []string{tenantID, missingContactTenantID},
	})
	if err != nil {
		t.Fatalf("CreateIncident() error = %v", err)
	}
	initialInput := StageControllerNotificationInput{
		TenantID: tenantID, NotificationKind: ControllerNotificationInitial,
		Summary:       "An access control allowed an unintended read path.",
		MeasuresTaken: "The path was disabled and credentials were rotated.",
	}
	if _, _, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "missing-contact", StageControllerNotificationInput{
			TenantID: missingContactTenantID, NotificationKind: ControllerNotificationInitial,
			Summary: "Summary", MeasuresTaken: "Measures",
		},
	); !errors.Is(err, ErrControllerContactMissing) {
		t.Fatalf("StageControllerNotification(missing contact) error = %v", err)
	}
	if _, _, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "wrong-tenant", StageControllerNotificationInput{
			TenantID:         "00000000-0000-4000-8000-000000000000",
			NotificationKind: ControllerNotificationInitial,
			Summary:          "Summary", MeasuresTaken: "Measures",
		},
	); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("StageControllerNotification(wrong tenant) error = %v", err)
	}

	initial, created, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "initial-stage", initialInput,
	)
	if err != nil || !created {
		t.Fatalf("StageControllerNotification() = %#v, %t, %v", initial, created, err)
	}
	if initial.Sequence != 1 || initial.Status != ControllerNotificationDraft ||
		initial.RecipientEmail != "privacy@acme.test" || initial.PrivacyProfileVersion != 3 ||
		initial.WorkspaceName != "Acme" {
		t.Fatalf("initial notification = %#v", initial)
	}
	if _, err := connection.Exec(ctx, `
		UPDATE breach_controller_notifications
		SET summary = 'rewritten content'
		WHERE id = $1
	`, initial.ID); err == nil {
		t.Fatal("notification content UPDATE error = nil, want immutable guard rejection")
	}
	if _, err := connection.Exec(ctx, `
		UPDATE tenants
		SET name = 'Acme Updated', privacy_contact_email = 'new-privacy@acme.test',
		    privacy_profile_version = 4
		WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("update tenant privacy profile: %v", err)
	}
	replayed, created, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "initial-stage", initialInput,
	)
	if err != nil || created || replayed.ID != initial.ID ||
		replayed.RecipientEmail != "privacy@acme.test" || replayed.WorkspaceName != "Acme" {
		t.Fatalf("idempotent stage = %#v, %t, %v", replayed, created, err)
	}
	conflictingInput := initialInput
	conflictingInput.Summary = "Different summary"
	if _, _, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "initial-stage", conflictingInput,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting stage error = %v", err)
	}
	if _, _, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "update-too-early",
		StageControllerNotificationInput{
			TenantID: tenantID, NotificationKind: ControllerNotificationUpdate,
			Summary: "Update", MeasuresTaken: "More measures",
		},
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("early update error = %v", err)
	}

	queued, changed, err := store.QueueControllerNotification(
		ctx, incident.ID, initial.ID, platformAdminID, "queue-initial",
	)
	if err != nil || !changed || queued.Status != ControllerNotificationPending {
		t.Fatalf("QueueControllerNotification() = %#v, %t, %v", queued, changed, err)
	}
	replayedQueue, changed, err := store.QueueControllerNotification(
		ctx, incident.ID, initial.ID, platformAdminID, "queue-initial",
	)
	if err != nil || changed || replayedQueue.ID != initial.ID {
		t.Fatalf("idempotent queue = %#v, %t, %v", replayedQueue, changed, err)
	}
	if _, _, err := store.RetryControllerNotification(
		ctx, incident.ID, initial.ID, platformAdminID, "retry-too-early",
	); !errors.Is(err, ErrNotificationNotQueueable) {
		t.Fatalf("early retry error = %v", err)
	}

	outboxStore := ControllerNotificationOutboxStore{Database: connection}
	pending, err := outboxStore.Pending(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Pending() = %#v, %v", pending, err)
	}
	if err := outboxStore.MarkDispatched(ctx, pending[0].ID, now); err != nil {
		t.Fatalf("MarkDispatched() error = %v", err)
	}
	payload, _ := json.Marshal(ControllerNotificationTask{
		OutboxEventID: pending[0].ID, TenantID: tenantID, ResourceID: initial.ID,
	})
	sender := &notificationTestSender{providerID: "resend-initial"}
	handler := ControllerNotificationHandler{
		Store: store, Sender: sender, From: "Fidibeki <updates@fidibeki.com>",
		PublicBaseURL: "https://api.staging.fidibeki.com", BaseDomain: "fidibeki.com",
		Now: func() time.Time { return now.Add(time.Minute) },
	}
	if err := handler.Handler()(ctx, asynq.NewTask(TaskControllerNotification, payload)); err != nil {
		t.Fatalf("controller notification handler error = %v", err)
	}
	if len(sender.messages) != 1 ||
		sender.messages[0].IdempotencyKey != "breach-controller-notification:"+initial.ID ||
		sender.messages[0].To != "privacy@acme.test" {
		t.Fatalf("sent messages = %#v", sender.messages)
	}

	update, created, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "update-stage",
		StageControllerNotificationInput{
			TenantID: tenantID, NotificationKind: ControllerNotificationUpdate,
			Summary:       "The investigation identified the affected time window.",
			MeasuresTaken: "Access logs were reviewed.",
		},
	)
	if err != nil || !created || update.Sequence != 2 ||
		update.RecipientEmail != "new-privacy@acme.test" || update.PrivacyProfileVersion != 4 {
		t.Fatalf("stage update = %#v, %t, %v", update, created, err)
	}
	closure, created, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "closure-stage",
		StageControllerNotificationInput{
			TenantID: tenantID, NotificationKind: ControllerNotificationClosure,
			Summary:       "The incident response work is complete.",
			MeasuresTaken: "The corrective controls were verified.",
		},
	)
	if err != nil || !created || closure.Sequence != 3 {
		t.Fatalf("stage closure = %#v, %t, %v", closure, created, err)
	}
	if _, _, err := store.QueueControllerNotification(
		ctx, incident.ID, closure.ID, platformAdminID, "queue-closure-too-early",
	); !errors.Is(err, ErrNotificationNotQueueable) {
		t.Fatalf("out-of-order closure queue error = %v", err)
	}
	if _, _, err := store.StageControllerNotification(
		ctx, incident.ID, platformAdminID, "update-after-closure",
		StageControllerNotificationInput{
			TenantID: tenantID, NotificationKind: ControllerNotificationUpdate,
			Summary: "Late update", MeasuresTaken: "Late measures",
		},
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("update after closure error = %v", err)
	}
	queuedUpdate, changed, err := store.QueueControllerNotification(
		ctx, incident.ID, update.ID, platformAdminID, "queue-update",
	)
	if err != nil || !changed || queuedUpdate.Status != ControllerNotificationPending {
		t.Fatalf("queue update = %#v, %t, %v", queuedUpdate, changed, err)
	}
	pending, err = outboxStore.Pending(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Pending(update) = %#v, %v", pending, err)
	}
	if err := outboxStore.MarkDispatched(ctx, pending[0].ID, now); err != nil {
		t.Fatalf("MarkDispatched(update) error = %v", err)
	}
	payload, _ = json.Marshal(ControllerNotificationTask{
		OutboxEventID: pending[0].ID, TenantID: tenantID, ResourceID: update.ID,
	})
	if err := handler.Handler()(ctx, asynq.NewTask(TaskControllerNotification, payload)); err != nil {
		t.Fatalf("update controller notification handler error = %v", err)
	}
	queuedClosure, changed, err := store.QueueControllerNotification(
		ctx, incident.ID, closure.ID, platformAdminID, "queue-closure",
	)
	if err != nil || !changed || queuedClosure.Status != ControllerNotificationPending {
		t.Fatalf("queue closure = %#v, %t, %v", queuedClosure, changed, err)
	}
	pending, err = outboxStore.Pending(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Pending(closure) = %#v, %v", pending, err)
	}
	if err := outboxStore.MarkDispatched(ctx, pending[0].ID, now); err != nil {
		t.Fatalf("MarkDispatched(closure) error = %v", err)
	}
	payload, _ = json.Marshal(ControllerNotificationTask{
		OutboxEventID: pending[0].ID, TenantID: tenantID, ResourceID: closure.ID,
	})
	failingSender := &notificationTestSender{err: errors.New("provider-secret-response")}
	failingHandler := handler
	failingHandler.Sender = failingSender
	sendErr := failingHandler.Handler()(
		ctx, asynq.NewTask(TaskControllerNotification, payload),
	)
	if sendErr == nil {
		t.Fatal("failing controller notification handler error = nil")
	}
	if strings.Contains(sendErr.Error(), "provider-secret-response") {
		t.Fatalf("worker error leaked provider response: %v", sendErr)
	}
	_, _, _, _, _, notifications, _, err := store.GetIncident(ctx, incident.ID)
	if err != nil {
		t.Fatalf("GetIncident() error = %v", err)
	}
	closure = notificationByID(t, notifications, closure.ID)
	if closure.Status != ControllerNotificationFailed || closure.LastError == nil ||
		*closure.LastError != "email provider delivery failed" ||
		len(closure.DeliveryAttempts) != 1 ||
		closure.DeliveryAttempts[0].ErrorMessage == nil ||
		strings.Contains(*closure.DeliveryAttempts[0].ErrorMessage, "provider-secret-response") {
		t.Fatalf("failed closure evidence = %#v", closure)
	}
	retried, changed, err := store.RetryControllerNotification(
		ctx, incident.ID, closure.ID, platformAdminID, "retry-closure",
	)
	if err != nil || !changed || retried.Status != ControllerNotificationPending {
		t.Fatalf("RetryControllerNotification() = %#v, %t, %v", retried, changed, err)
	}

	if _, err := connection.Exec(ctx, `
		UPDATE breach_controller_notification_attempts
		SET error_message = 'rewritten'
		WHERE notification_id = $1
	`, closure.ID); err == nil {
		t.Fatal("attempt UPDATE error = nil, want append-only trigger rejection")
	}
}

func notificationByID(
	t *testing.T,
	notifications []ControllerNotification,
	notificationID string,
) ControllerNotification {
	t.Helper()
	for _, notification := range notifications {
		if notification.ID == notificationID {
			return notification
		}
	}
	t.Fatalf("notification %s not found in %#v", notificationID, notifications)
	return ControllerNotification{}
}

type notificationTestSender struct {
	providerID string
	err        error
	messages   []email.Message
}

func (s *notificationTestSender) Send(
	_ context.Context,
	message email.Message,
) (string, error) {
	s.messages = append(s.messages, message)
	if s.err != nil {
		return "", s.err
	}
	return s.providerID, nil
}
