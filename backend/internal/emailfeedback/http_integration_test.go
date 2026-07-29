package emailfeedback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
	"github.com/acidiney/app.fidibeki/backend/internal/issue"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestEmailFeedbackCampaignHTTPFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, emailCampaignMigrationsPath(t))
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
	appURL := emailCampaignRoleURL(t, directURL, "fidibeki_app", "app-password")
	adminURL := emailCampaignRoleURL(t, directURL, "fidibeki_admin", "admin-password")
	if err := db.ConfigureRuntimeRoles(ctx, directURL, appURL, adminURL); err != nil {
		t.Fatalf("configure runtime roles: %v", err)
	}

	var tenantID, projectID, ruleID, ownerID, readerID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains, default_locale)
		VALUES ($1, 'Storefront', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{shop.example.com}', 'en')
		RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds,
			definition_key, definition_status, presentation, response_blocks,
			localizations, enabled
		) VALUES (
			$1, 'Post-purchase', 'all_pages', 'delay', 0,
			'post-purchase', 'active', 'prompt',
			'[{"key":"rating","type":"rating","required":false}]'::jsonb,
			'{"en":{"launcher_label":"Feedback","prompt":"How was it?",
			 "comment_placeholder":"Tell us more","contact_label":"Email",
			 "consent_label":"Email me","submit_label":"Send",
			 "success_message":"Thank you"}}'::jsonb,
			true
		) RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create Widget Definition: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2) RETURNING id
	`, tenantID, auth.TenantPermissionStrings()).Scan(&ownerID); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'reader@acme.test', '{project:read}') RETURNING id
	`, tenantID).Scan(&readerID); err != nil {
		t.Fatalf("create reader: %v", err)
	}
	rawInvitationKey := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUV"
	limitedInvitationKey := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUW"
	var invitationKeyID string
	if err := root.QueryRow(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions
		) VALUES (
			$1, 'Email feedback sender', $2, 'fk_live_0123...',
			ARRAY['email_feedback:send']
		) RETURNING id
	`, tenantID, auth.HashToken(rawInvitationKey)).Scan(&invitationKeyID); err != nil {
		t.Fatalf("create invitation API key: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions
		) VALUES (
			$1, 'Email feedback reader', $2, 'fk_live_0123...',
			ARRAY['project:read']
		)
	`, tenantID, auth.HashToken(limitedInvitationKey)); err != nil {
		t.Fatalf("create limited invitation API key: %v", err)
	}
	var betaTenantID, betaOwnerID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Beta', 'beta', 'growth', 'active') RETURNING id
	`).Scan(&betaTenantID); err != nil {
		t.Fatalf("create beta tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@beta.test', $2) RETURNING id
	`, betaTenantID, auth.TenantPermissionStrings()).Scan(&betaOwnerID); err != nil {
		t.Fatalf("create beta owner: %v", err)
	}
	betaInvitationKey := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUX"
	if _, err := root.Exec(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions
		) VALUES (
			$1, 'Beta email feedback sender', $2, 'fk_live_0123...',
			ARRAY['email_feedback:send']
		)
	`, betaTenantID, auth.HashToken(betaInvitationKey)); err != nil {
		t.Fatalf("create beta invitation API key: %v", err)
	}

	pools, err := db.OpenPools(ctx, appURL, adminURL, 8, 4)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwt, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	ownerToken, _, _ := jwt.IssueTenant(ownerID, tenantID, auth.TenantPermissionStrings())
	readerToken, _, _ := jwt.IssueTenant(
		readerID, tenantID, []string{string(auth.PermissionProjectRead)},
	)
	betaToken, _, _ := jwt.IssueTenant(
		betaOwnerID, betaTenantID, auth.TenantPermissionStrings(),
	)
	invitationTokens, err := NewInvitationTokenManager(strings.Repeat("i", 32))
	if err != nil {
		t.Fatalf("create invitation token manager: %v", err)
	}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Store: Store{}, JWT: jwt, InvitationTokens: invitationTokens,
		Now: func() time.Time { return now },
	})
	handler := appmiddleware.TenantAPIKeyRateLimit(
		appmiddleware.TenantAPIKeyRateLimitOptions{
			Limiter: allowEmailFeedbackCredentialLimiter{},
		},
	)(appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin:  db.BeginFromPool(pools.App),
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes))

	body := campaignBody(projectID, ruleID, "draft", "pt")
	denied := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback-campaigns", body, readerToken,
	)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("reader create = %d body=%s", denied.Code, denied.Body.String())
	}
	created := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback-campaigns", body, ownerToken,
	)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", created.Code, created.Body.String())
	}
	var envelope struct {
		Campaign Campaign `json:"campaign"`
	}
	if err := json.NewDecoder(created.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	campaign := envelope.Campaign
	if campaign.Status != CampaignDraft || campaign.Readiness.Ready ||
		!containsReason(campaign.Readiness.Reasons, "controller_profile_incomplete") ||
		!containsReason(campaign.Readiness.Reasons, "localization_missing") {
		t.Fatalf("created campaign = %#v", campaign)
	}

	blocked := emailCampaignRequest(
		handler, "acme", http.MethodPatch,
		"/v1/email-feedback-campaigns/"+campaign.ID,
		campaignBody(projectID, ruleID, "active", "pt"), ownerToken,
	)
	if blocked.Code != http.StatusUnprocessableEntity {
		t.Fatalf("activate unready = %d body=%s", blocked.Code, blocked.Body.String())
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants
		SET privacy_contact_email = 'privacy@acme.test',
		    privacy_rights_email = 'rights@acme.test',
		    privacy_policy_url = 'https://shop.example.com/privacy',
		    privacy_profile_version = 1
		WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("complete controller profile: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET localizations = localizations || '{
			"pt":{"launcher_label":"Feedback","prompt":"Como foi?",
			 "comment_placeholder":"Conte-nos mais","contact_label":"Email",
			 "consent_label":"Enviar atualizações","submit_label":"Enviar",
			 "success_message":"Obrigado"}
		}'::jsonb
		WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("add campaign localization: %v", err)
	}
	activated := emailCampaignRequest(
		handler, "acme", http.MethodPatch,
		"/v1/email-feedback-campaigns/"+campaign.ID,
		campaignBody(projectID, ruleID, "active", "pt"), ownerToken,
	)
	if activated.Code != http.StatusOK {
		t.Fatalf("activate = %d body=%s", activated.Code, activated.Body.String())
	}
	envelope = struct {
		Campaign Campaign `json:"campaign"`
	}{}
	if err := json.NewDecoder(activated.Body).Decode(&envelope); err != nil ||
		envelope.Campaign.Status != CampaignActive ||
		!envelope.Campaign.Readiness.Ready {
		t.Fatalf("activated campaign = %#v error=%v", envelope.Campaign, err)
	}

	invitationRequest := invitationBody(
		campaign.ID, "order-123", "Customer@Example.com", "", "https://shop.example.com/orders/123",
	)
	humanInvitation := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationRequest, ownerToken,
	)
	if humanInvitation.Code != http.StatusForbidden {
		t.Fatalf("human invitation = %d body=%s",
			humanInvitation.Code, humanInvitation.Body.String())
	}
	limitedInvitation := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationRequest, limitedInvitationKey,
	)
	if limitedInvitation.Code != http.StatusForbidden {
		t.Fatalf("limited invitation = %d body=%s",
			limitedInvitation.Code, limitedInvitation.Body.String())
	}
	createdInvitation := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationRequest, rawInvitationKey,
	)
	if createdInvitation.Code != http.StatusCreated {
		t.Fatalf("create invitation = %d body=%s",
			createdInvitation.Code, createdInvitation.Body.String())
	}
	var invitationEnvelope struct {
		Invitation Invitation `json:"invitation"`
	}
	if err := json.NewDecoder(createdInvitation.Body).Decode(&invitationEnvelope); err != nil {
		t.Fatalf("decode invitation: %v", err)
	}
	invitation := invitationEnvelope.Invitation
	rawInvitationToken, expectedTokenHash, err := invitationTokens.Token(invitation.ID)
	if err != nil {
		t.Fatalf("derive invitation token: %v", err)
	}
	if invitation.Status != InvitationQueued || invitation.Locale != "pt" ||
		invitation.ExternalEventID != "order-123" ||
		!invitation.ExpiresAt.Equal(now.Add(30*24*time.Hour)) ||
		strings.Contains(createdInvitation.Body.String(), "customer@example.com") ||
		strings.Contains(createdInvitation.Body.String(), rawInvitationToken) ||
		strings.Contains(createdInvitation.Body.String(), "recipient_email") ||
		strings.Contains(createdInvitation.Body.String(), "token_hash") ||
		strings.Contains(createdInvitation.Body.String(), "request_digest") {
		t.Fatalf("unsafe or invalid invitation response = %#v body=%s",
			invitation, createdInvitation.Body.String())
	}
	var storedRecipient, storedDigest, storedTokenHash, storedRequestDigest string
	var storedStatus string
	var snapshotCount, invitationCount, outboxCount int
	var outboxID, outboxData string
	if err := root.QueryRow(ctx, `
		SELECT recipient_email::text, recipient_digest, token_hash, request_digest,
		       status::text,
		       (jsonb_typeof(campaign_snapshot) = 'object')::int
		         + (jsonb_typeof(definition_snapshot) = 'object')::int
		         + (jsonb_typeof(localization_snapshot) = 'object')::int
		         + (jsonb_typeof(controller_snapshot) = 'object')::int
		FROM email_feedback_invitations
		WHERE id = $1
	`, invitation.ID).Scan(
		&storedRecipient, &storedDigest, &storedTokenHash, &storedRequestDigest,
		&storedStatus, &snapshotCount,
	); err != nil {
		t.Fatalf("load invitation ledger: %v", err)
	}
	if storedRecipient != "customer@example.com" ||
		storedDigest != digestString(storedRecipient) ||
		storedTokenHash != expectedTokenHash ||
		storedRequestDigest == "" || storedRequestDigest == storedRecipient ||
		storedStatus != string(InvitationQueued) || snapshotCount != 4 {
		t.Fatalf(
			"invitation ledger recipient=%q digest=%q token=%q request=%q status=%q snapshots=%d",
			storedRecipient, storedDigest, storedTokenHash, storedRequestDigest,
			storedStatus, snapshotCount,
		)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(id::text), ''), COALESCE(max(data::text), '')
		FROM outbox_events
		WHERE resource_type = 'email_feedback_invitation' AND resource_id = $1
	`, invitation.ID).Scan(&outboxCount, &outboxID, &outboxData); err != nil {
		t.Fatalf("load invitation outbox: %v", err)
	}
	if outboxCount != 1 || outboxData != "{}" ||
		strings.Contains(outboxData, storedRecipient) ||
		strings.Contains(outboxData, rawInvitationToken) {
		t.Fatalf("invitation outbox count=%d data=%q", outboxCount, outboxData)
	}

	deliveryNow := now.Add(time.Minute)
	deliverySender := &invitationTestSender{
		providerMessageIDs: []string{"resend-invitation-123"},
	}
	deliveryHandler := InvitationDeliveryHandler{
		Store:            Store{},
		Begin:            db.BeginFromPool(pools.Admin),
		Sender:           deliverySender,
		Tokens:           invitationTokens,
		From:             "Fidibeki <feedback@fidibeki.test>",
		PublicBaseURL:    "https://api.fidibeki.test",
		WebPublicBaseURL: "https://fidibeki.test",
		BaseDomain:       "fidibeki.test",
		Now:              func() time.Time { return deliveryNow },
	}
	deliveryPayload, err := json.Marshal(DeliveryTask{
		OutboxEventID: outboxID,
		TenantID:      tenantID,
		ResourceID:    invitation.ID,
	})
	if err != nil {
		t.Fatalf("encode invitation delivery: %v", err)
	}
	deliveryTask := asynq.NewTask(TaskDeliverInvitation, deliveryPayload)
	if err := deliveryHandler.Handler()(ctx, deliveryTask); err != nil {
		t.Fatalf("deliver invitation: %v", err)
	}
	if len(deliverySender.messages) != 1 {
		t.Fatalf("delivery messages = %d", len(deliverySender.messages))
	}
	deliveredMessage := deliverySender.messages[0]
	for _, value := range []string{
		"1: https://acme.fidibeki.test/email-feedback/" + rawInvitationToken + "?rating=1",
		"5: https://acme.fidibeki.test/email-feedback/" + rawInvitationToken + "?rating=5",
		"Partilhar feedback: https://acme.fidibeki.test/email-feedback/" + rawInvitationToken,
		"Política de privacidade: https://shop.example.com/privacy",
		"Não receber mais estes pedidos: https://acme.fidibeki.test/email-feedback/" +
			rawInvitationToken + "/opt-out",
	} {
		if !strings.Contains(deliveredMessage.Text, value) {
			t.Errorf("delivered invitation missing %q:\n%s", value, deliveredMessage.Text)
		}
	}
	if deliveredMessage.To != storedRecipient ||
		deliveredMessage.IdempotencyKey != "email-feedback-invitation:"+invitation.ID {
		t.Fatalf("delivery envelope = %#v", deliveredMessage)
	}
	var deliveredRecipient *string
	var deliveredProviderID string
	var deliveredSentAt time.Time
	if err := root.QueryRow(ctx, `
		SELECT status::text, recipient_email::text, provider_message_id, sent_at
		FROM email_feedback_invitations
		WHERE id = $1
	`, invitation.ID).Scan(
		&storedStatus, &deliveredRecipient, &deliveredProviderID, &deliveredSentAt,
	); err != nil {
		t.Fatalf("load delivered invitation: %v", err)
	}
	if storedStatus != string(InvitationSent) || deliveredRecipient != nil ||
		deliveredProviderID != "resend-invitation-123" ||
		!deliveredSentAt.Equal(deliveryNow) {
		t.Fatalf(
			"delivered status=%q recipient=%v provider=%q sent_at=%s",
			storedStatus, deliveredRecipient, deliveredProviderID, deliveredSentAt,
		)
	}
	if err := deliveryHandler.Handler()(ctx, deliveryTask); err != nil {
		t.Fatalf("repeat invitation delivery: %v", err)
	}
	if len(deliverySender.messages) != 1 {
		t.Fatalf("repeat delivery messages = %d", len(deliverySender.messages))
	}

	publicRouter := chi.NewRouter()
	RegisterPublicRoutes(publicRouter, PublicHTTPOptions{
		Store: Store{}, Resolver: pools.Admin, Begin: db.BeginFromPool(pools.App),
		BaseDomain: "fidibeki.test", Now: func() time.Time { return deliveryNow.Add(time.Minute) },
		FeedbackCore: feedback.SubmissionCore{Store: feedback.Store{}},
	})
	collectionPath := "/v1/public/email-feedback/" + rawInvitationToken
	var feedbackBeforeCollection int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks WHERE tenant_id = $1
	`, tenantID).Scan(&feedbackBeforeCollection); err != nil {
		t.Fatalf("count feedback before collection GET: %v", err)
	}
	collectionView := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodGet, collectionPath+"?rating=1", "",
	)
	if collectionView.Code != http.StatusOK ||
		!strings.Contains(collectionView.Body.String(), `"locale":"pt"`) ||
		!strings.Contains(collectionView.Body.String(), `"prompt":"Como foi?"`) ||
		!strings.Contains(collectionView.Body.String(), `"type":"rating"`) ||
		!strings.Contains(collectionView.Body.String(), `"controller_name":"Acme"`) ||
		strings.Contains(collectionView.Body.String(), `"rating":1`) ||
		strings.Contains(collectionView.Body.String(), `"acknowledged_at"`) ||
		strings.Contains(collectionView.Body.String(), `"acknowledged_by_member_id"`) {
		t.Fatalf("load collection = %d body=%s",
			collectionView.Code, collectionView.Body.String())
	}
	wrongHostCollection := publicEmailFeedbackRequest(
		publicRouter, "beta", http.MethodGet, collectionPath, "",
	)
	if wrongHostCollection.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant collection = %d body=%s",
			wrongHostCollection.Code, wrongHostCollection.Body.String())
	}
	var feedbackAfterCollection int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks WHERE tenant_id = $1
	`, tenantID).Scan(&feedbackAfterCollection); err != nil {
		t.Fatalf("count feedback after collection GET: %v", err)
	}
	if feedbackAfterCollection != feedbackBeforeCollection {
		t.Fatalf("collection GET created feedback: before=%d after=%d",
			feedbackBeforeCollection, feedbackAfterCollection)
	}
	if err := root.QueryRow(ctx, `
		SELECT status::text FROM email_feedback_invitations WHERE id = $1
	`, invitation.ID).Scan(&storedStatus); err != nil ||
		storedStatus != string(InvitationSent) {
		t.Fatalf("collection GET status=%q error=%v", storedStatus, err)
	}
	submitted := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodPost, collectionPath,
		`{"responses":{"rating":4}}`,
	)
	if submitted.Code != http.StatusCreated ||
		!strings.Contains(submitted.Body.String(), `"feedback_public_id":"fb_`) {
		t.Fatalf("submit collection = %d body=%s",
			submitted.Code, submitted.Body.String())
	}
	var submittedFeedbackID, submittedPublicID, submittedSource string
	var submittedRating int
	var submittedIssueID, submittedReporter *string
	var submittedNotify bool
	if err := root.QueryRow(ctx, `
		SELECT feedback.id, feedback.public_id, feedback.source::text,
		       feedback.rating, feedback.issue_id, feedback.reporter_email::text,
		       feedback.notify_reporter
		FROM feedbacks AS feedback
		WHERE feedback.tenant_id = $1 AND feedback.client_feedback_id = $2
	`, tenantID, invitation.ID).Scan(
		&submittedFeedbackID, &submittedPublicID, &submittedSource,
		&submittedRating, &submittedIssueID, &submittedReporter, &submittedNotify,
	); err != nil {
		t.Fatalf("load submitted email feedback: %v", err)
	}
	if submittedSource != "email" || submittedRating != 4 ||
		submittedIssueID != nil || submittedReporter != nil || submittedNotify {
		t.Fatalf(
			"submitted email feedback source=%q rating=%d issue=%v reporter=%v notify=%t",
			submittedSource, submittedRating, submittedIssueID, submittedReporter, submittedNotify,
		)
	}
	var submittedAt time.Time
	if err := root.QueryRow(ctx, `
		SELECT status::text, feedback_id, submitted_at
		FROM email_feedback_invitations WHERE id = $1
	`, invitation.ID).Scan(
		&storedStatus, &submittedFeedbackID, &submittedAt,
	); err != nil || storedStatus != string(InvitationSubmitted) ||
		submittedFeedbackID == "" {
		t.Fatalf("submitted invitation status=%q feedback=%q at=%s error=%v",
			storedStatus, submittedFeedbackID, submittedAt, err)
	}
	repeatedSubmission := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodPost, collectionPath,
		`{"responses":{"rating":4}}`,
	)
	if repeatedSubmission.Code != http.StatusNotFound {
		t.Fatalf("repeat collection submission = %d body=%s",
			repeatedSubmission.Code, repeatedSubmission.Body.String())
	}
	unavailableAfterSubmission := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodGet, collectionPath+"?rating=5", "",
	)
	if unavailableAfterSubmission.Code != http.StatusNotFound {
		t.Fatalf("submitted collection GET = %d body=%s",
			unavailableAfterSubmission.Code, unavailableAfterSubmission.Body.String())
	}
	optOutPath := "/v1/public/email-feedback/" + rawInvitationToken + "/opt-out"
	optOutView := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodGet, optOutPath, "",
	)
	if optOutView.Code != http.StatusOK ||
		!strings.Contains(optOutView.Body.String(), `"controller_name":"Acme"`) ||
		!strings.Contains(optOutView.Body.String(), `"locale":"pt"`) ||
		!strings.Contains(optOutView.Body.String(), `"opted_out":false`) ||
		strings.Contains(optOutView.Body.String(), storedRecipient) {
		t.Fatalf("load opt-out = %d body=%s", optOutView.Code, optOutView.Body.String())
	}
	if optOutView.Header().Get("Cache-Control") != "no-store" ||
		optOutView.Header().Get("Referrer-Policy") != "no-referrer" ||
		!strings.Contains(optOutView.Header().Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("opt-out privacy headers = %#v", optOutView.Header())
	}
	wrongHostOptOut := publicEmailFeedbackRequest(
		publicRouter, "beta", http.MethodGet, optOutPath, "",
	)
	if wrongHostOptOut.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant opt-out = %d body=%s",
			wrongHostOptOut.Code, wrongHostOptOut.Body.String())
	}
	optedOut := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodPost, optOutPath, "{}",
	)
	if optedOut.Code != http.StatusOK ||
		!strings.Contains(optedOut.Body.String(), `"opted_out":true`) {
		t.Fatalf("submit opt-out = %d body=%s", optedOut.Code, optedOut.Body.String())
	}
	repeatedOptOut := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodPost, optOutPath, "",
	)
	if repeatedOptOut.Code != http.StatusOK {
		t.Fatalf("repeat opt-out = %d body=%s",
			repeatedOptOut.Code, repeatedOptOut.Body.String())
	}
	var suppressionReason string
	var retainedTokenHash *string
	if err := root.QueryRow(ctx, `
		SELECT invitation.status::text, invitation.token_hash,
		       suppression.reason::text
		FROM email_feedback_invitations AS invitation
		JOIN email_feedback_suppressions AS suppression
		  ON suppression.tenant_id = invitation.tenant_id
		 AND suppression.recipient_digest = invitation.recipient_digest
		WHERE invitation.id = $1
	`, invitation.ID).Scan(
		&storedStatus, &retainedTokenHash, &suppressionReason,
	); err != nil {
		t.Fatalf("load opted-out invitation: %v", err)
	}
	if storedStatus != string(InvitationSubmitted) ||
		retainedTokenHash == nil || *retainedTokenHash != expectedTokenHash ||
		suppressionReason != "opt_out" {
		t.Fatalf("opted-out status=%q token=%v reason=%q",
			storedStatus, retainedTokenHash, suppressionReason)
	}

	concurrentInvitationResponse := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(
			campaign.ID, "order-concurrent", "concurrent@example.com", "",
			"https://shop.example.com/orders/concurrent",
		), rawInvitationKey,
	)
	if concurrentInvitationResponse.Code != http.StatusCreated {
		t.Fatalf("create concurrent invitation = %d body=%s",
			concurrentInvitationResponse.Code, concurrentInvitationResponse.Body.String())
	}
	var concurrentEnvelope struct {
		Invitation Invitation `json:"invitation"`
	}
	if err := json.NewDecoder(concurrentInvitationResponse.Body).Decode(
		&concurrentEnvelope,
	); err != nil {
		t.Fatalf("decode concurrent invitation: %v", err)
	}
	concurrentInvitation := concurrentEnvelope.Invitation
	concurrentToken, _, err := invitationTokens.Token(concurrentInvitation.ID)
	if err != nil {
		t.Fatalf("derive concurrent invitation token: %v", err)
	}
	var concurrentOutboxID string
	if err := root.QueryRow(ctx, `
		SELECT id::text FROM outbox_events
		WHERE resource_type = 'email_feedback_invitation' AND resource_id = $1
	`, concurrentInvitation.ID).Scan(&concurrentOutboxID); err != nil {
		t.Fatalf("load concurrent invitation outbox: %v", err)
	}
	concurrentPayload, _ := json.Marshal(DeliveryTask{
		OutboxEventID: concurrentOutboxID,
		TenantID:      tenantID,
		ResourceID:    concurrentInvitation.ID,
	})
	deliverySender.providerMessageIDs = append(
		deliverySender.providerMessageIDs, "resend-invitation-concurrent",
	)
	if err := deliveryHandler.Handler()(
		ctx, asynq.NewTask(TaskDeliverInvitation, concurrentPayload),
	); err != nil {
		t.Fatalf("deliver concurrent invitation: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET response_blocks =
		    '[{"key":"changed","type":"free_text","required":true}]'::jsonb
		WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("change live definition after invitation: %v", err)
	}
	concurrentPath := "/v1/public/email-feedback/" + concurrentToken
	concurrentResponses := make(chan *httptest.ResponseRecorder, 2)
	var submitters sync.WaitGroup
	for range 2 {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			concurrentResponses <- publicEmailFeedbackRequest(
				publicRouter, "acme", http.MethodPost, concurrentPath,
				`{"responses":{"rating":5}}`,
			)
		}()
	}
	submitters.Wait()
	close(concurrentResponses)
	createdCount, unavailableCount := 0, 0
	for response := range concurrentResponses {
		switch response.Code {
		case http.StatusCreated:
			createdCount++
		case http.StatusNotFound:
			unavailableCount++
		default:
			t.Fatalf("concurrent collection POST = %d body=%s",
				response.Code, response.Body.String())
		}
	}
	if createdCount != 1 || unavailableCount != 1 {
		t.Fatalf("concurrent results created=%d unavailable=%d",
			createdCount, unavailableCount)
	}
	var concurrentFeedbackCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks
		WHERE tenant_id = $1 AND client_feedback_id = $2 AND source = 'email'
	`, tenantID, concurrentInvitation.ID).Scan(&concurrentFeedbackCount); err != nil ||
		concurrentFeedbackCount != 1 {
		t.Fatalf("concurrent feedback count=%d error=%v",
			concurrentFeedbackCount, err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET response_blocks =
		    '[{"key":"rating","type":"rating","required":false}]'::jsonb
		WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("restore live definition after concurrent submission: %v", err)
	}

	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET collect_contact = true,
		    allow_comment = true,
		    response_blocks =
		        '[{"key":"detail","type":"free_text","required":true}]'::jsonb,
		    localizations = jsonb_set(
		        localizations,
		        '{pt}',
		        '{
		            "launcher_label":"Feedback",
		            "prompt":"Como foi a sua experiência?",
		            "comment_placeholder":"Mais algum contexto?",
		            "contact_label":"Email",
		            "consent_label":"Enviar-me atualizações",
		            "submit_label":"Enviar",
		            "success_message":"Obrigado",
		            "privacy_purpose_summary":"Enviar atualizações sobre este feedback.",
		            "privacy_lawful_basis_summary":"Consentimento.",
		            "privacy_retention_summary":"Até retirar o consentimento.",
		            "block_labels":{"detail":"O que aconteceu?"}
		        }'::jsonb
		    )
		WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("configure contact definition: %v", err)
	}
	contactInvitationResponse := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(
			campaign.ID, "order-contact", "invited@example.com", "pt",
			"https://shop.example.com/orders/contact",
		), rawInvitationKey,
	)
	if contactInvitationResponse.Code != http.StatusCreated {
		t.Fatalf("create contact invitation = %d body=%s",
			contactInvitationResponse.Code, contactInvitationResponse.Body.String())
	}
	var contactEnvelope struct {
		Invitation Invitation `json:"invitation"`
	}
	if err := json.NewDecoder(contactInvitationResponse.Body).Decode(&contactEnvelope); err != nil {
		t.Fatalf("decode contact invitation: %v", err)
	}
	contactInvitation := contactEnvelope.Invitation
	contactToken, _, err := invitationTokens.Token(contactInvitation.ID)
	if err != nil {
		t.Fatalf("derive contact invitation token: %v", err)
	}
	var contactOutboxID string
	if err := root.QueryRow(ctx, `
		SELECT id::text FROM outbox_events
		WHERE resource_type = 'email_feedback_invitation' AND resource_id = $1
	`, contactInvitation.ID).Scan(&contactOutboxID); err != nil {
		t.Fatalf("load contact invitation outbox: %v", err)
	}
	contactPayload, _ := json.Marshal(DeliveryTask{
		OutboxEventID: contactOutboxID,
		TenantID:      tenantID,
		ResourceID:    contactInvitation.ID,
	})
	deliverySender.providerMessageIDs = append(
		deliverySender.providerMessageIDs, "resend-invitation-contact",
	)
	if err := deliveryHandler.Handler()(
		ctx, asynq.NewTask(TaskDeliverInvitation, contactPayload),
	); err != nil {
		t.Fatalf("deliver contact invitation: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET collect_contact = false,
		    allow_comment = false,
		    response_blocks =
		        '[{"key":"rating","type":"rating","required":false}]'::jsonb
		WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("change live definition after contact invitation: %v", err)
	}
	contactPath := "/v1/public/email-feedback/" + contactToken
	contactViewResponse := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodGet, contactPath, "",
	)
	var contactView struct {
		Collection InvitationCollectionView `json:"collection"`
	}
	if contactViewResponse.Code != http.StatusOK ||
		json.NewDecoder(contactViewResponse.Body).Decode(&contactView) != nil ||
		!contactView.Collection.CollectContact ||
		len(contactView.Collection.ResponseBlocks) != 1 ||
		contactView.Collection.ResponseBlocks[0].Key != "detail" {
		t.Fatalf("captured contact view = %d %#v body=%s",
			contactViewResponse.Code, contactView, contactViewResponse.Body.String())
	}
	contactSubmissionBody, _ := json.Marshal(map[string]any{
		"responses": map[string]any{
			"detail": "I need the invoice download to be easier to find.",
		},
		"comment":         "The rest of checkout worked well.",
		"reporter_email":  "REPORTER@example.com",
		"notify_reporter": true,
		"consent": map[string]any{
			"purpose":                    feedback.ReporterConsentPurposeFeedbackUpdates,
			"notice_version":             feedback.ReporterConsentNoticeVersion,
			"controller_profile_version": contactView.Collection.ControllerProfileVersion,
			"widget_definition_version":  contactView.Collection.DefinitionVersion,
			"locale":                     "pt",
			"granted":                    true,
		},
	})
	contactSubmission := publicEmailFeedbackRequest(
		publicRouter, "acme", http.MethodPost, contactPath, string(contactSubmissionBody),
	)
	if contactSubmission.Code != http.StatusCreated {
		t.Fatalf("submit contact collection = %d body=%s",
			contactSubmission.Code, contactSubmission.Body.String())
	}
	var contactFeedbackID, contactSource, contactText, contactComment string
	var contactReporter, consentSource, consentSnapshot string
	var contactNotify bool
	var contactIssueID *string
	if err := root.QueryRow(ctx, `
		SELECT id, source::text, text, comment, reporter_email::text,
		       notify_reporter, reporter_consent_source,
		       reporter_consent_notice_snapshot::text, issue_id
		FROM feedbacks
		WHERE tenant_id = $1 AND client_feedback_id = $2
	`, tenantID, contactInvitation.ID).Scan(
		&contactFeedbackID, &contactSource, &contactText, &contactComment,
		&contactReporter, &contactNotify, &consentSource, &consentSnapshot,
		&contactIssueID,
	); err != nil {
		t.Fatalf("load contact email feedback: %v", err)
	}
	if contactSource != "email" ||
		contactText != "I need the invoice download to be easier to find." ||
		contactComment != "The rest of checkout worked well." ||
		contactReporter != "reporter@example.com" || !contactNotify ||
		consentSource != feedback.ReporterConsentSourceEmail ||
		!strings.Contains(consentSnapshot, `"controller_name": "Acme"`) ||
		contactIssueID != nil {
		t.Fatalf(
			"contact feedback source=%q text=%q comment=%q reporter=%q notify=%t consent=%q snapshot=%s issue=%v",
			contactSource, contactText, contactComment, contactReporter, contactNotify,
			consentSource, consentSnapshot, contactIssueID,
		)
	}
	var contactOutboxCount, contactTestimonialCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE resource_type = 'feedback' AND resource_id = $1
		  AND event = 'feedback.created'
	`, contactFeedbackID).Scan(&contactOutboxCount); err != nil {
		t.Fatalf("load contact feedback outbox: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM testimonials WHERE source_feedback_id = $1
	`, contactFeedbackID).Scan(&contactTestimonialCount); err != nil {
		t.Fatalf("load contact feedback testimonials: %v", err)
	}
	if contactOutboxCount != 1 || contactTestimonialCount != 0 {
		t.Fatalf("contact outbox=%d testimonials=%d",
			contactOutboxCount, contactTestimonialCount)
	}

	retriedInvitation := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationRequest, rawInvitationKey,
	)
	if retriedInvitation.Code != http.StatusOK ||
		!strings.Contains(retriedInvitation.Body.String(), invitation.ID) {
		t.Fatalf("retry invitation = %d body=%s",
			retriedInvitation.Code, retriedInvitation.Body.String())
	}
	conflictingInvitation := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(
			campaign.ID, "order-123", "other@example.com", "",
			"https://shop.example.com/orders/123",
		), rawInvitationKey,
	)
	if conflictingInvitation.Code != http.StatusConflict {
		t.Fatalf("conflicting invitation = %d body=%s",
			conflictingInvitation.Code, conflictingInvitation.Body.String())
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM email_feedback_invitations
		WHERE tenant_id = $1 AND campaign_id = $2 AND external_event_id = 'order-123'
	`, tenantID, campaign.ID).Scan(&invitationCount); err != nil || invitationCount != 1 {
		t.Fatalf("idempotent invitation count=%d error=%v", invitationCount, err)
	}

	if _, err := root.Exec(ctx, `
		UPDATE email_feedback_campaigns SET status = 'paused' WHERE id = $1
	`, campaign.ID); err != nil {
		t.Fatalf("pause campaign for invitation: %v", err)
	}
	inactiveInvitation := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(campaign.ID, "order-124", "next@example.com", "", ""),
		rawInvitationKey,
	)
	if inactiveInvitation.Code != http.StatusUnprocessableEntity {
		t.Fatalf("inactive invitation = %d body=%s",
			inactiveInvitation.Code, inactiveInvitation.Body.String())
	}
	retryWhilePaused := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationRequest, rawInvitationKey,
	)
	if retryWhilePaused.Code != http.StatusOK ||
		!strings.Contains(retryWhilePaused.Body.String(), invitation.ID) {
		t.Fatalf("paused exact retry = %d body=%s",
			retryWhilePaused.Code, retryWhilePaused.Body.String())
	}
	if _, err := root.Exec(ctx, `
		UPDATE email_feedback_campaigns SET status = 'active' WHERE id = $1
	`, campaign.ID); err != nil {
		t.Fatalf("reactivate campaign for invitation: %v", err)
	}
	unsupportedLocale := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(campaign.ID, "order-125", "locale@example.com", "fr", ""),
		rawInvitationKey,
	)
	if unsupportedLocale.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported locale = %d body=%s",
			unsupportedLocale.Code, unsupportedLocale.Body.String())
	}
	crossTenantInvitation := emailCampaignRequest(
		handler, "beta", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(campaign.ID, "order-126", "beta@example.com", "", ""),
		betaInvitationKey,
	)
	if crossTenantInvitation.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant invitation = %d body=%s",
			crossTenantInvitation.Code, crossTenantInvitation.Body.String())
	}

	retryResponse := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(campaign.ID, "order-retry", "retry@example.com", "", ""),
		rawInvitationKey,
	)
	if retryResponse.Code != http.StatusCreated {
		t.Fatalf("create retry invitation = %d body=%s",
			retryResponse.Code, retryResponse.Body.String())
	}
	var retryEnvelope struct {
		Invitation Invitation `json:"invitation"`
	}
	if err := json.NewDecoder(retryResponse.Body).Decode(&retryEnvelope); err != nil {
		t.Fatalf("decode retry invitation: %v", err)
	}
	var retryOutboxID string
	if err := root.QueryRow(ctx, `
		SELECT id::text FROM outbox_events
		WHERE resource_type = 'email_feedback_invitation' AND resource_id = $1
	`, retryEnvelope.Invitation.ID).Scan(&retryOutboxID); err != nil {
		t.Fatalf("load retry invitation outbox: %v", err)
	}
	retryPayload, _ := json.Marshal(DeliveryTask{
		OutboxEventID: retryOutboxID,
		TenantID:      tenantID,
		ResourceID:    retryEnvelope.Invitation.ID,
	})
	retryTask := asynq.NewTask(TaskDeliverInvitation, retryPayload)
	retrySender := &retryingInvitationTestSender{}
	retryHandler := deliveryHandler
	retryHandler.Sender = retrySender
	if err := retryHandler.Handler()(ctx, retryTask); err == nil {
		t.Fatal("transient invitation delivery error = nil")
	}
	var retryRecipient, retryLastError *string
	if err := root.QueryRow(ctx, `
		SELECT status::text, recipient_email::text, last_error
		FROM email_feedback_invitations WHERE id = $1
	`, retryEnvelope.Invitation.ID).Scan(
		&storedStatus, &retryRecipient, &retryLastError,
	); err != nil {
		t.Fatalf("load transient invitation failure: %v", err)
	}
	if storedStatus != string(InvitationQueued) || retryRecipient == nil ||
		retryLastError == nil || !strings.Contains(*retryLastError, "transient") {
		t.Fatalf("transient status=%q recipient=%v error=%v",
			storedStatus, retryRecipient, retryLastError)
	}
	if err := retryHandler.Handler()(ctx, retryTask); err != nil {
		t.Fatalf("retry invitation delivery: %v", err)
	}
	if len(retrySender.messages) != 2 ||
		retrySender.messages[0].IdempotencyKey != retrySender.messages[1].IdempotencyKey {
		t.Fatalf("retry delivery messages = %#v", retrySender.messages)
	}
	if err := root.QueryRow(ctx, `
		SELECT status::text, recipient_email::text, provider_message_id, last_error
		FROM email_feedback_invitations WHERE id = $1
	`, retryEnvelope.Invitation.ID).Scan(
		&storedStatus, &retryRecipient, &deliveredProviderID, &retryLastError,
	); err != nil {
		t.Fatalf("load retried invitation: %v", err)
	}
	if storedStatus != string(InvitationSent) || retryRecipient != nil ||
		deliveredProviderID != "resend-invitation-retry" || retryLastError != nil {
		t.Fatalf("retried status=%q recipient=%v provider=%q error=%v",
			storedStatus, retryRecipient, deliveredProviderID, retryLastError)
	}

	raceEmail := "race@example.com"
	raceResponse := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(campaign.ID, "order-race", raceEmail, "", ""),
		rawInvitationKey,
	)
	if raceResponse.Code != http.StatusCreated {
		t.Fatalf("create suppression-race invitation = %d body=%s",
			raceResponse.Code, raceResponse.Body.String())
	}
	var raceEnvelope struct {
		Invitation Invitation `json:"invitation"`
	}
	if err := json.NewDecoder(raceResponse.Body).Decode(&raceEnvelope); err != nil {
		t.Fatalf("decode suppression-race invitation: %v", err)
	}
	var raceOutboxID string
	if err := root.QueryRow(ctx, `
		SELECT id::text FROM outbox_events
		WHERE resource_type = 'email_feedback_invitation' AND resource_id = $1
	`, raceEnvelope.Invitation.ID).Scan(&raceOutboxID); err != nil {
		t.Fatalf("load suppression-race outbox: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO email_feedback_suppressions (
			tenant_id, recipient_digest, reason,
			first_suppressed_at, last_suppressed_at
		) VALUES ($1, $2, 'opt_out', $3, $3)
	`, tenantID, digestString(raceEmail), deliveryNow); err != nil {
		t.Fatalf("create suppression during delivery delay: %v", err)
	}
	racePayload, _ := json.Marshal(DeliveryTask{
		OutboxEventID: raceOutboxID,
		TenantID:      tenantID,
		ResourceID:    raceEnvelope.Invitation.ID,
	})
	deliveredCount := len(deliverySender.messages)
	if err := deliveryHandler.Handler()(
		ctx, asynq.NewTask(TaskDeliverInvitation, racePayload),
	); err != nil {
		t.Fatalf("suppress delayed invitation: %v", err)
	}
	var raceRecipient, raceToken *string
	if err := root.QueryRow(ctx, `
		SELECT status::text, recipient_email::text, token_hash
		FROM email_feedback_invitations WHERE id = $1
	`, raceEnvelope.Invitation.ID).Scan(
		&storedStatus, &raceRecipient, &raceToken,
	); err != nil {
		t.Fatalf("load delayed suppressed invitation: %v", err)
	}
	if len(deliverySender.messages) != deliveredCount ||
		storedStatus != string(InvitationSuppressed) ||
		raceRecipient != nil || raceToken != nil {
		t.Fatalf("delayed suppression messages=%d status=%q recipient=%v token=%v",
			len(deliverySender.messages), storedStatus, raceRecipient, raceToken)
	}

	complaintRequest := invitationBody(
		campaign.ID, "order-complaint", "complaint@example.com", "", "",
	)
	complaintResponse := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		complaintRequest, rawInvitationKey,
	)
	if complaintResponse.Code != http.StatusCreated {
		t.Fatalf("create complaint invitation = %d body=%s",
			complaintResponse.Code, complaintResponse.Body.String())
	}
	var complaintEnvelope struct {
		Invitation Invitation `json:"invitation"`
	}
	if err := json.NewDecoder(complaintResponse.Body).Decode(&complaintEnvelope); err != nil {
		t.Fatalf("decode complaint invitation: %v", err)
	}
	var complaintOutboxID string
	if err := root.QueryRow(ctx, `
		SELECT id::text FROM outbox_events
		WHERE resource_type = 'email_feedback_invitation' AND resource_id = $1
	`, complaintEnvelope.Invitation.ID).Scan(&complaintOutboxID); err != nil {
		t.Fatalf("load complaint invitation outbox: %v", err)
	}
	complaintPayload, _ := json.Marshal(DeliveryTask{
		OutboxEventID: complaintOutboxID,
		TenantID:      tenantID,
		ResourceID:    complaintEnvelope.Invitation.ID,
	})
	deliverySender.providerMessageIDs = append(
		deliverySender.providerMessageIDs, "resend-invitation-complaint",
	)
	if err := deliveryHandler.Handler()(
		ctx, asynq.NewTask(TaskDeliverInvitation, complaintPayload),
	); err != nil {
		t.Fatalf("deliver complaint invitation: %v", err)
	}
	resendNow := deliveryNow.Add(2 * time.Minute)
	webhookRouter := chi.NewRouter()
	issue.RegisterPublicRoutes(webhookRouter, issue.PublicHTTPOptions{
		Store:    issue.Store{Database: pools.Admin},
		Verifier: acceptingResendVerifier{},
		AdditionalResendEventAppliers: []issue.ResendEventApplier{
			ResendEventApplier{
				Database: pools.Admin,
				Now:      func() time.Time { return resendNow },
			},
		},
	})
	complaintWebhook := publicEmailFeedbackRequest(
		webhookRouter, "api", http.MethodPost, "/v1/webhooks/resend",
		`{"type":"email.complained","data":{"email_id":"resend-invitation-complaint",`+
			`"to":["spoofed@example.com"]}}`,
	)
	complaintWebhook.Header().Set("svix-id", "event-complaint")
	if complaintWebhook.Code != http.StatusOK {
		t.Fatalf("complaint webhook = %d body=%s",
			complaintWebhook.Code, complaintWebhook.Body.String())
	}
	var complainedAt *time.Time
	var complaintWebhookID *string
	var complaintRecipient, complaintToken *string
	var complaintSuppressedAt time.Time
	if err := root.QueryRow(ctx, `
		SELECT invitation.status::text, invitation.recipient_email::text,
		       invitation.token_hash, invitation.complained_at,
		       invitation.webhook_event_id, suppression.reason::text,
		       suppression.last_suppressed_at
		FROM email_feedback_invitations AS invitation
		JOIN email_feedback_suppressions AS suppression
		  ON suppression.tenant_id = invitation.tenant_id
		 AND suppression.recipient_digest = invitation.recipient_digest
		WHERE invitation.id = $1
	`, complaintEnvelope.Invitation.ID).Scan(
		&storedStatus, &complaintRecipient, &complaintToken, &complainedAt,
		&complaintWebhookID, &suppressionReason, &complaintSuppressedAt,
	); err != nil {
		t.Fatalf("load complained invitation: %v", err)
	}
	if storedStatus != string(InvitationComplained) ||
		complaintRecipient != nil || complaintToken != nil || complainedAt == nil ||
		complaintWebhookID == nil || *complaintWebhookID != "event-complaint" ||
		suppressionReason != "complaint" {
		t.Fatalf(
			"complained status=%q recipient=%v token=%v at=%v webhook=%v reason=%q",
			storedStatus, complaintRecipient, complaintToken, complainedAt,
			complaintWebhookID, suppressionReason,
		)
	}
	resendNow = resendNow.Add(10 * time.Minute)
	repeatedComplaintWebhook := publicEmailFeedbackRequest(
		webhookRouter, "api", http.MethodPost, "/v1/webhooks/resend",
		`{"type":"email.complained","data":{"email_id":"resend-invitation-complaint",`+
			`"to":["different-spoof@example.com"]}}`,
	)
	if repeatedComplaintWebhook.Code != http.StatusOK {
		t.Fatalf("repeat complaint webhook = %d body=%s",
			repeatedComplaintWebhook.Code, repeatedComplaintWebhook.Body.String())
	}
	var repeatedSuppressedAt time.Time
	if err := root.QueryRow(ctx, `
		SELECT last_suppressed_at
		FROM email_feedback_suppressions
		WHERE tenant_id = $1 AND recipient_digest = $2
	`, tenantID, digestString("complaint@example.com")).Scan(
		&repeatedSuppressedAt,
	); err != nil {
		t.Fatalf("load repeated complaint suppression: %v", err)
	}
	if !repeatedSuppressedAt.Equal(complaintSuppressedAt) {
		t.Fatalf("repeat complaint changed suppression timestamp from %s to %s",
			complaintSuppressedAt, repeatedSuppressedAt)
	}
	complaintSuppressed := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(
			campaign.ID, "order-after-complaint", "complaint@example.com", "", "",
		), rawInvitationKey,
	)
	if complaintSuppressed.Code != http.StatusCreated ||
		!strings.Contains(complaintSuppressed.Body.String(), `"status":"suppressed"`) {
		t.Fatalf("complaint suppression = %d body=%s",
			complaintSuppressed.Code, complaintSuppressed.Body.String())
	}

	suppressedEmail := "blocked@example.com"
	if _, err := root.Exec(ctx, `
		INSERT INTO email_feedback_suppressions (
			tenant_id, recipient_digest, reason,
			first_suppressed_at, last_suppressed_at
		) VALUES ($1, $2, 'opt_out', $3, $3)
	`, tenantID, digestString(suppressedEmail), now); err != nil {
		t.Fatalf("create email suppression: %v", err)
	}
	suppressedInvitation := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(campaign.ID, "order-127", suppressedEmail, "", ""),
		rawInvitationKey,
	)
	if suppressedInvitation.Code != http.StatusCreated ||
		!strings.Contains(suppressedInvitation.Body.String(), `"status":"suppressed"`) ||
		strings.Contains(suppressedInvitation.Body.String(), suppressedEmail) {
		t.Fatalf("suppressed invitation = %d body=%s",
			suppressedInvitation.Code, suppressedInvitation.Body.String())
	}
	var suppressedRecipient, suppressedToken *string
	var suppressedOutboxCount int
	if err := root.QueryRow(ctx, `
		SELECT recipient_email::text, token_hash
		FROM email_feedback_invitations
		WHERE tenant_id = $1 AND campaign_id = $2 AND external_event_id = 'order-127'
	`, tenantID, campaign.ID).Scan(&suppressedRecipient, &suppressedToken); err != nil {
		t.Fatalf("load suppressed invitation: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE resource_type = 'email_feedback_invitation'
		  AND resource_id = (
		      SELECT id FROM email_feedback_invitations
		      WHERE tenant_id = $1 AND campaign_id = $2
		        AND external_event_id = 'order-127'
		  )
	`, tenantID, campaign.ID).Scan(&suppressedOutboxCount); err != nil {
		t.Fatalf("load suppressed invitation outbox: %v", err)
	}
	if suppressedRecipient != nil || suppressedToken != nil || suppressedOutboxCount != 0 {
		t.Fatalf("suppressed recipient=%v token=%v outbox=%d",
			suppressedRecipient, suppressedToken, suppressedOutboxCount)
	}

	if _, err := root.Exec(ctx, `
		CREATE FUNCTION reject_email_feedback_outbox() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.event::text = 'email_feedback.invitation_created' THEN
				RAISE EXCEPTION 'forced email feedback outbox failure';
			END IF;
			RETURN NEW;
		END
		$$;
		CREATE TRIGGER reject_email_feedback_outbox
		BEFORE INSERT ON outbox_events
		FOR EACH ROW EXECUTE FUNCTION reject_email_feedback_outbox()
	`); err != nil {
		t.Fatalf("install outbox failure trigger: %v", err)
	}
	atomicFailure := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback/invitations",
		invitationBody(campaign.ID, "order-atomic", "atomic@example.com", "", ""),
		rawInvitationKey,
	)
	if atomicFailure.Code != http.StatusServiceUnavailable {
		t.Fatalf("atomic failure invitation = %d body=%s",
			atomicFailure.Code, atomicFailure.Body.String())
	}
	if _, err := root.Exec(ctx, `
		DROP TRIGGER reject_email_feedback_outbox ON outbox_events;
		DROP FUNCTION reject_email_feedback_outbox()
	`); err != nil {
		t.Fatalf("remove outbox failure trigger: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM email_feedback_invitations
		WHERE tenant_id = $1 AND campaign_id = $2
		  AND external_event_id = 'order-atomic'
	`, tenantID, campaign.ID).Scan(&invitationCount); err != nil || invitationCount != 0 {
		t.Fatalf("atomic rollback invitation count=%d error=%v", invitationCount, err)
	}

	listed := emailCampaignRequest(
		handler, "acme", http.MethodGet,
		"/v1/email-feedback-campaigns?project_id="+projectID, "", readerToken,
	)
	var listEnvelope struct {
		Campaigns []Campaign `json:"campaigns"`
	}
	if listed.Code != http.StatusOK ||
		json.NewDecoder(listed.Body).Decode(&listEnvelope) != nil ||
		len(listEnvelope.Campaigns) != 1 {
		t.Fatalf("list = %d %#v body=%s", listed.Code, listEnvelope, listed.Body.String())
	}
	crossTenant := emailCampaignRequest(
		handler, "beta", http.MethodPatch,
		"/v1/email-feedback-campaigns/"+campaign.ID,
		campaignBody(projectID, ruleID, "paused", "pt"), betaToken,
	)
	if crossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant update = %d body=%s", crossTenant.Code, crossTenant.Body.String())
	}
	duplicate := emailCampaignRequest(
		handler, "acme", http.MethodPost, "/v1/email-feedback-campaigns",
		campaignBody(projectID, ruleID, "draft", "pt"), ownerToken,
	)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate key = %d body=%s", duplicate.Code, duplicate.Body.String())
	}

	for _, status := range []string{"paused", "active", "archived"} {
		response := emailCampaignRequest(
			handler, "acme", http.MethodPatch,
			"/v1/email-feedback-campaigns/"+campaign.ID,
			campaignBody(projectID, ruleID, status, "pt"), ownerToken,
		)
		if response.Code != http.StatusOK {
			t.Fatalf("transition to %s = %d body=%s",
				status, response.Code, response.Body.String())
		}
	}
	reopen := emailCampaignRequest(
		handler, "acme", http.MethodPatch,
		"/v1/email-feedback-campaigns/"+campaign.ID,
		campaignBody(projectID, ruleID, "active", "pt"), ownerToken,
	)
	if reopen.Code != http.StatusConflict {
		t.Fatalf("reopen archived = %d body=%s", reopen.Code, reopen.Body.String())
	}
	if _, err := root.Exec(ctx, `DELETE FROM widget_rules WHERE id = $1`, ruleID); err == nil {
		t.Fatal("historical campaign allowed its Widget Definition to be deleted")
	}
}

func campaignBody(projectID, ruleID, status, locale string) string {
	return `{
		"project_id":` + quote(projectID) + `,
		"widget_rule_id":` + quote(ruleID) + `,
		"campaign_key":"purchase-followup",
		"name":"Purchase follow-up",
		"status":` + quote(status) + `,
		"default_locale":` + quote(locale) + `,
		"subject":"How was your experience?",
		"introduction":"Tell us how the purchase went.",
		"invitation_ttl_days":30
	}`
}

func invitationBody(
	campaignID string,
	externalEventID string,
	recipientEmail string,
	locale string,
	contextURL string,
) string {
	payload := map[string]any{
		"campaign_id":       campaignID,
		"external_event_id": externalEventID,
		"recipient_email":   recipientEmail,
	}
	if locale != "" {
		payload["locale"] = locale
	}
	if contextURL != "" {
		payload["context_url"] = contextURL
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func emailCampaignRequest(
	handler http.Handler,
	slug string,
	method string,
	path string,
	body string,
	token string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://"+slug+".localhost"+path, strings.NewReader(body))
	request.Host = slug + ".localhost"
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func publicEmailFeedbackRequest(
	handler http.Handler,
	slug string,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(
		method, "https://"+slug+".fidibeki.test"+path, strings.NewReader(body),
	)
	request.Host = slug + ".fidibeki.test"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.Contains(path, "/v1/webhooks/resend") {
		request.Header.Set("svix-id", "event-complaint")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type invitationTestSender struct {
	messages           []email.Message
	providerMessageIDs []string
}

func (sender *invitationTestSender) Send(
	_ context.Context,
	message email.Message,
) (string, error) {
	sender.messages = append(sender.messages, message)
	index := len(sender.messages) - 1
	if index >= len(sender.providerMessageIDs) {
		return "", io.ErrUnexpectedEOF
	}
	return sender.providerMessageIDs[index], nil
}

type acceptingResendVerifier struct{}

func (acceptingResendVerifier) Verify([]byte, http.Header) error {
	return nil
}

type retryingInvitationTestSender struct {
	messages []email.Message
}

func (sender *retryingInvitationTestSender) Send(
	_ context.Context,
	message email.Message,
) (string, error) {
	sender.messages = append(sender.messages, message)
	if len(sender.messages) == 1 {
		return "", errors.New("transient email provider failure")
	}
	return "resend-invitation-retry", nil
}

func containsReason(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func emailCampaignMigrationsPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "migrations")
}

func emailCampaignRoleURL(
	t *testing.T,
	raw string,
	role string,
	password string,
) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.User = url.UserPassword(role, password)
	return parsed.String()
}

type allowEmailFeedbackCredentialLimiter struct{}

func (allowEmailFeedbackCredentialLimiter) Allow(
	_ context.Context,
	_ string,
	window ratelimit.Window,
) (ratelimit.Decision, error) {
	return ratelimit.Decision{
		Allowed: true, Limit: window.Limit, Remaining: window.Limit - 1,
	}, nil
}
