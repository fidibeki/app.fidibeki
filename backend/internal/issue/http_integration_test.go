package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestIssueStatusNotificationAndDeliveryLifecycle(t *testing.T) {
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
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, issueMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app role: %v", err)
	}

	var tenantID, memberID, projectID, issueID, targetID, feedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2) RETURNING id
	`, tenantID, auth.TenantPermissionStrings()).Scan(&memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.com}') RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (
			tenant_id, project_id, public_id, title, reporter_email, notify_reporter
		) VALUES ($1, $2, 'ACME-2026-0001', 'Checkout is confusing', 'reporter@example.com', true)
		RETURNING id
	`, tenantID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title)
		VALUES ($1, $2, 'ACME-2026-0002', 'Target issue') RETURNING id
	`, tenantID, projectID).Scan(&targetID); err != nil {
		t.Fatalf("create merge target: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, issue_id, client_feedback_id,
			text, reporter_email, notify_reporter, page_url, text_process_status
		) VALUES ($1, $2, 'fb_Issue001', $3, gen_random_uuid(), 'Checkout is confusing',
		          'reporter@example.com', true, 'https://example.com/checkout', 'pending')
		RETURNING id
	`, tenantID, projectID, issueID).Scan(&feedbackID); err != nil {
		t.Fatalf("create feedback: %v", err)
	}

	pools, err := db.OpenPools(ctx, issueRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	accessTokens := newTestJWT(t, 10*time.Minute)
	reporterTokens := newTestJWT(t, 90*24*time.Hour)
	accessToken, _, err := accessTokens.IssueTenant(memberID, tenantID, auth.TenantPermissionStrings())
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{Store: Store{}, JWT: accessTokens, Now: func() time.Time { return now }})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "fidibeki.localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)

	list := issueRequest(handler, accessToken, http.MethodGet, "/v1/issues", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "ACME-2026-0001") {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	updated := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"in_progress","comment":"We are investigating"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body.String())
	}
	var updateCount, eventCount int
	var updateMemberID, oldStatus, newStatus string
	if err := root.QueryRow(ctx, `
		SELECT member_id, old_status, new_status
		FROM issue_updates WHERE issue_id = $1 ORDER BY created_at, id LIMIT 1
	`, issueID).Scan(&updateMemberID, &oldStatus, &newStatus); err != nil {
		t.Fatalf("read issue updates: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM issue_updates WHERE issue_id = $1`, issueID).
		Scan(&updateCount); err != nil {
		t.Fatalf("count issue updates: %v", err)
	}
	if updateCount != 1 || updateMemberID != memberID || oldStatus != "open" || newStatus != "in_progress" {
		t.Errorf("update history = count %d member %q %s -> %s", updateCount, updateMemberID, oldStatus, newStatus)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE resource_id = $1`, issueID).Scan(&eventCount); err != nil {
		t.Fatalf("count outbox events: %v", err)
	}
	if eventCount != 1 {
		t.Errorf("outbox event count = %d", eventCount)
	}
	detail := issueRequest(handler, accessToken, http.MethodGet, "/v1/issues/"+issueID, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "We are investigating") {
		t.Fatalf("issue detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	invalidStatus := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"invalid"}`)
	if invalidStatus.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid status = %d, body = %s", invalidStatus.Code, invalidStatus.Body.String())
	}
	malformed := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID, `{`)
	if malformed.Code != http.StatusBadRequest {
		t.Errorf("malformed status update = %d, body = %s", malformed.Code, malformed.Body.String())
	}
	longComment := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		fmt.Sprintf(`{"status":"resolved","comment":%q}`, strings.Repeat("x", 2001)))
	if longComment.Code != http.StatusUnprocessableEntity {
		t.Errorf("long status comment = %d, body = %s", longComment.Code, longComment.Body.String())
	}
	missing := issueRequest(handler, accessToken, http.MethodGet,
		"/v1/issues/00000000-0000-0000-0000-000000000000", "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing issue = %d, body = %s", missing.Code, missing.Body.String())
	}
	invalidPath := issueRequest(handler, accessToken, http.MethodGet, "/v1/issues/not-a-uuid", "")
	if invalidPath.Code != http.StatusNotFound {
		t.Errorf("invalid issue path = %d, body = %s", invalidPath.Code, invalidPath.Body.String())
	}
	noChange := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"in_progress"}`)
	if noChange.Code != http.StatusConflict {
		t.Errorf("same status = %d, body = %s", noChange.Code, noChange.Body.String())
	}
	invalidMerge := issueRequest(handler, accessToken, http.MethodPost, "/v1/issues/"+issueID+"/merge",
		`{"target_issue_id":"invalid"}`)
	if invalidMerge.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid merge target = %d, body = %s", invalidMerge.Code, invalidMerge.Body.String())
	}
	selfMerge := issueRequest(handler, accessToken, http.MethodPost, "/v1/issues/"+issueID+"/merge",
		fmt.Sprintf(`{"target_issue_id":%q}`, issueID))
	if selfMerge.Code != http.StatusConflict {
		t.Errorf("self merge = %d, body = %s", selfMerge.Code, selfMerge.Body.String())
	}

	merged := issueRequest(handler, accessToken, http.MethodPost, "/v1/issues/"+issueID+"/merge",
		fmt.Sprintf(`{"target_issue_id":%q}`, targetID))
	if merged.Code != http.StatusOK || !strings.Contains(merged.Body.String(), targetID) {
		t.Fatalf("merge status = %d, body = %s", merged.Code, merged.Body.String())
	}
	unmerged := issueRequest(handler, accessToken, http.MethodDelete, "/v1/issues/"+issueID+"/merge", "")
	if unmerged.Code != http.StatusOK || strings.Contains(unmerged.Body.String(), "merged_into_id") {
		t.Fatalf("unmerge status = %d, body = %s", unmerged.Code, unmerged.Body.String())
	}
	missingUnmerge := issueRequest(handler, accessToken, http.MethodDelete,
		"/v1/issues/00000000-0000-0000-0000-000000000000/merge", "")
	if missingUnmerge.Code != http.StatusNotFound {
		t.Errorf("missing unmerge = %d, body = %s", missingUnmerge.Code, missingUnmerge.Body.String())
	}

	store := Store{Database: pools.Admin}
	sender := &sequenceSender{}
	reporter := ReporterHandler{
		Store: store, Sender: sender, Tokens: reporterTokens,
		PublicBaseURL: "http://api.fidibeki.localhost:8088", Now: func() time.Time { return now },
	}
	firstEvent := latestIssueEvent(t, ctx, root, issueID)
	firstTask := reporterTask(t, firstEvent, tenantID, issueID)
	if err := reporter.Handler()(ctx, firstTask); err != nil {
		t.Fatalf("send first reporter notification: %v", err)
	}
	if err := reporter.Handler()(ctx, firstTask); err != nil {
		t.Fatalf("repeat first reporter notification: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0].IdempotencyKey != "reporter:"+firstEvent {
		t.Fatalf("sent messages = %#v", sender.messages)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM delivery_logs WHERE outbox_event_id = $1`, firstEvent).
		Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("delivery attempts = %d, error = %v", eventCount, err)
	}

	unsubscribeURL := strings.TrimPrefix(strings.Split(sender.messages[0].Text, "Unsubscribe: ")[1], " ")
	publicRouter := chi.NewRouter()
	RegisterPublicRoutes(publicRouter, PublicHTTPOptions{
		Store: store, Tokens: reporterTokens, Verifier: verifierStub{},
	})
	unsubscribeRequest := httptest.NewRequest(http.MethodGet, strings.TrimSpace(unsubscribeURL), nil)
	unsubscribeResponse := httptest.NewRecorder()
	publicRouter.ServeHTTP(unsubscribeResponse, unsubscribeRequest)
	if unsubscribeResponse.Code != http.StatusOK {
		t.Fatalf("unsubscribe status = %d, body = %s", unsubscribeResponse.Code, unsubscribeResponse.Body.String())
	}
	missingToken, _, err := reporterTokens.IssueReporterUnsubscribe(
		"00000000-0000-0000-0000-000000000000", tenantID,
	)
	if err != nil {
		t.Fatalf("issue missing reporter token: %v", err)
	}
	missingUnsubscribe := httptest.NewRecorder()
	publicRouter.ServeHTTP(missingUnsubscribe, httptest.NewRequest(
		http.MethodGet, "/v1/reporter/unsubscribe?token="+url.QueryEscape(missingToken), nil,
	))
	if missingUnsubscribe.Code != http.StatusBadRequest {
		t.Errorf("missing unsubscribe issue status = %d", missingUnsubscribe.Code)
	}
	assertReporterOptIn(t, ctx, root, issueID, feedbackID, false)
	if err := reporter.Handler()(ctx, firstTask); err != nil {
		t.Fatalf("skip unsubscribed reporter notification: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Errorf("unsubscribed reporter received %d messages", len(sender.messages))
	}

	setReporterOptIn(t, ctx, root, issueID, feedbackID, true)
	resolved := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"resolved"}`)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, body = %s", resolved.Code, resolved.Body.String())
	}
	secondEvent := latestIssueEvent(t, ctx, root, issueID)
	if err := reporter.Handler()(ctx, reporterTask(t, secondEvent, tenantID, issueID)); err != nil {
		t.Fatalf("send resolved notification: %v", err)
	}
	complaint := webhookRequest(publicRouter, "email.complained", sender.ids[1], "webhook-complaint")
	if complaint.Code != http.StatusOK {
		t.Fatalf("complaint status = %d, body = %s", complaint.Code, complaint.Body.String())
	}
	assertReporterOptIn(t, ctx, root, issueID, feedbackID, false)
	var deliveryStatus string
	if err := root.QueryRow(ctx, `SELECT status FROM delivery_logs WHERE provider_message_id = $1`, sender.ids[1]).Scan(&deliveryStatus); err != nil {
		t.Fatalf("read complaint delivery: %v", err)
	}
	if deliveryStatus != "sent" {
		t.Errorf("complaint delivery status = %q", deliveryStatus)
	}

	setReporterOptIn(t, ctx, root, issueID, feedbackID, true)
	closed := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"closed"}`)
	if closed.Code != http.StatusOK {
		t.Fatalf("close status = %d, body = %s", closed.Code, closed.Body.String())
	}
	thirdEvent := latestIssueEvent(t, ctx, root, issueID)
	if err := reporter.Handler()(ctx, reporterTask(t, thirdEvent, tenantID, issueID)); err != nil {
		t.Fatalf("send closed notification: %v", err)
	}
	bounce := webhookRequest(publicRouter, "email.bounced", sender.ids[2], "webhook-bounce")
	if bounce.Code != http.StatusOK {
		t.Fatalf("bounce status = %d, body = %s", bounce.Code, bounce.Body.String())
	}
	assertReporterOptIn(t, ctx, root, issueID, feedbackID, false)
	if err := root.QueryRow(ctx, `SELECT status FROM delivery_logs WHERE provider_message_id = $1`, sender.ids[2]).Scan(&deliveryStatus); err != nil {
		t.Fatalf("read bounced delivery: %v", err)
	}
	if deliveryStatus != "bounced" {
		t.Errorf("bounce delivery status = %q", deliveryStatus)
	}

	setReporterOptIn(t, ctx, root, issueID, feedbackID, true)
	reopened := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"open"}`)
	if reopened.Code != http.StatusOK {
		t.Fatalf("reopen status = %d, body = %s", reopened.Code, reopened.Body.String())
	}
	failureEvent := latestIssueEvent(t, ctx, root, issueID)
	failingSender := &senderStub{err: errors.New("provider unavailable")}
	failingReporter := ReporterHandler{
		Store: store, Sender: failingSender, Tokens: reporterTokens,
		PublicBaseURL: "http://api.fidibeki.localhost:8088", Now: func() time.Time { return now },
	}
	failureTask := reporterTask(t, failureEvent, tenantID, issueID)
	if err := failingReporter.Handler()(ctx, failureTask); err == nil {
		t.Fatal("failed reporter send error = nil")
	}
	if err := root.QueryRow(ctx, `SELECT status FROM delivery_logs WHERE outbox_event_id = $1`, failureEvent).
		Scan(&deliveryStatus); err != nil {
		t.Fatalf("read failed delivery: %v", err)
	}
	if deliveryStatus != "failed" {
		t.Errorf("failed delivery status = %q", deliveryStatus)
	}
	if err := reporter.Handler()(ctx, failureTask); err != nil {
		t.Fatalf("recover failed reporter delivery: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT status FROM delivery_logs WHERE outbox_event_id = $1`, failureEvent).
		Scan(&deliveryStatus); err != nil || deliveryStatus != "sent" {
		t.Errorf("recovered delivery status = %q, error = %v", deliveryStatus, err)
	}

	missingTask := reporterTask(
		t, "00000000-0000-0000-0000-000000000000", tenantID, issueID,
	)
	if err := reporter.Handler()(ctx, missingTask); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("missing reporter event error = %v, want SkipRetry", err)
	}
}

type sequenceSender struct {
	messages []email.Message
	ids      []string
}

func (s *sequenceSender) Send(_ context.Context, message email.Message) (string, error) {
	id := fmt.Sprintf("message-%d", len(s.messages)+1)
	s.messages = append(s.messages, message)
	s.ids = append(s.ids, id)
	return id, nil
}

func issueRequest(handler http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://acme.fidibeki.localhost"+path, strings.NewReader(body))
	request.Host = "acme.fidibeki.localhost"
	request.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func latestIssueEvent(t *testing.T, ctx context.Context, root *pgx.Conn, issueID string) string {
	t.Helper()
	var eventID string
	if err := root.QueryRow(ctx, `
		SELECT id FROM outbox_events WHERE resource_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1
	`, issueID).Scan(&eventID); err != nil {
		t.Fatalf("read latest issue event: %v", err)
	}
	return eventID
}

func reporterTask(t *testing.T, eventID, tenantID, issueID string) *asynq.Task {
	t.Helper()
	payload, err := json.Marshal(ReporterTask{
		OutboxEventID: eventID, TenantID: tenantID, ResourceID: issueID,
	})
	if err != nil {
		t.Fatalf("encode reporter task: %v", err)
	}
	return asynq.NewTask(TaskReporterNotify, payload)
}

func webhookRequest(handler http.Handler, eventType, messageID, webhookID string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"type":%q,"data":{"email_id":%q,"to":["reporter@example.com"]}}`, eventType, messageID)
	request := httptest.NewRequest(http.MethodPost, "/v1/webhooks/resend", strings.NewReader(body))
	request.Header.Set("svix-id", webhookID)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func setReporterOptIn(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	issueID, feedbackID string,
	enabled bool,
) {
	t.Helper()
	if _, err := root.Exec(ctx, `UPDATE issues SET notify_reporter = $2 WHERE id = $1`, issueID, enabled); err != nil {
		t.Fatalf("set issue reporter opt-in: %v", err)
	}
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET notify_reporter = $2 WHERE id = $1`, feedbackID, enabled); err != nil {
		t.Fatalf("set feedback reporter opt-in: %v", err)
	}
}

func assertReporterOptIn(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	issueID, feedbackID string,
	want bool,
) {
	t.Helper()
	var issueOptIn, feedbackOptIn bool
	if err := root.QueryRow(ctx, `SELECT notify_reporter FROM issues WHERE id = $1`, issueID).Scan(&issueOptIn); err != nil {
		t.Fatalf("read issue opt-in: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT notify_reporter FROM feedbacks WHERE id = $1`, feedbackID).Scan(&feedbackOptIn); err != nil {
		t.Fatalf("read feedback opt-in: %v", err)
	}
	if issueOptIn != want || feedbackOptIn != want {
		t.Errorf("opt-in = issue %t feedback %t, want %t", issueOptIn, feedbackOptIn, want)
	}
}

func issueMigrationsPath(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", "..", "infra", "migrations"))
}

func issueRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse direct URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
