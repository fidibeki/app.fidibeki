package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	aiprocessing "github.com/acidiney/app.fidibeki/backend/internal/ai"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/apikey"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/billing"
	"github.com/acidiney/app.fidibeki/backend/internal/breachregister"
	"github.com/acidiney/app.fidibeki/backend/internal/cache"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
	"github.com/acidiney/app.fidibeki/backend/internal/identity"
	"github.com/acidiney/app.fidibeki/backend/internal/integration"
	"github.com/acidiney/app.fidibeki/backend/internal/issue"
	"github.com/acidiney/app.fidibeki/backend/internal/issuetracker"
	"github.com/acidiney/app.fidibeki/backend/internal/logging"
	"github.com/acidiney/app.fidibeki/backend/internal/membernotify"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/observability"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/acidiney/app.fidibeki/backend/internal/platformadmin"
	"github.com/acidiney/app.fidibeki/backend/internal/privacy"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/queuemonitor"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/replay"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
	"github.com/acidiney/app.fidibeki/backend/internal/samlsso"
	"github.com/acidiney/app.fidibeki/backend/internal/server"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/tenantapi"
	"github.com/acidiney/app.fidibeki/backend/internal/tenantmember"
	"github.com/acidiney/app.fidibeki/backend/internal/testimonial"
	"github.com/acidiney/app.fidibeki/backend/internal/triage"
	"github.com/acidiney/app.fidibeki/backend/internal/twofactor"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetanalytics"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	jobworker "github.com/acidiney/app.fidibeki/backend/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	settings, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 1
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(settings.LogLevel)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "log level error: %v\n", err)
		return 1
	}
	logger := logging.New(os.Stdout, level)
	slog.SetDefault(logger)
	logger.Info("configuration loaded", "config", settings.Redacted())

	if err := observability.InitSentry(observability.SentryConfig{
		DSN: settings.SentryDSN, Environment: settings.Environment, Release: settings.AppVersion,
		Debug: settings.Environment == "local",
	}); err != nil {
		logger.Error("Sentry initialization failed", "error", err)
		return 1
	}
	defer observability.FlushSentry(2 * time.Second)

	if len(args) > 0 && args[0] == "migrate" {
		return runMigrations(logger, settings, args[1:])
	}
	if len(args) > 0 && args[0] != "serve" && args[0] != "worker" && args[0] != "admin" {
		logger.Error("unknown command", "command", args[0])
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var runErr error
	switch {
	case len(args) > 0 && args[0] == "worker":
		runErr = runWorker(ctx, logger, settings)
	case len(args) > 0 && args[0] == "admin":
		return runAdmin(ctx, logger, settings, args[1:])
	default:
		runErr = serve(ctx, logger, settings)
	}
	if runErr != nil {
		logger.Error("process stopped with error", "error", runErr)
		return 1
	}
	return 0
}

func runMigrations(logger *slog.Logger, settings config.Config, args []string) int {
	action := "up"
	if len(args) > 0 {
		action = args[0]
	}
	migrator, err := db.NewMigrator(settings.DatabaseURLDirect, settings.MigrationsPath)
	if err != nil {
		logger.Error("migration initialization failed", "error", err)
		return 1
	}
	defer func() {
		if err := migrator.Close(); err != nil {
			logger.Error("migration close failed", "error", err)
		}
	}()

	switch action {
	case "up":
		err = migrator.Up()
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err = db.ConfigureRuntimeRoles(
				ctx,
				settings.DatabaseURLDirect,
				settings.DatabaseURLApp,
				settings.DatabaseURLAdmin,
			)
			cancel()
		}
	case "down":
		err = migrator.Down()
	case "version":
		var version uint
		var dirty bool
		version, dirty, err = migrator.Version()
		if err == nil {
			logger.Info("migration version", "version", version, "dirty", dirty)
		}
	default:
		logger.Error("unknown migration action", "action", action)
		return 2
	}
	if err != nil {
		logger.Error("migration failed", "action", action, "error", err)
		return 1
	}
	logger.Info("migration completed", "action", action)
	return 0
}

func serve(ctx context.Context, logger *slog.Logger, settings config.Config) error {
	integrationCipher, err := integration.NewCipher(settings.IntegrationEncryptionKey)
	if err != nil {
		return err
	}
	twoFactorCipher, err := twofactor.NewCipher(settings.TwoFactorEncryptionKey)
	if err != nil {
		return err
	}
	pools, err := db.OpenPools(
		ctx,
		settings.DatabaseURLApp,
		settings.DatabaseURLAdmin,
		settings.AppDBMaxConns,
		settings.AdminDBMaxConns,
	)
	if err != nil {
		return err
	}
	defer pools.Close()

	redisClient, err := cache.Open(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			logger.Warn("Redis close failed", "error", err)
		}
	}()
	abuseKeyring, err := abuse.NewKeyring(
		settings.AbuseBucketSecret,
		settings.AbuseBucketPreviousSecret,
	)
	if err != nil {
		return fmt.Errorf("initialize abuse bucket keyring: %w", err)
	}
	publicLimiter := ratelimit.NewRedisLimiter(redisClient.Client())
	abuseEvaluator := abuse.Evaluator{Limiter: publicLimiter, Keyring: abuseKeyring}
	synchronousDiagnostics := abuse.Recorder{Database: pools.Admin, Store: abuse.Store{}}
	abuseDiagnostics := abuse.NewBufferedRecorder(synchronousDiagnostics, logger)
	go func() {
		if err := abuseDiagnostics.Run(ctx, time.Second); err != nil && ctx.Err() == nil {
			logger.Error("abuse diagnostic buffer stopped", "error", err)
		}
	}()

	objectStorage, err := storage.Open(storage.Config{
		Endpoint: settings.S3Endpoint, PublicEndpoint: settings.S3PublicEndpoint,
		Region: settings.S3Region, Bucket: settings.S3Bucket,
		AccessKey: settings.S3AccessKey, SecretKey: settings.S3SecretKey,
		VirtualHostStyle: settings.S3VirtualHostStyle,
	})
	if err != nil {
		return err
	}
	if settings.S3AutoCreate {
		if err := objectStorage.EnsureBucket(ctx, settings.S3Region); err != nil {
			return err
		}
	}
	jwtManager, err := auth.NewJWTManager(settings.JWTSecret, platformadmin.DefaultIssuer, settings.AccessTokenTTL)
	if err != nil {
		return fmt.Errorf("initialize JWT manager: %w", err)
	}
	reporterTokens, err := auth.NewJWTManager(
		settings.JWTSecret, platformadmin.DefaultIssuer, settings.ReporterTokenTTL,
	)
	if err != nil {
		return fmt.Errorf("initialize reporter token manager: %w", err)
	}
	// Digest footer links live as long as the mail that carries them, so they
	// share the long-lived unsubscribe TTL rather than the 10-minute session one.
	memberNotificationTokens, err := auth.NewJWTManager(
		settings.JWTSecret, platformadmin.DefaultIssuer, settings.ReporterTokenTTL,
	)
	if err != nil {
		return fmt.Errorf("initialize member notification token manager: %w", err)
	}
	emailQueue, err := email.OpenQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := emailQueue.Close(); err != nil {
			logger.Warn("email queue close failed", "error", err)
		}
	}()
	aiQueue, err := aiprocessing.OpenQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := aiQueue.Close(); err != nil {
			logger.Warn("AI queue close failed", "error", err)
		}
	}()
	aiScheduler := aiprocessing.Scheduler{
		Store: aiprocessing.Store{
			Database: pools.Admin, ProjectDailyLimit: settings.AIProjectDailyLimit,
			Diagnostics: abuseDiagnostics, Logger: logger,
		}, Queue: aiQueue,
		DailyLimit: settings.AIDailyLimit, Logger: logger,
	}
	verificationQueue, err := feedback.OpenVerificationQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := verificationQueue.Close(); err != nil {
			logger.Warn("attachment verification queue close failed", "error", err)
		}
	}()
	verificationScheduler := feedback.VerificationScheduler{
		Store: feedback.VerificationStore{Database: pools.Admin},
		Queue: verificationQueue, Storage: objectStorage, Logger: logger,
	}
	replayQueue, err := replay.OpenQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := replayQueue.Close(); err != nil {
			logger.Warn("replay queue close failed", "error", err)
		}
	}()
	replayStore := replay.Store{Database: pools.Admin}
	replayScheduler := replay.Scheduler{
		Store: replayStore, Queue: replayQueue, VerificationQueue: replayQueue,
		Storage: objectStorage, Logger: logger,
	}
	adminStore := platformadmin.Store{
		Database: pools.Admin, RefreshTTL: settings.RefreshTokenTTL,
		RefreshRetryGrace: settings.RefreshRetryGrace,
	}
	adminAuth := platformadmin.Service{
		Store: adminStore, JWT: jwtManager, Email: emailQueue,
		PublicBaseURL: settings.PublicBaseURL, BackofficeURL: settings.PlatformBackofficeURL,
		AuthFrom:     settings.ResendAuthFrom,
		MagicLinkTTL: settings.MagicLinkTTL, RefreshTTL: settings.RefreshTokenTTL,
	}
	billingStore := billing.Store{Database: pools.Admin}
	billingProvider := billing.NewStripeProvider(settings.StripeSecretKey, settings.StripeWebhookSecret)
	var signupVerifier billing.SignupVerifier = billing.LocalSignupVerifier{}
	if settings.TurnstileSecretKey != "" {
		signupVerifier = billing.TurnstileVerifier{
			SecretKey: settings.TurnstileSecretKey, ExpectedHost: settings.BaseDomain,
		}
	}
	billingService := billing.Service{
		Store: billingStore, Provider: billingProvider, Verifier: signupVerifier, Email: emailQueue,
		Prices: billing.PriceCatalog{
			tenant.PlanStarter: {Monthly: settings.StripePriceStarter, Yearly: settings.StripePriceStarterYearly},
			tenant.PlanGrowth:  {Monthly: settings.StripePriceGrowth, Yearly: settings.StripePriceGrowthYearly},
			tenant.PlanScale:   {Monthly: settings.StripePriceScale, Yearly: settings.StripePriceScaleYearly},
		},
		PublicBaseURL: settings.PublicBaseURL, BaseDomain: settings.BaseDomain,
		AuthFrom: settings.ResendAuthFrom,
	}
	deletionExecutor := billing.DeletionExecutor{
		Store: billingStore, Provider: billingProvider, Storage: objectStorage, Logger: logger,
	}
	platformHandler := platformadmin.NewHTTPHandler(platformadmin.HTTPOptions{
		Auth: adminAuth, Tenants: adminStore, Backoffice: adminStore, JWT: jwtManager, Deleter: deletionExecutor, LoginLimiter: publicLimiter,
		SecureCookies: settings.Environment != "local" && settings.Environment != "test",
		RefreshTTL:    settings.RefreshTokenTTL, Logger: logger,
	})
	var resendVerifier issue.WebhookVerifier
	if settings.ResendWebhookSecret != "" {
		resendVerifier, err = issue.NewWebhookVerifier(settings.ResendWebhookSecret)
		if err != nil {
			return err
		}
	}
	// Slack matches a redirect URL's host and port exactly, and tenants live on per-workspace
	// subdomains, so the install callback lands on the one fixed apex API host and carries the
	// tenant in server-side state instead of in the host.
	slackOAuth := integration.SlackOAuth{
		ClientID: settings.SlackClientID, ClientSecret: settings.SlackClientSecret,
		RedirectURL: settings.SlackRedirectURL,
		States:      integration.RedisInstallStateStore{Client: redisClient.Client()},
	}
	platformRoutes := chi.NewRouter()
	// Transactional mail addresses its header logo to the API host, so the API has
	// to answer for it. Anything else — the marketing site, the app — owns the apex
	// in one environment and not the other, and the header quietly degrades to alt
	// text wherever the guess is wrong.
	email.RegisterBrandLogoRoute(platformRoutes)
	integration.RegisterSlackCallbackRoute(platformRoutes, integration.SlackCallbackOptions{
		Store: integration.Store{Cipher: integrationCipher}, Slack: slackOAuth,
		Begin: db.BeginFromPool(pools.App), BaseDomain: settings.BaseDomain,
		WebPublicBaseURL: settings.WebPublicBaseURL, Logger: logger,
	})
	monitorRedis, err := asynq.ParseRedisURI(settings.RedisURL)
	if err != nil {
		return fmt.Errorf("configure queue monitor: %w", err)
	}
	queueMonitor, err := queuemonitor.New(monitorRedis)
	if err != nil {
		return fmt.Errorf("create queue monitor: %w", err)
	}
	defer func() {
		if err := queueMonitor.Close(); err != nil {
			logger.Warn("queue monitor close failed", "error", err)
		}
	}()
	platformRoutes.With(appmiddleware.RequirePlatformAdminCookie(
		jwtManager, platformadmin.PlatformQueueAccessCookie, auth.PermissionPlatformTenantRead,
	)).Mount(queuemonitor.RootPath, queueMonitor)
	billing.RegisterPlatformRoutes(platformRoutes, billing.HTTPOptions{Service: billingService, JWT: jwtManager})
	identity.RegisterPublicRoutes(platformRoutes, identity.HTTPOptions{
		Store: identity.Store{Database: pools.Admin}, Storage: objectStorage, Logger: logger,
	})
	reporterProgressOptions := issue.PublicHTTPOptions{
		Store: issue.Store{Database: pools.Admin}, Tokens: reporterTokens, Verifier: resendVerifier,
		Begin: db.BeginFromPool(pools.Admin), Limiter: ratelimit.NewRedisLimiter(redisClient.Client()),
		TrustedProxyCIDRs: settings.TrustedProxyCIDRs,
	}
	issue.RegisterPublicRoutes(platformRoutes, reporterProgressOptions)
	// Its own client rather than the outbox enqueuer, whose Enqueue speaks in
	// outbox events. Transcoding is not a domain event; it is one job.
	redisQueueOptions, err := asynq.ParseRedisURI(settings.RedisURL)
	if err != nil {
		return fmt.Errorf("parse queue Redis URL: %w", err)
	}
	transcodeQueue := asynq.NewClient(redisQueueOptions)
	defer func() {
		if err := transcodeQueue.Close(); err != nil {
			logger.Warn("transcode queue close failed", "error", err)
		}
	}()
	// The author answering a testimonial request is not a member and never will
	// be, so the token is the only credential. Resolution runs on the admin pool
	// because the workspace is unknown until the digest is matched; everything
	// after that happens inside a transaction pinned to that tenant.
	testimonialCollectOptions := testimonial.CollectHTTPOptions{
		Store: testimonial.Store{}, Resolver: pools.Admin,
		Begin: db.BeginFromPool(pools.App), Logger: logger,
		MediaStore: testimonial.Store{}, Uploads: objectStorage, Transcodes: transcodeQueue,
	}
	testimonial.RegisterCollectRoutes(platformRoutes, testimonialCollectOptions)
	testimonialRevokeOptions := testimonial.RevokeHTTPOptions{
		Store: testimonial.Store{}, Resolver: pools.Admin,
		Begin: db.BeginFromPool(pools.App), Logger: logger,
	}
	testimonial.RegisterRevokeRoutes(platformRoutes, testimonialRevokeOptions)
	// The assets a published wall points at. Without this every avatar and video
	// in a snapshot resolves to nothing on the customer's own website.
	testimonialMediaOptions := testimonial.MediaHTTPOptions{
		Store: testimonial.Store{}, Resolver: pools.Admin,
		Begin: db.BeginFromPool(pools.App), Signer: objectStorage, Logger: logger,
	}
	testimonial.RegisterMediaRoutes(platformRoutes, testimonialMediaOptions)
	membernotify.RegisterPublicRoutes(platformRoutes, membernotify.PublicHTTPOptions{
		Store: membernotify.Store{}, Database: pools.Admin, Tokens: memberNotificationTokens,
		Logger: logger,
	})
	privacyPublicOptions := privacy.PublicHTTPOptions{
		Store: privacy.Store{Database: pools.Admin}, Begin: db.BeginFromPool(pools.Admin),
		Storage: objectStorage, Limiter: ratelimit.NewRedisLimiter(redisClient.Client()),
		TrustedProxyCIDRs: settings.TrustedProxyCIDRs,
	}
	privacy.RegisterPublicRoutes(platformRoutes, privacyPublicOptions)
	breachregister.RegisterPlatformRoutes(platformRoutes, breachregister.HTTPOptions{
		Store: breachregister.Store{Database: pools.Admin}, JWT: jwtManager,
	})
	breachregister.RegisterPlatformOperationsRoutes(
		platformRoutes,
		breachregister.HTTPOptions{
			Store: breachregister.Store{Database: pools.Admin}, JWT: jwtManager,
		},
		platformadmin.PlatformOperationsAccessCookie,
	)
	feedback.RegisterPlatformSupportRoutes(platformRoutes, feedback.PlatformSupportAttachmentHTTPOptions{
		Begin: db.BeginFromPool(pools.App), Store: feedback.Store{}, JWT: jwtManager,
		Storage: objectStorage, Logger: logger,
	})
	replay.RegisterPlatformSupportRoutes(platformRoutes, replay.PlatformSupportStreamHTTPOptions{
		Begin: db.BeginFromPool(pools.App), Store: replay.Store{}, JWT: jwtManager,
		Storage: objectStorage, Logger: logger,
	})
	privacy.RegisterPlatformSupportRoutes(platformRoutes, privacy.PlatformSupportExportHTTPOptions{
		Begin: db.BeginFromPool(pools.App), Store: privacy.Store{}, JWT: jwtManager,
		Storage: objectStorage, Logger: logger,
	})
	platformRoutes.Mount("/", platformHandler)
	tenantStore := tenantmember.Store{
		RefreshTTL: settings.RefreshTokenTTL, RefreshRetryGrace: settings.RefreshRetryGrace,
	}
	tenantService := tenantmember.Service{
		Store: tenantStore, JWT: jwtManager, PublicBaseURL: settings.PublicBaseURL,
		BaseDomain: settings.BaseDomain, AuthFrom: settings.ResendAuthFrom, MagicLinkTTL: settings.MagicLinkTTL,
	}
	tenantRoutes := chi.NewRouter()
	billing.RegisterTenantRoutes(tenantRoutes, billing.HTTPOptions{Service: billingService, JWT: jwtManager})
	tenantmember.RegisterRoutes(tenantRoutes, tenantmember.HTTPOptions{
		Service: tenantService, Store: tenantStore, JWT: jwtManager, Email: emailQueue,
		TwoFactor: tenantmember.TwoFactorService{Store: tenantStore, Cipher: twoFactorCipher},
		Limiter:   publicLimiter, TrustedProxyCIDRs: settings.TrustedProxyCIDRs,
		SecureCookies: settings.Environment != "local" && settings.Environment != "test",
		RefreshTTL:    settings.RefreshTokenTTL, Logger: logger,
	})
	identity.RegisterTenantRoutes(tenantRoutes, identity.HTTPOptions{
		Store: identity.Store{Database: pools.Admin}, JWT: jwtManager, Storage: objectStorage,
		Limiter: publicLimiter, PublicBaseURL: settings.PublicBaseURL, Logger: logger,
	})
	membernotify.RegisterRoutes(tenantRoutes, membernotify.HTTPOptions{
		Store: membernotify.Store{}, JWT: jwtManager, Logger: logger,
	})
	privacy.RegisterRoutes(tenantRoutes, privacy.HTTPOptions{
		Store: privacy.Store{}, JWT: jwtManager, Storage: objectStorage, Email: emailQueue,
		PublicBaseURL: settings.PublicBaseURL, BaseDomain: settings.BaseDomain,
		From: settings.ResendAuthFrom, Logger: logger,
	})
	projectStore := project.Store{PublicBaseURL: settings.PublicBaseURL}
	project.RegisterRoutes(tenantRoutes, project.HTTPOptions{Store: projectStore, JWT: jwtManager})
	apikey.RegisterRoutes(tenantRoutes, apikey.HTTPOptions{Store: apikey.Store{}, JWT: jwtManager})
	integration.RegisterRoutes(tenantRoutes, integration.HTTPOptions{
		Store: integration.Store{Cipher: integrationCipher}, JWT: jwtManager,
		AllowInsecureURLs: settings.Environment == "local" || settings.Environment == "test",
		Slack:             slackOAuth, Logger: logger,
	})
	issue.RegisterRoutes(tenantRoutes, issue.HTTPOptions{Store: issue.Store{}, JWT: jwtManager})
	issuetracker.RegisterRoutes(tenantRoutes, issuetracker.HTTPOptions{
		Store: issuetracker.Store{Cipher: integrationCipher}, JWT: jwtManager,
		Clients:           issuetracker.DefaultClientFactory(nil),
		AllowInsecureURLs: settings.Environment == "local" || settings.Environment == "test",
	})
	feedback.RegisterTenantRoutes(tenantRoutes, feedback.TenantHTTPOptions{
		Store: feedback.Store{}, JWT: jwtManager,
	})
	widgetanalytics.RegisterTenantRoutes(tenantRoutes, widgetanalytics.TenantHTTPOptions{
		Store: widgetanalytics.Store{}, JWT: jwtManager, Logger: logger,
	})
	abuse.RegisterTenantRoutes(tenantRoutes, abuse.TenantHTTPOptions{
		Store: abuse.Store{}, JWT: jwtManager, Logger: logger,
	})
	triage.RegisterRoutes(tenantRoutes, triage.HTTPOptions{Store: triage.Store{}, JWT: jwtManager})
	testimonial.RegisterRoutes(tenantRoutes, testimonial.HTTPOptions{
		Store: testimonial.Store{}, JWT: jwtManager, Email: emailQueue,
		PublicBaseURL: settings.PublicBaseURL, WebPublicBaseURL: settings.WebPublicBaseURL,
		BaseDomain: settings.BaseDomain, From: settings.ResendAuthFrom, Logger: logger,
	})
	replay.RegisterTenantRoutes(tenantRoutes, replay.TenantHTTPOptions{
		Store: replay.Store{}, JWT: jwtManager, Storage: objectStorage, Logger: logger,
	})
	accessaudit.RegisterTenantRoutes(tenantRoutes, accessaudit.HTTPOptions{
		Store: accessaudit.Store{}, JWT: jwtManager, Logger: logger,
	})
	aiprocessing.RegisterRoutes(tenantRoutes, aiprocessing.HTTPOptions{
		Store: aiprocessing.Store{}, JWT: jwtManager, Scheduler: aiScheduler,
	})
	samlEndpoints := samlsso.Endpoints{
		WebBaseURL: settings.WebPublicBaseURL, BaseDomain: settings.BaseDomain,
	}
	samlStore := samlsso.Store{Cipher: integrationCipher}
	samlsso.RegisterRoutes(tenantRoutes, samlsso.HTTPOptions{
		Service: samlsso.Service{
			Store: samlStore, MagicLinks: tenantStore, Endpoints: samlEndpoints,
			Fetcher: samlsso.MetadataFetcher{
				AllowInsecure: settings.Environment == "local" || settings.Environment == "test",
			},
		},
		Store: samlStore, JWT: jwtManager, Logger: logger,
	})
	tenantapi.RegisterRoutes(tenantRoutes, tenantapi.Capabilities{
		SlackOneClickInstall: slackOAuth.Configured(),
	})
	tenantRoutes.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	})
	transactionalTenantHandler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: settings.BaseDomain,
		Resolver:   tenant.PostgresResolver{Admin: pools.Admin},
		Begin:      db.BeginFromPool(pools.App),
		Logger:     logger,
	})(tenantRoutes)
	tenantRoot := chi.NewRouter()
	issue.RegisterReporterProgressRoutes(tenantRoot, reporterProgressOptions)
	// Also mounted on the tenant host: the collection page is served from the
	// workspace domain the author was linked to, so a platform-only registration
	// would answer 404 for every real visitor.
	testimonial.RegisterCollectRoutes(tenantRoot, testimonialCollectOptions)
	// Same reason: the withdrawal link points at the workspace host the author
	// was given, so it has to answer there too.
	testimonial.RegisterRevokeRoutes(tenantRoot, testimonialRevokeOptions)
	testimonial.RegisterMediaRoutes(tenantRoot, testimonialMediaOptions)
	privacy.RegisterPublicRoutes(tenantRoot, privacyPublicOptions)
	attachmentStream := feedback.NewAttachmentStreamHTTPHandler(feedback.AttachmentStreamHTTPOptions{
		BaseDomain: settings.BaseDomain, Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Store: feedback.Store{}, JWT: jwtManager,
		Storage: objectStorage, Logger: logger,
	})
	replayStream := replay.NewStreamHTTPHandler(replay.StreamHTTPOptions{
		BaseDomain: settings.BaseDomain, Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Store: replay.Store{}, JWT: jwtManager,
		Storage: objectStorage, Logger: logger,
	})
	tenantRoot.Get("/v1/feedbacks/{feedbackID}/attachments/{attachmentID}/content", attachmentStream.ServeHTTP)
	tenantRoot.Head("/v1/feedbacks/{feedbackID}/attachments/{attachmentID}/content", attachmentStream.ServeHTTP)
	tenantRoot.Get("/v1/sessions/{sessionID}/replay", replayStream.ServeHTTP)
	tenantRoot.Mount("/", transactionalTenantHandler)
	widgetOptions := widgetapi.HTTPOptions{
		Store: projectStore, Database: pools.Admin, Logger: logger, SentryDSN: settings.SentryDSN,
	}
	widgetAuthorizer := widgetapi.Authorizer{
		Store: widgetOptions.Store, Database: widgetOptions.Database,
	}
	widgetRoutes := chi.NewRouter()
	widgetapi.RegisterRoutes(widgetRoutes, widgetOptions)
	feedback.RegisterRoutes(widgetRoutes, feedback.HTTPOptions{
		Authorizer: widgetAuthorizer, Begin: db.BeginFromPool(pools.App), Store: feedback.Store{},
		Limiter: publicLimiter, Abuse: abuseEvaluator, Diagnostics: abuseDiagnostics,
		Storage: objectStorage, Logger: logger,
		TrustedProxyCIDRs: settings.TrustedProxyCIDRs,
		Scheduler:         verificationScheduler, Analytics: widgetanalytics.Store{},
	})
	widgetanalytics.RegisterPublicRoutes(widgetRoutes, widgetanalytics.PublicHTTPOptions{
		Authorizer: widgetAuthorizer, Begin: db.BeginFromPool(pools.App),
		Store: widgetanalytics.Store{}, Consent: widgetanalytics.ConsentStore{},
		Limiter:           ratelimit.NewRedisLimiter(redisClient.Client()),
		TrustedProxyCIDRs: settings.TrustedProxyCIDRs, Logger: logger,
	})
	// The testimonials embed rides the widget host, which already carries the
	// permissive CORS and long-lived asset caching an embed needs. The authorizer
	// runs on the admin pool because it resolves a workspace before any tenant
	// context exists; every read after that is inside a transaction that pins
	// app.tenant_id, so row-level security still applies.
	testimonial.RegisterPublicRoutes(widgetRoutes, testimonial.PublicHTTPOptions{
		Store:      testimonial.Store{},
		Authorizer: testimonial.PublicAuthorizer{Database: pools.Admin},
		Begin:      db.BeginFromPool(pools.App),
		Logger:     logger,
	})
	// The media paths live inside the payload the line above serves, so they have
	// to answer on the same host. Mounting them anywhere else leaves an embed
	// asking widget.* for a video and being told it does not exist.
	testimonial.RegisterMediaRoutes(widgetRoutes, testimonialMediaOptions)
	replay.RegisterPublicRoutes(widgetRoutes, replay.HTTPOptions{
		Authorizer: widgetAuthorizer, Begin: db.BeginFromPool(pools.App), Store: replay.Store{},
		Storage: objectStorage, Scheduler: replayScheduler, Verifier: replayScheduler,
		Abuse: abuseEvaluator, Diagnostics: abuseDiagnostics,
		TrustedProxyCIDRs: settings.TrustedProxyCIDRs, Logger: logger,
	})
	widgetRoutes.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	})

	handler := server.New(server.Options{
		Logger:           logger,
		ReadinessTimeout: settings.ReadinessTimeout,
		Readiness: map[string]server.ReadinessCheck{
			"database": db.PoolsCheck{Pools: pools},
			"redis":    redisClient,
			"storage":  objectStorage,
		},
		BaseDomain:          settings.BaseDomain,
		InternalProxySecret: settings.InternalProxySecret,
		PlatformHandler:     platformRoutes,
		TenantHandler:       tenantRoot,
		WidgetHandler:       widgetRoutes,
		PanicReporter:       observability.SentryPanicReporter{},
	})
	httpServer := &http.Server{
		Addr:              settings.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("API listening", "address", settings.HTTPAddress)
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}
		logger.Info("API stopped")
		return nil
	}
}

func runWorker(ctx context.Context, logger *slog.Logger, settings config.Config) error {
	integrationCipher, err := integration.NewCipher(settings.IntegrationEncryptionKey)
	if err != nil {
		return err
	}
	pools, err := db.OpenPools(
		ctx, settings.DatabaseURLApp, settings.DatabaseURLAdmin,
		settings.AppDBMaxConns, settings.AdminDBMaxConns,
	)
	if err != nil {
		return err
	}
	defer pools.Close()
	redisClient, err := cache.Open(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			logger.Warn("Redis close failed", "error", err)
		}
	}()
	objectStorage, err := storage.Open(storage.Config{
		Endpoint: settings.S3Endpoint, PublicEndpoint: settings.S3PublicEndpoint,
		Region: settings.S3Region, Bucket: settings.S3Bucket,
		AccessKey: settings.S3AccessKey, SecretKey: settings.S3SecretKey,
		VirtualHostStyle: settings.S3VirtualHostStyle,
	})
	if err != nil {
		return err
	}
	billingEmailQueue, err := email.OpenQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := billingEmailQueue.Close(); err != nil {
			logger.Warn("billing email queue close failed", "error", err)
		}
	}()
	billingStore := billing.Store{Database: pools.Admin}
	billingProvider := billing.NewStripeProvider(settings.StripeSecretKey, settings.StripeWebhookSecret)
	deletionExecutor := billing.DeletionExecutor{
		Store: billingStore, Provider: billingProvider, Storage: objectStorage, Logger: logger,
	}
	dunning := billing.Dunning{
		Store: billingStore, Email: billingEmailQueue,
		PublicBaseURL: settings.PublicBaseURL, BaseDomain: settings.BaseDomain,
		AuthFrom: settings.ResendAuthFrom, Logger: logger,
	}
	memberDigestTokens, err := auth.NewJWTManager(
		settings.JWTSecret, platformadmin.DefaultIssuer, settings.ReporterTokenTTL,
	)
	if err != nil {
		return fmt.Errorf("initialize member notification token manager: %w", err)
	}
	memberDigests := membernotify.Scheduler{
		Store: membernotify.DigestStore{Database: pools.Admin}, Email: billingEmailQueue,
		Tokens: memberDigestTokens, PublicBaseURL: settings.PublicBaseURL,
		BaseDomain: settings.BaseDomain, AuthFrom: settings.ResendAuthFrom,
		MaxMembers: settings.DigestMembersPerRun, Logger: logger,
	}
	outboxQueue, err := outbox.OpenAsynqEnqueuer(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := outboxQueue.Close(); err != nil {
			logger.Warn("outbox queue close failed", "error", err)
		}
	}()
	dispatcher := outbox.Dispatcher{
		Store: outbox.Store{Database: pools.Admin},
		Resolver: outbox.MultiResolver{
			outbox.FeedbackResolver{}, issue.OutboxResolver{Database: pools.Admin},
			email.LifecycleResolver{}, privacy.RecipientOutboxResolver{},
			privacy.CaseCommunicationOutboxResolver{},
			integration.OutboxResolver{Database: pools.Admin},
			issuetracker.OutboxResolver{Database: pools.Admin},
		},
		Enqueuer: outboxQueue, Logger: logger,
	}
	breachNotificationDispatcher := outbox.Dispatcher{
		Store:    breachregister.ControllerNotificationOutboxStore{Database: pools.Admin},
		Resolver: breachregister.ControllerNotificationOutboxResolver{},
		Enqueuer: outboxQueue, Logger: logger,
	}
	aiQueue, err := aiprocessing.OpenQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := aiQueue.Close(); err != nil {
			logger.Warn("AI queue close failed", "error", err)
		}
	}()
	aiScheduler := aiprocessing.Scheduler{
		Store: aiprocessing.Store{
			Database: pools.Admin, ProjectDailyLimit: settings.AIProjectDailyLimit,
			Diagnostics: abuse.Recorder{Database: pools.Admin, Store: abuse.Store{}},
			Logger:      logger,
		}, Queue: aiQueue,
		DailyLimit: settings.AIDailyLimit, Logger: logger,
	}
	verificationQueue, err := feedback.OpenVerificationQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := verificationQueue.Close(); err != nil {
			logger.Warn("attachment verification queue close failed", "error", err)
		}
	}()
	verificationStore := feedback.VerificationStore{Database: pools.Admin}
	verificationScheduler := feedback.VerificationScheduler{
		Store: verificationStore, Queue: verificationQueue, Storage: objectStorage, Logger: logger,
	}
	replayQueue, err := replay.OpenQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := replayQueue.Close(); err != nil {
			logger.Warn("replay queue close failed", "error", err)
		}
	}()
	replayStore := replay.Store{Database: pools.Admin}
	replayScheduler := replay.Scheduler{
		Store: replayStore, Queue: replayQueue, VerificationQueue: replayQueue,
		Storage: objectStorage, Logger: logger,
	}
	replayProcessor := replay.Processor{Store: replayStore, Storage: objectStorage}
	replayVerificationProcessor := replay.BatchVerificationProcessor{
		Store: replayStore, Storage: objectStorage,
		Diagnostics: abuse.Recorder{Database: pools.Admin, Store: abuse.Store{}},
		Logger:      logger,
	}
	retentionHealth := retentionhealth.Store{Database: pools.Admin}
	replayCleaner := replay.Cleaner{
		Store: replayStore, Storage: objectStorage, Health: retentionHealth, Logger: logger,
	}
	privacyQueue, err := privacy.OpenQueue(settings.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := privacyQueue.Close(); err != nil {
			logger.Warn("privacy queue close failed", "error", err)
		}
	}()
	privacyStore := privacy.Store{Database: pools.Admin}
	privacyScheduler := privacy.Scheduler{Store: privacyStore, Queue: privacyQueue, Logger: logger}
	privacyExporter := privacy.Exporter{
		Database: pools.Admin, Store: privacyStore, Storage: objectStorage, Logger: logger,
	}
	privacyRetention := privacy.RetentionMaintainer{
		Store: privacyStore, Storage: objectStorage, Health: retentionHealth, Logger: logger,
	}
	analyticsMaintainer := widgetanalytics.Maintainer{
		Store:  widgetanalytics.MaintenanceStore{Database: pools.Admin},
		Health: retentionHealth, Logger: logger,
	}
	abuseMaintainer := abuse.Maintainer{
		Store: abuse.MaintenanceStore{Recorder: abuse.Recorder{
			Database: pools.Admin, Store: abuse.Store{},
		}},
		Health: retentionHealth, Logger: logger,
	}
	aiBudgetMaintainer := aiprocessing.BudgetMaintainer{
		Store:  aiprocessing.BudgetMaintenanceStore{Database: pools.Admin},
		Health: retentionHealth, Logger: logger,
	}
	identityMaintainer := identity.CleanupMaintainer{
		Store: identity.Store{Database: pools.Admin}, Storage: objectStorage,
		Health: retentionHealth, Logger: logger,
	}
	tenantAuthRetention := tenantmember.Maintainer{
		Store:  tenantmember.MaintenanceStore{Database: pools.Admin},
		Health: retentionHealth, Logger: logger,
	}
	platformAuthRetention := platformadmin.Maintainer{
		Store:  platformadmin.MaintenanceStore{Database: pools.Admin},
		Health: retentionHealth, Logger: logger,
	}
	samlAuthRetention := samlsso.Maintainer{
		Store:  samlsso.MaintenanceStore{Database: pools.Admin},
		Health: retentionHealth, Logger: logger,
	}
	// Without this sweep every other part of the testimonial feature works and the
	// Suggested queue is permanently empty: nothing else connects a feedback to a
	// candidate row. It runs on the admin pool because it spans every workspace.
	testimonialScoring := testimonial.ScoringMaintainer{
		Store: testimonial.Store{}, Database: pools.Admin, Logger: logger,
	}

	var sender email.Sender
	switch settings.EmailDriver {
	case "capture":
		sender = email.CaptureSender{Directory: settings.EmailCaptureDir}
	case "resend":
		sender = email.ResendSender{APIKey: settings.ResendAPIKey}
	default:
		return fmt.Errorf("unsupported email driver %q", settings.EmailDriver)
	}
	reporterTokens, err := auth.NewJWTManager(
		settings.JWTSecret, platformadmin.DefaultIssuer, settings.ReporterTokenTTL,
	)
	if err != nil {
		return fmt.Errorf("initialize reporter token manager: %w", err)
	}
	reporterHandler := issue.ReporterHandler{
		Store: issue.Store{Database: pools.Admin}, Sender: sender, Tokens: reporterTokens,
		PublicBaseURL: settings.PublicBaseURL, BaseDomain: settings.BaseDomain,
	}
	recipientHandler := privacy.RecipientHandler{
		Store: privacyStore, Sender: sender, From: settings.ResendAuthFrom,
		PublicBaseURL: settings.PublicBaseURL,
	}
	caseCommunicationHandler := privacy.CaseCommunicationHandler{
		Store: privacyStore, Sender: sender, From: settings.ResendAuthFrom,
		PublicBaseURL: settings.PublicBaseURL,
	}
	controllerNotificationHandler := breachregister.ControllerNotificationHandler{
		Store: breachregister.Store{Database: pools.Admin}, Sender: sender,
		From: settings.ResendAuthFrom, PublicBaseURL: settings.PublicBaseURL,
	}
	lifecycleHandler := email.LifecycleHandler{
		Database: pools.Admin, Sender: sender,
		PublicBaseURL: settings.PublicBaseURL, BaseDomain: settings.BaseDomain,
		AuthFrom:    settings.ResendAuthFrom,
		WelcomeFrom: settings.ResendWelcomeFrom, WelcomeReplyTo: settings.ResendWelcomeReplyTo,
	}
	integrationHandler := integration.DeliveryHandler{
		Store:         integration.Store{Database: pools.Admin, Cipher: integrationCipher},
		PublicBaseURL: settings.PublicBaseURL, BaseDomain: settings.BaseDomain,
	}
	issueTrackerHandler := issuetracker.AutoCreateHandler{
		Store:   issuetracker.Store{Database: pools.Admin, Cipher: integrationCipher},
		Clients: issuetracker.DefaultClientFactory(nil),
	}
	aiStore := aiprocessing.Store{
		Database: pools.Admin, ProjectDailyLimit: settings.AIProjectDailyLimit,
		Model:       settings.GeminiModel,
		Diagnostics: abuse.Recorder{Database: pools.Admin, Store: abuse.Store{}},
		Logger:      logger,
	}
	var analyzer aiprocessing.Analyzer = aiprocessing.UnavailableAnalyzer{}
	fileDeleteHandler := aiprocessing.UnavailableFileDeleteHandler(aiStore)
	if settings.GeminiAPIKey != "" {
		geminiAnalyzer, analyzerErr := aiprocessing.NewGeminiAnalyzer(
			ctx, settings.GeminiAPIKey, settings.GeminiModel, aiStore, logger,
		)
		err = analyzerErr
		if err != nil {
			return err
		}
		analyzer = geminiAnalyzer
		fileDeleteHandler = geminiAnalyzer.FileDeleteHandler(aiStore)
	} else {
		logger.Warn("Gemini API key is not configured; AI tasks will remain retryable")
	}
	processor := aiprocessing.Processor{
		Store: aiStore, Analyzer: analyzer, Storage: objectStorage,
		Limiter: ratelimit.NewRedisLimiter(redisClient.Client()), RPM: settings.GeminiRPM,
		Logger: logger, Diagnostics: abuse.Recorder{Database: pools.Admin, Store: abuse.Store{}},
	}
	verificationProcessor := feedback.VerificationProcessor{
		Store: verificationStore, Storage: objectStorage, Scanner: feedback.SignatureScanner{},
		AI:          aiScheduler,
		Diagnostics: abuse.Recorder{Database: pools.Admin, Store: abuse.Store{}},
		Logger:      logger,
	}
	worker, err := jobworker.New(jobworker.Options{
		RedisURL: settings.RedisURL, EmailConcurrency: settings.EmailConcurrency,
		AIConcurrency: settings.AIConcurrency, DomainConcurrency: settings.DomainConcurrency,
		MediaConcurrency: settings.MediaConcurrency, PrivacyConcurrency: settings.PrivacyConcurrency,
		Logger: logging.NewAsynq(logger), Sender: sender,
		PipelineHandler: aiScheduler.PipelineHandler(), TextHandler: processor.TextHandler(),
		AttachmentHandler: processor.AttachmentHandler(), FileDeleteHandler: fileDeleteHandler,
		AIErrorHandler:                processor.ErrorHandler(),
		ReporterHandler:               reporterHandler.Handler(),
		RecipientHandler:              recipientHandler.Handler(),
		CaseCommunicationHandler:      caseCommunicationHandler.Handler(),
		ControllerNotificationHandler: controllerNotificationHandler.Handler(),
		LifecycleHandler:              lifecycleHandler.Handler(),
		ReplayMergeHandler:            replayProcessor.MergeHandler(),
		ReplayVerificationHandler:     replayVerificationProcessor.Handler(),
		AttachmentVerificationHandler: verificationProcessor.Handler(),
		TestimonialTranscodeHandler: testimonial.TranscodeHandler(testimonial.TranscodeOptions{
			Store: testimonial.Store{}, Storage: objectStorage, Encoder: testimonial.FFmpeg{},
			Begin: db.BeginFromPool(pools.App), Logger: logger,
		}),
		IntegrationHandler:   integrationHandler.Handler(),
		IssueTrackerHandler:  issueTrackerHandler.Handler(),
		PrivacyExportHandler: privacyExporter.Handler(),
	})
	if err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	backgroundDone := make(chan error, 17)
	go func() { backgroundDone <- dispatcher.Run(workerCtx, 2*time.Second) }()
	go func() { backgroundDone <- breachNotificationDispatcher.Run(workerCtx, 2*time.Second) }()
	go func() { backgroundDone <- aiScheduler.RunReconciler(workerCtx, settings.AIReconcileEvery) }()
	go func() { backgroundDone <- replayScheduler.RunReconciler(workerCtx, time.Minute) }()
	go func() { backgroundDone <- verificationScheduler.Run(workerCtx, time.Minute) }()
	go func() { backgroundDone <- replayCleaner.Run(workerCtx, 24*time.Hour) }()
	go func() { backgroundDone <- privacyScheduler.Run(workerCtx, 30*time.Second) }()
	go func() { backgroundDone <- privacyRetention.Run(workerCtx, 24*time.Hour) }()
	go func() { backgroundDone <- analyticsMaintainer.Run(workerCtx, time.Hour) }()
	go func() { backgroundDone <- abuseMaintainer.Run(workerCtx, time.Hour) }()
	go func() { backgroundDone <- aiBudgetMaintainer.Run(workerCtx, 24*time.Hour) }()
	go func() { backgroundDone <- identityMaintainer.Run(workerCtx, time.Minute) }()
	go func() { backgroundDone <- tenantAuthRetention.Run(workerCtx, time.Hour) }()
	go func() { backgroundDone <- platformAuthRetention.Run(workerCtx, time.Hour) }()
	go func() { backgroundDone <- samlAuthRetention.Run(workerCtx, time.Hour) }()
	go func() { backgroundDone <- testimonialScoring.Run(workerCtx, 15*time.Minute) }()
	go func() { backgroundDone <- deletionExecutor.Run(workerCtx, 24*time.Hour) }()
	go func() { backgroundDone <- dunning.Run(workerCtx, 24*time.Hour) }()
	go func() { backgroundDone <- memberDigests.Run(workerCtx, time.Minute) }()
	workerErr := worker.Run(workerCtx)
	cancel()
	return errors.Join(
		workerErr,
		<-backgroundDone, <-backgroundDone, <-backgroundDone,
		<-backgroundDone, <-backgroundDone, <-backgroundDone, <-backgroundDone,
		<-backgroundDone, <-backgroundDone, <-backgroundDone, <-backgroundDone,
		<-backgroundDone, <-backgroundDone, <-backgroundDone, <-backgroundDone,
		<-backgroundDone, <-backgroundDone,
	)
}

// runSeedTestimonials installs the demo corpus and prints the URLs to open.
//
// The corpus is deliberately mixed: seeding only glowing quotes would make the
// Suggested queue look clever when it is doing nothing. Five of the eight must
// be refused, each for a different reason, so the demo shows the gates working.
func runSeedTestimonials(
	ctx context.Context,
	logger *slog.Logger,
	settings config.Config,
	args []string,
) int {
	projectPublicKey := ""
	for index := 0; index < len(args); index++ {
		if args[index] == "--project-key" && index+1 < len(args) {
			projectPublicKey = args[index+1]
			index++
			continue
		}
		logger.Error("unknown seed argument", "argument", args[index])
		return 2
	}

	pools, err := db.OpenPools(
		ctx, settings.DatabaseURLApp, settings.DatabaseURLAdmin,
		settings.AppDBMaxConns, settings.AdminDBMaxConns,
	)
	if err != nil {
		logger.Error("open database for testimonial seed", "error", err)
		return 1
	}
	defer pools.Close()

	var tenantID, projectID string
	query := `SELECT tenant_id, id FROM projects ORDER BY created_at LIMIT 1`
	queryArgs := []any{}
	if projectPublicKey != "" {
		query = `SELECT tenant_id, id FROM projects WHERE public_key = $1`
		queryArgs = append(queryArgs, projectPublicKey)
	}
	if err := pools.Admin.QueryRow(ctx, query, queryArgs...).Scan(&tenantID, &projectID); err != nil {
		logger.Error("no project to seed against; create one first", "error", err)
		return 1
	}

	result, err := testimonial.Seed(ctx, pools.Admin, testimonial.Store{},
		tenantID, projectID, time.Now().UTC())
	if err != nil {
		logger.Error("testimonial seed failed", "error", err)
		return 1
	}

	logger.Info("testimonial demo seeded",
		"feedback", result.FeedbackCount,
		"suggested", result.Suggested,
		"published", result.Published,
		"wall", result.WallPublicID,
	)
	// Printed rather than logged: these are the two things an operator needs to
	// paste somewhere, and they should survive any log formatting.
	fmt.Printf("\nBackoffice:  http://<tenant>.localhost:8088/testimonials\n")
	fmt.Printf("Mock site:   http://demo.localhost:8088/?wall=%s\n", result.WallPublicID)
	fmt.Printf("Wall JSON:   http://widget.localhost:8088/v1/public/walls/%s\n\n", result.WallPublicID)
	return 0
}

func runAdmin(
	ctx context.Context,
	logger *slog.Logger,
	settings config.Config,
	args []string,
) int {
	if len(args) == 0 {
		logger.Error("unknown admin command")
		return 2
	}
	if args[0] == "seed-testimonials" {
		return runSeedTestimonials(ctx, logger, settings, args[1:])
	}
	if args[0] != "bootstrap" {
		logger.Error("unknown admin command")
		return 2
	}
	emailAddress := ""
	for index := 1; index < len(args); index++ {
		if args[index] == "--email" && index+1 < len(args) {
			emailAddress = args[index+1]
			index++
			continue
		}
		logger.Error("unknown admin bootstrap argument", "argument", args[index])
		return 2
	}
	if emailAddress == "" {
		logger.Error("admin bootstrap requires --email")
		return 2
	}
	pools, err := db.OpenPools(
		ctx, settings.DatabaseURLApp, settings.DatabaseURLAdmin,
		settings.AppDBMaxConns, settings.AdminDBMaxConns,
	)
	if err != nil {
		logger.Error("open database for admin bootstrap", "error", err)
		return 1
	}
	defer pools.Close()
	admin, err := (platformadmin.Store{Database: pools.Admin}).BootstrapAdmin(
		ctx, emailAddress, auth.PlatformPermissionStrings(),
	)
	if err != nil {
		logger.Error("bootstrap platform admin", "error", err)
		return 1
	}
	logger.Info("platform admin bootstrapped", "admin_id", admin.ID, "email", admin.Email)
	return 0
}
