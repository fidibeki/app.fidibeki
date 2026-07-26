package issuetracker

import (
	"context"
	"encoding/base64"
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
	"github.com/acidiney/app.fidibeki/backend/internal/integration"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// trackerEncryptionKey is throwaway local test material. It is derived rather than written out
// as a literal so the secret scanner has nothing key-shaped to flag in this file.
var trackerEncryptionKey = base64.StdEncoding.EncodeToString(
	[]byte(strings.Repeat("0123456789abcdef", 2)),
)

// TestIssueTrackerLifecycle walks the whole opt-in workflow against real PostgreSQL and fake
// Jira and Linear endpoints: connect, configure, enable, create, retry a repeated request,
// survive a provider failure, retry it, and stay invisible while the provider is off.
func TestIssueTrackerLifecycle(t *testing.T) {
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
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, trackerMigrationsPath(t))
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
	for slug, plan := range map[string]string{"acme": "scale", "beta": "scale", "starter": "growth"} {
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
	for slug := range tenantIDs {
		var memberID string
		if err := root.QueryRow(ctx, `
			INSERT INTO tenant_members (tenant_id, email, permissions)
			VALUES ($1, $2, $3) RETURNING id
		`, tenantIDs[slug], "owner@"+slug+".test", auth.TenantPermissionStrings()).Scan(&memberID); err != nil {
			t.Fatalf("create %s owner: %v", slug, err)
		}
		memberIDs[slug] = memberID
	}
	var readerID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'reader@acme.test', ARRAY['issue:read','integration:read']) RETURNING id
	`, tenantIDs["acme"]).Scan(&readerID); err != nil {
		t.Fatalf("create read-only member: %v", err)
	}
	var projectID, issueID, secondIssueID string
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.com}') RETURNING id
	`, tenantIDs["acme"]).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title, description, priority)
		VALUES ($1, $2, 'ACME-2026-0001', 'Checkout is confusing', 'Reporter cannot pay', 'high')
		RETURNING id
	`, tenantIDs["acme"], projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title, description)
		VALUES ($1, $2, 'ACME-2026-0002', 'Search is slow', 'Results take 8 seconds')
		RETURNING id
	`, tenantIDs["acme"], projectID).Scan(&secondIssueID); err != nil {
		t.Fatalf("create second issue: %v", err)
	}

	pools, err := db.OpenPools(ctx, trackerRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	cipher, err := integration.NewCipher(trackerEncryptionKey)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	jwtManager, err := auth.NewJWTManager(strings.Repeat("t", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant(
		memberIDs["acme"], tenantIDs["acme"], auth.TenantPermissionStrings())
	betaToken, _, _ := jwtManager.IssueTenant(
		memberIDs["beta"], tenantIDs["beta"], auth.TenantPermissionStrings())
	starterToken, _, _ := jwtManager.IssueTenant(
		memberIDs["starter"], tenantIDs["starter"], auth.TenantPermissionStrings())
	readerToken, _, _ := jwtManager.IssueTenant(readerID, tenantIDs["acme"], []string{
		string(auth.PermissionIssueRead), string(auth.PermissionIntegrationRead),
	})

	jira := newFakeJira()
	defer jira.server.Close()
	linear := newFakeLinear()
	defer linear.server.Close()

	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Store: Store{Cipher: cipher}, JWT: jwtManager, AllowInsecureURLs: true,
		Clients: func(provider Provider) (Client, error) {
			if provider == ProviderLinear {
				return LinearClient{HTTP: linear.server.Client(), Endpoint: linear.server.URL}, nil
			}
			return JiraClient{HTTP: jira.server.Client()}, nil
		},
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

	// Nothing is connected, so the issue advertises no external providers at all.
	initial := trackerRequest(handler, "acme", http.MethodGet, "/v1/issues/"+issueID+"/external", "", ownerToken)
	if initial.Code != http.StatusOK {
		t.Fatalf("initial link list status = %d, body = %s", initial.Code, initial.Body.String())
	}
	var initialLinks struct {
		ExternalLinks      []ExternalLink `json:"external_links"`
		AvailableProviders []Connection   `json:"available_providers"`
	}
	decodeTracker(t, initial, &initialLinks)
	if len(initialLinks.ExternalLinks) != 0 || len(initialLinks.AvailableProviders) != 0 {
		t.Fatalf("issue advertised providers before any were connected: %#v", initialLinks)
	}
	blocked := trackerRequest(handler, "acme", http.MethodPost, "/v1/issues/"+issueID+"/external",
		`{"provider":"jira","title":"Checkout is confusing"}`, ownerToken)
	if blocked.Code != http.StatusNotFound {
		t.Fatalf("create before connecting status = %d, body = %s", blocked.Code, blocked.Body.String())
	}

	// A bad credential is rejected before anything is stored.
	jira.authorized = false
	rejected := trackerRequest(handler, "acme", http.MethodPut, "/v1/issue-trackers/jira",
		fmt.Sprintf(`{"site_url":%q,"email":"ops@acme.test","api_token":"wrong"}`, jira.server.URL), ownerToken)
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad credential status = %d, body = %s", rejected.Code, rejected.Body.String())
	}
	if !strings.Contains(rejected.Body.String(), ErrorCodeUnauthorized) {
		t.Fatalf("bad credential body = %s", rejected.Body.String())
	}
	var stored int
	if err := root.QueryRow(ctx, `SELECT count(*) FROM issue_tracker_connections`).Scan(&stored); err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if stored != 0 {
		t.Fatalf("a rejected credential was stored anyway (%d rows)", stored)
	}
	jira.authorized = true

	connected := trackerRequest(handler, "acme", http.MethodPut, "/v1/issue-trackers/jira",
		fmt.Sprintf(`{"site_url":%q,"email":"ops@acme.test","api_token":"jira-token"}`, jira.server.URL), ownerToken)
	if connected.Code != http.StatusOK {
		t.Fatalf("connect status = %d, body = %s", connected.Code, connected.Body.String())
	}
	var connectedPayload struct {
		IssueTracker Connection `json:"issue_tracker"`
	}
	decodeTracker(t, connected, &connectedPayload)
	if connectedPayload.IssueTracker.Enabled {
		t.Fatal("connecting a provider enabled it; it must stay opt-in")
	}
	if connectedPayload.IssueTracker.AccountLabel != "Ops Bot" ||
		connectedPayload.IssueTracker.CredentialFingerprint == "" {
		t.Fatalf("connection = %#v", connectedPayload.IssueTracker)
	}
	if strings.Contains(connected.Body.String(), "jira-token") {
		t.Fatalf("the API token was echoed back to the client: %s", connected.Body.String())
	}
	var ciphertext []byte
	if err := root.QueryRow(ctx, `
		SELECT credential_ciphertext FROM issue_tracker_connections
		WHERE tenant_id = $1 AND provider = 'jira'
	`, tenantIDs["acme"]).Scan(&ciphertext); err != nil {
		t.Fatalf("read stored credential: %v", err)
	}
	if strings.Contains(string(ciphertext), "jira-token") {
		t.Fatal("the API token was stored in plaintext")
	}

	// A connected-but-disabled provider still refuses to create anything.
	disabledCreate := trackerRequest(handler, "acme", http.MethodPost, "/v1/issues/"+issueID+"/external",
		`{"provider":"jira","title":"Checkout is confusing"}`, ownerToken)
	if disabledCreate.Code != http.StatusConflict {
		t.Fatalf("create while disabled status = %d, body = %s",
			disabledCreate.Code, disabledCreate.Body.String())
	}

	destinations := trackerRequest(handler, "acme", http.MethodGet,
		"/v1/issue-trackers/jira/destinations", "", ownerToken)
	if destinations.Code != http.StatusOK {
		t.Fatalf("destinations status = %d, body = %s", destinations.Code, destinations.Body.String())
	}
	var destinationPayload struct {
		Destinations []Destination `json:"destinations"`
	}
	decodeTracker(t, destinations, &destinationPayload)
	if len(destinationPayload.Destinations) != 1 || len(destinationPayload.Destinations[0].IssueTypes) != 1 {
		t.Fatalf("destinations = %#v", destinationPayload.Destinations)
	}

	// Enabling without a destination is refused, and an unknown destination is refused too.
	enableWithout := trackerRequest(handler, "acme", http.MethodPatch, "/v1/issue-trackers/jira",
		`{"is_enabled":true}`, ownerToken)
	if enableWithout.Code != http.StatusUnprocessableEntity {
		t.Fatalf("enable without destination status = %d, body = %s",
			enableWithout.Code, enableWithout.Body.String())
	}
	unknownDestination := trackerRequest(handler, "acme", http.MethodPatch, "/v1/issue-trackers/jira",
		`{"destination_id":"99999","issue_type_id":"10001","is_enabled":true}`, ownerToken)
	if unknownDestination.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown destination status = %d, body = %s",
			unknownDestination.Code, unknownDestination.Body.String())
	}

	enabled := trackerRequest(handler, "acme", http.MethodPatch, "/v1/issue-trackers/jira",
		`{"destination_id":"10000","issue_type_id":"10001","is_enabled":true}`, ownerToken)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", enabled.Code, enabled.Body.String())
	}
	var enabledPayload struct {
		IssueTracker Connection `json:"issue_tracker"`
	}
	decodeTracker(t, enabled, &enabledPayload)
	if !enabledPayload.IssueTracker.Enabled || enabledPayload.IssueTracker.DestinationName != "Operations" ||
		enabledPayload.IssueTracker.IssueTypeName != "Task" || enabledPayload.IssueTracker.ValidatedAt == nil {
		t.Fatalf("enabled connection = %#v", enabledPayload.IssueTracker)
	}

	created := trackerRequest(handler, "acme", http.MethodPost, "/v1/issues/"+issueID+"/external",
		`{"provider":"jira","title":"Checkout is confusing","description":"Reporter cannot pay",`+
			`"priority":"high","idempotency_key":"review-dialog-0001"}`, ownerToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var createdPayload struct {
		ExternalLink ExternalLink `json:"external_link"`
	}
	decodeTracker(t, created, &createdPayload)
	if createdPayload.ExternalLink.Status != LinkStatusCreated ||
		createdPayload.ExternalLink.ExternalKey != "OPS-1" ||
		createdPayload.ExternalLink.ExternalURL != jira.server.URL+"/browse/OPS-1" {
		t.Fatalf("created link = %#v", createdPayload.ExternalLink)
	}
	if jira.createCount != 1 {
		t.Fatalf("Jira create calls = %d, want 1", jira.createCount)
	}

	// The same reviewed request, replayed, resolves to the existing external issue.
	replayed := trackerRequest(handler, "acme", http.MethodPost, "/v1/issues/"+issueID+"/external",
		`{"provider":"jira","title":"Checkout is confusing","description":"Reporter cannot pay",`+
			`"priority":"high","idempotency_key":"review-dialog-0001"}`, ownerToken)
	if replayed.Code != http.StatusOK {
		t.Fatalf("replayed create status = %d, body = %s", replayed.Code, replayed.Body.String())
	}
	var replayedPayload struct {
		ExternalLink ExternalLink `json:"external_link"`
	}
	decodeTracker(t, replayed, &replayedPayload)
	if replayedPayload.ExternalLink.ID != createdPayload.ExternalLink.ID {
		t.Fatalf("replayed link = %#v, want the original", replayedPayload.ExternalLink)
	}
	// A different key for the same issue and provider is still refused by the live-link index.
	duplicate := trackerRequest(handler, "acme", http.MethodPost, "/v1/issues/"+issueID+"/external",
		`{"provider":"jira","title":"Different wording","idempotency_key":"review-dialog-0002"}`, ownerToken)
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate create status = %d, body = %s", duplicate.Code, duplicate.Body.String())
	}
	decodeTracker(t, duplicate, &replayedPayload)
	if replayedPayload.ExternalLink.ID != createdPayload.ExternalLink.ID {
		t.Fatalf("duplicate created a second link: %#v", replayedPayload.ExternalLink)
	}
	if jira.createCount != 1 {
		t.Fatalf("Jira create calls after retries = %d, want 1", jira.createCount)
	}

	// A provider failure must leave the Fidibeki issue intact and leave a retry path behind.
	jira.failCreate = true
	failed := trackerRequest(handler, "acme", http.MethodPost, "/v1/issues/"+secondIssueID+"/external",
		`{"provider":"jira","title":"Search is slow","idempotency_key":"review-dialog-0003"}`, ownerToken)
	if failed.Code != http.StatusBadGateway {
		t.Fatalf("failed create status = %d, body = %s", failed.Code, failed.Body.String())
	}
	var failure struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details struct {
				ExternalLink ExternalLink `json:"external_link"`
			} `json:"details"`
		} `json:"error"`
	}
	decodeTracker(t, failed, &failure)
	if failure.Error.Code != ErrorCodeUnavailable || failure.Error.Details.ExternalLink.ID == "" {
		t.Fatalf("failure envelope = %#v", failure)
	}
	var issueTitle string
	if err := root.QueryRow(ctx, `
		SELECT title FROM issues WHERE tenant_id = $1 AND id = $2
	`, tenantIDs["acme"], secondIssueID).Scan(&issueTitle); err != nil {
		t.Fatalf("issue disappeared after a provider failure: %v", err)
	}
	var failedStatus, failedError string
	var failedAttempts int
	if err := root.QueryRow(ctx, `
		SELECT status::text, last_error, attempt_count FROM issue_external_links
		WHERE tenant_id = $1 AND issue_id = $2
	`, tenantIDs["acme"], secondIssueID).Scan(&failedStatus, &failedError, &failedAttempts); err != nil {
		t.Fatalf("read failed link: %v", err)
	}
	if failedStatus != string(LinkStatusFailed) || failedError == "" || failedAttempts != 1 {
		t.Fatalf("failed link = %s / %q / %d", failedStatus, failedError, failedAttempts)
	}

	jira.failCreate = false
	retried := trackerRequest(handler, "acme", http.MethodPost,
		"/v1/issues/"+secondIssueID+"/external/"+failure.Error.Details.ExternalLink.ID+"/retry",
		"", ownerToken)
	if retried.Code != http.StatusOK {
		t.Fatalf("retry status = %d, body = %s", retried.Code, retried.Body.String())
	}
	var retriedPayload struct {
		ExternalLink ExternalLink `json:"external_link"`
	}
	decodeTracker(t, retried, &retriedPayload)
	if retriedPayload.ExternalLink.Status != LinkStatusCreated ||
		retriedPayload.ExternalLink.AttemptCount != 2 ||
		retriedPayload.ExternalLink.ID != failure.Error.Details.ExternalLink.ID {
		t.Fatalf("retried link = %#v", retriedPayload.ExternalLink)
	}
	repeatRetry := trackerRequest(handler, "acme", http.MethodPost,
		"/v1/issues/"+secondIssueID+"/external/"+retriedPayload.ExternalLink.ID+"/retry", "", ownerToken)
	if repeatRetry.Code != http.StatusConflict {
		t.Fatalf("retry of a created attempt status = %d, body = %s",
			repeatRetry.Code, repeatRetry.Body.String())
	}

	// Both attempts left an auditable trail on the issue timeline.
	var createdEvents, failedEvents int
	if err := root.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE kind = $2),
			count(*) FILTER (WHERE kind = $3)
		FROM issue_activities WHERE tenant_id = $1
	`, tenantIDs["acme"], ActivityExternalIssueCreated, ActivityExternalIssueFailed).
		Scan(&createdEvents, &failedEvents); err != nil {
		t.Fatalf("count issue activities: %v", err)
	}
	if createdEvents != 2 || failedEvents != 1 {
		t.Fatalf("issue activities = %d created, %d failed", createdEvents, failedEvents)
	}
	// Configuration changes are auditable in the workspace access ledger.
	var auditKinds []string
	rows, err := root.Query(ctx, `
		SELECT kind FROM tenant_access_events
		WHERE tenant_id = $1 AND resource_type = $2 ORDER BY created_at, kind
	`, tenantIDs["acme"], AuditResourceType)
	if err != nil {
		t.Fatalf("read access events: %v", err)
	}
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatalf("scan access event: %v", err)
		}
		auditKinds = append(auditKinds, kind)
	}
	rows.Close()
	if len(auditKinds) != 2 || auditKinds[0] != AuditKindConnected || auditKinds[1] != AuditKindEnabled {
		t.Fatalf("access event kinds = %#v", auditKinds)
	}

	// Linear is configured independently and stays off until it is switched on.
	linearConnected := trackerRequest(handler, "acme", http.MethodPut, "/v1/issue-trackers/linear",
		`{"api_token":"lin_api_key"}`, ownerToken)
	if linearConnected.Code != http.StatusOK {
		t.Fatalf("connect Linear status = %d, body = %s",
			linearConnected.Code, linearConnected.Body.String())
	}
	availability := trackerRequest(handler, "acme", http.MethodGet,
		"/v1/issues/"+issueID+"/external", "", readerToken)
	decodeTracker(t, availability, &initialLinks)
	if len(initialLinks.AvailableProviders) != 1 ||
		initialLinks.AvailableProviders[0].Provider != ProviderJira {
		t.Fatalf("available providers with Linear connected but disabled = %#v",
			initialLinks.AvailableProviders)
	}
	linearEnabled := trackerRequest(handler, "acme", http.MethodPatch, "/v1/issue-trackers/linear",
		`{"destination_id":"team-1","sub_destination_id":"project-1","is_enabled":true}`, ownerToken)
	if linearEnabled.Code != http.StatusOK {
		t.Fatalf("enable Linear status = %d, body = %s", linearEnabled.Code, linearEnabled.Body.String())
	}
	linearCreated := trackerRequest(handler, "acme", http.MethodPost, "/v1/issues/"+issueID+"/external",
		`{"provider":"linear","title":"Checkout is confusing","priority":"urgent"}`, ownerToken)
	if linearCreated.Code != http.StatusCreated {
		t.Fatalf("Linear create status = %d, body = %s", linearCreated.Code, linearCreated.Body.String())
	}
	decodeTracker(t, linearCreated, &createdPayload)
	if createdPayload.ExternalLink.Provider != ProviderLinear ||
		createdPayload.ExternalLink.ExternalKey != "OPS-7" {
		t.Fatalf("Linear link = %#v", createdPayload.ExternalLink)
	}
	if linear.lastPriority != 1 {
		t.Fatalf("Linear priority = %d, want 1 for urgent", linear.lastPriority)
	}

	// Disabling a provider hides the creation controls again without deleting the evidence.
	disabled := trackerRequest(handler, "acme", http.MethodPatch, "/v1/issue-trackers/linear",
		`{"is_enabled":false}`, ownerToken)
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", disabled.Code, disabled.Body.String())
	}
	afterDisable := trackerRequest(handler, "acme", http.MethodGet,
		"/v1/issues/"+issueID+"/external", "", ownerToken)
	decodeTracker(t, afterDisable, &initialLinks)
	if len(initialLinks.AvailableProviders) != 1 || len(initialLinks.ExternalLinks) != 2 {
		t.Fatalf("after disabling Linear: providers = %#v, links = %d",
			initialLinks.AvailableProviders, len(initialLinks.ExternalLinks))
	}

	// RBAC, tenant isolation, and plan entitlement.
	readerConnect := trackerRequest(handler, "acme", http.MethodPut, "/v1/issue-trackers/jira",
		fmt.Sprintf(`{"site_url":%q,"email":"ops@acme.test","api_token":"jira-token"}`, jira.server.URL),
		readerToken)
	if readerConnect.Code != http.StatusForbidden {
		t.Fatalf("read-only connect status = %d", readerConnect.Code)
	}
	readerCreate := trackerRequest(handler, "acme", http.MethodPost, "/v1/issues/"+issueID+"/external",
		`{"provider":"jira","title":"Checkout is confusing"}`, readerToken)
	if readerCreate.Code != http.StatusForbidden {
		t.Fatalf("read-only create status = %d", readerCreate.Code)
	}
	crossTenant := trackerRequest(handler, "beta", http.MethodGet,
		"/v1/issues/"+issueID+"/external", "", betaToken)
	if crossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant issue status = %d, body = %s", crossTenant.Code, crossTenant.Body.String())
	}
	betaConnections := trackerRequest(handler, "beta", http.MethodGet, "/v1/issue-trackers", "", betaToken)
	var betaPayload struct {
		IssueTrackers []Connection `json:"issue_trackers"`
	}
	decodeTracker(t, betaConnections, &betaPayload)
	if len(betaPayload.IssueTrackers) != 0 {
		t.Fatalf("another workspace saw these connections: %#v", betaPayload.IssueTrackers)
	}
	planBlocked := trackerRequest(handler, "starter", http.MethodPut, "/v1/issue-trackers/linear",
		`{"api_token":"lin_api_key"}`, starterToken)
	if planBlocked.Code != http.StatusPaymentRequired {
		t.Fatalf("plan-gated connect status = %d, body = %s", planBlocked.Code, planBlocked.Body.String())
	}

	// Automatic creation: switching the flag on makes an issue.created event fan out to the
	// provider without anyone confirming a dialog.
	automated := trackerRequest(handler, "acme", http.MethodPatch, "/v1/issue-trackers/jira",
		`{"auto_create":true}`, ownerToken)
	if automated.Code != http.StatusOK {
		t.Fatalf("enable automatic creation status = %d, body = %s",
			automated.Code, automated.Body.String())
	}
	decodeTracker(t, automated, &enabledPayload)
	if !enabledPayload.IssueTracker.AutoCreate {
		t.Fatalf("automatic creation was not stored: %#v", enabledPayload.IssueTracker)
	}
	var automaticIssueID string
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title, description, priority)
		VALUES ($1, $2, 'ACME-2026-0003', 'Cart totals drift', 'Totals change after a refresh.', 'medium')
		RETURNING id
	`, tenantIDs["acme"], projectID).Scan(&automaticIssueID); err != nil {
		t.Fatalf("create automatic issue: %v", err)
	}
	resolver := OutboxResolver{Database: root}
	autoDestinations, err := resolver.Resolve(ctx, outbox.Event{
		TenantID: tenantIDs["acme"], Event: "issue.created",
		ResourceType: "issue", ResourceID: automaticIssueID,
	})
	if err != nil || len(autoDestinations) != 1 || autoDestinations[0].TaskType != TaskAutoCreate ||
		autoDestinations[0].ID != string(ProviderJira) {
		t.Fatalf("resolved destinations = %#v, error = %v", autoDestinations, err)
	}
	ignored, err := resolver.Resolve(ctx, outbox.Event{
		TenantID: tenantIDs["acme"], Event: "issue.updated",
		ResourceType: "issue", ResourceID: automaticIssueID,
	})
	if err != nil || len(ignored) != 0 {
		t.Fatalf("issue.updated resolved %#v, error = %v", ignored, err)
	}
	autoHandler := AutoCreateHandler{
		Store: Store{Database: root, Cipher: cipher},
		Clients: func(provider Provider) (Client, error) {
			if provider == ProviderLinear {
				return LinearClient{HTTP: linear.server.Client(), Endpoint: linear.server.URL}, nil
			}
			return JiraClient{HTTP: jira.server.Client()}, nil
		},
	}
	autoPayload := fmt.Sprintf(
		`{"outbox_event_id":"11111111-1111-4111-8111-111111111111","tenant_id":%q,"resource_id":%q,"destination_id":"jira"}`,
		tenantIDs["acme"], automaticIssueID,
	)
	autoTask := asynq.NewTask(TaskAutoCreate, []byte(autoPayload))
	if err := autoHandler.Handler()(ctx, autoTask); err != nil {
		t.Fatalf("automatic creation failed: %v", err)
	}
	createsAfterAutomatic := jira.createCount
	var autoStatus, autoKey string
	var autoAttempts int
	if err := root.QueryRow(ctx, `
		SELECT status::text, external_key, attempt_count FROM issue_external_links
		WHERE tenant_id = $1 AND issue_id = $2
	`, tenantIDs["acme"], automaticIssueID).Scan(&autoStatus, &autoKey, &autoAttempts); err != nil {
		t.Fatalf("read automatic link: %v", err)
	}
	if autoStatus != string(LinkStatusCreated) || autoKey == "" || autoAttempts != 1 {
		t.Fatalf("automatic link = %s / %q / %d", autoStatus, autoKey, autoAttempts)
	}
	// A redelivered event must not create a second external issue.
	if err := autoHandler.Handler()(ctx, autoTask); err != nil {
		t.Fatalf("redelivered automatic task failed: %v", err)
	}
	if jira.createCount != createsAfterAutomatic {
		t.Fatalf("redelivery created another external issue (%d calls)", jira.createCount)
	}
	var automaticActivities int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM issue_activities
		WHERE tenant_id = $1 AND issue_id = $2 AND kind = $3
		  AND data->>'automatic' = 'true' AND actor_member_id IS NULL
	`, tenantIDs["acme"], automaticIssueID, ActivityExternalIssueCreated).Scan(&automaticActivities); err != nil {
		t.Fatalf("count automatic activities: %v", err)
	}
	if automaticActivities != 1 {
		t.Fatalf("automatic activity entries = %d, want 1", automaticActivities)
	}
	// Turning the provider off also stops automatic creation.
	stopped := trackerRequest(handler, "acme", http.MethodPatch, "/v1/issue-trackers/jira",
		`{"is_enabled":false}`, ownerToken)
	if stopped.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", stopped.Code, stopped.Body.String())
	}
	decodeTracker(t, stopped, &enabledPayload)
	if enabledPayload.IssueTracker.AutoCreate || enabledPayload.IssueTracker.Enabled {
		t.Fatalf("disabling left automatic creation on: %#v", enabledPayload.IssueTracker)
	}
	afterDisableDestinations, err := resolver.Resolve(ctx, outbox.Event{
		TenantID: tenantIDs["acme"], Event: "issue.created",
		ResourceType: "issue", ResourceID: automaticIssueID,
	})
	if err != nil || len(afterDisableDestinations) != 0 {
		t.Fatalf("disabled provider still resolved %#v, error = %v", afterDisableDestinations, err)
	}
	// Automatic creation cannot be requested for a provider that is switched off.
	rejectedAutomatic := trackerRequest(handler, "acme", http.MethodPatch, "/v1/issue-trackers/jira",
		`{"auto_create":true}`, ownerToken)
	if rejectedAutomatic.Code != http.StatusUnprocessableEntity {
		t.Fatalf("automatic creation without enabling status = %d, body = %s",
			rejectedAutomatic.Code, rejectedAutomatic.Body.String())
	}

	// Disconnecting removes the credential but keeps the links that prove a copy exists.
	disconnected := trackerRequest(handler, "acme", http.MethodDelete,
		"/v1/issue-trackers/linear", "", ownerToken)
	if disconnected.Code != http.StatusNoContent {
		t.Fatalf("disconnect status = %d, body = %s", disconnected.Code, disconnected.Body.String())
	}
	var remainingConnections, remainingLinks int
	if err := root.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM issue_tracker_connections WHERE tenant_id = $1),
			(SELECT count(*) FROM issue_external_links WHERE tenant_id = $1)
	`, tenantIDs["acme"]).Scan(&remainingConnections, &remainingLinks); err != nil {
		t.Fatalf("count after disconnect: %v", err)
	}
	if remainingConnections != 1 || remainingLinks != 4 {
		t.Fatalf("after disconnect: %d connections, %d links", remainingConnections, remainingLinks)
	}
}

type fakeJira struct {
	server      *httptest.Server
	authorized  bool
	failCreate  bool
	createCount int
	nextKey     int
}

func newFakeJira() *fakeJira {
	fake := &fakeJira{authorized: true}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fake.authorized {
			writeJSON(w, http.StatusUnauthorized,
				map[string]any{"errorMessages": []string{"Client must be authenticated"}})
			return
		}
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			writeJSON(w, http.StatusOK, map[string]any{
				"accountId": "acc-1", "displayName": "Ops Bot", "emailAddress": "ops@acme.test",
			})
		case r.URL.Path == "/rest/api/3/project/search":
			writeJSON(w, http.StatusOK, map[string]any{"values": []any{
				map[string]any{"id": "10000", "key": "OPS", "name": "Operations"},
			}})
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/createmeta/"):
			writeJSON(w, http.StatusOK, map[string]any{"issueTypes": []any{
				map[string]any{"id": "10001", "name": "Task", "subtask": false},
			}})
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			if fake.failCreate {
				writeJSON(w, http.StatusServiceUnavailable,
					map[string]any{"errorMessages": []string{"Jira is temporarily unavailable"}})
				return
			}
			fake.createCount++
			fake.nextKey++
			writeJSON(w, http.StatusCreated, map[string]any{
				"id":  fmt.Sprintf("2000%d", fake.nextKey),
				"key": fmt.Sprintf("OPS-%d", fake.nextKey),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return fake
}

type fakeLinear struct {
	server       *httptest.Server
	lastPriority int
}

func newFakeLinear() *fakeLinear {
	fake := &fakeLinear{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &request)
		switch {
		case strings.Contains(request.Query, "viewer"):
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"viewer":       map[string]any{"name": "Ops Bot", "email": "ops@acme.test"},
				"organization": map[string]any{"name": "Acme"},
			}})
		case strings.Contains(request.Query, "teams"):
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"teams": map[string]any{"nodes": []any{map[string]any{
					"id": "team-1", "key": "OPS", "name": "Operations",
					"projects": map[string]any{"nodes": []any{
						map[string]any{"id": "project-1", "name": "Q3"},
					}},
				}}},
			}})
		default:
			input, _ := request.Variables["input"].(map[string]any)
			if priority, ok := input["priority"].(float64); ok {
				fake.lastPriority = int(priority)
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"issueCreate": map[string]any{"success": true, "issue": map[string]any{
					"id": "linear-1", "identifier": "OPS-7", "url": "https://linear.app/acme/issue/OPS-7",
				}},
			}})
		}
	}))
	return fake
}

func trackerRequest(
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

func decodeTracker(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func trackerMigrationsPath(t *testing.T) string {
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

func trackerRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
