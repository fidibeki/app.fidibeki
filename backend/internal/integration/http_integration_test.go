package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
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
	memberIDs := map[string]string{}
	for _, slug := range []string{"acme", "growth", "beta"} {
		var memberID string
		if err := root.QueryRow(ctx, `
			INSERT INTO tenant_members (tenant_id, email, permissions)
			VALUES ($1, $2, $3) RETURNING id
		`, tenantIDs[slug], "owner@"+slug+".test", auth.TenantPermissionStrings()).Scan(&memberID); err != nil {
			t.Fatalf("create %s integration owner: %v", slug, err)
		}
		memberIDs[slug] = memberID
	}
	var creatorID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'creator@acme.test', ARRAY['integration:create']) RETURNING id
	`, tenantIDs["acme"]).Scan(&creatorID); err != nil {
		t.Fatalf("create integration-only member: %v", err)
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
	ownerToken, _, _ := jwtManager.IssueTenant(memberIDs["acme"], tenantIDs["acme"], auth.TenantPermissionStrings())
	growthToken, _, _ := jwtManager.IssueTenant(memberIDs["growth"], tenantIDs["growth"], auth.TenantPermissionStrings())
	betaToken, _, _ := jwtManager.IssueTenant(memberIDs["beta"], tenantIDs["beta"], auth.TenantPermissionStrings())
	createOnlyToken, _, _ := jwtManager.IssueTenant(creatorID, tenantIDs["acme"], []string{
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
		"events":["feedback.created","issue.updated"],
		"privacy_fields":["description","page","rating","classification","workflow","attachments"]
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
		len(created.Integration.CustomHeaderNames) != 1 ||
		created.Integration.PrivacyProfileVersion != PrivacyProfileVersion ||
		created.Integration.DestinationCapabilities.MessageRecall {
		t.Fatalf("create response = %#v", created)
	}
	privacyResponse := integrationRequest(
		handler, "acme", http.MethodPatch,
		"/v1/integrations/"+created.Integration.ID+"/privacy",
		`{"privacy_fields":[]}`, ownerToken,
	)
	if privacyResponse.Code != http.StatusOK {
		t.Fatalf("minimum privacy update status = %d, body = %s",
			privacyResponse.Code, privacyResponse.Body.String())
	}
	var privacyUpdated struct {
		Integration Integration `json:"integration"`
	}
	decodeIntegrationResponse(t, privacyResponse, &privacyUpdated)
	if len(privacyUpdated.Integration.PrivacyFields) != 0 {
		t.Fatalf("minimum privacy fields = %#v", privacyUpdated.Integration.PrivacyFields)
	}
	privacyDenied := integrationRequest(
		handler, "acme", http.MethodPatch,
		"/v1/integrations/"+created.Integration.ID+"/privacy",
		`{"privacy_fields":["description"]}`, createOnlyToken,
	)
	if privacyDenied.Code != http.StatusForbidden {
		t.Fatalf("create-only privacy update status = %d", privacyDenied.Code)
	}
	privacyCrossTenant := integrationRequest(
		handler, "beta", http.MethodPatch,
		"/v1/integrations/"+created.Integration.ID+"/privacy",
		`{"privacy_fields":["description"]}`, betaToken,
	)
	if privacyCrossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant privacy update status = %d, body = %s",
			privacyCrossTenant.Code, privacyCrossTenant.Body.String())
	}
	privacyInvalid := integrationRequest(
		handler, "acme", http.MethodPatch,
		"/v1/integrations/"+created.Integration.ID+"/privacy",
		`{"privacy_fields":["reporter_email"]}`, ownerToken,
	)
	if privacyInvalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid privacy update status = %d, body = %s",
			privacyInvalid.Code, privacyInvalid.Body.String())
	}
	privacyResponse = integrationRequest(
		handler, "acme", http.MethodPatch,
		"/v1/integrations/"+created.Integration.ID+"/privacy",
		`{"privacy_fields":["description","page","rating","classification","workflow","attachments"]}`,
		ownerToken,
	)
	if privacyResponse.Code != http.StatusOK {
		t.Fatalf("full privacy update status = %d, body = %s",
			privacyResponse.Code, privacyResponse.Body.String())
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
			title, description, page_url, rating, text, text_process_status, ai_processed_at,
			priority, ai_category, ai_actionability, ai_priority
		) VALUES ($1, $2, $3, 'fb_Intg0001', gen_random_uuid(),
		          'Checkout is confusing', 'The payment step is unclear.',
		          'https://example.test/checkout', 4, 'The checkout is confusing', 'skipped', now(),
		          'high', 'checkout', 'actionable', 'urgent')
		RETURNING id
	`, tenantIDs["acme"], projectID, issueID).Scan(&feedbackID); err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at,
			safety_status, safety_checked_at, detected_content_type
		) VALUES ($1, $2, 'screenshot', gen_random_uuid(), $3,
		          'image/png', 1024, 'completed', now(), 'safe', now(), 'image/png')
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
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET ai_processed_at = NULL WHERE id = $1`, feedbackID); err != nil {
		t.Fatalf("mark integration feedback moderation pending: %v", err)
	}
	_, err = delivery.Store.LoadNotification(ctx, DeliveryTask{
		OutboxEventID: outboxID, TenantID: tenantIDs["acme"],
		ResourceID: feedbackID, IntegrationID: created.Integration.ID,
	})
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("pending moderation LoadNotification() error = %v, want ErrNotReady", err)
	}
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET ai_processed_at = now() WHERE id = $1`, feedbackID); err != nil {
		t.Fatalf("complete integration feedback moderation: %v", err)
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
	var categories []string
	var destinationKind string
	var profileVersion int
	var canRecall bool
	if err := root.QueryRow(ctx, `
		SELECT data_categories, destination_kind::text, privacy_profile_version,
		       (destination_capabilities->>'message_recall')::boolean
		FROM delivery_logs
		WHERE outbox_event_id = $1 AND integration_id = $2
	`, outboxID, created.Integration.ID).Scan(
		&categories, &destinationKind, &profileVersion, &canRecall,
	); err != nil {
		t.Fatalf("read disclosure ledger: %v", err)
	}
	if destinationKind != string(KindWebhook) || profileVersion != PrivacyProfileVersion ||
		canRecall || !slices.Contains(categories, "description") ||
		!slices.Contains(categories, "attachment_metadata") {
		t.Errorf("disclosure ledger = kind %q version %d recall %t categories %#v",
			destinationKind, profileVersion, canRecall, categories)
	}
	var restrictedFeedbackID, restrictedCaseID, restrictedOutboxID string
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, client_feedback_id, title, description,
			page_url, reporter_email, text_process_status, ai_processed_at
		) VALUES (
			$1, $2, 'fb_Rstr0001', gen_random_uuid(), 'Restricted title',
			'RESTRICTED-DESCRIPTION', 'https://example.test/private?token=secret',
			'restricted@example.test', 'skipped', now()
		) RETURNING id
	`, tenantIDs["acme"], projectID).Scan(&restrictedFeedbackID); err != nil {
		t.Fatalf("create restricted integration feedback: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO privacy_cases (
			tenant_id, public_id, subject_type, right_type, controller_role, status,
			subject_email, subject_email_digest, requested_by_member_id, owner_member_id,
			verification_expires_at, verification_method, verified_at, received_at,
			deadline_at, idempotency_key, request_fingerprint
		) VALUES (
			$1, 'PRIV-RESTRICTED-1', 'reporter', 'restriction',
			'customer_controller_assistance', 'in_progress',
			'restricted@example.test', digest('restricted@example.test', 'sha256'), $2, $2,
			now() + interval '1 day', 'email_link', now(), now(), now() + interval '30 days',
			'integration-restriction-test', digest('integration-restriction-test', 'sha256')
		) RETURNING id
	`, tenantIDs["acme"], memberIDs["acme"]).Scan(&restrictedCaseID); err != nil {
		t.Fatalf("create restricted integration privacy case: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO privacy_processing_restrictions (
			tenant_id, privacy_case_id, purposes, reason, activated_at
		) VALUES ($1, $2, '{integration}', 'Reporter restriction request', now())
	`, tenantIDs["acme"], restrictedCaseID); err != nil {
		t.Fatalf("activate integration restriction: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
		VALUES ($1, 'feedback.created', 'feedback', $2, 'restricted-test') RETURNING id
	`, tenantIDs["acme"], restrictedFeedbackID).Scan(&restrictedOutboxID); err != nil {
		t.Fatalf("create restricted integration event: %v", err)
	}
	restrictedPayload, _ := json.Marshal(DeliveryTask{
		OutboxEventID: restrictedOutboxID, TenantID: tenantIDs["acme"],
		ResourceID: restrictedFeedbackID, IntegrationID: created.Integration.ID,
	})
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, restrictedPayload)); err != nil {
		t.Fatalf("suppress restricted integration delivery: %v", err)
	}
	var restrictedStatus, restrictedError string
	if err := root.QueryRow(ctx, `
		SELECT status::text, error_message
		FROM delivery_logs
		WHERE outbox_event_id = $1 AND integration_id = $2
	`, restrictedOutboxID, created.Integration.ID).Scan(
		&restrictedStatus, &restrictedError,
	); err != nil {
		t.Fatalf("read restricted integration ledger: %v", err)
	}
	if requestCount != 1 || restrictedStatus != "failed" ||
		restrictedError != "integration delivery suppressed by privacy restriction" {
		t.Errorf("restricted delivery = requests %d status %q error %q",
			requestCount, restrictedStatus, restrictedError)
	}
	const unsafeDescription = "The payment step is unclear."
	const unsafeText = "The checkout is confusing"
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks SET text_moderation_flags = '{profanity}',
		       ai_suggested_title = 'Checkout feedback needs review', ai_processed_at = now()
		WHERE id = $1
	`, feedbackID); err != nil {
		t.Fatalf("flag integration feedback: %v", err)
	}
	var moderatedOutboxID string
	if err := root.QueryRow(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
		VALUES ($1, 'feedback.created', 'feedback', $2, 'moderated-test') RETURNING id
	`, tenantIDs["acme"], feedbackID).Scan(&moderatedOutboxID); err != nil {
		t.Fatalf("create moderated feedback event: %v", err)
	}
	moderatedPayload, _ := json.Marshal(DeliveryTask{
		OutboxEventID: moderatedOutboxID, TenantID: tenantIDs["acme"],
		ResourceID: feedbackID, IntegrationID: created.Integration.ID,
	})
	if err := delivery.Handler()(ctx, asynq.NewTask(TaskDeliver, moderatedPayload)); err != nil {
		t.Fatalf("deliver moderated webhook: %v", err)
	}
	if requestCount != 2 || !strings.Contains(string(deliveredBody), "Checkout feedback needs review") ||
		!strings.Contains(string(deliveredBody), `"description":"[MODERATED]"`) ||
		strings.Contains(string(deliveredBody), unsafeDescription) || strings.Contains(string(deliveredBody), unsafeText) {
		t.Fatalf("moderated webhook requests = %d body = %s", requestCount, deliveredBody)
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
	if requestCount != 3 || !strings.Contains(string(deliveredBody), `"name":"Alex Morgan"`) ||
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

	verifySlackOneClickInstall(t, ctx, slackInstallHarness{
		root: root, pools: pools, cipher: cipher, jwt: jwtManager,
		tenantIDs: tenantIDs, memberIDs: memberIDs,
		ownerToken: ownerToken, growthToken: growthToken, betaToken: betaToken,
	})
}

type slackInstallHarness struct {
	root      *pgx.Conn
	pools     *db.Pools
	cipher    *Cipher
	jwt       *auth.JWTManager
	tenantIDs map[string]string
	memberIDs map[string]string

	ownerToken  string
	growthToken string
	betaToken   string
}

// verifySlackOneClickInstall drives the whole round trip against the real schema: the
// tenant-scoped install endpoint, then the apex callback, which runs outside the tenant
// transaction middleware and has to scope itself from the state it consumes.
func verifySlackOneClickInstall(t *testing.T, ctx context.Context, harness slackInstallHarness) {
	t.Helper()

	const webhookURL = "https://hooks.slack.com/services/T-ACME/B-INSTALL/secret-path"
	const accessToken = "xoxb-install-token"
	channel, channelID := "#feedback", "C-FEEDBACK"
	slackAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/oauth.v2.access":
			_, _ = fmt.Fprintf(w, `{"ok":true,"access_token":%q,"team":{"id":"T-ACME","name":"Acme HQ"},
				"incoming_webhook":{"url":%q,"channel":%q,"channel_id":%q}}`,
				accessToken, webhookURL, channel, channelID)
		case "/api/auth.revoke":
			_, _ = fmt.Fprint(w, `{"ok":true,"revoked":true}`)
		default:
			t.Errorf("unexpected Slack path %q", r.URL.Path)
		}
	}))
	defer slackAPI.Close()

	oauth := SlackOAuth{
		ClientID: "client-id", ClientSecret: "client-secret",
		RedirectURL: "https://api.localhost" + SlackCallbackPath,
		Client:      rewritingDoer{base: slackAPI.URL, client: slackAPI.Client()},
		States:      newFakeStateStore(),
	}
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Store: Store{Cipher: harness.cipher}, JWT: harness.jwt,
		AllowInsecureURLs: true, Slack: oauth,
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: harness.pools.Admin},
		Begin: db.BeginFromPool(harness.pools.App), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

	// The callback lives on the apex host with no tenant middleware at all.
	apex := chi.NewRouter()
	RegisterSlackCallbackRoute(apex, SlackCallbackOptions{
		Store: Store{Cipher: harness.cipher}, Slack: oauth,
		Begin:      db.BeginFromPool(harness.pools.App),
		BaseDomain: "localhost", WebPublicBaseURL: "http://localhost",
	})

	install := func(slug, token string) *httptest.ResponseRecorder {
		return integrationRequest(handler, slug, http.MethodPost, "/v1/integrations/slack/install",
			`{"events":["feedback.created","issue.resolved"],"privacy_fields":["description"]}`, token)
	}
	callback := func(query string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "http://api.localhost"+SlackCallbackPath+query, nil)
		request.Host = "api.localhost"
		recorder := httptest.NewRecorder()
		apex.ServeHTTP(recorder, request)
		return recorder
	}
	beginInstall := func(t *testing.T, slug, token string) string {
		t.Helper()
		response := install(slug, token)
		if response.Code != http.StatusOK {
			t.Fatalf("install status = %d, body = %s", response.Code, response.Body.String())
		}
		var decoded struct {
			AuthorizeURL string `json:"authorize_url"`
		}
		decodeIntegrationResponse(t, response, &decoded)
		return mustQueryParam(t, decoded.AuthorizeURL, "state")
	}

	// The plan gate and RBAC apply to the install endpoint exactly as they do to a create.
	if gated := install("growth", harness.growthToken); gated.Code != http.StatusPaymentRequired {
		t.Errorf("growth install status = %d, want %d", gated.Code, http.StatusPaymentRequired)
	}
	// Permissions are read from the member row rather than the token, so denial has to be
	// proven with a member that genuinely lacks integration:create.
	var readerID string
	if err := harness.root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'reader@acme.test', ARRAY['integration:read']) RETURNING id
	`, harness.tenantIDs["acme"]).Scan(&readerID); err != nil {
		t.Fatalf("create read-only member: %v", err)
	}
	readOnly, _, _ := harness.jwt.IssueTenant(
		readerID, harness.tenantIDs["acme"], []string{string(auth.PermissionIntegrationRead)},
	)
	if denied := install("acme", readOnly); denied.Code != http.StatusForbidden {
		t.Errorf("install without integration:create status = %d", denied.Code)
	}
	invalidEvents := integrationRequest(handler, "acme", http.MethodPost,
		"/v1/integrations/slack/install", `{"events":[]}`, harness.ownerToken)
	if invalidEvents.Code != http.StatusUnprocessableEntity {
		t.Errorf("install without events status = %d", invalidEvents.Code)
	}

	// A complete install: the user never sees or supplies the webhook URL.
	token := beginInstall(t, "acme", harness.ownerToken)
	response := callback("?code=install-code&state=" + token)
	if response.Code != http.StatusFound {
		t.Fatalf("callback status = %d, body = %s", response.Code, response.Body.String())
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "http://acme.localhost/settings") ||
		!strings.Contains(location, "slack=installed") {
		t.Errorf("callback redirect = %q", location)
	}

	var storedID, storedURL, storedName, storedSource string
	var storedChannel, storedTeam *string
	var tokenCiphertext []byte
	var storedEvents, storedPrivacy []string
	if err := harness.root.QueryRow(ctx, `
		SELECT id, webhook_url, name, install_source, channel, slack_team_name,
		       slack_access_token_ciphertext, events::text[], privacy_fields
		FROM integrations WHERE tenant_id = $1 AND install_source = 'oauth'
	`, harness.tenantIDs["acme"]).Scan(&storedID, &storedURL, &storedName, &storedSource,
		&storedChannel, &storedTeam, &tokenCiphertext, &storedEvents, &storedPrivacy); err != nil {
		t.Fatalf("read installed integration: %v", err)
	}
	if storedURL != webhookURL || storedSource != string(InstallSourceOAuth) {
		t.Errorf("stored install = %q / %q", storedURL, storedSource)
	}
	if storedChannel == nil || *storedChannel != channel || storedName != channel {
		t.Errorf("stored channel = %v, name = %q", storedChannel, storedName)
	}
	if storedTeam == nil || *storedTeam != "Acme HQ" {
		t.Errorf("stored team = %v", storedTeam)
	}
	// The events chosen before leaving for Slack survive the round trip in server-side state.
	if len(storedEvents) != 2 || len(storedPrivacy) != 1 || storedPrivacy[0] != PrivacyFieldDescription {
		t.Errorf("stored events = %v, privacy = %v", storedEvents, storedPrivacy)
	}
	if bytesContainAny(tokenCiphertext, accessToken) {
		t.Error("slack bot token was stored in plaintext")
	}

	// Re-installing the same channel must update that destination, not add a second one
	// that would double-post every notification.
	secondToken := beginInstall(t, "acme", harness.ownerToken)
	if again := callback("?code=install-code&state=" + secondToken); again.Code != http.StatusFound {
		t.Fatalf("re-install status = %d", again.Code)
	}
	var installCount int
	if err := harness.root.QueryRow(ctx, `
		SELECT count(*) FROM integrations WHERE tenant_id = $1 AND install_source = 'oauth'
	`, harness.tenantIDs["acme"]).Scan(&installCount); err != nil {
		t.Fatalf("count installs: %v", err)
	}
	if installCount != 1 {
		t.Errorf("installs after re-install = %d, want 1", installCount)
	}

	// A second channel in the same workspace is a genuinely separate destination.
	channel, channelID = "#alerts", "C-ALERTS"
	thirdToken := beginInstall(t, "acme", harness.ownerToken)
	if other := callback("?code=install-code&state=" + thirdToken); other.Code != http.StatusFound {
		t.Fatalf("second channel install status = %d", other.Code)
	}
	if err := harness.root.QueryRow(ctx, `
		SELECT count(*) FROM integrations WHERE tenant_id = $1 AND install_source = 'oauth'
	`, harness.tenantIDs["acme"]).Scan(&installCount); err != nil {
		t.Fatalf("count installs: %v", err)
	}
	if installCount != 2 {
		t.Errorf("installs after second channel = %d, want 2", installCount)
	}

	// An install started by another workspace lands in that workspace only. The callback
	// derives the tenant from server-side state, so acme cannot be reached from beta's flow.
	channel, channelID = "#beta-feed", "C-BETA"
	betaToken := beginInstall(t, "beta", harness.betaToken)
	if betaCallback := callback("?code=install-code&state=" + betaToken); betaCallback.Code != http.StatusFound {
		t.Fatalf("beta install status = %d", betaCallback.Code)
	}
	var betaCount, acmeCount int
	if err := harness.root.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE tenant_id = $1), count(*) FILTER (WHERE tenant_id = $2)
		FROM integrations WHERE install_source = 'oauth'
	`, harness.tenantIDs["beta"], harness.tenantIDs["acme"]).Scan(&betaCount, &acmeCount); err != nil {
		t.Fatalf("count cross-tenant installs: %v", err)
	}
	if betaCount != 1 || acmeCount != 2 {
		t.Errorf("beta installs = %d, acme installs = %d; want 1 and 2", betaCount, acmeCount)
	}

	// Removing an OAuth install revokes the token with Slack and drops the row.
	removed := integrationRequest(handler, "acme", http.MethodDelete,
		"/v1/integrations/"+storedID, "", harness.ownerToken)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("delete install status = %d, body = %s", removed.Code, removed.Body.String())
	}
	var remaining int
	if err := harness.root.QueryRow(ctx, `
		SELECT count(*) FROM integrations WHERE tenant_id = $1 AND id = $2
	`, harness.tenantIDs["acme"], storedID).Scan(&remaining); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if remaining != 0 {
		t.Errorf("rows after delete = %d, want 0", remaining)
	}
	var auditKinds []string
	if err := harness.root.QueryRow(ctx, `
		SELECT array_agg(kind ORDER BY kind) FROM tenant_access_events
		WHERE tenant_id = $1 AND resource_type = $2
	`, harness.tenantIDs["acme"], AuditResourceType).Scan(&auditKinds); err != nil {
		t.Fatalf("read install audit: %v", err)
	}
	if !slices.Contains(auditKinds, AuditKindInstalled) || !slices.Contains(auditKinds, AuditKindUninstall) {
		t.Errorf("audit kinds = %v, want install and uninstall recorded", auditKinds)
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
	body := fmt.Sprintf(`{
		"kind":%q,"name":%q,"webhook_url":%q,"events":["feedback.created"],
		"privacy_fields":["description","page","rating","classification","workflow","attachments"]
	}`,
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
