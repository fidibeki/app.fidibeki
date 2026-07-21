package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/server"
)

type readinessCheckFunc func(context.Context) error

func (f readinessCheckFunc) Check(ctx context.Context) error {
	return f(ctx)
}

func TestHealthzReportsProcessLiveness(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	server.New(server.Options{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", got)
	}

	var response struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("status body = %q, want ok", response.Status)
	}
}

func TestEveryResponseHasARequestID(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/healthz", "/not-found"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			server.New(server.Options{}).ServeHTTP(recorder, request)

			requestID := recorder.Header().Get("X-Request-ID")
			if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(requestID) {
				t.Fatalf("X-Request-ID = %q, want UUID v4", requestID)
			}
		})
	}
}

func TestReadyzReportsDependencyHealth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		checks     map[string]server.ReadinessCheck
		wantStatus int
		wantBody   map[string]string
	}{
		{
			name: "all dependencies ready",
			checks: map[string]server.ReadinessCheck{
				"database": readinessCheckFunc(func(context.Context) error { return nil }),
				"redis":    readinessCheckFunc(func(context.Context) error { return nil }),
				"storage":  readinessCheckFunc(func(context.Context) error { return nil }),
			},
			wantStatus: http.StatusOK,
			wantBody: map[string]string{
				"database": "ok",
				"redis":    "ok",
				"storage":  "ok",
			},
		},
		{
			name: "dependency unavailable",
			checks: map[string]server.ReadinessCheck{
				"database": readinessCheckFunc(func(context.Context) error { return nil }),
				"redis":    readinessCheckFunc(func(context.Context) error { return errors.New("connection refused") }),
				"storage":  readinessCheckFunc(func(context.Context) error { return nil }),
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody: map[string]string{
				"database": "ok",
				"redis":    "unavailable",
				"storage":  "ok",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			server.New(server.Options{Readiness: tt.checks}).ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			var response struct {
				Checks map[string]string `json:"checks"`
			}
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			for name, want := range tt.wantBody {
				if got := response.Checks[name]; got != want {
					t.Errorf("check %q = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestReadyzFailsClosedWithoutChecksAndOnTimeout(t *testing.T) {
	t.Parallel()

	assertUnavailable := func(t *testing.T, options server.Options) {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		server.New(options).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable ||
			!strings.Contains(recorder.Body.String(), "unavailable") {
			t.Fatalf("readiness response = %d %s", recorder.Code, recorder.Body.String())
		}
	}

	assertUnavailable(t, server.Options{})
	blocker := make(chan struct{})
	defer close(blocker)
	assertUnavailable(t, server.Options{
		ReadinessTimeout: 10 * time.Millisecond,
		Readiness: map[string]server.ReadinessCheck{
			"blocked": readinessCheckFunc(func(context.Context) error {
				<-blocker
				return nil
			}),
		},
	})
}

func TestUnknownRoutesUseTheAPIErrorEnvelope(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	server.New(server.Options{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want not_found", response.Error.Code)
	}
	if response.Error.Message == "" {
		t.Error("error.message is empty")
	}
}

func TestHostRouterSeparatesPlatformAndTenantTraffic(t *testing.T) {
	t.Parallel()

	platform := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("platform"))
	})
	tenant := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("tenant"))
	})
	widget := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("widget"))
	})
	handler := server.New(server.Options{
		BaseDomain: "localhost", PlatformHandler: platform, TenantHandler: tenant, WidgetHandler: widget,
	})

	tests := []struct {
		name       string
		host       string
		wantStatus int
		wantBody   string
	}{
		{name: "platform", host: "api.localhost:8080", wantStatus: http.StatusOK, wantBody: "platform"},
		{name: "tenant", host: "acme.localhost:8080", wantStatus: http.StatusOK, wantBody: "tenant"},
		{name: "widget", host: "widget.localhost:8080", wantStatus: http.StatusOK, wantBody: "widget"},
		{name: "root", host: "localhost:8080", wantStatus: http.StatusNotFound, wantBody: `"code":"not_found"`},
		{name: "unknown", host: "unknown.example.test", wantStatus: http.StatusNotFound, wantBody: `"code":"unknown_tenant"`},
		{name: "reserved", host: "admin.localhost", wantStatus: http.StatusNotFound, wantBody: `"code":"unknown_tenant"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/v1/test", nil)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if !bytes.Contains(recorder.Body.Bytes(), []byte(tt.wantBody)) {
				t.Errorf("body = %q, want to contain %q", recorder.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestHostRouterAcceptsOnlyAuthenticatedInternalProxyHost(t *testing.T) {
	t.Parallel()

	const secret = "0123456789abcdef0123456789abcdef"
	tenantHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("tenant"))
	})
	handler := server.New(server.Options{
		BaseDomain: "localhost", TenantHandler: tenantHandler, InternalProxySecret: secret,
	})

	for _, tt := range []struct {
		name       string
		secret     string
		wantStatus int
	}{
		{name: "authenticated", secret: secret, wantStatus: http.StatusOK},
		{name: "spoofed", secret: "wrong", wantStatus: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://api:8080/v1/config", nil)
			request.Header.Set("X-Fidibeki-Proxy-Host", "acme.localhost:4332")
			request.Header.Set("X-Fidibeki-Proxy-Secret", tt.secret)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestRequestsAreLoggedWithCorrelationFields(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	server.New(server.Options{Logger: logger}).ServeHTTP(recorder, request)

	var entry map[string]any
	if err := json.NewDecoder(&output).Decode(&entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	for name, want := range map[string]any{
		"msg":        "request completed",
		"request_id": recorder.Header().Get("X-Request-ID"),
		"method":     http.MethodGet,
		"route":      "/healthz",
		"status":     float64(http.StatusOK),
	} {
		if got := entry[name]; got != want {
			t.Errorf("log field %s = %#v, want %#v", name, got, want)
		}
	}
	if _, exists := entry["duration_ms"]; !exists {
		t.Error("log field duration_ms is missing")
	}
}
