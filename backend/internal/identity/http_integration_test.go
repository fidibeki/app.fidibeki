package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
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
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestIdentityHTTPAssetLifecycle(t *testing.T) {
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
		t.Fatalf("start PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := db.NewMigrator(directURL, identityMigrationsPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatal(err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatal(err)
	}
	var tenantID, ownerID, memberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name, permissions)
		VALUES ($1, 'owner@acme.test', 'Owner', $2) RETURNING id
	`, tenantID, auth.TenantPermissionStrings()).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name, permissions)
		VALUES ($1, 'member@acme.test', 'Member', ARRAY['project:read']) RETURNING id
	`, tenantID).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET owner_member_id = $2 WHERE id = $1
	`, tenantID, ownerID); err != nil {
		t.Fatal(err)
	}
	pools, err := db.OpenPools(
		ctx, identityRoleURL(t, directURL, "fidibeki_app", "app-password"),
		directURL, 4, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pools.Close)
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Hour)
	ownerToken, _, _ := jwtManager.IssueTenantMember(
		ownerID, tenantID, auth.TenantPermissionStrings(), 0,
	)
	memberToken, _, _ := jwtManager.IssueTenantMember(
		memberID, tenantID, []string{"project:read"}, 0,
	)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store := Store{Database: pools.Admin, Now: func() time.Time { return now }}
	objects := &identityStorageStub{objects: map[string][]byte{}}
	limiter := &identityLimiterStub{allowed: true}
	routes := chi.NewRouter()
	RegisterTenantRoutes(routes, HTTPOptions{
		Store: store, JWT: jwtManager, Storage: objects, Limiter: limiter,
		PublicBaseURL: "https://api.staging.fidibeki.com",
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)

	firstLogo := performIdentityUpload(
		handler, "/v1/organization-profile/logo", ownerToken,
		"logo.png", "image/png", encodePNG(t, identityTestImage(120, 60)),
	)
	if firstLogo.Code != http.StatusOK {
		t.Fatalf("first logo status = %d, body = %s", firstLogo.Code, firstLogo.Body.String())
	}
	firstLogoVersion := responseVersion(t, firstLogo, "logo_version")
	assertCurrentVersion(t, root, "tenants", "logo_version", tenantID, firstLogoVersion)
	if len(objects.objects) != 2 {
		t.Fatalf("stored logo objects = %d, want 2", len(objects.objects))
	}
	var logoAudit int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'organization.logo_updated'
		  AND actor_member_id = $2
	`, tenantID, ownerID).Scan(&logoAudit); err != nil || logoAudit != 1 {
		t.Fatalf("logo audit count = %d, error = %v", logoAudit, err)
	}

	publicRoutes := chi.NewRouter()
	RegisterPublicRoutes(publicRoutes, HTTPOptions{
		Store: store, Storage: objects,
	})
	publicLogo := httptest.NewRecorder()
	publicRoutes.ServeHTTP(publicLogo, httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/v1/public/workspace-logo/%s/%s/64.png", tenantID, firstLogoVersion),
		nil,
	))
	if publicLogo.Code != http.StatusOK ||
		publicLogo.Header().Get("Content-Type") != "image/png" ||
		publicLogo.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
		t.Fatalf("public logo status = %d headers = %#v", publicLogo.Code, publicLogo.Header())
	}

	invalidLogo := performIdentityUpload(
		handler, "/v1/organization-profile/logo", ownerToken,
		"logo.svg", "image/svg+xml", []byte("<svg/>"),
	)
	if invalidLogo.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(invalidLogo.Body.String(), `"image"`) {
		t.Fatalf("invalid logo status = %d, body = %s", invalidLogo.Code, invalidLogo.Body.String())
	}
	assertCurrentVersion(t, root, "tenants", "logo_version", tenantID, firstLogoVersion)

	unauthorizedLogo := performIdentityUpload(
		handler, "/v1/organization-profile/logo", memberToken,
		"logo.png", "image/png", encodePNG(t, identityTestImage(40, 40)),
	)
	if unauthorizedLogo.Code != http.StatusForbidden {
		t.Fatalf("unauthorized logo status = %d, body = %s", unauthorizedLogo.Code,
			unauthorizedLogo.Body.String())
	}

	avatar := performIdentityUpload(
		handler, "/v1/profile/avatar", memberToken,
		"avatar.jpg", "image/jpeg", encodeJPEG(t, identityTestImage(80, 120)),
	)
	if avatar.Code != http.StatusOK {
		t.Fatalf("avatar status = %d, body = %s", avatar.Code, avatar.Body.String())
	}
	avatarVersion := responseVersion(t, avatar, "avatar_version")
	assertCurrentVersion(t, root, "tenant_members", "avatar_version", memberID, avatarVersion)
	memberAvatar := performIdentityRequest(
		handler, http.MethodGet,
		fmt.Sprintf("/v1/members/%s/avatar/%s/64.png", memberID, avatarVersion),
		nil, "", ownerToken,
	)
	if memberAvatar.Code != http.StatusOK ||
		memberAvatar.Header().Get("Cache-Control") != "private, max-age=3600" {
		t.Fatalf("member avatar status = %d headers = %#v", memberAvatar.Code, memberAvatar.Header())
	}
	crossMemberAvatar := performIdentityRequest(
		handler, http.MethodGet,
		fmt.Sprintf("/v1/members/%s/avatar/%s/64.png", ownerID, avatarVersion),
		nil, "", ownerToken,
	)
	if crossMemberAvatar.Code != http.StatusNotFound {
		t.Fatalf("cross-member avatar status = %d", crossMemberAvatar.Code)
	}

	secondLogo := performIdentityUpload(
		handler, "/v1/organization-profile/logo", ownerToken,
		"logo.webp", "image/webp", identityWebPFixture(t),
	)
	if secondLogo.Code != http.StatusUnprocessableEntity {
		t.Fatalf("undersized WebP status = %d, body = %s", secondLogo.Code, secondLogo.Body.String())
	}
	secondLogo = performIdentityUpload(
		handler, "/v1/organization-profile/logo", ownerToken,
		"logo.png", "image/png", encodePNG(t, identityTestImage(60, 120)),
	)
	if secondLogo.Code != http.StatusOK {
		t.Fatalf("second logo status = %d, body = %s", secondLogo.Code, secondLogo.Body.String())
	}
	secondLogoVersion := responseVersion(t, secondLogo, "logo_version")
	staleLogo := httptest.NewRecorder()
	publicRoutes.ServeHTTP(staleLogo, httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/v1/public/workspace-logo/%s/%s/64.png", tenantID, firstLogoVersion),
		nil,
	))
	if staleLogo.Code != http.StatusNotFound {
		t.Fatalf("stale logo status = %d", staleLogo.Code)
	}

	objects.failPut = errors.New("storage unavailable")
	failedReplacement := performIdentityUpload(
		handler, "/v1/organization-profile/logo", ownerToken,
		"logo.png", "image/png", encodePNG(t, identityTestImage(50, 50)),
	)
	if failedReplacement.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed replacement status = %d, body = %s",
			failedReplacement.Code, failedReplacement.Body.String())
	}
	objects.failPut = nil
	assertCurrentVersion(t, root, "tenants", "logo_version", tenantID, secondLogoVersion)

	now = now.Add(20 * time.Minute)
	objects.failRemove = errors.New("storage cleanup unavailable")
	if err := (CleanupMaintainer{Store: store, Storage: objects}).Maintain(ctx); err == nil {
		t.Fatal("cleanup storage failure returned no error")
	}
	var cleanupAttempts int
	var cleanupError string
	if err := root.QueryRow(ctx, `
		SELECT attempts, last_error
		FROM identity_asset_deletions
		WHERE tenant_id = $1 AND asset_kind = 'workspace_logo' AND version = $2
	`, tenantID, firstLogoVersion).Scan(&cleanupAttempts, &cleanupError); err != nil {
		t.Fatalf("read deferred cleanup: %v", err)
	}
	if cleanupAttempts != 1 || !strings.Contains(cleanupError, "storage cleanup unavailable") {
		t.Fatalf("deferred cleanup attempts = %d, error = %q", cleanupAttempts, cleanupError)
	}
	objects.failRemove = nil
	now = now.Add(3 * time.Minute)
	if err := (CleanupMaintainer{Store: store, Storage: objects}).Maintain(ctx); err != nil {
		t.Fatalf("maintain identity cleanup: %v", err)
	}
	for _, size := range []int{64, 256} {
		oldKey, _ := ObjectKey(tenantID, ImageKindLogo, nil, firstLogoVersion, size)
		if _, exists := objects.objects[oldKey]; exists {
			t.Errorf("old logo object %q still exists", oldKey)
		}
		currentKey, _ := ObjectKey(tenantID, ImageKindLogo, nil, secondLogoVersion, size)
		if _, exists := objects.objects[currentKey]; !exists {
			t.Errorf("current logo object %q was removed", currentKey)
		}
	}

	deletedAvatar := performIdentityRequest(
		handler, http.MethodDelete, "/v1/profile/avatar", nil, "", memberToken,
	)
	if deletedAvatar.Code != http.StatusOK ||
		!strings.Contains(deletedAvatar.Body.String(), `"removed":true`) {
		t.Fatalf("delete avatar status = %d, body = %s", deletedAvatar.Code,
			deletedAvatar.Body.String())
	}
	if err := (CleanupMaintainer{Store: store, Storage: objects}).Maintain(ctx); err != nil {
		t.Fatalf("clean removed avatar: %v", err)
	}
	deletedAvatarRead := performIdentityRequest(
		handler, http.MethodGet,
		fmt.Sprintf("/v1/members/%s/avatar/%s/64.png", memberID, avatarVersion),
		nil, "", ownerToken,
	)
	if deletedAvatarRead.Code != http.StatusNotFound {
		t.Fatalf("deleted avatar read status = %d", deletedAvatarRead.Code)
	}

	limiter.allowed = false
	limiter.retryAfter = 3 * time.Second
	rateLimited := performIdentityUpload(
		handler, "/v1/profile/avatar", memberToken,
		"avatar.png", "image/png", encodePNG(t, identityTestImage(40, 40)),
	)
	if rateLimited.Code != http.StatusTooManyRequests ||
		rateLimited.Header().Get("Retry-After") != "3" {
		t.Fatalf("rate limited status = %d retry = %q body = %s",
			rateLimited.Code, rateLimited.Header().Get("Retry-After"), rateLimited.Body.String())
	}
}

type identityStorageStub struct {
	objects    map[string][]byte
	failPut    error
	failRemove error
}

func (stub *identityStorageStub) PutObject(
	_ context.Context,
	key string,
	reader io.Reader,
	_ int64,
	_ string,
) error {
	if stub.failPut != nil {
		return stub.failPut
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	stub.objects[key] = body
	return nil
}

func (stub *identityStorageStub) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	body, exists := stub.objects[key]
	if !exists {
		return nil, fmt.Errorf("open object: %w", storage.ErrObjectNotFound)
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (stub *identityStorageStub) RemoveObject(_ context.Context, key string) error {
	if stub.failRemove != nil {
		return stub.failRemove
	}
	delete(stub.objects, key)
	return nil
}

type identityLimiterStub struct {
	allowed    bool
	retryAfter time.Duration
	err        error
}

func (stub *identityLimiterStub) Allow(
	context.Context,
	string,
	ratelimit.Window,
) (ratelimit.Decision, error) {
	return ratelimit.Decision{
		Allowed: stub.allowed, RetryAfter: stub.retryAfter,
	}, stub.err
}

func performIdentityUpload(
	handler http.Handler,
	path, token, filename, contentType string,
	body []byte,
) *httptest.ResponseRecorder {
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename=%q`, filename))
	header.Set("Content-Type", contentType)
	part, _ := writer.CreatePart(header)
	_, _ = part.Write(body)
	_ = writer.Close()
	return performIdentityRequest(
		handler, http.MethodPut, path, requestBody.Bytes(), writer.FormDataContentType(), token,
	)
}

func performIdentityRequest(
	handler http.Handler,
	method, path string,
	body []byte,
	contentType, token string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://acme.localhost"+path, bytes.NewReader(body))
	request.Host = "acme.localhost"
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func responseVersion(t *testing.T, response *httptest.ResponseRecorder, field string) string {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	version, _ := payload[field].(string)
	if version == "" {
		t.Fatalf("%s response = %#v", field, payload)
	}
	return version
}

func assertCurrentVersion(
	t *testing.T,
	root *pgx.Conn,
	table, column, id, want string,
) {
	t.Helper()
	var got string
	if err := root.QueryRow(
		context.Background(),
		fmt.Sprintf("SELECT %s::text FROM %s WHERE id = $1", column, table),
		id,
	).Scan(&got); err != nil || got != want {
		t.Fatalf("%s.%s version = %q, error = %v, want %q", table, column, got, err, want)
	}
}

func identityTestImage(width, height int) *image.NRGBA {
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	for index := range source.Pix {
		source.Pix[index] = byte(index % 251)
	}
	return source
}

func encodeJPEG(t *testing.T, source image.Image) []byte {
	t.Helper()
	var body bytes.Buffer
	if err := jpeg.Encode(&body, source, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func identityWebPFixture(t *testing.T) []byte {
	t.Helper()
	body, err := base64.StdEncoding.DecodeString(
		"UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA",
	)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func identityMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
}

func identityRoleURL(t *testing.T, directURL, username, password string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}
