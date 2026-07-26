package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type LookupEnv func(string) (string, bool)

type Config struct {
	Environment   string
	HTTPAddress   string
	BaseDomain    string
	PublicBaseURL string
	// WebPublicBaseURL supplies the scheme and port of the browser-facing tier
	// when it differs from the API's own public URL, which it does in local
	// development. Every tenant-facing browser URL — SAML entity ID, assertion
	// consumer, and login redirects — is derived from it, so no deployment URL
	// is ever hardcoded. Defaults to PublicBaseURL.
	WebPublicBaseURL          string
	DatabaseURLApp            string
	DatabaseURLAdmin          string
	DatabaseURLDirect         string
	RedisURL                  string
	S3Endpoint                string
	S3PublicEndpoint          string
	S3Region                  string
	S3Bucket                  string
	S3AccessKey               string
	S3SecretKey               string
	S3VirtualHostStyle        bool
	JWTSecret                 string
	AbuseBucketSecret         string
	AbuseBucketPreviousSecret string
	InternalProxySecret       string
	TrustedProxyCIDRs         []netip.Prefix
	IntegrationEncryptionKey  string
	TwoFactorEncryptionKey    string
	StripeSecretKey           string
	StripeWebhookSecret       string
	StripePriceStarter        string
	StripePriceGrowth         string
	StripePriceScale          string
	StripePriceStarterYearly  string
	StripePriceGrowthYearly   string
	StripePriceScaleYearly    string
	TurnstileSecretKey        string
	TurnstileSiteKey          string
	AccessTokenTTL            time.Duration
	MagicLinkTTL              time.Duration
	RefreshTokenTTL           time.Duration
	RefreshRetryGrace         time.Duration
	ReporterTokenTTL          time.Duration
	EmailDriver               string
	EmailCaptureDir           string
	ResendAPIKey              string
	ResendAuthFrom            string
	ResendWelcomeFrom         string
	ResendWelcomeReplyTo      string
	ResendWebhookSecret       string
	EmailConcurrency          int
	AIConcurrency             int
	DomainConcurrency         int
	MediaConcurrency          int
	PrivacyConcurrency        int
	GeminiAPIKey              string
	GeminiModel               string
	GeminiRPM                 int
	AIDailyLimit              int
	AIProjectDailyLimit       int
	AIReconcileEvery          time.Duration
	DigestMembersPerRun       int
	AppDBMaxConns             int32
	AdminDBMaxConns           int32
	S3AutoCreate              bool
	SentryDSN                 string
	AppVersion                string
	LogLevel                  string
	MigrationsPath            string
	ReadinessTimeout          time.Duration
	ShutdownTimeout           time.Duration
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	return FromLookup(os.LookupEnv)
}

func FromLookup(lookup LookupEnv) (Config, error) {
	get := func(name, fallback string) string {
		if value, ok := lookup(name); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		return fallback
	}

	readinessTimeout, readinessErr := time.ParseDuration(get("READINESS_TIMEOUT", "2s"))
	shutdownTimeout, shutdownErr := time.ParseDuration(get("SHUTDOWN_TIMEOUT", "10s"))
	accessTokenTTL, accessTokenTTLErr := time.ParseDuration(get("AUTH_ACCESS_TOKEN_TTL", "10m"))
	magicLinkTTL, magicLinkTTLErr := time.ParseDuration(get("AUTH_MAGIC_LINK_TTL", "10m"))
	refreshTokenTTL, refreshTokenTTLErr := time.ParseDuration(get("AUTH_REFRESH_TOKEN_TTL", "720h"))
	refreshRetryGrace, refreshRetryGraceErr := time.ParseDuration(get("AUTH_REFRESH_RETRY_GRACE", "0s"))
	reporterTokenTTL, reporterTokenTTLErr := time.ParseDuration(get("REPORTER_UNSUBSCRIBE_TTL", "2160h"))
	appDBMaxConns, appDBMaxConnsErr := strconv.ParseInt(get("DATABASE_APP_MAX_CONNS", "10"), 10, 32)
	adminDBMaxConns, adminDBMaxConnsErr := strconv.ParseInt(get("DATABASE_ADMIN_MAX_CONNS", "5"), 10, 32)
	s3AutoCreate, s3AutoCreateErr := strconv.ParseBool(get("S3_AUTO_CREATE_BUCKET", "false"))
	s3VirtualHostStyle, s3VirtualHostStyleErr := strconv.ParseBool(
		get("S3_VIRTUAL_HOST_STYLE", "false"),
	)
	emailConcurrency, emailConcurrencyErr := strconv.Atoi(get("EMAIL_QUEUE_CONCURRENCY", "20"))
	aiConcurrency, aiConcurrencyErr := strconv.Atoi(get("AI_QUEUE_CONCURRENCY", "3"))
	domainConcurrency, domainConcurrencyErr := strconv.Atoi(get("DOMAIN_QUEUE_CONCURRENCY", "4"))
	mediaConcurrency, mediaConcurrencyErr := strconv.Atoi(get("MEDIA_QUEUE_CONCURRENCY", "2"))
	privacyConcurrency, privacyConcurrencyErr := strconv.Atoi(get("PRIVACY_QUEUE_CONCURRENCY", "1"))
	geminiRPM, geminiRPMErr := strconv.Atoi(get("GEMINI_RPM", "15"))
	aiDailyLimit, aiDailyLimitErr := strconv.Atoi(get("AI_DAILY_REQUEST_LIMIT", "5000"))
	aiProjectDailyLimit, aiProjectDailyLimitErr := strconv.Atoi(
		get("AI_PROJECT_DAILY_REQUEST_LIMIT", "2000"),
	)
	aiReconcileEvery, aiReconcileEveryErr := time.ParseDuration(get("AI_RECONCILE_INTERVAL", "5m"))
	digestMembersPerRun, digestMembersPerRunErr := strconv.Atoi(
		get("DIGEST_MEMBERS_PER_RUN", "200"),
	)
	trustedProxyCIDRs, trustedProxyCIDRsErr := parseCIDRs(get("TRUSTED_PROXY_CIDRS", ""))
	s3Endpoint := get("S3_ENDPOINT", "")
	jwtSecret := get("JWT_SECRET", "")
	abuseBucketSecret := get("ABUSE_BUCKET_SECRET", deriveAbuseBucketSecret(jwtSecret))
	httpAddress := ":8080"
	if port := get("PORT", ""); port != "" {
		httpAddress = ":" + port
	}
	config := Config{
		Environment:               get("APP_ENV", ""),
		HTTPAddress:               get("HTTP_ADDRESS", httpAddress),
		BaseDomain:                get("BASE_DOMAIN", ""),
		PublicBaseURL:             get("PUBLIC_BASE_URL", ""),
		WebPublicBaseURL:          get("WEB_PUBLIC_BASE_URL", get("PUBLIC_BASE_URL", "")),
		DatabaseURLApp:            get("DATABASE_URL_APP", ""),
		DatabaseURLAdmin:          get("DATABASE_URL_ADMIN", ""),
		DatabaseURLDirect:         get("DATABASE_URL_DIRECT", ""),
		RedisURL:                  get("REDIS_URL", ""),
		S3Endpoint:                s3Endpoint,
		S3PublicEndpoint:          get("S3_PUBLIC_ENDPOINT", s3Endpoint),
		S3Region:                  get("S3_REGION", "us-east-1"),
		S3Bucket:                  get("S3_BUCKET", ""),
		S3AccessKey:               get("S3_ACCESS_KEY", ""),
		S3SecretKey:               get("S3_SECRET_KEY", ""),
		S3VirtualHostStyle:        s3VirtualHostStyle,
		JWTSecret:                 jwtSecret,
		AbuseBucketSecret:         abuseBucketSecret,
		AbuseBucketPreviousSecret: get("ABUSE_BUCKET_PREVIOUS_SECRET", ""),
		InternalProxySecret:       get("INTERNAL_PROXY_SECRET", ""),
		TrustedProxyCIDRs:         trustedProxyCIDRs,
		IntegrationEncryptionKey:  get("INTEGRATION_ENCRYPTION_KEY", ""),
		TwoFactorEncryptionKey:    get("TWO_FACTOR_ENCRYPTION_KEY", ""),
		StripeSecretKey:           get("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:       get("STRIPE_WEBHOOK_SECRET", ""),
		StripePriceStarter:        get("STRIPE_PRICE_STARTER", ""),
		StripePriceGrowth:         get("STRIPE_PRICE_GROWTH", ""),
		StripePriceScale:          get("STRIPE_PRICE_SCALE", ""),
		StripePriceStarterYearly:  get("STRIPE_PRICE_STARTER_YEARLY", ""),
		StripePriceGrowthYearly:   get("STRIPE_PRICE_GROWTH_YEARLY", ""),
		StripePriceScaleYearly:    get("STRIPE_PRICE_SCALE_YEARLY", ""),
		TurnstileSecretKey:        get("TURNSTILE_SECRET_KEY", ""),
		TurnstileSiteKey:          get("TURNSTILE_SITE_KEY", ""),
		AccessTokenTTL:            accessTokenTTL,
		MagicLinkTTL:              magicLinkTTL,
		RefreshTokenTTL:           refreshTokenTTL,
		RefreshRetryGrace:         refreshRetryGrace,
		ReporterTokenTTL:          reporterTokenTTL,
		EmailDriver:               get("EMAIL_DRIVER", "capture"),
		EmailCaptureDir:           get("EMAIL_CAPTURE_DIR", "/tmp/fidibeki-emails"),
		ResendAPIKey:              get("RESEND_API_KEY", ""),
		ResendAuthFrom:            get("RESEND_AUTH_FROM", "Fidibeki <auth@fidibeki.com>"),
		ResendWelcomeFrom:         get("RESEND_WELCOME_FROM", "Acidiney from Fidibeki <acidiney.dias@fidibeki.com>"),
		ResendWelcomeReplyTo:      get("RESEND_WELCOME_REPLY_TO", "acidiney.dias@fidibeki.com"),
		ResendWebhookSecret:       get("RESEND_WEBHOOK_SECRET", ""),
		EmailConcurrency:          emailConcurrency,
		AIConcurrency:             aiConcurrency,
		DomainConcurrency:         domainConcurrency,
		MediaConcurrency:          mediaConcurrency,
		PrivacyConcurrency:        privacyConcurrency,
		GeminiAPIKey:              get("GEMINI_API_KEY", ""),
		GeminiModel:               get("GEMINI_MODEL", "gemini-3.1-flash-lite"),
		GeminiRPM:                 geminiRPM,
		AIDailyLimit:              aiDailyLimit,
		AIProjectDailyLimit:       aiProjectDailyLimit,
		DigestMembersPerRun:       digestMembersPerRun,
		AIReconcileEvery:          aiReconcileEvery,
		AppDBMaxConns:             int32(appDBMaxConns),
		AdminDBMaxConns:           int32(adminDBMaxConns),
		S3AutoCreate:              s3AutoCreate,
		SentryDSN:                 get("SENTRY_DSN", ""),
		AppVersion:                get("APP_VERSION", "dev"),
		LogLevel:                  get("LOG_LEVEL", "info"),
		MigrationsPath:            get("MIGRATIONS_PATH", "../infra/migrations"),
		ReadinessTimeout:          readinessTimeout,
		ShutdownTimeout:           shutdownTimeout,
	}

	var validationErrors []error
	if readinessErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("READINESS_TIMEOUT: %w", readinessErr))
	}
	if shutdownErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", shutdownErr))
	}
	if accessTokenTTLErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_ACCESS_TOKEN_TTL: %w", accessTokenTTLErr))
	}
	if magicLinkTTLErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_MAGIC_LINK_TTL: %w", magicLinkTTLErr))
	}
	if refreshTokenTTLErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_REFRESH_TOKEN_TTL: %w", refreshTokenTTLErr))
	}
	if refreshRetryGraceErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_REFRESH_RETRY_GRACE: %w", refreshRetryGraceErr))
	}
	if reporterTokenTTLErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("REPORTER_UNSUBSCRIBE_TTL: %w", reporterTokenTTLErr))
	}
	if appDBMaxConnsErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("DATABASE_APP_MAX_CONNS: %w", appDBMaxConnsErr))
	}
	if adminDBMaxConnsErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("DATABASE_ADMIN_MAX_CONNS: %w", adminDBMaxConnsErr))
	}
	if s3AutoCreateErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("S3_AUTO_CREATE_BUCKET: %w", s3AutoCreateErr))
	}
	if s3VirtualHostStyleErr != nil {
		validationErrors = append(
			validationErrors,
			fmt.Errorf("S3_VIRTUAL_HOST_STYLE: %w", s3VirtualHostStyleErr),
		)
	}
	if emailConcurrencyErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("EMAIL_QUEUE_CONCURRENCY: %w", emailConcurrencyErr))
	}
	if aiConcurrencyErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AI_QUEUE_CONCURRENCY: %w", aiConcurrencyErr))
	}
	if domainConcurrencyErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("DOMAIN_QUEUE_CONCURRENCY: %w", domainConcurrencyErr))
	}
	if mediaConcurrencyErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("MEDIA_QUEUE_CONCURRENCY: %w", mediaConcurrencyErr))
	}
	if privacyConcurrencyErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("PRIVACY_QUEUE_CONCURRENCY: %w", privacyConcurrencyErr))
	}
	if geminiRPMErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("GEMINI_RPM: %w", geminiRPMErr))
	}
	if aiDailyLimitErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AI_DAILY_REQUEST_LIMIT: %w", aiDailyLimitErr))
	}
	if aiProjectDailyLimitErr != nil {
		validationErrors = append(validationErrors,
			fmt.Errorf("AI_PROJECT_DAILY_REQUEST_LIMIT: %w", aiProjectDailyLimitErr))
	}
	if aiReconcileEveryErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AI_RECONCILE_INTERVAL: %w", aiReconcileEveryErr))
	}
	if digestMembersPerRunErr != nil {
		validationErrors = append(validationErrors,
			fmt.Errorf("DIGEST_MEMBERS_PER_RUN: %w", digestMembersPerRunErr))
	}
	if trustedProxyCIDRsErr != nil {
		validationErrors = append(validationErrors, fmt.Errorf("TRUSTED_PROXY_CIDRS: %w", trustedProxyCIDRsErr))
	}
	validationErrors = append(validationErrors, config.validate()...)
	if len(validationErrors) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(validationErrors...))
	}

	return config, nil
}

func (c Config) validate() []error {
	var validationErrors []error
	required := map[string]string{
		"APP_ENV":                    c.Environment,
		"BASE_DOMAIN":                c.BaseDomain,
		"PUBLIC_BASE_URL":            c.PublicBaseURL,
		"DATABASE_URL_APP":           c.DatabaseURLApp,
		"DATABASE_URL_ADMIN":         c.DatabaseURLAdmin,
		"DATABASE_URL_DIRECT":        c.DatabaseURLDirect,
		"REDIS_URL":                  c.RedisURL,
		"S3_ENDPOINT":                c.S3Endpoint,
		"S3_BUCKET":                  c.S3Bucket,
		"S3_ACCESS_KEY":              c.S3AccessKey,
		"S3_SECRET_KEY":              c.S3SecretKey,
		"JWT_SECRET":                 c.JWTSecret,
		"INTEGRATION_ENCRYPTION_KEY": c.IntegrationEncryptionKey,
		"TWO_FACTOR_ENCRYPTION_KEY":  c.TwoFactorEncryptionKey,
	}
	for name, value := range required {
		if value == "" {
			validationErrors = append(validationErrors, fmt.Errorf("%s is required", name))
		}
	}

	if c.Environment != "" && c.Environment != "local" && c.Environment != "test" &&
		c.Environment != "staging" && c.Environment != "production" {
		validationErrors = append(validationErrors, fmt.Errorf("APP_ENV must be local, test, staging, or production"))
	}
	if strings.Contains(c.BaseDomain, "://") || strings.Contains(c.BaseDomain, "/") {
		validationErrors = append(validationErrors, fmt.Errorf("BASE_DOMAIN must be a hostname without scheme or path"))
	}
	if c.PublicBaseURL != "" {
		parsed, err := url.ParseRequestURI(c.PublicBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			validationErrors = append(validationErrors, fmt.Errorf("PUBLIC_BASE_URL must be an absolute URL"))
		}
	}
	if c.WebPublicBaseURL != "" {
		parsed, err := url.ParseRequestURI(c.WebPublicBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			validationErrors = append(validationErrors, fmt.Errorf("WEB_PUBLIC_BASE_URL must be an absolute URL"))
		}
	}
	for name, value := range map[string]string{
		"DATABASE_URL_APP": c.DatabaseURLApp, "DATABASE_URL_ADMIN": c.DatabaseURLAdmin,
		"DATABASE_URL_DIRECT": c.DatabaseURLDirect, "REDIS_URL": c.RedisURL,
		"S3_ENDPOINT": c.S3Endpoint, "S3_PUBLIC_ENDPOINT": c.S3PublicEndpoint,
	} {
		if value == "" {
			continue
		}
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			validationErrors = append(validationErrors, fmt.Errorf("%s must be an absolute URL", name))
		}
	}
	if c.JWTSecret != "" && len(c.JWTSecret) < 32 {
		validationErrors = append(validationErrors, fmt.Errorf("JWT_SECRET must be at least 32 characters"))
	}
	if c.AbuseBucketSecret != "" && len(c.AbuseBucketSecret) < 32 {
		validationErrors = append(validationErrors, fmt.Errorf("ABUSE_BUCKET_SECRET must be at least 32 characters"))
	}
	if c.AbuseBucketPreviousSecret != "" && len(c.AbuseBucketPreviousSecret) < 32 {
		validationErrors = append(validationErrors,
			fmt.Errorf("ABUSE_BUCKET_PREVIOUS_SECRET must be at least 32 characters"))
	}
	if c.AbuseBucketPreviousSecret != "" &&
		hmac.Equal([]byte(c.AbuseBucketSecret), []byte(c.AbuseBucketPreviousSecret)) {
		validationErrors = append(validationErrors,
			fmt.Errorf("ABUSE_BUCKET_PREVIOUS_SECRET must differ from ABUSE_BUCKET_SECRET"))
	}
	if c.InternalProxySecret != "" && len(c.InternalProxySecret) < 32 {
		validationErrors = append(validationErrors, fmt.Errorf("INTERNAL_PROXY_SECRET must be at least 32 characters"))
	}
	if c.IntegrationEncryptionKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(c.IntegrationEncryptionKey)
		if err != nil || len(decoded) != 32 {
			validationErrors = append(validationErrors,
				fmt.Errorf("INTEGRATION_ENCRYPTION_KEY must be base64 encoding of exactly 32 bytes"))
		}
	}
	if c.TwoFactorEncryptionKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(c.TwoFactorEncryptionKey)
		if err != nil || len(decoded) != 32 {
			validationErrors = append(validationErrors,
				fmt.Errorf("TWO_FACTOR_ENCRYPTION_KEY must be base64 encoding of exactly 32 bytes"))
		}
	}
	if c.TwoFactorEncryptionKey != "" && c.TwoFactorEncryptionKey == c.IntegrationEncryptionKey {
		validationErrors = append(validationErrors,
			fmt.Errorf("TWO_FACTOR_ENCRYPTION_KEY must differ from INTEGRATION_ENCRYPTION_KEY"))
	}
	if c.AccessTokenTTL <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_ACCESS_TOKEN_TTL must be positive"))
	}
	if c.MagicLinkTTL <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_MAGIC_LINK_TTL must be positive"))
	}
	if c.RefreshTokenTTL <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_REFRESH_TOKEN_TTL must be positive"))
	}
	if c.RefreshRetryGrace < 0 || c.RefreshRetryGrace > 30*time.Second {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_REFRESH_RETRY_GRACE must be between 0s and 30s"))
	}
	if c.RefreshRetryGrace > c.RefreshTokenTTL {
		validationErrors = append(validationErrors, fmt.Errorf("AUTH_REFRESH_RETRY_GRACE must not exceed AUTH_REFRESH_TOKEN_TTL"))
	}
	if c.ReporterTokenTTL <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("REPORTER_UNSUBSCRIBE_TTL must be positive"))
	}
	if c.EmailDriver != "capture" && c.EmailDriver != "resend" {
		validationErrors = append(validationErrors, fmt.Errorf("EMAIL_DRIVER must be capture or resend"))
	}
	if c.EmailDriver == "capture" && c.EmailCaptureDir == "" {
		validationErrors = append(validationErrors, fmt.Errorf("EMAIL_CAPTURE_DIR is required for capture email driver"))
	}
	if c.EmailDriver == "resend" {
		if c.ResendAPIKey == "" {
			validationErrors = append(validationErrors, fmt.Errorf("RESEND_API_KEY is required for resend email driver"))
		}
		if c.ResendAuthFrom == "" {
			validationErrors = append(validationErrors, fmt.Errorf("RESEND_AUTH_FROM is required for resend email driver"))
		}
		if _, err := mail.ParseAddress(c.ResendWelcomeFrom); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("RESEND_WELCOME_FROM must be a valid email address"))
		}
		if _, err := mail.ParseAddress(c.ResendWelcomeReplyTo); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("RESEND_WELCOME_REPLY_TO must be a valid email address"))
		}
		if c.Environment == "production" && c.ResendWebhookSecret == "" {
			validationErrors = append(validationErrors, fmt.Errorf("RESEND_WEBHOOK_SECRET is required for resend email driver in production"))
		}
	}
	if c.EmailConcurrency <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("EMAIL_QUEUE_CONCURRENCY must be positive"))
	}
	if c.AIConcurrency <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("AI_QUEUE_CONCURRENCY must be positive"))
	}
	if c.DomainConcurrency <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("DOMAIN_QUEUE_CONCURRENCY must be positive"))
	}
	if c.MediaConcurrency <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("MEDIA_QUEUE_CONCURRENCY must be positive"))
	}
	if c.PrivacyConcurrency <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("PRIVACY_QUEUE_CONCURRENCY must be positive"))
	}
	if c.GeminiModel == "" {
		validationErrors = append(validationErrors, fmt.Errorf("GEMINI_MODEL is required"))
	}
	if c.GeminiRPM <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("GEMINI_RPM must be positive"))
	}
	if c.AIDailyLimit <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("AI_DAILY_REQUEST_LIMIT must be positive"))
	}
	if c.AIProjectDailyLimit <= 0 {
		validationErrors = append(validationErrors,
			fmt.Errorf("AI_PROJECT_DAILY_REQUEST_LIMIT must be positive"))
	}
	if c.AIReconcileEvery <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("AI_RECONCILE_INTERVAL must be positive"))
	}
	if c.DigestMembersPerRun <= 0 {
		validationErrors = append(validationErrors,
			fmt.Errorf("DIGEST_MEMBERS_PER_RUN must be positive"))
	}
	if c.Environment == "production" && c.GeminiAPIKey == "" {
		validationErrors = append(validationErrors, fmt.Errorf("GEMINI_API_KEY is required in production"))
	}
	if c.Environment == "production" || c.Environment == "staging" {
		if c.InternalProxySecret == "" {
			validationErrors = append(validationErrors, fmt.Errorf("INTERNAL_PROXY_SECRET is required in %s", c.Environment))
		}
		for name, value := range map[string]string{
			"STRIPE_SECRET_KEY": c.StripeSecretKey, "STRIPE_WEBHOOK_SECRET": c.StripeWebhookSecret,
			"STRIPE_PRICE_STARTER": c.StripePriceStarter, "STRIPE_PRICE_GROWTH": c.StripePriceGrowth,
			"STRIPE_PRICE_SCALE":          c.StripePriceScale,
			"STRIPE_PRICE_STARTER_YEARLY": c.StripePriceStarterYearly,
			"STRIPE_PRICE_GROWTH_YEARLY":  c.StripePriceGrowthYearly,
			"STRIPE_PRICE_SCALE_YEARLY":   c.StripePriceScaleYearly,
			"TURNSTILE_SECRET_KEY":        c.TurnstileSecretKey,
			"TURNSTILE_SITE_KEY":          c.TurnstileSiteKey,
		} {
			if value == "" {
				validationErrors = append(validationErrors, fmt.Errorf("%s is required in %s", name, c.Environment))
			}
		}
	}
	if c.ReadinessTimeout <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("READINESS_TIMEOUT must be positive"))
	}
	if c.ShutdownTimeout <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("SHUTDOWN_TIMEOUT must be positive"))
	}
	if c.AppDBMaxConns <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("DATABASE_APP_MAX_CONNS must be positive"))
	}
	if c.AdminDBMaxConns <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("DATABASE_ADMIN_MAX_CONNS must be positive"))
	}
	if c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		validationErrors = append(validationErrors, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error"))
	}

	return validationErrors
}

func (c Config) Redacted() map[string]any {
	return map[string]any{
		"environment":                  c.Environment,
		"http_address":                 c.HTTPAddress,
		"base_domain":                  c.BaseDomain,
		"public_base_url":              c.PublicBaseURL,
		"web_public_base_url":          c.WebPublicBaseURL,
		"database_url_app":             "[REDACTED]",
		"database_url_admin":           "[REDACTED]",
		"database_url_direct":          "[REDACTED]",
		"redis_url":                    "[REDACTED]",
		"s3_endpoint":                  c.S3Endpoint,
		"s3_public_endpoint":           c.S3PublicEndpoint,
		"s3_region":                    c.S3Region,
		"s3_bucket":                    c.S3Bucket,
		"s3_access_key":                "[REDACTED]",
		"s3_secret_key":                "[REDACTED]",
		"s3_virtual_host_style":        c.S3VirtualHostStyle,
		"jwt_secret":                   "[REDACTED]",
		"abuse_bucket_secret":          "[REDACTED]",
		"abuse_bucket_previous_secret": "[REDACTED]",
		"internal_proxy_secret":        "[REDACTED]",
		"trusted_proxy_cidrs":          c.TrustedProxyCIDRs,
		"integration_encryption_key":   "[REDACTED]",
		"stripe_secret_key":            "[REDACTED]",
		"stripe_webhook_secret":        "[REDACTED]",
		"stripe_price_starter":         c.StripePriceStarter,
		"stripe_price_growth":          c.StripePriceGrowth,
		"stripe_price_scale":           c.StripePriceScale,
		"stripe_price_starter_yearly":  c.StripePriceStarterYearly,
		"stripe_price_growth_yearly":   c.StripePriceGrowthYearly,
		"stripe_price_scale_yearly":    c.StripePriceScaleYearly,
		"turnstile_secret_key":         "[REDACTED]",
		"turnstile_site_key":           c.TurnstileSiteKey,
		"access_token_ttl":             c.AccessTokenTTL.String(),
		"magic_link_ttl":               c.MagicLinkTTL.String(),
		"refresh_token_ttl":            c.RefreshTokenTTL.String(),
		"refresh_retry_grace":          c.RefreshRetryGrace.String(),
		"reporter_token_ttl":           c.ReporterTokenTTL.String(),
		"email_driver":                 c.EmailDriver,
		"email_capture_dir":            c.EmailCaptureDir,
		"resend_api_key":               "[REDACTED]",
		"resend_auth_from":             c.ResendAuthFrom,
		"resend_welcome_from":          c.ResendWelcomeFrom,
		"resend_welcome_reply_to":      c.ResendWelcomeReplyTo,
		"resend_webhook_secret":        "[REDACTED]",
		"email_concurrency":            c.EmailConcurrency,
		"ai_concurrency":               c.AIConcurrency,
		"domain_concurrency":           c.DomainConcurrency,
		"media_concurrency":            c.MediaConcurrency,
		"privacy_concurrency":          c.PrivacyConcurrency,
		"gemini_api_key":               "[REDACTED]",
		"gemini_model":                 c.GeminiModel,
		"gemini_rpm":                   c.GeminiRPM,
		"ai_daily_limit":               c.AIDailyLimit,
		"ai_project_daily_limit":       c.AIProjectDailyLimit,
		"ai_reconcile_every":           c.AIReconcileEvery.String(),
		"digest_members_per_run":       c.DigestMembersPerRun,
		"sentry_dsn":                   "[REDACTED]",
		"app_version":                  c.AppVersion,
		"log_level":                    c.LogLevel,
		"app_db_max_conns":             c.AppDBMaxConns,
		"admin_db_max_conns":           c.AdminDBMaxConns,
		"readiness_timeout":            c.ReadinessTimeout.String(),
		"shutdown_timeout":             c.ShutdownTimeout.String(),
	}
}

func deriveAbuseBucketSecret(jwtSecret string) string {
	if jwtSecret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	_, _ = mac.Write([]byte("fidibeki:abuse-buckets:v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

func parseCIDRs(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	values := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid network prefix", strings.TrimSpace(value))
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}
