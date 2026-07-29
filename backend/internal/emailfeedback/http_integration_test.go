package emailfeedback

import (
	"context"
	"encoding/json"
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
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
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
	var outboxData string
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
		SELECT count(*), COALESCE(max(data::text), '')
		FROM outbox_events
		WHERE resource_type = 'email_feedback_invitation' AND resource_id = $1
	`, invitation.ID).Scan(&outboxCount, &outboxData); err != nil {
		t.Fatalf("load invitation outbox: %v", err)
	}
	if outboxCount != 1 || outboxData != "{}" ||
		strings.Contains(outboxData, storedRecipient) ||
		strings.Contains(outboxData, rawInvitationToken) {
		t.Fatalf("invitation outbox count=%d data=%q", outboxCount, outboxData)
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
