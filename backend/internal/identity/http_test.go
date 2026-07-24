package identity

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestIdentityRequestAndUploadValidation(t *testing.T) {
	handler := &HTTPHandler{}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/v1/profile/avatar", nil)
	if _, _, _, ok := handler.requestState(response, request); ok {
		t.Fatal("requestState accepted a request without tenant context")
	}
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("request state status = %d", response.Code)
	}

	var missingImageBody bytes.Buffer
	missingImageWriter := multipart.NewWriter(&missingImageBody)
	if err := missingImageWriter.WriteField("other", "value"); err != nil {
		t.Fatal(err)
	}
	if err := missingImageWriter.Close(); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(
		http.MethodPut, "/v1/profile/avatar", bytes.NewReader(missingImageBody.Bytes()),
	)
	request.Header.Set("Content-Type", missingImageWriter.FormDataContentType())
	if _, ok := handler.readImage(response, request, ImageKindAvatar); ok {
		t.Fatal("readImage accepted a multipart request without an image")
	}
	if response.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(response.Body.String(), `"is required"`) {
		t.Fatalf("missing image status = %d body = %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(
		http.MethodPut, "/v1/profile/avatar", strings.NewReader("not multipart"),
	)
	request.Header.Set("Content-Type", "text/plain")
	if _, ok := handler.readImage(response, request, ImageKindAvatar); ok {
		t.Fatal("readImage accepted a non-multipart request")
	}
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid multipart status = %d", response.Code)
	}
}

func TestPublicWorkspaceLogoRejectsInvalidAndUnavailableLookups(t *testing.T) {
	router := chi.NewRouter()
	RegisterPublicRoutes(router, HTTPOptions{})
	for _, testCase := range []struct {
		name   string
		path   string
		status int
	}{
		{
			name:   "invalid tenant",
			path:   "/v1/public/workspace-logo/invalid/" + uuid.NewString() + "/64.png",
			status: http.StatusNotFound,
		},
		{
			name: "database unavailable",
			path: "/v1/public/workspace-logo/" + uuid.NewString() + "/" +
				uuid.NewString() + "/64.png",
			status: http.StatusServiceUnavailable,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			router.ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d", response.Code, testCase.status)
			}
		})
	}
}

func TestIdentityObjectKeysAndWorkspaceLogoURLs(t *testing.T) {
	tenantID := uuid.NewString()
	memberID := uuid.NewString()
	version := uuid.NewString()

	logoKey, err := ObjectKey(tenantID, ImageKindLogo, nil, version, 64)
	if err != nil || !strings.Contains(logoKey, "/identity/logos/") {
		t.Fatalf("logo key = %q, error = %v", logoKey, err)
	}
	avatarKey, err := ObjectKey(tenantID, ImageKindAvatar, &memberID, version, 256)
	if err != nil || !strings.Contains(avatarKey, "/identity/members/"+memberID+"/") {
		t.Fatalf("avatar key = %q, error = %v", avatarKey, err)
	}

	otherMemberID := memberID
	cases := []struct {
		name     string
		tenantID string
		kind     ImageKind
		memberID *string
		version  string
		size     int
	}{
		{name: "invalid tenant", tenantID: "invalid", kind: ImageKindLogo, version: version, size: 64},
		{name: "invalid version", tenantID: tenantID, kind: ImageKindLogo, version: "invalid", size: 64},
		{name: "invalid size", tenantID: tenantID, kind: ImageKindLogo, version: version, size: 32},
		{name: "logo with member", tenantID: tenantID, kind: ImageKindLogo, memberID: &otherMemberID, version: version, size: 64},
		{name: "avatar without member", tenantID: tenantID, kind: ImageKindAvatar, version: version, size: 64},
		{name: "avatar with invalid member", tenantID: tenantID, kind: ImageKindAvatar, memberID: pointerTo("invalid"), version: version, size: 64},
		{name: "unknown kind", tenantID: tenantID, kind: ImageKind("unknown"), version: version, size: 64},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ObjectKey(
				testCase.tenantID, testCase.kind, testCase.memberID, testCase.version,
				testCase.size,
			); !errors.Is(err, ErrAssetNotFound) {
				t.Fatalf("ObjectKey error = %v, want ErrAssetNotFound", err)
			}
		})
	}

	logoURL, err := WorkspaceLogoURL(
		"https://api.staging.fidibeki.com/base?old=true#fragment",
		tenantID, version, 256,
	)
	wantURL := "https://api.staging.fidibeki.com/v1/public/workspace-logo/" +
		tenantID + "/" + version + "/256.png"
	if err != nil || logoURL != wantURL {
		t.Fatalf("workspace logo URL = %q, error = %v, want %q", logoURL, err, wantURL)
	}
	for _, invalidBaseURL := range []string{"", "/relative", "://invalid"} {
		if _, err := WorkspaceLogoURL(invalidBaseURL, tenantID, version, 64); err == nil {
			t.Fatalf("WorkspaceLogoURL(%q) returned no error", invalidBaseURL)
		}
	}
	if _, err := WorkspaceLogoURL("https://api.example.com", tenantID, version, 32); err == nil {
		t.Fatal("WorkspaceLogoURL accepted an unsupported size")
	}
}

func TestIdentityVariantWritesRollbackPartialUploads(t *testing.T) {
	tenantID := uuid.NewString()
	version := uuid.NewString()
	objectStorage := &identityFailureStorage{failPutAt: 2}
	variants := ImageVariants{Small: []byte("small"), Large: []byte("large")}

	if err := writeVariants(
		context.Background(), objectStorage, tenantID, ImageKindLogo, nil, version, variants,
	); err == nil {
		t.Fatal("writeVariants returned no error")
	}
	if len(objectStorage.removed) != 1 ||
		!strings.HasSuffix(objectStorage.removed[0], "/64.png") {
		t.Fatalf("rolled back objects = %#v, want the first variant", objectStorage.removed)
	}

	removeUploadedVersion(
		context.Background(), objectStorage, tenantID, ImageKindLogo, nil, version,
	)
	if len(objectStorage.removed) != 3 {
		t.Fatalf("removed objects after version cleanup = %d, want 3", len(objectStorage.removed))
	}
	removeUploadedVersion(
		context.Background(), nil, tenantID, ImageKindLogo, nil, version,
	)
	if err := writeVariants(
		context.Background(), nil, tenantID, ImageKindLogo, nil, version, variants,
	); err == nil {
		t.Fatal("writeVariants accepted unavailable storage")
	}
}

func TestIdentityMutationAvailabilityAndRateLimitErrors(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/v1/profile/avatar", nil)

	response := httptest.NewRecorder()
	if (&HTTPHandler{}).allowMutation(response, request, "avatar:test") {
		t.Fatal("mutation was allowed without a limiter")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing limiter status = %d", response.Code)
	}

	response = httptest.NewRecorder()
	handler := &HTTPHandler{options: HTTPOptions{
		Limiter: &identityLimiterStub{err: errors.New("redis unavailable")},
	}}
	if handler.allowMutation(response, request, "avatar:test") {
		t.Fatal("mutation was allowed after a limiter error")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("limiter error status = %d", response.Code)
	}

	response = httptest.NewRecorder()
	handler.options.Limiter = &identityLimiterStub{allowed: true}
	if !handler.allowMutation(response, request, "avatar:test") {
		t.Fatal("allowed mutation was rejected")
	}
}

func TestIdentityImageStreamingFailures(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/image.png", nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name    string
		storage ObjectStorage
		status  int
	}{
		{name: "unavailable storage", status: http.StatusServiceUnavailable},
		{
			name: "missing object",
			storage: &identityFailureStorage{
				openErr: storage.ErrObjectNotFound,
			},
			status: http.StatusNotFound,
		},
		{
			name: "storage failure",
			storage: &identityFailureStorage{
				openErr: errors.New("storage unavailable"),
			},
			status: http.StatusServiceUnavailable,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler := &HTTPHandler{options: HTTPOptions{
				Storage: testCase.storage,
				Logger:  logger,
			}}
			handler.streamPNG(response, request, "image.png", "private, max-age=60")
			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d", response.Code, testCase.status)
			}
		})
	}

	response := httptest.NewRecorder()
	handler := &HTTPHandler{options: HTTPOptions{
		Storage: &identityFailureStorage{
			reader: &identityFailingReadCloser{
				reader:   strings.NewReader("png"),
				readErr:  errors.New("connection reset"),
				closeErr: errors.New("close failed"),
			},
		},
		Logger: logger,
	}}
	handler.streamPNG(response, request, "image.png", "private, max-age=60")
	if response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("streamed response status = %d headers = %#v", response.Code, response.Header())
	}
}

func TestIdentityCleanupRunStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	maintainer := CleanupMaintainer{
		Store:   Store{},
		Storage: &identityFailureStorage{},
		Health:  &identityHealthRecorderStub{},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := maintainer.Run(ctx, 0); err != nil {
		t.Fatalf("Run error = %v", err)
	}
}

func TestIdentityCleanupHelperFailures(t *testing.T) {
	if err := (CleanupMaintainer{}).Maintain(context.Background()); err == nil {
		t.Fatal("Maintain accepted unavailable storage")
	}
	if err := (CleanupMaintainer{
		Storage: &identityFailureStorage{},
	}).Maintain(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "health recorder is unavailable") {
		t.Fatalf("Maintain(no health recorder) error = %v", err)
	}

	tenantID := uuid.NewString()
	version := uuid.NewString()
	if err := writeVariants(
		context.Background(), &identityFailureStorage{}, "invalid", ImageKindLogo, nil,
		version, ImageVariants{},
	); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("writeVariants invalid key error = %v", err)
	}
	if err := removeCandidateObjects(
		context.Background(),
		&identityFailureStorage{removeErr: storage.ErrObjectNotFound},
		DeletionCandidate{
			TenantID: tenantID,
			Kind:     ImageKindLogo,
			Version:  version,
		},
	); err != nil {
		t.Fatalf("removeCandidateObjects missing object error = %v", err)
	}
	if err := removeCandidateObjects(
		context.Background(),
		&identityFailureStorage{removeErr: errors.New("storage unavailable")},
		DeletionCandidate{
			TenantID: tenantID,
			Kind:     ImageKindLogo,
			Version:  version,
		},
	); err == nil || !strings.Contains(err.Error(), "storage unavailable") {
		t.Fatalf("removeCandidateObjects storage error = %v", err)
	}
	if err := removeCandidateObjects(
		context.Background(),
		&identityFailureStorage{},
		DeletionCandidate{TenantID: "invalid", Kind: ImageKindLogo, Version: version},
	); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("removeCandidateObjects invalid key error = %v", err)
	}

	if got := truncateError(errors.New("  concise failure  ")); got != "concise failure" {
		t.Fatalf("truncateError concise = %q", got)
	}
	longFailure := strings.Repeat("x", 501)
	if got := truncateError(errors.New(longFailure)); len(got) != 500 {
		t.Fatalf("truncateError length = %d, want 500", len(got))
	}
}

func pointerTo(value string) *string {
	return &value
}

type identityHealthRecorderStub struct {
	runs []retentionhealth.Run
	err  error
}

func (recorder *identityHealthRecorderStub) RecordRun(
	_ context.Context,
	run retentionhealth.Run,
) error {
	recorder.runs = append(recorder.runs, run)
	return recorder.err
}

type identityFailureStorage struct {
	failPutAt int
	putCalls  int
	openErr   error
	reader    io.ReadCloser
	removed   []string
	removeErr error
}

func (stub *identityFailureStorage) PutObject(
	_ context.Context,
	_ string,
	_ io.Reader,
	_ int64,
	_ string,
) error {
	stub.putCalls++
	if stub.failPutAt == stub.putCalls {
		return errors.New("put failed")
	}
	return nil
}

func (stub *identityFailureStorage) OpenObject(
	context.Context,
	string,
) (io.ReadCloser, error) {
	if stub.openErr != nil {
		return nil, stub.openErr
	}
	if stub.reader != nil {
		return stub.reader, nil
	}
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (stub *identityFailureStorage) RemoveObject(_ context.Context, key string) error {
	stub.removed = append(stub.removed, key)
	return stub.removeErr
}

type identityFailingReadCloser struct {
	reader   io.Reader
	readErr  error
	closeErr error
}

func (reader *identityFailingReadCloser) Read(target []byte) (int, error) {
	read, err := reader.reader.Read(target)
	if err == io.EOF && reader.readErr != nil {
		return read, reader.readErr
	}
	return read, err
}

func (reader *identityFailingReadCloser) Close() error {
	return reader.closeErr
}

var _ ratelimit.Limiter = (*identityLimiterStub)(nil)
