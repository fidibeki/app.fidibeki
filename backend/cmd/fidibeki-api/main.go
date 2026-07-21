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

	aiprocessing "github.com/acidiney/app.fidibeki/backend/internal/ai"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/apikey"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/billing"
	"github.com/acidiney/app.fidibeki/backend/internal/cache"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
	"github.com/acidiney/app.fidibeki/backend/internal/integration"
	"github.com/acidiney/app.fidibeki/backend/internal/issue"
	"github.com/acidiney/app.fidibeki/backend/internal/logging"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/observability"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/acidiney/app.fidibeki/backend/internal/platformadmin"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/queuemonitor"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/replay"
	"github.com/acidiney/app.fidibeki/backend/internal/server"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/tenantapi"
	"github.com/acidiney/app.fidibeki/backend/internal/tenantmember"
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
		Store: aiprocessing.Store{Database: pools.Admin}, Queue: aiQueue,
		DailyLimit: settings.AIDailyLimit, Logger: logger,
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
	replayScheduler := replay.Scheduler{Store: replayStore, Queue: replayQueue, Logger: logger}
	adminStore := platformadmin.Store{
		Database: pools.Admin, RefreshTTL: settings.RefreshTokenTTL,
	}
	adminAuth := platformadmin.Service{
		Store: adminStore, JWT: jwtManager, Email: emailQueue,
		PublicBaseURL: settings.PublicBaseURL, AuthFrom: settings.ResendAuthFrom,
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
			tenant.PlanStarter: settings.StripePriceStarter,
			tenant.PlanGrowth:  settings.StripePriceGrowth,
			tenant.PlanScale:   settings.StripePriceScale,
		},
		PublicBaseURL: settings.PublicBaseURL, BaseDomain: settings.BaseDomain,
		AuthFrom: settings.ResendAuthFrom,
	}
	deletionExecutor := billing.DeletionExecutor{
		Store: billingStore, Provider: billingProvider, Storage: objectStorage, Logger: logger,
	}
	platformHandler := platformadmin.NewHTTPHandler(platformadmin.HTTPOptions{
		Auth: adminAuth, Tenants: adminStore, JWT: jwtManager, Deleter: deletionExecutor,
		SecureCookies: settings.Environment != "local" && settings.Environment != "test",
		RefreshTTL:    settings.RefreshTokenTTL,
	})
	var resendVerifier issue.WebhookVerifier
	if settings.ResendWebhookSecret != "" {
		resendVerifier, err = issue.NewWebhookVerifier(settings.ResendWebhookSecret)
		if err != nil {
			return err
		}
	}
	platformRoutes := chi.NewRouter()
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
	issue.RegisterPublicRoutes(platformRoutes, issue.PublicHTTPOptions{
		Store: issue.Store{Database: pools.Admin}, Tokens: reporterTokens, Verifier: resendVerifier,
	})
	platformRoutes.Mount("/", platformHandler)
	tenantStore := tenantmember.Store{RefreshTTL: settings.RefreshTokenTTL}
	tenantService := tenantmember.Service{
		Store: tenantStore, JWT: jwtManager, PublicBaseURL: settings.PublicBaseURL,
		BaseDomain: settings.BaseDomain, AuthFrom: settings.ResendAuthFrom, MagicLinkTTL: settings.MagicLinkTTL,
	}
	tenantRoutes := chi.NewRouter()
	billing.RegisterTenantRoutes(tenantRoutes, billing.HTTPOptions{Service: billingService, JWT: jwtManager})
	tenantmember.RegisterRoutes(tenantRoutes, tenantmember.HTTPOptions{
		Service: tenantService, Store: tenantStore, JWT: jwtManager, Email: emailQueue,
		SecureCookies: settings.Environment != "local" && settings.Environment != "test",
		RefreshTTL:    settings.RefreshTokenTTL,
	})
	project.RegisterRoutes(tenantRoutes, project.HTTPOptions{Store: project.Store{}, JWT: jwtManager})
	apikey.RegisterRoutes(tenantRoutes, apikey.HTTPOptions{Store: apikey.Store{}, JWT: jwtManager})
	integration.RegisterRoutes(tenantRoutes, integration.HTTPOptions{
		Store: integration.Store{Cipher: integrationCipher}, JWT: jwtManager,
		AllowInsecureURLs: settings.Environment == "local" || settings.Environment == "test",
	})
	issue.RegisterRoutes(tenantRoutes, issue.HTTPOptions{Store: issue.Store{}, JWT: jwtManager})
	feedback.RegisterTenantRoutes(tenantRoutes, feedback.TenantHTTPOptions{
		Store: feedback.Store{}, JWT: jwtManager,
	})
	replay.RegisterTenantRoutes(tenantRoutes, replay.TenantHTTPOptions{
		Store: replay.Store{}, JWT: jwtManager, Storage: objectStorage, Logger: logger,
	})
	aiprocessing.RegisterRoutes(tenantRoutes, aiprocessing.HTTPOptions{
		Store: aiprocessing.Store{}, JWT: jwtManager, Scheduler: aiScheduler,
	})
	tenantapi.RegisterRoutes(tenantRoutes)
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
		Store: project.Store{}, Database: pools.Admin, Logger: logger, SentryDSN: settings.SentryDSN,
	}
	widgetAuthorizer := widgetapi.Authorizer{
		Store: widgetOptions.Store, Database: widgetOptions.Database,
	}
	widgetRoutes := chi.NewRouter()
	widgetapi.RegisterRoutes(widgetRoutes, widgetOptions)
	feedback.RegisterRoutes(widgetRoutes, feedback.HTTPOptions{
		Authorizer: widgetAuthorizer, Begin: db.BeginFromPool(pools.App), Store: feedback.Store{},
		Limiter: ratelimit.NewRedisLimiter(redisClient.Client()), Storage: objectStorage, Logger: logger,
		Scheduler: aiScheduler,
	})
	replay.RegisterPublicRoutes(widgetRoutes, replay.HTTPOptions{
		Authorizer: widgetAuthorizer, Begin: db.BeginFromPool(pools.App), Store: replay.Store{},
		Storage: objectStorage, Scheduler: replayScheduler, Logger: logger,
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
		Store: billingStore, Email: billingEmailQueue, AuthFrom: settings.ResendAuthFrom, Logger: logger,
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
			outbox.FeedbackResolver{}, issue.OutboxResolver{},
			integration.OutboxResolver{Database: pools.Admin},
		},
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
		Store: aiprocessing.Store{Database: pools.Admin}, Queue: aiQueue,
		DailyLimit: settings.AIDailyLimit, Logger: logger,
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
	replayScheduler := replay.Scheduler{Store: replayStore, Queue: replayQueue, Logger: logger}
	replayProcessor := replay.Processor{Store: replayStore, Storage: objectStorage}
	replayCleaner := replay.Cleaner{Store: replayStore, Storage: objectStorage, Logger: logger}

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
		PublicBaseURL: settings.PublicBaseURL,
	}
	integrationHandler := integration.DeliveryHandler{
		Store: integration.Store{Database: pools.Admin, Cipher: integrationCipher},
	}
	aiStore := aiprocessing.Store{Database: pools.Admin}
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
		Logger: logger,
	}
	worker, err := jobworker.New(jobworker.Options{
		RedisURL: settings.RedisURL, EmailConcurrency: settings.EmailConcurrency,
		AIConcurrency: settings.AIConcurrency, Sender: sender,
		PipelineHandler: aiScheduler.PipelineHandler(), TextHandler: processor.TextHandler(),
		AttachmentHandler: processor.AttachmentHandler(), FileDeleteHandler: fileDeleteHandler,
		AIErrorHandler:     processor.ErrorHandler(),
		ReporterHandler:    reporterHandler.Handler(),
		ReplayMergeHandler: replayProcessor.MergeHandler(),
		IntegrationHandler: integrationHandler.Handler(),
	})
	if err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	backgroundDone := make(chan error, 6)
	go func() { backgroundDone <- dispatcher.Run(workerCtx, 2*time.Second) }()
	go func() { backgroundDone <- aiScheduler.RunReconciler(workerCtx, settings.AIReconcileEvery) }()
	go func() { backgroundDone <- replayScheduler.RunReconciler(workerCtx, time.Minute) }()
	go func() { backgroundDone <- replayCleaner.Run(workerCtx, 24*time.Hour) }()
	go func() { backgroundDone <- deletionExecutor.Run(workerCtx, 24*time.Hour) }()
	go func() { backgroundDone <- dunning.Run(workerCtx, 24*time.Hour) }()
	workerErr := worker.Run(workerCtx)
	cancel()
	return errors.Join(
		workerErr,
		<-backgroundDone, <-backgroundDone, <-backgroundDone,
		<-backgroundDone, <-backgroundDone, <-backgroundDone,
	)
}

func runAdmin(
	ctx context.Context,
	logger *slog.Logger,
	settings config.Config,
	args []string,
) int {
	if len(args) == 0 || args[0] != "bootstrap" {
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
