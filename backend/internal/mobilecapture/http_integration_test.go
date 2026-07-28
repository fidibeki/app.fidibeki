package mobilecapture

import (
	"context"
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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMobileCaptureHTTPFlow(t *testing.T) {
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
	migrator, err := db.NewMigrator(directURL, mobileMigrationsPath(t))
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
	appURL := mobileRoleURL(t, directURL, "fidibeki_app", "app-password")
	adminURL := mobileRoleURL(t, directURL, "fidibeki_admin", "admin-password")
	if err := db.ConfigureRuntimeRoles(ctx, directURL, appURL, adminURL); err != nil {
		t.Fatalf("configure runtime roles: %v", err)
	}

	var tenantID, projectID, ruleID, ownerID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (
			name, slug, plan, status, privacy_contact_email, privacy_rights_email,
			privacy_policy_url, privacy_profile_version
		) VALUES (
			'Acme', 'acme', 'growth', 'active', 'privacy@acme.test',
			'rights@acme.test', 'https://app.example.com/privacy', 1
		) RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	publicKey := "pk_live_ABCDEFGHIJKLMNOPQRSTUV"
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (
			tenant_id, name, public_key, allowed_domains, default_locale
		) VALUES ($1, 'Acme App', $2, '{app.example.com}', 'en')
		RETURNING id
	`, tenantID, publicKey).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds,
			can_attach_screenshot, definition_key, definition_version,
			definition_status, presentation, response_blocks, localizations,
			allow_comment, collect_contact
		) VALUES (
			$1, 'Mobile feedback', 'all_pages', 'delay', 0, true,
			'mobile-feedback', 1, 'active', 'launcher',
			'[{"key":"feedback","type":"free_text","required":true}]'::jsonb,
			'{
				"en": {
					"launcher_label":"Feedback","prompt":"How was it?",
					"comment_placeholder":"Tell us more","contact_label":"Email",
					"consent_label":"Email me updates","submit_label":"Send",
					"success_message":"Received",
					"privacy_purpose_summary":"Send feedback updates.",
					"privacy_lawful_basis_summary":"Consent.",
					"privacy_retention_summary":"Until withdrawal."
				}
			}'::jsonb, true, true
		) RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create widget definition: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2) RETURNING id
	`, tenantID, auth.TenantPermissionStrings()).Scan(&ownerID); err != nil {
		t.Fatalf("create tenant owner: %v", err)
	}
	rawAPIKey := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUV"
	var apiKeyID string
	if err := root.QueryRow(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions
		) VALUES ($1, 'Mobile SDK', $2, 'fk_live_0123...', $3)
		RETURNING id
	`, tenantID, auth.HashToken(rawAPIKey), []string{
		string(auth.PermissionMobileCaptureCreate),
	}).Scan(&apiKeyID); err != nil {
		t.Fatalf("create mobile API key: %v", err)
	}
	limitedAPIKey := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUW"
	if _, err := root.Exec(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions
		) VALUES ($1, 'Limited SDK', $2, 'fk_live_0123...', '{project:read}')
	`, tenantID, auth.HashToken(limitedAPIKey)); err != nil {
		t.Fatalf("create limited API key: %v", err)
	}

	pools, err := db.OpenPools(
		ctx, appURL, adminURL, 8, 4,
	)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwtManager, err := auth.NewJWTManager(
		strings.Repeat("s", 32), "fidibeki", 10*time.Minute,
	)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	humanToken, _, err := jwtManager.IssueTenant(
		ownerID, tenantID, auth.TenantPermissionStrings(),
	)
	if err != nil {
		t.Fatalf("issue human token: %v", err)
	}
	now := time.Now().UTC()
	store := Store{ProjectStore: project.Store{
		PublicBaseURL: "https://api.staging.fidibeki.com",
	}}
	tenantRoutes := chi.NewRouter()
	RegisterTenantRoutes(tenantRoutes, TenantHTTPOptions{
		Store: store, JWT: jwtManager, Now: func() time.Time { return now },
	})
	tenantHandler := appmiddleware.TenantAPIKeyRateLimit(
		appmiddleware.TenantAPIKeyRateLimitOptions{Limiter: allowCredentialLimiter{}},
	)(appmiddleware.TenantTransaction(
		appmiddleware.TenantTransactionOptions{
			BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
			Begin:  db.BeginFromPool(pools.App),
			Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		},
	)(tenantRoutes))
	objectStorage := &mobileStorageStub{object: storage.ObjectInfo{
		Size: 2048, ContentType: "image/png",
	}}
	newPublicHandler := func(
		evaluator AbuseEvaluator,
		objectStorage feedback.ObjectStorage,
	) http.Handler {
		routes := chi.NewRouter()
		RegisterPublicRoutes(routes, PublicHTTPOptions{
			Store: store, Resolver: pools.Admin, Begin: db.BeginFromPool(pools.App),
			// Deliberately drift the injected core clock. The mobile handler must
			// pin every deadline in one submission to its request timestamp.
			FeedbackCore: feedback.SubmissionCore{Store: feedback.Store{}, Now: func() time.Time {
				return now.Add(time.Millisecond)
			}},
			Storage: objectStorage, Abuse: evaluator,
			Now: func() time.Time { return now },
		})
		return routes
	}
	publicRoutes := newPublicHandler(allowMobileAbuse{}, objectStorage)

	forged := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("09000000-0000-4000-8000-000000000000", false),
		TokenPrefix+strings.Repeat("A", 43),
	)
	if forged.Code != http.StatusForbidden ||
		!strings.Contains(forged.Body.String(), "mobile capture denied") {
		t.Fatalf("forged capture status = %d, body = %s",
			forged.Code, forged.Body.String())
	}

	humanMint := performMobileRequest(
		tenantHandler, "acme", http.MethodPost, "/v1/mobile/capture-sessions",
		validMintBody(projectID, PlatformIOS), humanToken,
	)
	if humanMint.Code != http.StatusForbidden {
		t.Fatalf("human mint status = %d, body = %s", humanMint.Code, humanMint.Body.String())
	}
	limitedMint := performMobileRequest(
		tenantHandler, "acme", http.MethodPost, "/v1/mobile/capture-sessions",
		validMintBody(projectID, PlatformIOS), limitedAPIKey,
	)
	if limitedMint.Code != http.StatusForbidden {
		t.Fatalf("limited key mint status = %d, body = %s", limitedMint.Code, limitedMint.Body.String())
	}
	if _, err := root.Exec(ctx, `UPDATE tenants SET status = 'suspended' WHERE id = $1`, tenantID); err != nil {
		t.Fatalf("suspend tenant: %v", err)
	}
	inactiveMint := performMobileRequest(
		tenantHandler, "acme", http.MethodPost, "/v1/mobile/capture-sessions",
		validMintBody(projectID, PlatformIOS), rawAPIKey,
	)
	if inactiveMint.Code != http.StatusForbidden {
		t.Fatalf("inactive tenant mint status = %d, body = %s", inactiveMint.Code, inactiveMint.Body.String())
	}
	if _, err := root.Exec(ctx, `UPDATE tenants SET status = 'active' WHERE id = $1`, tenantID); err != nil {
		t.Fatalf("reactivate tenant: %v", err)
	}

	conflictToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformAndroid,
	)
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET definition_version = definition_version + 1 WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("mutate definition before submit: %v", err)
	}
	conflictSubmit := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("10000000-0000-4000-8000-000000000001", false),
		conflictToken,
	)
	if conflictSubmit.Code != http.StatusConflict ||
		!strings.Contains(conflictSubmit.Body.String(), "configuration_changed") {
		t.Fatalf("configuration conflict status = %d, body = %s",
			conflictSubmit.Code, conflictSubmit.Body.String())
	}

	controllerConflictToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	if _, err := root.Exec(ctx, `
		UPDATE tenants
		SET privacy_profile_version = privacy_profile_version + 1
		WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("mutate controller profile before submit: %v", err)
	}
	controllerConflict := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("11000000-0000-4000-8000-000000000001", false),
		controllerConflictToken,
	)
	if controllerConflict.Code != http.StatusConflict ||
		!strings.Contains(controllerConflict.Body.String(), "configuration_changed") {
		t.Fatalf("controller conflict status = %d, body = %s",
			controllerConflict.Code, controllerConflict.Body.String())
	}

	suspendedToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformAndroid,
	)
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET status = 'suspended' WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("suspend tenant after mint: %v", err)
	}
	suspendedSubmit := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("12000000-0000-4000-8000-000000000001", false),
		suspendedToken,
	)
	if suspendedSubmit.Code != http.StatusForbidden {
		t.Fatalf("post-mint suspended status = %d, body = %s",
			suspendedSubmit.Code, suspendedSubmit.Body.String())
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET status = 'active' WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("reactivate tenant after mint: %v", err)
	}
	deletingToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET deletion_requested_at = $2 WHERE id = $1
	`, tenantID, now); err != nil {
		t.Fatalf("mark tenant deleting after mint: %v", err)
	}
	deletingSubmit := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("13000000-0000-4000-8000-000000000001", false),
		deletingToken,
	)
	if deletingSubmit.Code != http.StatusForbidden {
		t.Fatalf("post-mint deleting status = %d, body = %s",
			deletingSubmit.Code, deletingSubmit.Body.String())
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET deletion_requested_at = NULL WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("restore tenant deletion state: %v", err)
	}

	failClosedToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformAndroid,
	)
	failClosedID := "14000000-0000-4000-8000-000000000001"
	failClosed := performMobileRequest(
		newPublicHandler(
			selectiveMobileAbuse{unavailablePolicy: abuse.MobileSubmissionPolicy.Name},
			objectStorage,
		),
		"", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody(failClosedID, false), failClosedToken,
	)
	if failClosed.Code != http.StatusServiceUnavailable {
		t.Fatalf("fail-closed abuse status = %d, body = %s",
			failClosed.Code, failClosed.Body.String())
	}
	var blockedCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks
		WHERE project_id = $1 AND client_feedback_id = $2
	`, projectID, failClosedID).Scan(&blockedCount); err != nil || blockedCount != 0 {
		t.Fatalf("fail-closed abuse feedback count = %d, error = %v", blockedCount, err)
	}

	degradedToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	degradedID := "15000000-0000-4000-8000-000000000001"
	degraded := performMobileRequest(
		newPublicHandler(
			selectiveMobileAbuse{unavailablePolicy: abuse.MobileAttachmentGrantPolicy.Name},
			objectStorage,
		),
		"", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody(degradedID, true), degradedToken,
	)
	if degraded.Code != http.StatusCreated {
		t.Fatalf("attachment degradation status = %d, body = %s",
			degraded.Code, degraded.Body.String())
	}
	var degradedSubmission feedback.SubmissionResponse
	if err := json.NewDecoder(degraded.Body).Decode(&degradedSubmission); err != nil {
		t.Fatalf("decode attachment degradation: %v", err)
	}
	if len(degradedSubmission.Attachments) != 0 ||
		len(degradedSubmission.Warnings) != 1 ||
		degradedSubmission.Warnings[0].Reason != abuse.ReasonUploadRateLimited {
		t.Fatalf("attachment degradation response = %#v", degradedSubmission)
	}
	var degradedAttachmentCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM attachments WHERE feedback_id = $1
	`, degradedSubmission.FeedbackID).Scan(&degradedAttachmentCount); err != nil ||
		degradedAttachmentCount != 0 {
		t.Fatalf("degraded attachment count = %d, error = %v",
			degradedAttachmentCount, err)
	}

	mainToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	var storedHash, storedPlatform string
	if err := root.QueryRow(ctx, `
		SELECT token_hash, platform::text
		FROM mobile_capture_sessions WHERE token_hash = $1
	`, auth.HashToken(mainToken)).Scan(&storedHash, &storedPlatform); err != nil {
		t.Fatalf("load capture token metadata: %v", err)
	}
	if storedHash == mainToken || storedPlatform != "ios" ||
		!strings.HasPrefix(mainToken, TokenPrefix) {
		t.Fatalf("stored session token = %q, platform = %q", storedHash, storedPlatform)
	}

	resolvedMain, err := store.ResolveToken(ctx, pools.Admin, mainToken)
	if err != nil {
		t.Fatalf("resolve main capture token: %v", err)
	}
	lockTx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin capture lifecycle lock transaction: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `
		SELECT set_config('app.tenant_id', $1, true)
	`, tenantID); err != nil {
		_ = lockTx.Rollback(ctx)
		t.Fatalf("set capture lifecycle lock tenant: %v", err)
	}
	lockedSession, err := store.LockSession(ctx, lockTx, resolvedMain, mainToken)
	if err != nil {
		_ = lockTx.Rollback(ctx)
		t.Fatalf("lock capture lifecycle: %v", err)
	}
	if !lockedSession.AuthorizedAt(now) {
		_ = lockTx.Rollback(ctx)
		t.Fatal("freshly locked capture lifecycle was not authorized")
	}
	revocationConn, err := pgx.Connect(ctx, directURL)
	if err != nil {
		_ = lockTx.Rollback(ctx)
		t.Fatalf("connect lifecycle revocation writer: %v", err)
	}
	revocationCtx, cancelRevocation := context.WithTimeout(ctx, 150*time.Millisecond)
	_, revocationErr := revocationConn.Exec(revocationCtx, `
		UPDATE tenants SET status = 'suspended' WHERE id = $1
	`, tenantID)
	cancelRevocation()
	if revocationErr == nil {
		_ = revocationConn.Close(ctx)
		_ = lockTx.Rollback(ctx)
		t.Fatal("tenant lifecycle mutation was not serialized by capture authorization")
	}
	if err := lockTx.Rollback(ctx); err != nil {
		_ = revocationConn.Close(ctx)
		t.Fatalf("release capture lifecycle lock: %v", err)
	}
	_ = revocationConn.Close(ctx)
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET status = 'active' WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("mutate tenant lifecycle after release: %v", err)
	}

	staleClockToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	staleResolved, err := store.ResolveToken(ctx, pools.Admin, staleClockToken)
	if err != nil {
		t.Fatalf("resolve stale-clock capture token: %v", err)
	}
	blockerConn, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect stale-clock blocker: %v", err)
	}
	blockerTx, err := blockerConn.Begin(ctx)
	if err != nil {
		_ = blockerConn.Close(ctx)
		t.Fatalf("begin stale-clock blocker: %v", err)
	}
	var blockedCaptureID string
	if err := blockerTx.QueryRow(ctx, `
		SELECT id FROM mobile_capture_sessions WHERE id = $1 FOR UPDATE
	`, staleResolved.SessionID).Scan(&blockedCaptureID); err != nil {
		_ = blockerTx.Rollback(ctx)
		_ = blockerConn.Close(ctx)
		t.Fatalf("lock stale-clock capture row: %v", err)
	}
	var staleClock atomic.Value
	staleClock.Store(now)
	staleNow := func() time.Time { return staleClock.Load().(time.Time) }
	staleRoutes := chi.NewRouter()
	RegisterPublicRoutes(staleRoutes, PublicHTTPOptions{
		Store: store, Resolver: pools.Admin, Begin: db.BeginFromPool(pools.App),
		FeedbackCore: feedback.SubmissionCore{
			Store: feedback.Store{}, Now: staleNow,
		},
		Storage: objectStorage, Abuse: allowMobileAbuse{}, Now: staleNow,
	})
	staleResponse := make(chan *httptest.ResponseRecorder, 1)
	staleClientID := "a0500000-0000-4000-8000-00000000000a"
	go func() {
		staleResponse <- performMobileRequest(
			staleRoutes, "", http.MethodPost, "/v1/mobile/feedback",
			validSubmissionBody(staleClientID, false), staleClockToken,
		)
	}()
	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWait()
	for {
		var waiting bool
		if err := root.QueryRow(waitCtx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE usename = 'fidibeki_app'
				  AND wait_event_type = 'Lock'
				  AND query ILIKE '%mobile_capture_sessions%'
			)
		`).Scan(&waiting); err != nil {
			_ = blockerTx.Rollback(ctx)
			_ = blockerConn.Close(ctx)
			t.Fatalf("observe stale-clock wait: %v", err)
		}
		if waiting {
			break
		}
		select {
		case <-waitCtx.Done():
			_ = blockerTx.Rollback(ctx)
			_ = blockerConn.Close(ctx)
			t.Fatalf("mobile submission did not wait on capture lock: %v", waitCtx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	staleClock.Store(now.Add(SubmitTTL + time.Second))
	if err := blockerTx.Rollback(ctx); err != nil {
		_ = blockerConn.Close(ctx)
		t.Fatalf("release stale-clock blocker: %v", err)
	}
	if err := blockerConn.Close(ctx); err != nil {
		t.Fatalf("close stale-clock blocker: %v", err)
	}
	staleResult := <-staleResponse
	if staleResult.Code != http.StatusForbidden {
		t.Fatalf("post-wait expired submit status = %d, body = %s",
			staleResult.Code, staleResult.Body.String())
	}
	var staleFeedbackCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks
		WHERE project_id = $1 AND client_feedback_id = $2
	`, projectID, staleClientID).Scan(&staleFeedbackCount); err != nil ||
		staleFeedbackCount != 0 {
		t.Fatalf("post-wait expired feedback count = %d, error = %v",
			staleFeedbackCount, err)
	}

	uuidVariantToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformAndroid,
	)
	uuidVariantBody := validSubmissionBody(
		"{A1000000-0000-4000-8000-00000000000A}", false,
	)
	uuidVariantCreated := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		uuidVariantBody, uuidVariantToken,
	)
	if uuidVariantCreated.Code != http.StatusCreated {
		t.Fatalf("UUID variant submit status = %d, body = %s",
			uuidVariantCreated.Code, uuidVariantCreated.Body.String())
	}
	uuidVariantRetry := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		uuidVariantBody, uuidVariantToken,
	)
	if uuidVariantRetry.Code != http.StatusOK {
		t.Fatalf("UUID variant retry status = %d, body = %s",
			uuidVariantRetry.Code, uuidVariantRetry.Body.String())
	}

	clientFeedbackID := "20000000-0000-4000-8000-000000000002"
	submissionBody := validSubmissionBody(clientFeedbackID, true)
	created := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		submissionBody, mainToken,
	)
	if created.Code != http.StatusCreated {
		t.Fatalf("mobile submit status = %d, body = %s", created.Code, created.Body.String())
	}
	var first feedback.SubmissionResponse
	if err := json.NewDecoder(created.Body).Decode(&first); err != nil {
		t.Fatalf("decode mobile submission: %v", err)
	}
	if first.FeedbackID == "" || first.IssuePublicID != "" || len(first.Attachments) != 1 {
		t.Fatalf("mobile submission response = %#v", first)
	}
	var source, applicationID, screen, appVersion, osVersion, pageURL string
	var issueCount, outboxCount int
	if err := root.QueryRow(ctx, `
		SELECT source::text, mobile_application_id, mobile_screen,
		       mobile_app_version, mobile_os_version, page_url
		FROM feedbacks WHERE id = $1
	`, first.FeedbackID).Scan(
		&source, &applicationID, &screen, &appVersion, &osVersion, &pageURL,
	); err != nil {
		t.Fatalf("load mobile feedback: %v", err)
	}
	if source != "ios" || applicationID != "com.example.fidibeki" ||
		screen != "/chart" || appVersion != "2.4.0" || osVersion != "18.5" ||
		pageURL != "https://app.example.com/chart" {
		t.Fatalf("mobile feedback context = %q %q %q %q %q %q",
			source, applicationID, screen, appVersion, osVersion, pageURL)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks WHERE id = $1 AND issue_id IS NOT NULL
	`, first.FeedbackID).Scan(&issueCount); err != nil {
		t.Fatalf("count feedback issues: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE resource_type = 'feedback' AND resource_id = $1
	`, first.FeedbackID).Scan(&outboxCount); err != nil {
		t.Fatalf("count feedback outbox: %v", err)
	}
	if issueCount != 0 || outboxCount != 1 {
		t.Fatalf("mobile feedback issue/outbox counts = %d/%d, want 0/1", issueCount, outboxCount)
	}

	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET enabled = false, definition_status = 'paused',
		    definition_version = definition_version + 1
		WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("pause accepted definition: %v", err)
	}
	complete := performMobileRequest(
		publicRoutes, "", http.MethodPost,
		"/v1/mobile/attachments/"+first.Attachments[0].AttachmentID+"/complete",
		fmt.Sprintf(`{"complete_token":%q}`, first.Attachments[0].CompleteToken),
		mainToken,
	)
	if complete.Code != http.StatusNoContent {
		t.Fatalf("attachment complete status = %d, body = %s", complete.Code, complete.Body.String())
	}
	var completed bool
	if err := root.QueryRow(ctx, `
		SELECT completed_at IS NOT NULL FROM attachments WHERE id = $1
	`, first.Attachments[0].AttachmentID).Scan(&completed); err != nil || !completed {
		t.Fatalf("attachment completed = %v, error = %v", completed, err)
	}
	retry := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		submissionBody, mainToken,
	)
	if retry.Code != http.StatusOK {
		t.Fatalf("exact retry status = %d, body = %s", retry.Code, retry.Body.String())
	}
	changedBody := strings.Replace(
		submissionBody, "The chart is difficult to read", "The chart is now easy to read", 1,
	)
	changedDigest := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		changedBody, mainToken,
	)
	if changedDigest.Code != http.StatusConflict {
		t.Fatalf("changed-digest retry status = %d, body = %s",
			changedDigest.Code, changedDigest.Body.String())
	}

	differentID := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("30000000-0000-4000-8000-000000000003", false),
		mainToken,
	)
	if differentID.Code != http.StatusConflict {
		t.Fatalf("different-id retry status = %d, body = %s",
			differentID.Code, differentID.Body.String())
	}
	duplicateJSON := strings.Replace(
		validSubmissionBody(clientFeedbackID, false),
		`"responses":`, `"responses":{"feedback":"outer"},"responses":`, 1,
	)
	duplicate := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		duplicateJSON, mainToken,
	)
	if duplicate.Code != http.StatusBadRequest {
		t.Fatalf("duplicate JSON status = %d, body = %s", duplicate.Code, duplicate.Body.String())
	}

	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET enabled = true, definition_status = 'active' WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("reactivate definition: %v", err)
	}
	concurrentRetryToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	concurrentRetryBody := validSubmissionBody(
		"30500000-0000-4000-8000-000000000003", false,
	)
	concurrentRetryStatuses := make(chan int, 2)
	var concurrentRetry sync.WaitGroup
	for range 2 {
		concurrentRetry.Add(1)
		go func() {
			defer concurrentRetry.Done()
			response := performMobileRequest(
				publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
				concurrentRetryBody, concurrentRetryToken,
			)
			concurrentRetryStatuses <- response.Code
		}()
	}
	concurrentRetry.Wait()
	close(concurrentRetryStatuses)
	concurrentRetryCounts := map[int]int{}
	for status := range concurrentRetryStatuses {
		concurrentRetryCounts[status]++
	}
	if concurrentRetryCounts[http.StatusCreated] != 1 ||
		concurrentRetryCounts[http.StatusOK] != 1 {
		t.Fatalf("concurrent exact-retry statuses = %#v, want one 201 and one 200",
			concurrentRetryCounts)
	}

	collisionToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformAndroid,
	)
	collision := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		submissionBody, collisionToken,
	)
	if collision.Code != http.StatusConflict {
		t.Fatalf("cross-session collision status = %d, body = %s",
			collision.Code, collision.Body.String())
	}
	var feedbackCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks WHERE project_id = $1 AND client_feedback_id = $2
	`, projectID, clientFeedbackID).Scan(&feedbackCount); err != nil || feedbackCount != 1 {
		t.Fatalf("idempotent feedback count = %d, error = %v", feedbackCount, err)
	}

	webClientID := "31000000-0000-4000-8000-000000000003"
	var webFeedbackID, webAttachmentID string
	const webTokenHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, widget_rule_id, public_id, client_feedback_id,
			title, description, text, responses, page_url
		) VALUES (
			$1, $2, $3, 'fb_WEBCOLL1', $4,
			'Web collision', 'Web collision', 'Web collision', '{}'::jsonb,
			'https://app.example.com/chart'
		) RETURNING id
	`, tenantID, projectID, ruleID, webClientID).Scan(&webFeedbackID); err != nil {
		t.Fatalf("seed web collision feedback: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, complete_token_hash
		) VALUES (
			$1, $2, 'screenshot', gen_random_uuid(), $3,
			'image/png', 2048, $4
		) RETURNING id
	`, tenantID, webFeedbackID, "feedback/"+webFeedbackID+"/web-collision.png",
		webTokenHash,
	).Scan(&webAttachmentID); err != nil {
		t.Fatalf("seed web collision attachment: %v", err)
	}
	var webUploadExpiry time.Time
	if err := root.QueryRow(ctx, `
		SELECT upload_expires_at FROM attachments WHERE id = $1
	`, webAttachmentID).Scan(&webUploadExpiry); err != nil {
		t.Fatalf("read web collision grant: %v", err)
	}
	webCollisionToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	webCollision := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody(webClientID, true), webCollisionToken,
	)
	if webCollision.Code != http.StatusConflict {
		t.Fatalf("web/mobile collision status = %d, body = %s",
			webCollision.Code, webCollision.Body.String())
	}
	var collisionHash string
	var collisionGeneration int
	var collisionExpiry time.Time
	if err := root.QueryRow(ctx, `
		SELECT complete_token_hash, verification_generation, upload_expires_at
		FROM attachments WHERE id = $1
	`, webAttachmentID).Scan(
		&collisionHash, &collisionGeneration, &collisionExpiry,
	); err != nil {
		t.Fatalf("read web grant after collision: %v", err)
	}
	if collisionHash != webTokenHash || collisionGeneration != 1 ||
		!collisionExpiry.Equal(webUploadExpiry) {
		t.Fatalf("collision rotated attachment grant = hash %q generation %d expiry %s",
			collisionHash, collisionGeneration, collisionExpiry)
	}

	concurrentIOS := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	concurrentAndroid := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformAndroid,
	)
	concurrentID := "50000000-0000-4000-8000-000000000005"
	concurrentBody := validSubmissionBody(concurrentID, false)
	statuses := make(chan int, 2)
	var concurrent sync.WaitGroup
	for _, token := range []string{concurrentIOS, concurrentAndroid} {
		concurrent.Add(1)
		go func() {
			defer concurrent.Done()
			response := performMobileRequest(
				publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
				concurrentBody, token,
			)
			statuses <- response.Code
		}()
	}
	concurrent.Wait()
	close(statuses)
	statusCounts := map[int]int{}
	for status := range statuses {
		statusCounts[status]++
	}
	if statusCounts[http.StatusCreated] != 1 || statusCounts[http.StatusConflict] != 1 {
		t.Fatalf("concurrent submission statuses = %#v, want one 201 and one 409", statusCounts)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks WHERE project_id = $1 AND client_feedback_id = $2
	`, projectID, concurrentID).Scan(&feedbackCount); err != nil || feedbackCount != 1 {
		t.Fatalf("concurrent feedback count = %d, error = %v", feedbackCount, err)
	}

	mismatchToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformAndroid,
	)
	mismatchBody := validSubmissionBody(
		"51000000-0000-4000-8000-000000000005", true,
	)
	mismatchCreated := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		mismatchBody, mismatchToken,
	)
	if mismatchCreated.Code != http.StatusCreated {
		t.Fatalf("mismatch submission status = %d, body = %s",
			mismatchCreated.Code, mismatchCreated.Body.String())
	}
	var mismatchSubmission feedback.SubmissionResponse
	if err := json.NewDecoder(mismatchCreated.Body).Decode(&mismatchSubmission); err != nil {
		t.Fatalf("decode mismatch submission: %v", err)
	}
	if len(mismatchSubmission.Attachments) != 1 {
		t.Fatalf("mismatch submission attachments = %#v", mismatchSubmission.Attachments)
	}
	objectStorage.object.Size = 1024
	mismatchedCompletion := performMobileRequest(
		publicRoutes, "", http.MethodPost,
		"/v1/mobile/attachments/"+mismatchSubmission.Attachments[0].AttachmentID+"/complete",
		fmt.Sprintf(
			`{"complete_token":%q}`,
			mismatchSubmission.Attachments[0].CompleteToken,
		),
		mismatchToken,
	)
	objectStorage.object.Size = 2048
	if mismatchedCompletion.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(mismatchedCompletion.Body.String(), "size_bytes") {
		t.Fatalf("mismatched completion status = %d, body = %s",
			mismatchedCompletion.Code, mismatchedCompletion.Body.String())
	}
	if _, err := root.Exec(ctx, `
		UPDATE attachments
		SET upload_refreshed_at = $2, upload_expires_at = $3
		WHERE id = $1
	`, mismatchSubmission.Attachments[0].AttachmentID,
		now.Add(-2*time.Minute), now.Add(-time.Minute),
	); err != nil {
		t.Fatalf("expire mismatched attachment: %v", err)
	}
	expiredCompletion := performMobileRequest(
		publicRoutes, "", http.MethodPost,
		"/v1/mobile/attachments/"+mismatchSubmission.Attachments[0].AttachmentID+"/complete",
		fmt.Sprintf(
			`{"complete_token":%q}`,
			mismatchSubmission.Attachments[0].CompleteToken,
		),
		mismatchToken,
	)
	if expiredCompletion.Code != http.StatusGone ||
		!strings.Contains(expiredCompletion.Body.String(), abuse.ReasonUploadExpired) {
		t.Fatalf("expired completion status = %d, body = %s",
			expiredCompletion.Code, expiredCompletion.Body.String())
	}

	deadlineToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	deadlineBody := validSubmissionBody(
		"60000000-0000-4000-8000-000000000006", true,
	)
	deadlineCreated := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		deadlineBody, deadlineToken,
	)
	if deadlineCreated.Code != http.StatusCreated {
		t.Fatalf("deadline submission status = %d, body = %s",
			deadlineCreated.Code, deadlineCreated.Body.String())
	}
	var deadlineSubmission feedback.SubmissionResponse
	if err := json.NewDecoder(deadlineCreated.Body).Decode(&deadlineSubmission); err != nil {
		t.Fatalf("decode deadline submission: %v", err)
	}
	if len(deadlineSubmission.Attachments) != 1 {
		t.Fatalf("deadline submission attachments = %#v", deadlineSubmission.Attachments)
	}
	if _, err := root.Exec(ctx, `
		UPDATE mobile_capture_sessions
		SET accepted_at = $2, retry_expires_at = $3
		WHERE token_hash = $1
	`, auth.HashToken(deadlineToken), now.Add(-2*time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatalf("separate retry deadline: %v", err)
	}
	expiredRetry := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		deadlineBody, deadlineToken,
	)
	if expiredRetry.Code != http.StatusForbidden {
		t.Fatalf("expired retry status = %d, body = %s",
			expiredRetry.Code, expiredRetry.Body.String())
	}
	independentCompletion := performMobileRequest(
		publicRoutes, "", http.MethodPost,
		"/v1/mobile/attachments/"+deadlineSubmission.Attachments[0].AttachmentID+"/complete",
		fmt.Sprintf(
			`{"complete_token":%q}`,
			deadlineSubmission.Attachments[0].CompleteToken,
		),
		deadlineToken,
	)
	if independentCompletion.Code != http.StatusNoContent {
		t.Fatalf("completion after retry expiry status = %d, body = %s",
			independentCompletion.Code, independentCompletion.Body.String())
	}
	foreignCompletion := performMobileRequest(
		publicRoutes, "", http.MethodPost,
		"/v1/mobile/attachments/"+deadlineSubmission.Attachments[0].AttachmentID+"/complete",
		fmt.Sprintf(
			`{"complete_token":%q}`,
			deadlineSubmission.Attachments[0].CompleteToken,
		),
		mainToken,
	)
	if foreignCompletion.Code != http.StatusNotFound {
		t.Fatalf("foreign attachment status = %d, body = %s",
			foreignCompletion.Code, foreignCompletion.Body.String())
	}

	deletedIssuerRaw := "fk_live_Z123456789ABCDEFGHIJKLMNOPQRSTUV"
	var deletedIssuerID string
	if err := root.QueryRow(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions
		) VALUES (
			$1, 'Disposable mobile SDK', $2, 'fk_live_Z123...', $3
		) RETURNING id
	`, tenantID, auth.HashToken(deletedIssuerRaw), []string{
		string(auth.PermissionMobileCaptureCreate),
	}).Scan(&deletedIssuerID); err != nil {
		t.Fatalf("create disposable issuer: %v", err)
	}
	deletedIssuerToken := mintMobileSession(
		t, tenantHandler, root, projectID, deletedIssuerRaw, PlatformAndroid,
	)
	if _, err := root.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, deletedIssuerID); err != nil {
		t.Fatalf("delete disposable issuer: %v", err)
	}
	deletedIssuerSubmit := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("61000000-0000-4000-8000-000000000006", false),
		deletedIssuerToken,
	)
	if deletedIssuerSubmit.Code != http.StatusForbidden {
		t.Fatalf("deleted issuer status = %d, body = %s",
			deletedIssuerSubmit.Code, deletedIssuerSubmit.Body.String())
	}

	if _, err := root.Exec(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, widget_rule_id, public_id, client_feedback_id,
			title, description, text, responses, page_url
		)
		SELECT $1, $2, $3, 'fb_Q' || lpad(sequence::text, 7, '0'),
		       gen_random_uuid(), 'Quota fixture', 'Quota fixture',
		       'Quota fixture', '{}'::jsonb, 'https://app.example.com/chart'
		FROM generate_series(1, 500) AS sequence
	`, tenantID, projectID, ruleID); err != nil {
		t.Fatalf("seed plan quota: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET plan = 'starter' WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("set starter quota: %v", err)
	}
	planToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformIOS,
	)
	planLimited := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("62000000-0000-4000-8000-000000000006", false),
		planToken,
	)
	if planLimited.Code != http.StatusPaymentRequired ||
		!strings.Contains(planLimited.Body.String(), "plan_limit_exceeded") {
		t.Fatalf("plan quota status = %d, body = %s",
			planLimited.Code, planLimited.Body.String())
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET plan = 'growth' WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("restore growth plan: %v", err)
	}

	cleanupHashes := []string{
		strings.Repeat("b", 64),
		strings.Repeat("c", 64),
		strings.Repeat("d", 64),
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO mobile_capture_sessions (
			tenant_id, project_id, widget_rule_id, issuer_api_key_id, token_hash,
			platform, application_id, screen, display_locale,
			definition_version, controller_profile_version,
			created_at, submit_expires_at, cleanup_after
		) VALUES (
			$1, $2, $3, $4, $5, 'ios', 'com.example.fidibeki', '/chart', 'en',
			1, 2, $6::timestamptz - interval '4 hours',
			$6::timestamptz - interval '3 hours',
			$6::timestamptz - interval '2 hours'
		)
	`, tenantID, projectID, ruleID, apiKeyID, cleanupHashes[0], now); err != nil {
		t.Fatalf("seed oldest cleanup session: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO mobile_capture_sessions (
			tenant_id, project_id, widget_rule_id, issuer_api_key_id, token_hash,
			platform, application_id, screen, display_locale,
			definition_version, controller_profile_version, feedback_id,
			client_feedback_id, request_digest, accepted_at, retry_expires_at,
			created_at, submit_expires_at, cleanup_after
		) VALUES (
			$1, $2, $3, $4, $5, 'android', 'com.example.fidibeki', '/chart', 'en',
			1, 2, $6, '63000000-0000-4000-8000-000000000006',
			$7, $8::timestamptz - interval '150 minutes',
			$8::timestamptz - interval '2 hours',
			$8::timestamptz - interval '4 hours',
			$8::timestamptz - interval '3 hours',
			$8::timestamptz - interval '1 hour'
		)
	`, tenantID, projectID, ruleID, apiKeyID, cleanupHashes[1], first.FeedbackID,
		strings.Repeat("e", 64), now); err != nil {
		t.Fatalf("seed bound cleanup session: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO mobile_capture_sessions (
			tenant_id, project_id, widget_rule_id, issuer_api_key_id, token_hash,
			platform, application_id, screen, display_locale,
			definition_version, controller_profile_version,
			created_at, submit_expires_at, cleanup_after
		) VALUES (
			$1, $2, $3, $4, $5, 'ios', 'com.example.fidibeki', '/chart', 'en',
			1, 2, $6::timestamptz, $6::timestamptz + interval '10 minutes',
			$6::timestamptz + interval '24 hours'
		)
	`, tenantID, projectID, ruleID, apiKeyID, cleanupHashes[2], now); err != nil {
		t.Fatalf("seed live cleanup session: %v", err)
	}
	maintenance := MaintenanceStore{Database: pools.Admin, Store: store}
	deleted, err := maintenance.DeleteExpired(ctx, now, 1)
	if err != nil || deleted != 1 {
		t.Fatalf("delete oldest cleanup session = %d, error = %v", deleted, err)
	}
	var cleanupRemaining int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM mobile_capture_sessions WHERE token_hash = ANY($1)
	`, cleanupHashes).Scan(&cleanupRemaining); err != nil || cleanupRemaining != 2 {
		t.Fatalf("cleanup rows after first batch = %d, error = %v",
			cleanupRemaining, err)
	}
	deleted, err = maintenance.DeleteExpired(ctx, now, 10)
	if err != nil || deleted != 1 {
		t.Fatalf("delete bound cleanup session = %d, error = %v", deleted, err)
	}
	var liveCleanupRows int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM mobile_capture_sessions WHERE token_hash = $1
	`, cleanupHashes[2]).Scan(&liveCleanupRows); err != nil || liveCleanupRows != 1 {
		t.Fatalf("live cleanup rows = %d, error = %v", liveCleanupRows, err)
	}

	firstDeadlineToken := mintMobileSession(
		t, tenantHandler, root, projectID, rawAPIKey, PlatformAndroid,
	)
	now = now.Add(SubmitTTL + time.Second)
	expiredFirstSubmit := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		validSubmissionBody("70000000-0000-4000-8000-000000000007", false),
		firstDeadlineToken,
	)
	if expiredFirstSubmit.Code != http.StatusForbidden {
		t.Fatalf("expired first-submit status = %d, body = %s",
			expiredFirstSubmit.Code, expiredFirstSubmit.Body.String())
	}

	if _, err := root.Exec(ctx, `
		UPDATE api_keys SET expires_at = $2 WHERE id = $1
	`, apiKeyID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("expire issuer API key: %v", err)
	}
	revoked := performMobileRequest(
		publicRoutes, "", http.MethodPost, "/v1/mobile/feedback",
		submissionBody, mainToken,
	)
	if revoked.Code != http.StatusForbidden ||
		!strings.Contains(revoked.Body.String(), "mobile capture denied") {
		t.Fatalf("revoked issuer status = %d, body = %s", revoked.Code, revoked.Body.String())
	}
}

type allowMobileAbuse struct{}

func (allowMobileAbuse) Evaluate(
	_ context.Context,
	policy abuse.Policy,
	_ abuse.Request,
) (abuse.Decision, error) {
	return abuse.Decision{
		Allowed: true, Reason: policy.Reason, Limit: 1000, Remaining: 999,
	}, nil
}

type selectiveMobileAbuse struct {
	unavailablePolicy string
}

func (evaluator selectiveMobileAbuse) Evaluate(
	_ context.Context,
	policy abuse.Policy,
	_ abuse.Request,
) (abuse.Decision, error) {
	if policy.Name == evaluator.unavailablePolicy {
		return abuse.Decision{
			Unavailable: true, Reason: policy.Reason,
		}, nil
	}
	return abuse.Decision{
		Allowed: true, Reason: policy.Reason, Limit: 1000, Remaining: 999,
	}, nil
}

type allowCredentialLimiter struct{}

func (allowCredentialLimiter) Allow(
	_ context.Context,
	_ string,
	window ratelimit.Window,
) (ratelimit.Decision, error) {
	return ratelimit.Decision{
		Allowed: true, Limit: window.Limit, Remaining: window.Limit - 1,
	}, nil
}

type mobileStorageStub struct {
	object storage.ObjectInfo
}

func (*mobileStorageStub) PresignPut(
	_ context.Context,
	key string,
	_ time.Duration,
) (string, error) {
	return "https://storage.test/" + key, nil
}

func (storageStub *mobileStorageStub) Stat(
	context.Context,
	string,
) (storage.ObjectInfo, error) {
	return storageStub.object, nil
}

func (*mobileStorageStub) RemoveObject(context.Context, string) error {
	return nil
}

func mintMobileSession(
	t *testing.T,
	handler http.Handler,
	root *pgx.Conn,
	projectID string,
	apiKey string,
	platform Platform,
) string {
	t.Helper()
	response := performMobileRequest(
		handler, "acme", http.MethodPost, "/v1/mobile/capture-sessions",
		validMintBody(projectID, platform), apiKey,
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("mint %s status = %d, body = %s",
			platform, response.Code, response.Body.String())
	}
	var payload CreateSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s mint: %v", platform, err)
	}
	if payload.Token == "" || payload.Configuration.DisplayLocale != "en" ||
		payload.Configuration.Definition.DefinitionKey != "mobile-feedback" ||
		payload.Configuration.ControllerProfile == nil ||
		payload.Configuration.ContactNotice == nil {
		t.Fatalf("%s mint payload = %#v", platform, payload)
	}
	var rawTokenCount int
	if err := root.QueryRow(context.Background(), `
		SELECT count(*) FROM mobile_capture_sessions WHERE token_hash = $1
	`, payload.Token).Scan(&rawTokenCount); err != nil {
		t.Fatalf("check raw capture token persistence: %v", err)
	}
	if rawTokenCount != 0 {
		t.Fatalf("raw capture token stored in %d rows", rawTokenCount)
	}
	return payload.Token
}

func validMintBody(projectID string, platform Platform) string {
	return fmt.Sprintf(`{
		"project_id":%q,
		"widget_definition_key":"mobile-feedback",
		"platform":%q,
		"application_id":"com.example.fidibeki",
		"screen":"/chart/?asset=BAI#price",
		"context_url":"https://app.example.com/chart?asset=BAI#price",
		"display_locale":"en-US"
	}`, projectID, platform)
}

func validSubmissionBody(clientFeedbackID string, screenshot bool) string {
	attachment := ""
	if screenshot {
		attachment = `,"screenshot":{
			"client_attachment_id":"40000000-0000-4000-8000-000000000004",
			"content_type":"image/png","size_bytes":2048
		}`
	}
	return fmt.Sprintf(`{
		"client_feedback_id":%q,
		"responses":{"feedback":"The chart is difficult to read"},
		"comment":"Please improve the labels",
		"app_version":"2.4.0","os_version":"18.5","device_locale":"pt-PT"
		%s
	}`, clientFeedbackID, attachment)
}

func performMobileRequest(
	handler http.Handler,
	tenantSlug string,
	method string,
	path string,
	body string,
	token string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if tenantSlug != "" {
		request.Host = tenantSlug + ".localhost"
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func mobileMigrationsPath(t *testing.T) string {
	t.Helper()
	_, current, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(current), "..", "..", "..", "infra", "migrations")
}

func mobileRoleURL(
	t *testing.T,
	directURL string,
	username string,
	password string,
) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}
