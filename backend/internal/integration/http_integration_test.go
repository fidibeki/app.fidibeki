package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestIntegrationHTTPFanoutAndDeliveryFlow(t *testing.T) {
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
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, integrationMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}

	tenantIDs := map[string]string{}
	for slug, plan := range map[string]string{"acme": "scale", "growth": "growth", "beta": "scale"} {
		var id string
		if err := root.QueryRow(ctx, `
			INSERT INTO tenants (name, slug, plan, status)
			VALUES ($1, $2, $3, 'active') RETURNING id
		`, strings.ToUpper(slug), slug, plan).Scan(&id); err != nil {
			t.Fatalf("create tenant %s: %v", slug, err)
		}
		tenantIDs[slug] = id
	}
	pools, err := db.OpenPools(ctx, integrationRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	cipher, err := NewCipher(testEncryptionKey)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	jwtManager, err := auth.NewJWTManager(strings.Repeat("i", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant("owner", tenantIDs["acme"], auth.TenantPermissionStrings())
	growthToken, _, _ := jwtManager.IssueTenant("growth", tenantIDs["growth"], auth.TenantPermissionStrings())
	betaToken, _, _ := jwtManager.IssueTenant("beta", tenantIDs["beta"], auth.TenantPermissionStrings())
	createOnlyToken, _, _ := jwtManager.IssueTenant("creator", tenantIDs["acme"], []string{
		string(auth.PermissionIntegrationCreate),
	})

	requestCount := 0
	var deliveredBody []byte
	var deliveredHeaders http.Header
	successEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		deliveredBody, _ = io.ReadAll(r.Body)
		deliveredHeaders = r.Header.Clone()
		w.Header().Set("Request-Id", "webhook-message-1")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer successEndpoint.Close()
	failureEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}))
	defer failureEndpoint.Close()
	var slackBody []byte
	slackEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackBody, _ = io.ReadAll(r.Body)
		w.Header().Set("X-Slack-Req-Id", "slack-message-1")
		w.WriteHeader(http.StatusOK)
	}))
	defer slackEndpoint.Close()

	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Store: Store{Cipher: cipher}, JWT: jwtManager, AllowInsecureURLs: true,
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

	createBody := fmt.Sprintf(`{
		"kind":"webhook","name":"Product webhook","webhook_url":%q,
		"custom_headers":{"Authorization":"Bearer stored-secret"},
		"events":["feedback.created","issue.updated"]
	}`, successEndpoint.URL)
	createdResponse := integrationRequest(handler, "acme", http.MethodPost, "/v1/integrations", createBody, ownerToken)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		Integration   Integration `json:"integration"`
		WebhookSecret string      `json:"webhook_secret"`
	}
	decodeIntegrationResponse(t, createdResponse, &created)
	if created.Integration.ID == "" || created.WebhookSecret == "" ||
		created.Integration.WebhookSecretFingerprint == nil ||
		len(created.Integration.CustomHeaderNames) != 1 {
		t.Fatalf("create response = %#v", created)
	}
	var secretCiphertext, headerCiphertext []byte
	var fingerprint string
	if err := root.QueryRow(ctx, `
		SELECT webhook_secret_ciphertext, custom_headers_ciphertext, webhook_secret_fingerprint
		FROM integrations WHERE id = $1
	`, created.Integration.ID).Scan(&secretCiphertext, &headerCiphertext, &fingerprint); err != nil {
		t.Fatalf("inspect encrypted integration: %v", err)
	}
	if bytesContainAny(secretCiphertext, created.WebhookSecret) ||
		bytesContainAny(headerCiphertext, "stored-secret") || fingerprint != SecretFingerprint(created.WebhookSecret) {
		t.Fatal("integration credentials were not encrypted/fingerprinted correctly")
	}
	listed := integrationRequest(handler, "acme", http.MethodGet, "/v1/integrations", "", ownerToken)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), created.WebhookSecret) ||
		strings.Contains(listed.Body.String(), "stored-secret") || !strings.Contains(listed.Body.String(), "Authorization") {
		t.Errorf("list status = %d, body = %s", listed.Code, listed.Body.String())
	}
	growthCreate := integrationRequest(handler, "growth", http.MethodPost, "/v1/integrations", createBody, growthToken)
	if growthCreate.Code != http.StatusPaymentRequired || !strings.Contains(growthCreate.Body.String(), "plan_feature_unavailable") {
		t.Errorf("growth create status = %d, body = %s", growthCreate.Code, growthCreate.Body.String())
	}
	crossToken := integrationRequest(handler, "acme", http.MethodGet, "/v1/integrations", "", betaToken)
	if crossToken.Code != http.StatusForbidden {
		t.Errorf("cross-tenant token status = %d", crossToken.Code)
	}
	malformed := integrationRequest(handler, "acme", http.MethodPost, "/v1/integrations", `{`, ownerToken)
	if malformed.Code != http.StatusBadRequest {
		t.Errorf("malformed status = %d", malformed.Code)
	}
	invalid := integrationRequest(handler, "acme", http.MethodPost, "/v1/integrations",
		`{"kind":"email","name":"Bad","webhook_url":"javascript:alert(1)","events":[]}`, ownerToken)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid status = %d, body = %s", invalid.Code, invalid.Body.String())
	}

	failingID := createIntegrationThroughAPI(t, handler, "acme", ownerToken, KindWebhook, failureEndpoint.URL)
	slackID := createIntegrationThroughAPI(t, handler, "acme", ownerToken, KindSlack, slackEndpoint.URL)
	var projectID, issueID, feedbackID, outboxID string
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.test}') RETURNING id
	`, tenantIDs["acme"]).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title)
		VALUES ($1, $2, 'ACME-2026-0001', 'Checkout feedback') RETURNING id
	`, tenantIDs["acme"], projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, issue_id, public_id, client_feedback_id,
			title, description, page_url, rating, text, text_process_status,
			priority, ai_category, ai_actionability, ai_priority
		) VALUES ($1, $2, $3, 'fb_Intg0001', gen_random_uuid(),
		          'Checkout is confusing', 'The payment step is unclear.',
		          'https://example.test/checkout', 4, 'The checkout is confusing', 'skipped',
		          'high', 'checkout', 'actionable', 'urgent')
		RETURNING id
	`, tenantIDs["acme"], projectID, issueID).Scan(&feedbackID); err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at
		) VALUES ($1, $2, 'screenshot', gen_random_uuid(), $3,
		          'image/png', 1024, 'completed', now())
	`, tenantIDs["acme"], feedbackID, "integration/"+feedbackID+"/screenshot.png"); err != nil {
		t.Fatalf("create feedback attachment: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id)
		VALUES ($1, 'feedback.created', 'feedback', $2) RETURNING id
	`, tenantIDs["acme"], feedbackID).Scan(&outboxID); err != nil {
		t.Fatalf("create outbox event: %v", err)
	}

	event := outbox.Event{
		ID: outboxID, TenantID: tenantIDs["acme"], Event: EventFeedbackCreated,
		ResourceType: "feedback", ResourceID: feedbackID,
	}
	destinations, err := (OutboxResolver{Database: pools.Admin}).Resolve(ctx, event)
	if err != nil || len(destinations) != 3 {
		t.Fatalf("Resolve() = %#v, %v", destinations, err)
	}
	seen := map[string]bool{}
	for _, destination := range destinations {
		seen[destination.ID] = destination.TaskType == TaskDeliver && destination.Kind == "integration"
	}
	if !seen[created.Integration.ID] || !seen[failingID] || !seen[slackID] {
		t.Errorf("resolved destinations = %#v", destinations)
	}

	delivery := DeliveryHandler{
		Store:         Store{Database: pools.Admin, Cipher: cipher},
		PublicBaseURL: "http://api.localhost", BaseDomain: "localhost",
	}
	taskPayload := func(integrationID string) []byte {
		payload, _ := json.Marshal(DeliveryTask{
			OutboxEventID: outboxID, TenantID: tenantIDs["acme"],
			ResourceID: feedbackID, IntegrationID: integrationID,
		})
		return payload
	}
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, taskPayload(created.Integration.ID))); err != nil {
		t.Fatalf("deliver webhook: %v", err)
	}
	if requestCount != 1 || deliveredHeaders.Get("Authorization") != "Bearer stored-secret" ||
		!strings.Contains(string(deliveredBody), `"event":"feedback.created"`) ||
		!strings.Contains(string(deliveredBody), `"schema_version":"1.0"`) ||
		!strings.Contains(string(deliveredBody), `"name":"Acme web"`) ||
		!strings.Contains(string(deliveredBody), `"count":1`) ||
		!strings.Contains(string(deliveredBody), `http://acme.localhost/feedback/`+feedbackID) {
		t.Fatalf("webhook requests = %d, headers = %#v, body = %s", requestCount, deliveredHeaders, deliveredBody)
	}
	mac := hmac.New(sha256.New, []byte(created.WebhookSecret))
	_, _ = mac.Write(deliveredBody)
	if deliveredHeaders.Get("X-Fidibeki-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Errorf("webhook signature = %q", deliveredHeaders.Get("X-Fidibeki-Signature"))
	}
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, taskPayload(created.Integration.ID))); err != nil {
		t.Fatalf("repeat delivery: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("idempotent repeat sent %d provider requests, want 1", requestCount)
	}
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, taskPayload(slackID))); err != nil {
		t.Fatalf("deliver Slack: %v", err)
	}
	if !strings.Contains(string(slackBody), `"blocks"`) ||
		!strings.Contains(string(slackBody), "fb_Intg0001") ||
		!strings.Contains(string(slackBody), `"type":"actions"`) ||
		!strings.Contains(string(slackBody), "View attachments") {
		t.Errorf("Slack body = %s", slackBody)
	}
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, taskPayload(failingID))); err == nil {
		t.Error("failed endpoint delivery error = nil")
	}
	var sentCount, failedCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'sent'), count(*) FILTER (WHERE status = 'failed')
		FROM delivery_logs WHERE outbox_event_id = $1
	`, outboxID).Scan(&sentCount, &failedCount); err != nil {
		t.Fatalf("count delivery logs: %v", err)
	}
	if sentCount != 2 || failedCount != 1 {
		t.Errorf("delivery log counts = sent %d failed %d", sentCount, failedCount)
	}

	var actorID, updateID, issueOutboxID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name)
		VALUES ($1, 'alex@example.test', 'Alex Morgan') RETURNING id
	`, tenantIDs["acme"]).Scan(&actorID); err != nil {
		t.Fatalf("create integration actor: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issue_updates (
			tenant_id, issue_id, member_id, old_status, new_status, comment
		) VALUES ($1, $2, $3, 'open', 'in_progress', 'Investigating payment validation')
		RETURNING id
	`, tenantIDs["acme"], issueID, actorID).Scan(&updateID); err != nil {
		t.Fatalf("create issue integration update: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE issues SET status = 'in_progress', priority = 'urgent' WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("update rich issue integration fixture: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO issue_activities (
			tenant_id, issue_id, actor_member_id, kind, data
		) VALUES ($1, $2, $3, 'issue.status_changed', jsonb_build_object(
			'legacy_update_id', $4::text,
			'transition_comment', 'Investigating payment validation',
			'changes', jsonb_build_object(
				'status', jsonb_build_object('from', 'open', 'to', 'in_progress'),
				'priority', jsonb_build_object('from', null, 'to', 'urgent')
			)
		))
	`, tenantIDs["acme"], issueID, actorID, updateID); err != nil {
		t.Fatalf("create rich issue integration activity: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE integrations SET events = array_append(events, 'issue.updated'::event_type)
		WHERE id = $1 AND NOT ('issue.updated'::event_type = ANY(events))
	`, slackID); err != nil {
		t.Fatalf("subscribe Slack fixture to issue updates: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
		VALUES ($1, 'issue.updated', 'issue', $2, $3) RETURNING id
	`, tenantIDs["acme"], issueID, updateID).Scan(&issueOutboxID); err != nil {
		t.Fatalf("create issue integration outbox: %v", err)
	}
	issueTaskPayload := func(integrationID string) []byte {
		payload, _ := json.Marshal(DeliveryTask{
			OutboxEventID: issueOutboxID, TenantID: tenantIDs["acme"],
			ResourceID: issueID, IntegrationID: integrationID,
		})
		return payload
	}
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, issueTaskPayload(created.Integration.ID))); err != nil {
		t.Fatalf("deliver rich issue webhook: %v", err)
	}
	if requestCount != 2 || !strings.Contains(string(deliveredBody), `"name":"Alex Morgan"`) ||
		!strings.Contains(string(deliveredBody), `"comment":"Investigating payment validation"`) ||
		!strings.Contains(string(deliveredBody), `"status":{"from":"open","to":"in_progress"}`) ||
		!strings.Contains(string(deliveredBody), `http://acme.localhost/issues/`+issueID) {
		t.Fatalf("rich issue webhook requests = %d, body = %s", requestCount, deliveredBody)
	}
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, issueTaskPayload(slackID))); err != nil {
		t.Fatalf("deliver rich issue Slack message: %v", err)
	}
	if !strings.Contains(string(slackBody), "Alex Morgan") ||
		!strings.Contains(string(slackBody), `open -\u0026gt; in progress`) ||
		!strings.Contains(string(slackBody), "Investigating payment validation") ||
		!strings.Contains(string(slackBody), `4/5`) ||
		!strings.Contains(string(slackBody), `checkout`) ||
		!strings.Contains(string(slackBody), "Open issue") {
		t.Errorf("rich issue Slack body = %s", slackBody)
	}

	deniedDelete := integrationRequest(handler, "acme", http.MethodDelete,
		"/v1/integrations/"+created.Integration.ID, "", createOnlyToken)
	if deniedDelete.Code != http.StatusForbidden {
		t.Errorf("delete without permission status = %d", deniedDelete.Code)
	}
	deleted := integrationRequest(handler, "acme", http.MethodDelete,
		"/v1/integrations/"+created.Integration.ID, "", ownerToken)
	if deleted.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	deletedAgain := integrationRequest(handler, "acme", http.MethodDelete,
		"/v1/integrations/"+created.Integration.ID, "", ownerToken)
	if deletedAgain.Code != http.StatusNotFound {
		t.Errorf("repeat delete status = %d", deletedAgain.Code)
	}
	invalidDelete := integrationRequest(handler, "acme", http.MethodDelete,
		"/v1/integrations/not-a-uuid", "", ownerToken)
	if invalidDelete.Code != http.StatusNotFound {
		t.Errorf("invalid delete status = %d", invalidDelete.Code)
	}
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, taskPayload(created.Integration.ID))); err != nil {
		t.Errorf("delivery after integration removal error = %v", err)
	}
	var linkedID *string
	var deliveryKey string
	if err := root.QueryRow(ctx, `
		SELECT integration_id, delivery_key FROM delivery_logs
		WHERE outbox_event_id = $1 AND delivery_key = $2
	`, outboxID, "integration:"+created.Integration.ID).Scan(&linkedID, &deliveryKey); err != nil {
		t.Fatalf("read preserved delivery audit: %v", err)
	}
	if linkedID != nil || deliveryKey != "integration:"+created.Integration.ID {
		t.Errorf("preserved delivery = integration %#v key %q", linkedID, deliveryKey)
	}
}

func createIntegrationThroughAPI(
	t *testing.T,
	handler http.Handler,
	slug, token string,
	kind Kind,
	endpoint string,
) string {
	t.Helper()
	body := fmt.Sprintf(`{"kind":%q,"name":%q,"webhook_url":%q,"events":["feedback.created"]}`,
		kind, strings.ToUpper(string(kind)), endpoint)
	response := integrationRequest(handler, slug, http.MethodPost, "/v1/integrations", body, token)
	if response.Code != http.StatusCreated {
		t.Fatalf("create %s status = %d, body = %s", kind, response.Code, response.Body.String())
	}
	var decoded struct {
		Integration Integration `json:"integration"`
	}
	decodeIntegrationResponse(t, response, &decoded)
	return decoded.Integration.ID
}

func integrationRequest(
	handler http.Handler,
	slug, method, path, body, token string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://"+slug+".localhost"+path, strings.NewReader(body))
	request.Host = slug + ".localhost"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeIntegrationResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func bytesContainAny(value []byte, plaintext string) bool {
	return strings.Contains(string(value), plaintext)
}

func integrationMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	return path
}

func integrationRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
