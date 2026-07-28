package config_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
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
		"TWO_FACTOR_ENCRYPTION_KEY",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
}

func TestLoadReadsEnvironmentAndRedactsSecrets(t *testing.T) {
	values := map[string]string{
		"APP_ENV":                      "test",
		"HTTP_ADDRESS":                 ":9090",
		"BASE_DOMAIN":                  "localhost",
		"PUBLIC_BASE_URL":              "http://api.localhost:8088",
		"DATABASE_URL_APP":             "postgres://app:app-secret@localhost/fidibeki",
		"DATABASE_URL_ADMIN":           "postgres://admin:admin-secret@localhost/fidibeki",
		"DATABASE_URL_DIRECT":          "postgres://root:root-secret@localhost/fidibeki",
		"REDIS_URL":                    "redis://:redis-secret@localhost:6379/0",
		"S3_ENDPOINT":                  "http://localhost:9000",
		"S3_BUCKET":                    "fidibeki",
		"S3_ACCESS_KEY":                "access-secret",
		"S3_SECRET_KEY":                "storage-secret",
		"JWT_SECRET":                   "0123456789abcdef0123456789abcdef",
		"ABUSE_BUCKET_SECRET":          "abuse-current-secret-0123456789abcdef",
		"ABUSE_BUCKET_PREVIOUS_SECRET": "abuse-previous-secret-0123456789abcdef",
		"INTERNAL_PROXY_SECRET":        "abcdef0123456789abcdef0123456789",
		"TRUSTED_PROXY_CIDRS":          "10.0.0.0/8, 192.0.2.0/24",
		"INTEGRATION_ENCRYPTION_KEY":   "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"TWO_FACTOR_ENCRYPTION_KEY":    "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=",
		"AUTH_REFRESH_RETRY_GRACE":     "10s",
		"SENTRY_DSN":                   "https://public:private@sentry.example.test/1",
		"GEMINI_API_KEY":               "gemini-secret",
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
		"abuse-current-secret", "abuse-previous-secret",
		"gemini-secret",
		"MDEyMzQ1Njc4OWFiY2Rl",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("redacted config contains %q: %s", secret, encoded)
		}
	}
}

func TestRedactedConfigurationHidesRedisCA(t *testing.T) {
	t.Parallel()
	const certificate = "private Redis CA material"
	encoded, err := json.Marshal((config.Config{RedisCACert: certificate}).Redacted())
	if err != nil {
		t.Fatalf("marshal redacted config: %v", err)
	}
	if strings.Contains(string(encoded), certificate) {
		t.Fatalf("redacted config contains Redis CA: %s", encoded)
	}
}

func TestConfigurationLoadsValidatedValues(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"APP_ENV":                    "test",
		"HTTP_ADDRESS":               ":8080",
		"BASE_DOMAIN":                "fidibeki.localhost",
		"PUBLIC_BASE_URL":            "http://api.fidibeki.localhost:8088",
		"PLATFORM_BACKOFFICE_URL":    "http://admin.fidibeki.localhost:3001",
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
		"TRUSTED_PROXY_CIDRS":        "10.0.0.0/8, 192.0.2.0/24",
		"INTEGRATION_ENCRYPTION_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"TWO_FACTOR_ENCRYPTION_KEY":  "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=",
		"AUTH_REFRESH_RETRY_GRACE":   "10s",
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
	if got.PlatformBackofficeURL != "http://admin.fidibeki.localhost:3001" {
		t.Errorf("PlatformBackofficeURL = %q", got.PlatformBackofficeURL)
	}
	if got.ReadinessTimeout.String() != "2s" {
		t.Errorf("ReadinessTimeout = %s, want 2s", got.ReadinessTimeout)
	}
	if got.GeminiModel != "gemini-3.1-flash-lite" || got.GeminiRPM != 15 ||
		got.AIConcurrency != 3 || got.AIDailyLimit != 5000 ||
		got.AIProjectDailyLimit != 2000 || got.AIReconcileEvery != 5*time.Minute {
		t.Errorf("AI defaults = model %q rpm %d concurrency %d daily %d project %d reconcile %s",
			got.GeminiModel, got.GeminiRPM, got.AIConcurrency, got.AIDailyLimit,
			got.AIProjectDailyLimit, got.AIReconcileEvery)
	}
	if got.DomainConcurrency != 4 || got.MediaConcurrency != 2 || got.PrivacyConcurrency != 1 {
		t.Errorf("queue concurrency defaults = domain %d media %d privacy %d",
			got.DomainConcurrency, got.MediaConcurrency, got.PrivacyConcurrency)
	}
	if got.ReporterTokenTTL != 90*24*time.Hour {
		t.Errorf("ReporterTokenTTL = %s, want 2160h", got.ReporterTokenTTL)
	}
	if got.RefreshRetryGrace != 10*time.Second {
		t.Errorf("RefreshRetryGrace = %s, want 10s", got.RefreshRetryGrace)
	}
	if len(got.AbuseBucketSecret) != 64 ||
		got.AbuseBucketSecret == values["JWT_SECRET"] ||
		got.AbuseBucketPreviousSecret != "" {
		t.Errorf("derived abuse bucket key is invalid")
	}
	if len(got.TrustedProxyCIDRs) != 2 || got.TrustedProxyCIDRs[0].String() != "10.0.0.0/8" ||
		got.TrustedProxyCIDRs[1].String() != "192.0.2.0/24" {
		t.Errorf("TrustedProxyCIDRs = %#v", got.TrustedProxyCIDRs)
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
		"TWO_FACTOR_ENCRYPTION_KEY":  "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=",
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

func TestConfigurationRequiresSecureBackofficeOriginOutsideLocalAndTest(t *testing.T) {
	t.Parallel()
	values := validEnvironmentValues("staging")
	delete(values, "PLATFORM_BACKOFFICE_URL")
	lookup := func(name string) (string, bool) { value, ok := values[name]; return value, ok }
	if _, err := config.FromLookup(lookup); err == nil || !strings.Contains(err.Error(), "PLATFORM_BACKOFFICE_URL must be an HTTPS origin") {
		t.Fatalf("missing backoffice origin error = %v", err)
	}
	values["PLATFORM_BACKOFFICE_URL"] = "http://admin.fidibeki.test"
	if _, err := config.FromLookup(lookup); err == nil || !strings.Contains(err.Error(), "PLATFORM_BACKOFFICE_URL must be an HTTPS origin") {
		t.Fatalf("insecure backoffice origin error = %v", err)
	}
	values["PLATFORM_BACKOFFICE_URL"] = "https://admin.fidibeki.test"
	if _, err := config.FromLookup(lookup); err != nil {
		t.Fatalf("secure backoffice origin error = %v", err)
	}
}

func TestConfigurationRequiresEncryptedDatabaseTransportOutsideLocalAndTest(t *testing.T) {
	t.Parallel()
	modes := []string{"", "disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
	for _, environment := range []string{"local", "test", "staging", "production"} {
		for _, name := range []string{"DATABASE_URL_APP", "DATABASE_URL_ADMIN", "DATABASE_URL_DIRECT"} {
			for _, mode := range modes {
				modeName := mode
				if modeName == "" {
					modeName = "missing"
				}
				t.Run(environment+"/"+name+"/"+modeName, func(t *testing.T) {
					t.Parallel()
					values := validEnvironmentValues(environment)
					values[name] = databaseURLWithSSLMode(mode)
					_, err := config.FromLookup(func(name string) (string, bool) {
						value, exists := values[name]
						return value, exists
					})
					secureMode := mode == "require" || mode == "verify-ca" || mode == "verify-full"
					if !insecureTransportAllowed(environment) && !secureMode {
						want := name + " must use sslmode=require, verify-ca, or verify-full in " + environment
						if err == nil || !strings.Contains(err.Error(), want) {
							t.Fatalf("FromLookup() error = %v, want %q", err, want)
						}
						return
					}
					if err != nil {
						t.Fatalf("FromLookup() error = %v, want database URL accepted", err)
					}
				})
			}
		}
	}
}

func TestConfigurationRequiresEncryptedRedisTransportOutsideLocalAndTest(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"local", "test", "staging", "production"} {
		for _, scheme := range []string{"redis", "rediss"} {
			t.Run(environment+"/"+scheme, func(t *testing.T) {
				t.Parallel()
				values := validEnvironmentValues(environment)
				values["REDIS_URL"] = scheme + "://redis.fidibeki.test:6379/0"
				_, err := config.FromLookup(func(name string) (string, bool) {
					value, exists := values[name]
					return value, exists
				})
				if scheme == "redis" && !insecureTransportAllowed(environment) {
					want := "REDIS_URL must use rediss:// in " + environment
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("FromLookup() error = %v, want %q", err, want)
					}
					return
				}
				if err != nil {
					t.Fatalf("FromLookup() error = %v, want Redis URL accepted", err)
				}
			})
		}
	}
}

func TestConfigurationValidatesRedisCertificateSettings(t *testing.T) {
	t.Parallel()
	caCertificate := testCACertificate(t)

	values := validEnvironmentValues("staging")
	values["REDIS_CA_CERT"] = caCertificate
	if _, err := config.FromLookup(func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	}); err != nil {
		t.Fatalf("valid Redis CA configuration error = %v", err)
	}

	values["REDIS_CA_CERT"] = "not a certificate"
	if _, err := config.FromLookup(func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	}); err == nil || !strings.Contains(err.Error(), "REDIS_CA_CERT must contain a valid PEM certificate") {
		t.Fatalf("invalid Redis CA configuration error = %v", err)
	}

	values = validEnvironmentValues("local")
	values["REDIS_CA_CERT"] = caCertificate
	if _, err := config.FromLookup(func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	}); err == nil || !strings.Contains(err.Error(), "REDIS_CA_CERT requires REDIS_URL to use rediss://") {
		t.Fatalf("plaintext Redis CA configuration error = %v", err)
	}

	values = validEnvironmentValues("staging")
	values["REDIS_URL"] += "?skip_verify=true"
	if _, err := config.FromLookup(func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	}); err == nil || !strings.Contains(err.Error(), "must not disable TLS certificate verification") {
		t.Fatalf("disabled Redis certificate verification error = %v", err)
	}
}

func TestConfigurationRejectsUnsupportedDatabaseAndRedisSchemes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "DATABASE_URL_APP", value: "mysql://user:secret@database.test/fidibeki", want: "must use postgres:// or postgresql://"},
		{name: "DATABASE_URL_ADMIN", value: databaseURLWithSSLMode("REQUIRE"), want: `has unsupported sslmode "REQUIRE"`},
		{name: "DATABASE_URL_DIRECT", value: databaseURLWithSSLMode("require") + "&sslmode=disable", want: "must set sslmode exactly once"},
		{name: "REDIS_URL", value: "https://redis.fidibeki.test", want: "must use redis:// or rediss://"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			values := validEnvironmentValues("staging")
			values[test.name] = test.value
			_, err := config.FromLookup(func(name string) (string, bool) {
				value, exists := values[name]
				return value, exists
			})
			want := test.name + " " + test.want
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("FromLookup() error = %v, want %q", err, want)
			}
		})
	}
}

func TestConfigurationAcceptsSecurePostgreSQLScheme(t *testing.T) {
	t.Parallel()
	values := validEnvironmentValues("production")
	values["DATABASE_URL_DIRECT"] = strings.Replace(
		databaseURLWithSSLMode("verify-full"), "postgres://", "postgresql://", 1,
	)
	if _, err := config.FromLookup(func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	}); err != nil {
		t.Fatalf("FromLookup() error = %v", err)
	}
}

func TestConfigurationRequiresSecureStorageEndpointsOutsideLocalAndTest(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"local", "test", "staging", "production"} {
		for _, name := range []string{"S3_ENDPOINT", "S3_PUBLIC_ENDPOINT"} {
			for _, scheme := range []string{"http", "https"} {
				t.Run(environment+"/"+name+"/"+scheme, func(t *testing.T) {
					t.Parallel()
					values := validEnvironmentValues(environment)
					values[name] = scheme + "://storage.fidibeki.test"
					_, err := config.FromLookup(func(name string) (string, bool) {
						value, exists := values[name]
						return value, exists
					})
					insecureAllowed := environment == "local" || environment == "test"
					if scheme == "http" && !insecureAllowed {
						want := name + " must use HTTPS in " + environment
						if err == nil || !strings.Contains(err.Error(), want) {
							t.Fatalf("FromLookup() error = %v, want %q", err, want)
						}
						return
					}
					if err != nil {
						t.Fatalf("FromLookup() error = %v, want endpoint accepted", err)
					}
				})
			}
		}
	}
}

func TestConfigurationRequiresSecurePublicURLsOutsideLocalAndTest(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"local", "test", "staging", "production"} {
		for _, name := range []string{"PUBLIC_BASE_URL", "WEB_PUBLIC_BASE_URL"} {
			for _, scheme := range []string{"http", "https"} {
				t.Run(environment+"/"+name+"/"+scheme, func(t *testing.T) {
					t.Parallel()
					values := validEnvironmentValues(environment)
					values[name] = scheme + "://api.fidibeki.test"
					_, err := config.FromLookup(func(name string) (string, bool) {
						value, exists := values[name]
						return value, exists
					})
					insecureAllowed := environment == "local" || environment == "test"
					if scheme == "http" && !insecureAllowed {
						want := name + " must use HTTPS in " + environment
						if err == nil || !strings.Contains(err.Error(), want) {
							t.Fatalf("FromLookup() error = %v, want %q", err, want)
						}
						return
					}
					if err != nil {
						t.Fatalf("FromLookup() error = %v, want URL accepted", err)
					}
				})
			}
		}
	}
}

func TestConfigurationRejectsUnsafePublicURLs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "embedded credentials", value: "https://user:secret@api.fidibeki.test", want: "must not include credentials"},
		{name: "unsupported scheme", value: "ftp://api.fidibeki.test", want: "must use HTTP or HTTPS"},
	} {
		for _, setting := range []string{"PUBLIC_BASE_URL", "WEB_PUBLIC_BASE_URL"} {
			t.Run(setting+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				values := validEnvironmentValues("staging")
				values[setting] = test.value
				_, err := config.FromLookup(func(name string) (string, bool) {
					value, exists := values[name]
					return value, exists
				})
				want := setting + " " + test.want
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("FromLookup() error = %v, want %q", err, want)
				}
			})
		}
	}
}

func validEnvironmentValues(environment string) map[string]string {
	databaseURL := databaseURLWithSSLMode("disable")
	redisURL := "redis://redis.fidibeki.test:6379/0"
	if !insecureTransportAllowed(environment) {
		databaseURL = databaseURLWithSSLMode("verify-full")
		redisURL = "rediss://redis.fidibeki.test:6379/0"
	}
	return map[string]string{
		"APP_ENV":                     environment,
		"BASE_DOMAIN":                 "fidibeki.test",
		"PUBLIC_BASE_URL":             "https://api.fidibeki.test",
		"PLATFORM_BACKOFFICE_URL":     "https://admin.fidibeki.test",
		"DATABASE_URL_APP":            databaseURL,
		"DATABASE_URL_ADMIN":          databaseURL,
		"DATABASE_URL_DIRECT":         databaseURL,
		"REDIS_URL":                   redisURL,
		"S3_ENDPOINT":                 "https://storage.fidibeki.test",
		"S3_PUBLIC_ENDPOINT":          "https://uploads.fidibeki.test",
		"S3_BUCKET":                   "fidibeki",
		"S3_ACCESS_KEY":               "key",
		"S3_SECRET_KEY":               "secret",
		"JWT_SECRET":                  strings.Repeat("jwt-", 8),
		"INTERNAL_PROXY_SECRET":       strings.Repeat("proxy-", 6),
		"INTEGRATION_ENCRYPTION_KEY":  base64.StdEncoding.EncodeToString([]byte(strings.Repeat("i", 32))),
		"TWO_FACTOR_ENCRYPTION_KEY":   base64.StdEncoding.EncodeToString([]byte(strings.Repeat("t", 32))),
		"STRIPE_SECRET_KEY":           "fixture",
		"STRIPE_WEBHOOK_SECRET":       "fixture",
		"STRIPE_PRICE_STARTER":        "fixture",
		"STRIPE_PRICE_GROWTH":         "fixture",
		"STRIPE_PRICE_SCALE":          "fixture",
		"STRIPE_PRICE_STARTER_YEARLY": "fixture",
		"STRIPE_PRICE_GROWTH_YEARLY":  "fixture",
		"STRIPE_PRICE_SCALE_YEARLY":   "fixture",
		"TURNSTILE_SECRET_KEY":        "fixture",
		"TURNSTILE_SITE_KEY":          "fixture",
		"GEMINI_API_KEY":              "fixture",
	}
}

func databaseURLWithSSLMode(mode string) string {
	const databaseURL = "postgres://user:secret@database.fidibeki.test/fidibeki"
	if mode == "" {
		return databaseURL
	}
	return databaseURL + "?sslmode=" + mode
}

func insecureTransportAllowed(environment string) bool {
	return environment == "local" || environment == "test"
}

func testCACertificate(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Fidibeki Redis Test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestConfigurationRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"APP_ENV":                        "preview",
		"BASE_DOMAIN":                    "https://fidibeki.localhost/path",
		"PUBLIC_BASE_URL":                "relative-url",
		"DATABASE_URL_APP":               "not-a-url",
		"DATABASE_URL_ADMIN":             "postgres://",
		"DATABASE_URL_DIRECT":            "://missing-scheme",
		"REDIS_URL":                      "redis-without-scheme",
		"S3_ENDPOINT":                    "/relative/storage",
		"S3_BUCKET":                      "fidibeki",
		"S3_ACCESS_KEY":                  "fidibeki",
		"S3_SECRET_KEY":                  "secret",
		"JWT_SECRET":                     "too-short",
		"ABUSE_BUCKET_SECRET":            "short",
		"ABUSE_BUCKET_PREVIOUS_SECRET":   "short",
		"INTERNAL_PROXY_SECRET":          "too-short",
		"TRUSTED_PROXY_CIDRS":            "not-a-network",
		"INTEGRATION_ENCRYPTION_KEY":     "not-base64",
		"TWO_FACTOR_ENCRYPTION_KEY":      "not-base64-either",
		"READINESS_TIMEOUT":              "soon",
		"SHUTDOWN_TIMEOUT":               "0s",
		"DATABASE_APP_MAX_CONNS":         "many",
		"DATABASE_ADMIN_MAX_CONNS":       "0",
		"S3_AUTO_CREATE_BUCKET":          "sometimes",
		"LOG_LEVEL":                      "verbose",
		"AUTH_ACCESS_TOKEN_TTL":          "later",
		"AUTH_MAGIC_LINK_TTL":            "0s",
		"AUTH_REFRESH_TOKEN_TTL":         "never",
		"AUTH_REFRESH_RETRY_GRACE":       "31s",
		"REPORTER_UNSUBSCRIBE_TTL":       "never",
		"EMAIL_DRIVER":                   "smtp",
		"EMAIL_QUEUE_CONCURRENCY":        "many",
		"AI_QUEUE_CONCURRENCY":           "none",
		"DOMAIN_QUEUE_CONCURRENCY":       "none",
		"MEDIA_QUEUE_CONCURRENCY":        "none",
		"PRIVACY_QUEUE_CONCURRENCY":      "none",
		"GEMINI_RPM":                     "fast",
		"AI_DAILY_REQUEST_LIMIT":         "unlimited",
		"AI_PROJECT_DAILY_REQUEST_LIMIT": "unlimited",
		"AI_RECONCILE_INTERVAL":          "later",
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
		"ABUSE_BUCKET_SECRET", "ABUSE_BUCKET_PREVIOUS_SECRET",
		"INTERNAL_PROXY_SECRET", "TRUSTED_PROXY_CIDRS", "INTEGRATION_ENCRYPTION_KEY",
		"TWO_FACTOR_ENCRYPTION_KEY",
		"READINESS_TIMEOUT", "SHUTDOWN_TIMEOUT", "DATABASE_APP_MAX_CONNS",
		"DATABASE_ADMIN_MAX_CONNS", "S3_AUTO_CREATE_BUCKET", "LOG_LEVEL",
		"AUTH_ACCESS_TOKEN_TTL", "AUTH_MAGIC_LINK_TTL", "AUTH_REFRESH_TOKEN_TTL",
		"AUTH_REFRESH_RETRY_GRACE",
		"REPORTER_UNSUBSCRIBE_TTL",
		"EMAIL_DRIVER", "EMAIL_QUEUE_CONCURRENCY",
		"AI_QUEUE_CONCURRENCY", "DOMAIN_QUEUE_CONCURRENCY",
		"MEDIA_QUEUE_CONCURRENCY", "PRIVACY_QUEUE_CONCURRENCY",
		"GEMINI_RPM", "AI_DAILY_REQUEST_LIMIT",
		"AI_PROJECT_DAILY_REQUEST_LIMIT", "AI_RECONCILE_INTERVAL",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error %q does not mention %s", err, expected)
		}
	}
}
