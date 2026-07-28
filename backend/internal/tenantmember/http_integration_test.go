package tenantmember

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestTenantAuthenticationInvitationAndPermissionsHTTPFlow(t *testing.T) {
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
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, tenantMigrationsPath(t))
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
	var tenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	var ownerMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2)
		RETURNING id
	`, tenantID, auth.TenantPermissionStrings()).Scan(&ownerMemberID); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := root.Exec(ctx, `UPDATE tenants SET owner_member_id = $2 WHERE id = $1`, tenantID, ownerMemberID); err != nil {
		t.Fatalf("assign owner: %v", err)
	}

	pools, err := db.OpenPools(
		ctx,
		tenantRoleURL(t, directURL, "fidibeki_app", "app-password"),
		directURL,
		4,
		2,
	)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	queue := &messageCapture{}
	now := time.Now().UTC().Truncate(time.Second)
	store := Store{
		RefreshTTL: 30 * 24 * time.Hour, RefreshRetryGrace: 10 * time.Second,
		Now: func() time.Time { return now },
	}
	service := Service{
		Store: store, JWT: jwtManager, PublicBaseURL: "http://api.localhost:8088",
		BaseDomain: "localhost", AuthFrom: "Fidibeki <auth@fidibeki.com>", Now: func() time.Time { return now },
	}
	var middlewareLogs bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&middlewareLogs, nil))
	probe, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin auth probe: %v", err)
	}
	var configuredTenant string
	if err := probe.QueryRow(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID).Scan(&configuredTenant); err != nil {
		t.Fatalf("set auth probe tenant: %v", err)
	}
	if _, err := service.RequestMagicLink(ctx, probe, tenant.Tenant{ID: tenantID, Slug: "acme", Name: "Acme"}, "owner@acme.test"); err != nil {
		t.Fatalf("direct RequestMagicLink() error = %v", err)
	}
	_ = probe.Rollback(ctx)
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Service: service, Store: store, JWT: jwtManager, Email: queue,
		RefreshTTL: 30 * 24 * time.Hour, Logger: testLogger,
	})
	transactionalHandler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: testLogger,
	})(routes)
	handler := appmiddleware.TenantAPIKeyRateLimit(
		appmiddleware.TenantAPIKeyRateLimitOptions{
			Limiter: &countingLimiter{limit: 100_000},
		},
	)(transactionalHandler)

	login := performTenantRequest(handler, http.MethodPost, "/auth/login", `{"email":"owner@acme.test"}`, "", nil)
	if login.Code != http.StatusAccepted || len(queue.messages) != 1 {
		t.Fatalf("login status = %d, messages = %d, body = %s, logs = %s", login.Code, len(queue.messages), login.Body.String(), middlewareLogs.String())
	}
	ownerMagicToken := messageToken(t, queue.messages[0])
	if !strings.Contains(queue.messages[0].Text, "http://acme.localhost:8088/auth/verify") {
		t.Errorf("tenant magic link = %s", queue.messages[0].Text)
	}

	verified := performTenantRequest(handler, http.MethodPost, "/auth/verify", `{"token":"`+ownerMagicToken+`"}`, "", nil)
	ownerSession := decodeTenantSession(t, verified)
	if ownerSession.Member.Role != "owner" {
		t.Fatalf("owner session role = %q, want owner", ownerSession.Member.Role)
	}
	ownerCookie := responseCookie(t, verified, tenantRefreshCookie)
	var welcomeEvents int
	var welcomeEventID string
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE tenant_id = $1 AND event = 'tenant.owner_activated'
	`, tenantID).Scan(&welcomeEvents); err != nil || welcomeEvents != 1 {
		t.Fatalf("owner welcome events = %d error = %v", welcomeEvents, err)
	}
	if err := root.QueryRow(ctx, `
		SELECT id FROM outbox_events
		WHERE tenant_id = $1 AND event = 'tenant.owner_activated'
	`, tenantID).Scan(&welcomeEventID); err != nil {
		t.Fatalf("load owner welcome event: %v", err)
	}
	welcomePayload, _ := json.Marshal(map[string]string{
		"outbox_event_id": welcomeEventID, "tenant_id": tenantID, "resource_id": tenantID,
	})
	welcomeSender := &lifecycleMessageSender{}
	welcomeHandler := email.LifecycleHandler{
		Database: pools.Admin, Sender: welcomeSender,
		PublicBaseURL: "http://api.localhost:8088", BaseDomain: "localhost",
		AuthFrom:       "Fidibeki <auth@fidibeki.com>",
		WelcomeFrom:    "Acidiney from Fidibeki <acidiney.dias@fidibeki.com>",
		WelcomeReplyTo: "acidiney.dias@fidibeki.com", Now: func() time.Time { return now },
	}.Handler()
	for attempt := 0; attempt < 2; attempt++ {
		if err := welcomeHandler(ctx, asynq.NewTask(email.TaskLifecycleEmail, welcomePayload)); err != nil {
			t.Fatalf("founder welcome attempt %d: %v", attempt+1, err)
		}
	}
	if len(welcomeSender.messages) != 1 || welcomeSender.messages[0].ReplyTo != "acidiney.dias@fidibeki.com" ||
		strings.Contains(welcomeSender.messages[0].Text, "token=") {
		t.Fatalf("founder welcome messages = %#v", welcomeSender.messages)
	}
	if ownerCookie.Path != "/" {
		t.Fatalf("tenant refresh cookie path = %q, want / for server-rendered route refresh", ownerCookie.Path)
	}
	appearance := performTenantRequest(
		handler, http.MethodPatch, "/v1/widget-appearance",
		`{"theme":"dark","accent_color":"#46c754","surface_color":"#101817","text_color":"#f7faf9",`+
			`"logo_url":"https://cdn.example.com/acme.svg"}`,
		ownerSession.AccessToken, nil,
	)
	if appearance.Code != http.StatusOK || !strings.Contains(appearance.Body.String(), `"accent_color":"#46c754"`) {
		t.Fatalf("widget appearance status = %d, body = %s", appearance.Code, appearance.Body.String())
	}
	var storedAppearance string
	if err := root.QueryRow(ctx, `SELECT widget_appearance_defaults::text FROM tenants WHERE id = $1`, tenantID).
		Scan(&storedAppearance); err != nil || !strings.Contains(storedAppearance, `"logo_url"`) {
		t.Fatalf("stored widget appearance = %q, error = %v", storedAppearance, err)
	}
	organizationProfile := performTenantRequest(
		handler, http.MethodPatch, "/v1/organization-profile",
		`{"name":"Acme Studio","short_name":"ACME","website_url":"https://acme.test",`+
			`"support_email":"support@acme.test","accent_color":"#46c754",`+
			`"default_locale":"pt-ao","default_timezone":"Africa/Luanda",`+
			`"privacy_policy_url":"https://acme.test/privacy",`+
			`"privacy_contact_email":"privacy@acme.test","privacy_rights_email":"rights@acme.test",`+
			`"terms_url":"https://acme.test/terms"}`,
		ownerSession.AccessToken, nil,
	)
	if organizationProfile.Code != http.StatusOK ||
		!strings.Contains(organizationProfile.Body.String(), `"default_locale":"pt-AO"`) ||
		!strings.Contains(organizationProfile.Body.String(), `"short_name":"ACME"`) ||
		!strings.Contains(organizationProfile.Body.String(), `"privacy_profile_version":1`) {
		t.Fatalf("organization profile status = %d, body = %s, logs = %s", organizationProfile.Code,
			organizationProfile.Body.String(), middlewareLogs.String())
	}
	storedProfile := performTenantRequest(
		handler, http.MethodGet, "/v1/organization-profile", "", ownerSession.AccessToken, nil,
	)
	if storedProfile.Code != http.StatusOK ||
		!strings.Contains(storedProfile.Body.String(), `"default_timezone":"Africa/Luanda"`) ||
		!strings.Contains(storedProfile.Body.String(), `"privacy_contact_email":"privacy@acme.test"`) ||
		!strings.Contains(storedProfile.Body.String(), `"privacy_rights_email":"rights@acme.test"`) {
		t.Fatalf("stored organization profile status = %d, body = %s", storedProfile.Code,
			storedProfile.Body.String())
	}
	if err := root.QueryRow(ctx, `
		SELECT widget_appearance_defaults::text
		FROM tenants WHERE id = $1
	`, tenantID).Scan(&storedAppearance); err != nil ||
		!strings.Contains(storedAppearance, `"logo_url"`) ||
		!strings.Contains(storedAppearance, `"surface_color"`) {
		t.Fatalf("organization profile dropped widget appearance = %q, error = %v",
			storedAppearance, err)
	}
	var profileAuditBefore, profileAuditAfter string
	if err := root.QueryRow(ctx, `
		SELECT data->'before'->>'name', data->'after'->>'name'
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'organization.profile_updated'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(&profileAuditBefore, &profileAuditAfter); err != nil ||
		profileAuditBefore != "Acme" || profileAuditAfter != "Acme Studio" {
		t.Fatalf("organization profile audit = %q/%q, error = %v",
			profileAuditBefore, profileAuditAfter, err)
	}
	invalidOrganizationProfile := performTenantRequest(
		handler, http.MethodPatch, "/v1/organization-profile",
		`{"name":"","website_url":"javascript:alert(1)","support_email":"bad",`+
			`"privacy_policy_url":"http://acme.test/privacy?unsafe=true",`+
			`"privacy_contact_email":"bad","privacy_rights_email":"also-bad",`+
			`"accent_color":"#ffffff","default_locale":"fr-FR","default_timezone":"Nowhere"}`,
		ownerSession.AccessToken, nil,
	)
	if invalidOrganizationProfile.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(invalidOrganizationProfile.Body.String(), `"website_url"`) ||
		!strings.Contains(invalidOrganizationProfile.Body.String(), `"privacy_policy_url"`) ||
		!strings.Contains(invalidOrganizationProfile.Body.String(), `"privacy_contact_email"`) ||
		!strings.Contains(invalidOrganizationProfile.Body.String(), `"default_timezone"`) {
		t.Fatalf("invalid organization profile status = %d, body = %s",
			invalidOrganizationProfile.Code, invalidOrganizationProfile.Body.String())
	}
	secondVerify := performTenantRequest(handler, http.MethodPost, "/auth/verify", `{"token":"`+ownerMagicToken+`"}`, "", nil)
	if secondVerify.Code != http.StatusUnauthorized {
		t.Errorf("second verify status = %d", secondVerify.Code)
	}

	legacyInvite := performTenantRequest(
		handler, http.MethodPost, "/auth/invite",
		`{"email":"legacy@other.test","permissions":["project:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if legacyInvite.Code != http.StatusAccepted || len(queue.messages) != 2 {
		t.Fatalf("pre-policy invite status = %d, messages = %d, body = %s", legacyInvite.Code, len(queue.messages), legacyInvite.Body.String())
	}
	legacyMagicToken := messageToken(t, queue.messages[1])

	domainRequest := performTenantRequest(
		handler, http.MethodPost, "/v1/member-domains",
		`{"email":"security@acme.test"}`,
		ownerSession.AccessToken, nil,
	)
	if domainRequest.Code != http.StatusAccepted || len(queue.messages) != 3 {
		t.Fatalf("domain verification request status = %d, messages = %d, body = %s", domainRequest.Code, len(queue.messages), domainRequest.Body.String())
	}
	pendingDomains := performTenantRequest(handler, http.MethodGet, "/v1/member-domains", "", ownerSession.AccessToken, nil)
	if pendingDomains.Code != http.StatusOK || !strings.Contains(pendingDomains.Body.String(), `"domain":"acme.test"`) ||
		!strings.Contains(pendingDomains.Body.String(), `"status":"pending"`) {
		t.Fatalf("pending member domains status = %d, body = %s", pendingDomains.Code, pendingDomains.Body.String())
	}
	domainVerified := performTenantRequest(
		handler, http.MethodPost, "/auth/member-domains/verify",
		`{"token":"`+messageToken(t, queue.messages[2])+`"}`, "", nil,
	)
	if domainVerified.Code != http.StatusOK || !strings.Contains(domainVerified.Body.String(), `"status":"verified"`) {
		t.Fatalf("domain verify status = %d, body = %s", domainVerified.Code, domainVerified.Body.String())
	}
	domainVerifyReplay := performTenantRequest(
		handler, http.MethodPost, "/auth/member-domains/verify",
		`{"token":"`+messageToken(t, queue.messages[2])+`"}`, "", nil,
	)
	if domainVerifyReplay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed domain verification status = %d, body = %s", domainVerifyReplay.Code, domainVerifyReplay.Body.String())
	}

	legacyActivation := performTenantRequest(
		handler, http.MethodPost, "/auth/verify", `{"token":"`+legacyMagicToken+`"}`, "", nil,
	)
	if legacyActivation.Code != http.StatusUnprocessableEntity || !strings.Contains(legacyActivation.Body.String(), "not verified") {
		t.Fatalf("pre-policy invitation activation status = %d, body = %s", legacyActivation.Code, legacyActivation.Body.String())
	}
	blockedInvite := performTenantRequest(
		handler, http.MethodPost, "/auth/invite",
		`{"email":"blocked@other.test","permissions":["project:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if blockedInvite.Code != http.StatusUnprocessableEntity || len(queue.messages) != 3 ||
		!strings.Contains(blockedInvite.Body.String(), "not verified") {
		t.Fatalf("unverified-domain invite status = %d, messages = %d, body = %s", blockedInvite.Code, len(queue.messages), blockedInvite.Body.String())
	}

	invite := performTenantRequest(
		handler, http.MethodPost, "/auth/invite",
		`{"email":"viewer@acme.test","permissions":["project:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if invite.Code != http.StatusAccepted || len(queue.messages) != 4 {
		t.Fatalf("invite status = %d, messages = %d, body = %s", invite.Code, len(queue.messages), invite.Body.String())
	}
	viewerMagicToken := messageToken(t, queue.messages[3])
	viewerVerified := performTenantRequest(handler, http.MethodPost, "/auth/verify", `{"token":"`+viewerMagicToken+`"}`, "", nil)
	viewerSession := decodeTenantSession(t, viewerVerified)
	viewerCookie := responseCookie(t, viewerVerified, tenantRefreshCookie)
	if len(viewerSession.Member.Permissions) != 1 || viewerSession.Member.Permissions[0] != "project:read" {
		t.Errorf("viewer permissions = %#v", viewerSession.Member.Permissions)
	}
	if viewerSession.Member.Locale != nil || viewerSession.Member.Timezone != nil {
		t.Fatalf("new viewer regional preferences = %v/%v, want inherit",
			viewerSession.Member.Locale, viewerSession.Member.Timezone)
	}

	members := performTenantRequest(handler, http.MethodGet, "/v1/members", "", ownerSession.AccessToken, nil)
	if members.Code != http.StatusOK || !strings.Contains(members.Body.String(), "viewer@acme.test") ||
		!strings.Contains(members.Body.String(), `"role":"owner"`) {
		t.Errorf("members status = %d, body = %s", members.Code, members.Body.String())
	}
	viewerForbidden := performTenantRequest(handler, http.MethodGet, "/v1/members", "", viewerSession.AccessToken, nil)
	if viewerForbidden.Code != http.StatusForbidden {
		t.Errorf("viewer members status = %d", viewerForbidden.Code)
	}
	viewerOwnProfile := performTenantRequest(
		handler, http.MethodPatch, "/v1/profile",
		`{"name":"  Jamie Analyst  "}`, viewerSession.AccessToken, nil,
	)
	if viewerOwnProfile.Code != http.StatusOK ||
		!strings.Contains(viewerOwnProfile.Body.String(), `"name":"Jamie Analyst"`) {
		t.Fatalf("viewer own profile status = %d, body = %s",
			viewerOwnProfile.Code, viewerOwnProfile.Body.String())
	}
	var viewerProfileName string
	var viewerProfileAudit int
	if err := root.QueryRow(ctx, `
		SELECT name FROM tenant_members WHERE tenant_id = $1 AND id = $2
	`, tenantID, viewerSession.Member.ID).Scan(&viewerProfileName); err != nil ||
		viewerProfileName != "Jamie Analyst" {
		t.Fatalf("viewer profile name = %q, error = %v", viewerProfileName, err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'member.profile_updated'
		  AND actor_member_id = $2 AND subject_member_id = $2
	`, tenantID, viewerSession.Member.ID).Scan(&viewerProfileAudit); err != nil ||
		viewerProfileAudit != 1 {
		t.Fatalf("viewer profile audit = %d, error = %v", viewerProfileAudit, err)
	}
	regionalProfile := performTenantRequest(
		handler, http.MethodPatch, "/v1/profile",
		`{"locale":" PT_ao ","timezone":" Africa/Luanda "}`, viewerSession.AccessToken, nil,
	)
	var regionalEnvelope struct {
		Member Member `json:"member"`
	}
	if regionalProfile.Code != http.StatusOK ||
		json.Unmarshal(regionalProfile.Body.Bytes(), &regionalEnvelope) != nil {
		t.Fatalf("regional profile status = %d, body = %s",
			regionalProfile.Code, regionalProfile.Body.String())
	}
	if regionalEnvelope.Member.Name == nil || *regionalEnvelope.Member.Name != "Jamie Analyst" ||
		regionalEnvelope.Member.Locale == nil || *regionalEnvelope.Member.Locale != "pt-AO" ||
		regionalEnvelope.Member.Timezone == nil || *regionalEnvelope.Member.Timezone != "Africa/Luanda" {
		t.Fatalf("regional profile member = %#v", regionalEnvelope.Member)
	}
	refreshedViewer := performTenantRequest(
		handler, http.MethodPost, "/auth/refresh", "", "", viewerCookie,
	)
	refreshedViewerSession := decodeTenantSession(t, refreshedViewer)
	if refreshedViewerSession.Member.Locale == nil ||
		*refreshedViewerSession.Member.Locale != "pt-AO" ||
		refreshedViewerSession.Member.Timezone == nil ||
		*refreshedViewerSession.Member.Timezone != "Africa/Luanda" {
		t.Fatalf("refreshed viewer regional preferences = %#v", refreshedViewerSession.Member)
	}
	regionalMembers := performTenantRequest(
		handler, http.MethodGet, "/v1/members", "", ownerSession.AccessToken, nil,
	)
	if regionalMembers.Code != http.StatusOK ||
		!strings.Contains(regionalMembers.Body.String(), `"locale":"pt-AO"`) ||
		!strings.Contains(regionalMembers.Body.String(), `"timezone":"Africa/Luanda"`) {
		t.Fatalf("member listing regional preferences = %d, body = %s",
			regionalMembers.Code, regionalMembers.Body.String())
	}
	var auditPreviousLocale, auditLocale, auditPreviousTimezone, auditTimezone *string
	if err := root.QueryRow(ctx, `
		SELECT data->>'previous_locale', data->>'locale',
		       data->>'previous_timezone', data->>'timezone'
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'member.profile_updated'
		  AND actor_member_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tenantID, viewerSession.Member.ID).Scan(
		&auditPreviousLocale, &auditLocale, &auditPreviousTimezone, &auditTimezone,
	); err != nil {
		t.Fatalf("read member regional audit: %v", err)
	}
	if auditPreviousLocale != nil || auditPreviousTimezone != nil ||
		auditLocale == nil || *auditLocale != "pt-AO" ||
		auditTimezone == nil || *auditTimezone != "Africa/Luanda" {
		t.Fatalf("member regional audit = %v/%v and %v/%v",
			auditPreviousLocale, auditLocale, auditPreviousTimezone, auditTimezone)
	}
	clearLocale := performTenantRequest(
		handler, http.MethodPatch, "/v1/profile",
		`{"locale":null}`, viewerSession.AccessToken, nil,
	)
	var clearLocaleEnvelope struct {
		Member Member `json:"member"`
	}
	if clearLocale.Code != http.StatusOK ||
		json.Unmarshal(clearLocale.Body.Bytes(), &clearLocaleEnvelope) != nil ||
		clearLocaleEnvelope.Member.Locale != nil ||
		clearLocaleEnvelope.Member.Timezone == nil ||
		*clearLocaleEnvelope.Member.Timezone != "Africa/Luanda" {
		t.Fatalf("clear locale response = %d, %#v, body = %s",
			clearLocale.Code, clearLocaleEnvelope.Member, clearLocale.Body.String())
	}
	clearTimezone := performTenantRequest(
		handler, http.MethodPatch, "/v1/profile",
		`{"timezone":""}`, viewerSession.AccessToken, nil,
	)
	var clearTimezoneEnvelope struct {
		Member Member `json:"member"`
	}
	if clearTimezone.Code != http.StatusOK ||
		json.Unmarshal(clearTimezone.Body.Bytes(), &clearTimezoneEnvelope) != nil ||
		clearTimezoneEnvelope.Member.Locale != nil || clearTimezoneEnvelope.Member.Timezone != nil {
		t.Fatalf("clear timezone response = %d, %#v, body = %s",
			clearTimezone.Code, clearTimezoneEnvelope.Member, clearTimezone.Body.String())
	}
	noOpProfile := performTenantRequest(
		handler, http.MethodPatch, "/v1/profile", `{}`, viewerSession.AccessToken, nil,
	)
	if noOpProfile.Code != http.StatusOK {
		t.Fatalf("empty profile patch status = %d, body = %s",
			noOpProfile.Code, noOpProfile.Body.String())
	}
	sameProfile := performTenantRequest(
		handler, http.MethodPatch, "/v1/profile",
		`{"name":"Jamie Analyst","locale":null,"timezone":null}`,
		viewerSession.AccessToken, nil,
	)
	if sameProfile.Code != http.StatusOK {
		t.Fatalf("unchanged profile patch status = %d, body = %s",
			sameProfile.Code, sameProfile.Body.String())
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'member.profile_updated'
		  AND actor_member_id = $2 AND subject_member_id = $2
	`, tenantID, viewerSession.Member.ID).Scan(&viewerProfileAudit); err != nil ||
		viewerProfileAudit != 4 {
		t.Fatalf("viewer profile audit after regional updates = %d, error = %v",
			viewerProfileAudit, err)
	}
	invalidViewerProfile := performTenantRequest(
		handler, http.MethodPatch, "/v1/profile",
		`{"name":"`+strings.Repeat("x", 121)+`","locale":"fr-FR","timezone":"Nowhere/Unknown"}`,
		viewerSession.AccessToken, nil,
	)
	if invalidViewerProfile.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(invalidViewerProfile.Body.String(), `"name"`) ||
		!strings.Contains(invalidViewerProfile.Body.String(), `"locale"`) ||
		!strings.Contains(invalidViewerProfile.Body.String(), `"timezone"`) {
		t.Fatalf("invalid viewer profile status = %d, body = %s",
			invalidViewerProfile.Code, invalidViewerProfile.Body.String())
	}
	foreignToken, _, err := jwtManager.IssueTenantMember(
		viewerSession.Member.ID, "00000000-0000-4000-8000-000000000099",
		viewerSession.Member.Permissions, viewerSession.Member.AuthorizationVersion,
	)
	if err != nil {
		t.Fatalf("issue foreign tenant token: %v", err)
	}
	foreignProfile := performTenantRequest(
		handler, http.MethodPatch, "/v1/profile",
		`{"locale":"pt"}`, foreignToken, nil,
	)
	if foreignProfile.Code != http.StatusForbidden {
		t.Fatalf("foreign tenant profile status = %d, body = %s",
			foreignProfile.Code, foreignProfile.Body.String())
	}
	viewerProfileUpdate := performTenantRequest(
		handler, http.MethodPatch, "/v1/organization-profile",
		`{"name":"Unauthorized"}`, viewerSession.AccessToken, nil,
	)
	if viewerProfileUpdate.Code != http.StatusForbidden {
		t.Errorf("viewer organization profile update status = %d, body = %s",
			viewerProfileUpdate.Code, viewerProfileUpdate.Body.String())
	}

	updatedViewer := performTenantRequest(
		handler, http.MethodPatch, "/v1/members/"+viewerSession.Member.ID,
		`{"permissions":["project:read","feedback:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if updatedViewer.Code != http.StatusOK || !strings.Contains(updatedViewer.Body.String(), "feedback:read") {
		t.Errorf("update member status = %d, body = %s, logs = %s", updatedViewer.Code, updatedViewer.Body.String(), middlewareLogs.String())
	}
	protectedOwner := performTenantRequest(
		handler, http.MethodPatch, "/v1/members/"+ownerSession.Member.ID,
		`{"permissions":["project:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if protectedOwner.Code != http.StatusConflict || !strings.Contains(protectedOwner.Body.String(), `"code":"owner_protected"`) {
		t.Fatalf("protected owner update status = %d, body = %s, logs = %s", protectedOwner.Code, protectedOwner.Body.String(), middlewareLogs.String())
	}
	duplicateInvite := performTenantRequest(
		handler, http.MethodPost, "/auth/invite",
		`{"email":"viewer@acme.test","permissions":["project:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if duplicateInvite.Code != http.StatusConflict {
		t.Errorf("duplicate invite status = %d, body = %s", duplicateInvite.Code, duplicateInvite.Body.String())
	}
	additionalDomainRequest := performTenantRequest(
		handler, http.MethodPost, "/v1/member-domains",
		`{"email":"security@other.test"}`,
		ownerSession.AccessToken, nil,
	)
	if additionalDomainRequest.Code != http.StatusAccepted || len(queue.messages) != 5 {
		t.Fatalf("additional domain request status = %d, messages = %d, body = %s", additionalDomainRequest.Code, len(queue.messages), additionalDomainRequest.Body.String())
	}
	additionalDomainVerified := performTenantRequest(
		handler, http.MethodPost, "/auth/member-domains/verify",
		`{"token":"`+messageToken(t, queue.messages[4])+`"}`, "", nil,
	)
	if additionalDomainVerified.Code != http.StatusOK || !strings.Contains(additionalDomainVerified.Body.String(), `"domain":"other.test"`) {
		t.Fatalf("additional domain verify status = %d, body = %s", additionalDomainVerified.Code, additionalDomainVerified.Body.String())
	}
	additionalInvite := performTenantRequest(
		handler, http.MethodPost, "/auth/invite",
		`{"email":"allowed@other.test","permissions":["project:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if additionalInvite.Code != http.StatusAccepted || len(queue.messages) != 6 {
		t.Fatalf("verified additional-domain invite status = %d, messages = %d, body = %s", additionalInvite.Code, len(queue.messages), additionalInvite.Body.String())
	}
	additionalActivation := performTenantRequest(
		handler, http.MethodPost, "/auth/verify",
		`{"token":"`+messageToken(t, queue.messages[5])+`"}`, "", nil,
	)
	if additionalActivation.Code != http.StatusOK {
		t.Fatalf("verified additional-domain activation status = %d, body = %s", additionalActivation.Code, additionalActivation.Body.String())
	}
	additionalSession := decodeTenantSession(t, additionalActivation)
	machineToken := "fk_live_" + strings.Repeat("A", 32)
	var machineCredentialID string
	if err := root.QueryRow(ctx, `
		INSERT INTO api_keys (tenant_id, name, token_hash, token_prefix, permissions)
		VALUES ($1, 'Lifecycle machine key', $2, 'fk_live_AAAA...', ARRAY['member:update'])
		RETURNING id
	`, tenantID, auth.HashToken(machineToken)).Scan(&machineCredentialID); err != nil {
		t.Fatalf("create machine tenant credential: %v", err)
	}
	machineDenied := performTenantRequest(
		handler, http.MethodPatch, "/v1/members/"+additionalSession.Member.ID+"/status",
		`{"status":"deactivated"}`, machineToken, nil,
	)
	if machineDenied.Code != http.StatusForbidden || !strings.Contains(machineDenied.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("machine lifecycle change status = %d, body = %s", machineDenied.Code, machineDenied.Body.String())
	}
	var auditedMachineCredential string
	if err := root.QueryRow(ctx, `
		SELECT data->>'actor_credential_id'
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'member.lifecycle_change_denied'
		  AND data->>'reason' = 'actor_is_not_active_member'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(&auditedMachineCredential); err != nil || auditedMachineCredential != machineCredentialID {
		t.Fatalf("machine audit credential = %q, error = %v", auditedMachineCredential, err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO tenant_magic_links (
			tenant_id, member_id, email, invited_permissions, token_hash, expires_at
		) VALUES ($1, $2, 'allowed@other.test', '{}'::text[], $3, $4)
	`, tenantID, additionalSession.Member.ID, auth.HashToken("pending-before-deactivation"), now.Add(time.Hour)); err != nil {
		t.Fatalf("create pending member magic link: %v", err)
	}
	invalidTransfer := performTenantRequest(
		handler, http.MethodPost, "/v1/ownership/transfer",
		`{"new_owner_member_id":"not-a-uuid","confirmation":"acme"}`,
		ownerSession.AccessToken, nil,
	)
	if invalidTransfer.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid ownership transfer status = %d, body = %s", invalidTransfer.Code, invalidTransfer.Body.String())
	}
	selfTransfer := performTenantRequest(
		handler, http.MethodPost, "/v1/ownership/transfer",
		`{"new_owner_member_id":"`+ownerSession.Member.ID+`","confirmation":"acme"}`,
		ownerSession.AccessToken, nil,
	)
	if selfTransfer.Code != http.StatusUnprocessableEntity || !strings.Contains(selfTransfer.Body.String(), `"code":"invalid_ownership_target"`) {
		t.Fatalf("self ownership transfer status = %d, body = %s", selfTransfer.Code, selfTransfer.Body.String())
	}
	var unverifiedMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'pending@acme.test', ARRAY['project:read']) RETURNING id
	`, tenantID).Scan(&unverifiedMemberID); err != nil {
		t.Fatalf("create unverified ownership target: %v", err)
	}
	unverifiedTransfer := performTenantRequest(
		handler, http.MethodPost, "/v1/ownership/transfer",
		`{"new_owner_member_id":"`+unverifiedMemberID+`","confirmation":"acme"}`,
		ownerSession.AccessToken, nil,
	)
	if unverifiedTransfer.Code != http.StatusUnprocessableEntity || !strings.Contains(unverifiedTransfer.Body.String(), `"code":"invalid_ownership_target"`) {
		t.Fatalf("unverified ownership transfer status = %d, body = %s", unverifiedTransfer.Code, unverifiedTransfer.Body.String())
	}
	viewerLogout := performTenantRequest(handler, http.MethodPost, "/auth/logout", "", "", viewerCookie)
	if viewerLogout.Code != http.StatusNoContent {
		t.Errorf("viewer logout status = %d, body = %s", viewerLogout.Code, viewerLogout.Body.String())
	}
	transfer := performTenantRequest(
		handler, http.MethodPost, "/v1/ownership/transfer",
		`{"new_owner_member_id":"`+viewerSession.Member.ID+`","confirmation":"acme"}`,
		ownerSession.AccessToken, nil,
	)
	if transfer.Code != http.StatusOK || !strings.Contains(transfer.Body.String(), `"role":"owner"`) ||
		!strings.Contains(transfer.Body.String(), `"email":"viewer@acme.test"`) {
		t.Fatalf("ownership transfer status = %d, body = %s, logs = %s", transfer.Code, transfer.Body.String(), middlewareLogs.String())
	}
	invalidatedViewer := performTenantRequest(handler, http.MethodGet, "/v1/members", "", viewerSession.AccessToken, nil)
	if invalidatedViewer.Code != http.StatusUnauthorized {
		t.Fatalf("pre-transfer viewer token status = %d, body = %s", invalidatedViewer.Code, invalidatedViewer.Body.String())
	}
	formerOwnerRetry := performTenantRequest(
		handler, http.MethodPost, "/v1/ownership/transfer",
		`{"new_owner_member_id":"`+ownerSession.Member.ID+`","confirmation":"acme"}`,
		ownerSession.AccessToken, nil,
	)
	if formerOwnerRetry.Code != http.StatusConflict {
		t.Fatalf("former owner transfer status = %d, body = %s", formerOwnerRetry.Code, formerOwnerRetry.Body.String())
	}
	protectedNewOwner := performTenantRequest(
		handler, http.MethodPatch, "/v1/members/"+viewerSession.Member.ID+"/status",
		`{"status":"deactivated"}`, ownerSession.AccessToken, nil,
	)
	if protectedNewOwner.Code != http.StatusConflict || !strings.Contains(protectedNewOwner.Body.String(), `"code":"owner_protected"`) {
		t.Fatalf("protected new owner status = %d, body = %s", protectedNewOwner.Code, protectedNewOwner.Body.String())
	}
	deactivated := performTenantRequest(
		handler, http.MethodPatch, "/v1/members/"+additionalSession.Member.ID+"/status",
		`{"status":"deactivated"}`, ownerSession.AccessToken, nil,
	)
	if deactivated.Code != http.StatusOK || !strings.Contains(deactivated.Body.String(), `"status":"deactivated"`) {
		t.Fatalf("deactivate member status = %d, body = %s", deactivated.Code, deactivated.Body.String())
	}
	var revokedSessionCount int
	var sessionsRevokedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT (data->>'revoked_session_count')::integer,
		       (data->>'sessions_revoked_at')::timestamptz
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'member.deactivated' AND subject_member_id = $2
	`, tenantID, additionalSession.Member.ID).Scan(&revokedSessionCount, &sessionsRevokedAt); err != nil ||
		revokedSessionCount != 1 || sessionsRevokedAt == nil {
		t.Fatalf("deactivation session audit = %d/%v, error = %v", revokedSessionCount, sessionsRevokedAt, err)
	}
	var pendingLinks int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_magic_links
		WHERE tenant_id = $1 AND member_id = $2 AND consumed_at IS NULL
	`, tenantID, additionalSession.Member.ID).Scan(&pendingLinks); err != nil || pendingLinks != 0 {
		t.Fatalf("pending links after deactivation = %d, error = %v", pendingLinks, err)
	}
	deactivatedAccess := performTenantRequest(handler, http.MethodGet, "/v1/members", "", additionalSession.AccessToken, nil)
	if deactivatedAccess.Code != http.StatusUnauthorized {
		t.Fatalf("deactivated member token status = %d, body = %s", deactivatedAccess.Code, deactivatedAccess.Body.String())
	}
	deactivatedLogin := performTenantRequest(handler, http.MethodPost, "/auth/login", `{"email":"allowed@other.test"}`, "", nil)
	if deactivatedLogin.Code != http.StatusAccepted || len(queue.messages) != 6 {
		t.Fatalf("deactivated member login status = %d, messages = %d", deactivatedLogin.Code, len(queue.messages))
	}
	reactivated := performTenantRequest(
		handler, http.MethodPatch, "/v1/members/"+additionalSession.Member.ID+"/status",
		`{"status":"active"}`, ownerSession.AccessToken, nil,
	)
	if reactivated.Code != http.StatusOK || !strings.Contains(reactivated.Body.String(), `"status":"active"`) ||
		!strings.Contains(reactivated.Body.String(), `"permissions":[]`) {
		t.Fatalf("reactivate member status = %d, body = %s", reactivated.Code, reactivated.Body.String())
	}
	removed := performTenantRequest(
		handler, http.MethodDelete, "/v1/members/"+additionalSession.Member.ID,
		"", ownerSession.AccessToken, nil,
	)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"status":"removed"`) {
		t.Fatalf("remove member status = %d, body = %s", removed.Code, removed.Body.String())
	}
	var accessAuditKinds []string
	rows, err := pools.Admin.Query(ctx, `
		SELECT kind FROM tenant_access_events WHERE tenant_id = $1 ORDER BY created_at, id
	`, tenantID)
	if err != nil {
		t.Fatalf("list tenant access audit: %v", err)
	}
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatalf("scan tenant access audit: %v", err)
		}
		accessAuditKinds = append(accessAuditKinds, kind)
	}
	rows.Close()
	for _, expected := range []string{
		"member.permission_change_denied", "ownership.transferred", "ownership.transfer_denied",
		"member.lifecycle_change_denied", "member.deactivated", "member.reactivated", "member.removed",
	} {
		if !slices.Contains(accessAuditKinds, expected) {
			t.Errorf("access audit kinds = %v, missing %q", accessAuditKinds, expected)
		}
	}
	invalidLogin := performTenantRequest(handler, http.MethodPost, "/auth/login", `{"email":"invalid"}`, "", nil)
	if invalidLogin.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid login status = %d", invalidLogin.Code)
	}

	startRefresh := make(chan struct{})
	refreshResponses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			<-startRefresh
			refreshResponses <- performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", ownerCookie)
		}()
	}
	close(startRefresh)
	refreshed := <-refreshResponses
	reused := <-refreshResponses
	if refreshed.Code != http.StatusOK || reused.Code != http.StatusOK {
		t.Fatalf(
			"concurrent refresh statuses = %d/%d, bodies = %s / %s",
			refreshed.Code, reused.Code, refreshed.Body.String(), reused.Body.String(),
		)
	}
	rotatedCookie := responseCookie(t, refreshed, tenantRefreshCookie)
	retryCookie := responseCookie(t, reused, tenantRefreshCookie)
	if rotatedCookie.Value != retryCookie.Value {
		t.Fatal("concurrent refreshes returned different successor cookies")
	}
	var ownerSessionCount int
	if err := pools.Admin.QueryRow(ctx, `
		SELECT count(*) FROM tenant_member_sessions WHERE tenant_id = $1 AND member_id = $2
	`, tenantID, ownerSession.Member.ID).Scan(&ownerSessionCount); err != nil || ownerSessionCount != 2 {
		t.Fatalf("owner session count = %d, error = %v, want 2", ownerSessionCount, err)
	}

	now = now.Add(time.Minute)
	lateReuse := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", ownerCookie)
	if lateReuse.Code != http.StatusUnauthorized {
		t.Errorf("late reused refresh status = %d", lateReuse.Code)
	}
	revokedFamily := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", rotatedCookie)
	if revokedFamily.Code != http.StatusUnauthorized {
		t.Errorf("revoked family refresh status = %d", revokedFamily.Code)
	}
	revokedRetry := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", retryCookie)
	if revokedRetry.Code != http.StatusUnauthorized {
		t.Errorf("revoked retry refresh status = %d", revokedRetry.Code)
	}

	relogin := performTenantRequest(handler, http.MethodPost, "/auth/login", `{"email":"owner@acme.test"}`, "", nil)
	if relogin.Code != http.StatusAccepted || len(queue.messages) != 7 {
		t.Fatalf("relogin status = %d, messages = %d", relogin.Code, len(queue.messages))
	}
	reverified := performTenantRequest(
		handler, http.MethodPost, "/auth/verify",
		`{"token":"`+messageToken(t, queue.messages[6])+`"}`, "", nil,
	)
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE tenant_id = $1 AND event = 'tenant.owner_activated'
	`, tenantID).Scan(&welcomeEvents); err != nil || welcomeEvents != 1 {
		t.Fatalf("owner welcome events after re-login = %d error = %v", welcomeEvents, err)
	}
	fingerprintCookie := responseCookie(t, reverified, tenantRefreshCookie)
	fingerprintRefresh := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", fingerprintCookie)
	if fingerprintRefresh.Code != http.StatusOK {
		t.Fatalf("fingerprint refresh status = %d", fingerprintRefresh.Code)
	}
	fingerprintRotatedCookie := responseCookie(t, fingerprintRefresh, tenantRefreshCookie)
	mismatchedRetry := performTenantRequestWithUserAgent(
		handler, http.MethodPost, "/auth/refresh", "", "", fingerprintCookie, "different-agent",
	)
	if mismatchedRetry.Code != http.StatusUnauthorized {
		t.Errorf("mismatched fingerprint refresh status = %d", mismatchedRetry.Code)
	}
	mismatchedRevokedFamily := performTenantRequest(
		handler, http.MethodPost, "/auth/refresh", "", "", fingerprintRotatedCookie,
	)
	if mismatchedRevokedFamily.Code != http.StatusUnauthorized {
		t.Errorf("mismatched fingerprint family status = %d", mismatchedRevokedFamily.Code)
	}

	logoutLogin := performTenantRequest(handler, http.MethodPost, "/auth/login", `{"email":"owner@acme.test"}`, "", nil)
	if logoutLogin.Code != http.StatusAccepted || len(queue.messages) != 8 {
		t.Fatalf("logout login status = %d, messages = %d", logoutLogin.Code, len(queue.messages))
	}
	logoutVerified := performTenantRequest(
		handler, http.MethodPost, "/auth/verify",
		`{"token":"`+messageToken(t, queue.messages[7])+`"}`, "", nil,
	)
	logoutParentCookie := responseCookie(t, logoutVerified, tenantRefreshCookie)
	logoutRefresh := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", logoutParentCookie)
	if logoutRefresh.Code != http.StatusOK {
		t.Fatalf("logout setup refresh status = %d", logoutRefresh.Code)
	}
	logoutChildCookie := responseCookie(t, logoutRefresh, tenantRefreshCookie)
	logoutResponse := performTenantRequest(handler, http.MethodPost, "/auth/logout", "", "", logoutParentCookie)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("predecessor logout status = %d", logoutResponse.Code)
	}
	logoutChildRefresh := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", logoutChildCookie)
	if logoutChildRefresh.Code != http.StatusUnauthorized {
		t.Errorf("logged-out successor refresh status = %d", logoutChildRefresh.Code)
	}

	expiryLogin := performTenantRequest(handler, http.MethodPost, "/auth/login", `{"email":"owner@acme.test"}`, "", nil)
	if expiryLogin.Code != http.StatusAccepted || len(queue.messages) != 9 {
		t.Fatalf("expiry login status = %d, messages = %d", expiryLogin.Code, len(queue.messages))
	}
	expiryVerified := performTenantRequest(
		handler, http.MethodPost, "/auth/verify",
		`{"token":"`+messageToken(t, queue.messages[8])+`"}`, "", nil,
	)
	expiryCookie := responseCookie(t, expiryVerified, tenantRefreshCookie)
	now = now.Add(31 * 24 * time.Hour)
	expiredRefresh := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", expiryCookie)
	if expiredRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("expired refresh status = %d", expiredRefresh.Code)
	}
	var expiryPersisted bool
	if err := pools.Admin.QueryRow(ctx, `
		SELECT revoked_at IS NOT NULL FROM tenant_member_sessions
		WHERE tenant_id = $1 AND refresh_hash = $2
	`, tenantID, auth.HashToken(expiryCookie.Value)).Scan(&expiryPersisted); err != nil || !expiryPersisted {
		t.Fatalf("expired session revocation persisted = %t, error = %v", expiryPersisted, err)
	}

	if _, err := root.Exec(ctx, `
		INSERT INTO tenant_saml_configurations (
			tenant_id, mode, idp_entity_id, idp_sso_url,
			sp_private_key, sp_certificate, last_tested_at
		)
		VALUES ($1, 'required', 'https://idp.acme.test/entity', 'https://idp.acme.test/sso',
		        $2, 'test-certificate', $3)
	`, tenantID, []byte("sealed-test-key"), now); err != nil {
		t.Fatalf("enable required SSO for break-glass audit: %v", err)
	}
	requiredLocalRefresh := "required-local-refresh"
	if _, err := root.Exec(ctx, `
		INSERT INTO tenant_member_sessions (tenant_id, member_id, refresh_hash)
		VALUES ($1, $2, $3)
	`, tenantID, ownerMemberID, auth.HashToken(requiredLocalRefresh)); err != nil {
		t.Fatalf("create residual local session under required SSO: %v", err)
	}
	blockedLocalRefresh := performTenantRequest(
		handler, http.MethodPost, "/auth/refresh", "", "",
		&http.Cookie{Name: tenantRefreshCookie, Value: requiredLocalRefresh},
	)
	if blockedLocalRefresh.Code != http.StatusUnauthorized {
		t.Fatalf(
			"required-mode local refresh status = %d, body = %s, want unauthorized",
			blockedLocalRefresh.Code, blockedLocalRefresh.Body.String(),
		)
	}
	var requiredLocalRevoked bool
	if err := root.QueryRow(ctx, `
		SELECT revoked_at IS NOT NULL FROM tenant_member_sessions
		WHERE tenant_id = $1 AND refresh_hash = $2
	`, tenantID, auth.HashToken(requiredLocalRefresh)).Scan(&requiredLocalRevoked); err != nil ||
		!requiredLocalRevoked {
		t.Fatalf(
			"required-mode local session revoked = %t, error = %v, want true",
			requiredLocalRevoked, err,
		)
	}
	messageCount := len(queue.messages)
	breakGlassLogin := performTenantRequest(
		handler, http.MethodPost, "/auth/login", `{"email":"viewer@acme.test"}`, "", nil,
	)
	if breakGlassLogin.Code != http.StatusAccepted || len(queue.messages) != messageCount+1 {
		t.Fatalf(
			"break-glass login status = %d, messages = %d, body = %s, want accepted/%d",
			breakGlassLogin.Code, len(queue.messages), breakGlassLogin.Body.String(), messageCount+1,
		)
	}
	breakGlassVerified := performTenantRequest(
		handler, http.MethodPost, "/auth/verify",
		`{"token":"`+messageToken(t, queue.messages[messageCount])+`"}`, "", nil,
	)
	if breakGlassVerified.Code != http.StatusOK {
		t.Fatalf(
			"break-glass verification status = %d, body = %s",
			breakGlassVerified.Code, breakGlassVerified.Body.String(),
		)
	}
	var breakGlassSuccessEvents int
	if err := pools.Admin.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'sso.break_glass_login_succeeded'
		  AND actor_member_id = $2 AND outcome = 'success'
	`, tenantID, viewerSession.Member.ID).Scan(&breakGlassSuccessEvents); err != nil ||
		breakGlassSuccessEvents != 1 {
		t.Fatalf(
			"successful break-glass audit events = %d, error = %v, want 1",
			breakGlassSuccessEvents, err,
		)
	}
}

type tenantSessionResponse struct {
	AccessToken string `json:"access_token"`
	Member      Member `json:"member"`
}

func decodeTenantSession(t *testing.T, recorder *httptest.ResponseRecorder) tenantSessionResponse {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("session status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response tenantSessionResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return response
}

func performTenantRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	accessToken string,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	return performTenantRequestWithUserAgent(handler, method, path, body, accessToken, cookie, "")
}

func performTenantRequestWithUserAgent(
	handler http.Handler,
	method string,
	path string,
	body string,
	accessToken string,
	cookie *http.Cookie,
	userAgent string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://acme.localhost"+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	request.Header.Set("User-Agent", userAgent)
	handler.ServeHTTP(recorder, request)
	return recorder
}

func responseCookie(t *testing.T, recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response cookie %q missing", name)
	return nil
}

func messageToken(t *testing.T, message email.Message) string {
	t.Helper()
	for _, field := range strings.Fields(message.Text) {
		parsed, err := url.Parse(field)
		if err != nil || parsed.RawQuery != "" {
			continue
		}
		fragment, err := url.ParseQuery(parsed.Fragment)
		if err == nil && fragment.Get("token") != "" {
			return fragment.Get("token")
		}
	}
	t.Fatal("message token missing")
	return ""
}

type messageCapture struct {
	messages []email.Message
}

type lifecycleMessageSender struct {
	messages []email.Message
}

func (s *lifecycleMessageSender) Send(_ context.Context, message email.Message) (string, error) {
	s.messages = append(s.messages, message)
	return "welcome-message", nil
}

func (c *messageCapture) Enqueue(_ context.Context, message email.Message) error {
	c.messages = append(c.messages, message)
	return nil
}

func tenantMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	return path
}

func tenantRoleURL(t *testing.T, directURL, username, password string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}
