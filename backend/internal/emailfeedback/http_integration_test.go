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
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{Store: Store{}, JWT: jwt})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin:  db.BeginFromPool(pools.App),
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

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
