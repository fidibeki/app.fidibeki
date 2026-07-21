package config_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
)

func TestConfigurationReportsEveryMissingRequiredValue(t *testing.T) {
	t.Parallel()

	_, err := config.FromLookup(func(string) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("FromLookup() error = nil, want validation error")
	}

	for _, name := range []string{
		"APP_ENV",
		"BASE_DOMAIN",
		"PUBLIC_BASE_URL",
		"DATABASE_URL_APP",
		"DATABASE_URL_ADMIN",
		"DATABASE_URL_DIRECT",
		"REDIS_URL",
		"S3_ENDPOINT",
		"S3_BUCKET",
		"S3_ACCESS_KEY",
		"S3_SECRET_KEY",
		"JWT_SECRET",
		"INTEGRATION_ENCRYPTION_KEY",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
}

func TestLoadReadsEnvironmentAndRedactsSecrets(t *testing.T) {
	values := map[string]string{
		"APP_ENV":                    "test",
		"HTTP_ADDRESS":               ":9090",
		"BASE_DOMAIN":                "localhost",
		"PUBLIC_BASE_URL":            "http://api.localhost:8088",
		"DATABASE_URL_APP":           "postgres://app:app-secret@localhost/fidibeki",
		"DATABASE_URL_ADMIN":         "postgres://admin:admin-secret@localhost/fidibeki",
		"DATABASE_URL_DIRECT":        "postgres://root:root-secret@localhost/fidibeki",
		"REDIS_URL":                  "redis://:redis-secret@localhost:6379/0",
		"S3_ENDPOINT":                "http://localhost:9000",
		"S3_BUCKET":                  "fidibeki",
		"S3_ACCESS_KEY":              "access-secret",
		"S3_SECRET_KEY":              "storage-secret",
		"JWT_SECRET":                 "0123456789abcdef0123456789abcdef",
		"INTERNAL_PROXY_SECRET":      "abcdef0123456789abcdef0123456789",
		"INTEGRATION_ENCRYPTION_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"SENTRY_DSN":                 "https://public:private@sentry.example.test/1",
		"GEMINI_API_KEY":             "gemini-secret",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	t.Chdir(t.TempDir())

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.HTTPAddress != ":9090" || got.AppDBMaxConns != 10 || got.AdminDBMaxConns != 5 {
		t.Errorf("loaded config = %#v", got)
	}
	encoded, err := json.Marshal(got.Redacted())
	if err != nil {
		t.Fatalf("marshal redacted config: %v", err)
	}
	for _, secret := range []string{
		"app-secret", "admin-secret", "root-secret", "redis-secret", "access-secret",
		"storage-secret", "0123456789abcdef", "public:private",
		"abcdef0123456789abcdef",
		"gemini-secret",
		"MDEyMzQ1Njc4OWFiY2Rl",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("redacted config contains %q: %s", secret, encoded)
		}
	}
}

func TestConfigurationLoadsValidatedValues(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"APP_ENV":                    "test",
		"HTTP_ADDRESS":               ":8080",
		"BASE_DOMAIN":                "fidibeki.localhost",
		"PUBLIC_BASE_URL":            "http://api.fidibeki.localhost:8088",
		"DATABASE_URL_APP":           "postgres://fidibeki_app:app@localhost/fidibeki",
		"DATABASE_URL_ADMIN":         "postgres://fidibeki_admin:admin@localhost/fidibeki",
		"DATABASE_URL_DIRECT":        "postgres://fidibeki_admin:admin@localhost/fidibeki",
		"REDIS_URL":                  "redis://localhost:6379/0",
		"S3_ENDPOINT":                "http://localhost:9000",
		"S3_BUCKET":                  "fidibeki",
		"S3_ACCESS_KEY":              "fidibeki",
		"S3_SECRET_KEY":              "fidibeki-local-secret",
		"S3_VIRTUAL_HOST_STYLE":      "true",
		"JWT_SECRET":                 "0123456789abcdef0123456789abcdef",
		"INTEGRATION_ENCRYPTION_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	}

	got, err := config.FromLookup(func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	})
	if err != nil {
		t.Fatalf("FromLookup() error = %v", err)
	}
	if got.HTTPAddress != ":8080" {
		t.Errorf("HTTPAddress = %q, want :8080", got.HTTPAddress)
	}
	if got.BaseDomain != "fidibeki.localhost" {
		t.Errorf("BaseDomain = %q, want fidibeki.localhost", got.BaseDomain)
	}
	if got.ReadinessTimeout.String() != "2s" {
		t.Errorf("ReadinessTimeout = %s, want 2s", got.ReadinessTimeout)
	}
	if got.GeminiModel != "gemini-2.5-flash" || got.GeminiRPM != 15 ||
		got.AIConcurrency != 3 || got.AIDailyLimit != 5000 || got.AIReconcileEvery != 5*time.Minute {
		t.Errorf("AI defaults = model %q rpm %d concurrency %d daily %d reconcile %s",
			got.GeminiModel, got.GeminiRPM, got.AIConcurrency, got.AIDailyLimit, got.AIReconcileEvery)
	}
	if got.ReporterTokenTTL != 90*24*time.Hour {
		t.Errorf("ReporterTokenTTL = %s, want 2160h", got.ReporterTokenTTL)
	}
	if !got.S3VirtualHostStyle {
		t.Error("S3VirtualHostStyle = false, want true")
	}
}

func TestConfigurationUsesPlatformPortWhenAddressIsUnset(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"APP_ENV":                    "test",
		"PORT":                       "9091",
		"BASE_DOMAIN":                "fidibeki.localhost",
		"PUBLIC_BASE_URL":            "http://api.fidibeki.localhost:8088",
		"DATABASE_URL_APP":           "postgres://fidibeki_app:app@localhost/fidibeki",
		"DATABASE_URL_ADMIN":         "postgres://fidibeki_admin:admin@localhost/fidibeki",
		"DATABASE_URL_DIRECT":        "postgres://fidibeki_admin:admin@localhost/fidibeki",
		"REDIS_URL":                  "redis://localhost:6379/0",
		"S3_ENDPOINT":                "http://localhost:9000",
		"S3_BUCKET":                  "fidibeki",
		"S3_ACCESS_KEY":              "fidibeki",
		"S3_SECRET_KEY":              "fidibeki-local-secret",
		"JWT_SECRET":                 "0123456789abcdef0123456789abcdef",
		"INTEGRATION_ENCRYPTION_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	}
	got, err := config.FromLookup(func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	})
	if err != nil {
		t.Fatalf("FromLookup() error = %v", err)
	}
	if got.HTTPAddress != ":9091" {
		t.Errorf("HTTPAddress = %q, want :9091", got.HTTPAddress)
	}
}

func TestConfigurationRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"APP_ENV":                    "preview",
		"BASE_DOMAIN":                "https://fidibeki.localhost/path",
		"PUBLIC_BASE_URL":            "relative-url",
		"DATABASE_URL_APP":           "not-a-url",
		"DATABASE_URL_ADMIN":         "postgres://",
		"DATABASE_URL_DIRECT":        "://missing-scheme",
		"REDIS_URL":                  "redis-without-scheme",
		"S3_ENDPOINT":                "/relative/storage",
		"S3_BUCKET":                  "fidibeki",
		"S3_ACCESS_KEY":              "fidibeki",
		"S3_SECRET_KEY":              "secret",
		"JWT_SECRET":                 "too-short",
		"INTERNAL_PROXY_SECRET":      "too-short",
		"INTEGRATION_ENCRYPTION_KEY": "not-base64",
		"READINESS_TIMEOUT":          "soon",
		"SHUTDOWN_TIMEOUT":           "0s",
		"DATABASE_APP_MAX_CONNS":     "many",
		"DATABASE_ADMIN_MAX_CONNS":   "0",
		"S3_AUTO_CREATE_BUCKET":      "sometimes",
		"LOG_LEVEL":                  "verbose",
		"AUTH_ACCESS_TOKEN_TTL":      "later",
		"AUTH_MAGIC_LINK_TTL":        "0s",
		"AUTH_REFRESH_TOKEN_TTL":     "never",
		"REPORTER_UNSUBSCRIBE_TTL":   "never",
		"EMAIL_DRIVER":               "smtp",
		"EMAIL_QUEUE_CONCURRENCY":    "many",
		"AI_QUEUE_CONCURRENCY":       "none",
		"GEMINI_RPM":                 "fast",
		"AI_DAILY_REQUEST_LIMIT":     "unlimited",
		"AI_RECONCILE_INTERVAL":      "later",
	}

	_, err := config.FromLookup(func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	})
	if err == nil {
		t.Fatal("FromLookup() error = nil, want validation error")
	}
	for _, expected := range []string{
		"APP_ENV", "BASE_DOMAIN", "PUBLIC_BASE_URL", "DATABASE_URL_APP", "DATABASE_URL_ADMIN",
		"DATABASE_URL_DIRECT", "REDIS_URL", "S3_ENDPOINT", "JWT_SECRET",
		"INTERNAL_PROXY_SECRET", "INTEGRATION_ENCRYPTION_KEY",
		"READINESS_TIMEOUT", "SHUTDOWN_TIMEOUT", "DATABASE_APP_MAX_CONNS",
		"DATABASE_ADMIN_MAX_CONNS", "S3_AUTO_CREATE_BUCKET", "LOG_LEVEL",
		"AUTH_ACCESS_TOKEN_TTL", "AUTH_MAGIC_LINK_TTL", "AUTH_REFRESH_TOKEN_TTL",
		"REPORTER_UNSUBSCRIBE_TTL",
		"EMAIL_DRIVER", "EMAIL_QUEUE_CONCURRENCY",
		"AI_QUEUE_CONCURRENCY", "GEMINI_RPM", "AI_DAILY_REQUEST_LIMIT", "AI_RECONCILE_INTERVAL",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error %q does not mention %s", err, expected)
		}
	}
}
