package worker

import (
	"context"
	"fmt"

	"github.com/fidibeki/app.fidibeki/backend/internal/ai"
	"github.com/fidibeki/app.fidibeki/backend/internal/breachregister"
	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/fidibeki/app.fidibeki/backend/internal/emailfeedback"
	"github.com/fidibeki/app.fidibeki/backend/internal/feedback"
	"github.com/fidibeki/app.fidibeki/backend/internal/integration"
	"github.com/fidibeki/app.fidibeki/backend/internal/issue"
	"github.com/fidibeki/app.fidibeki/backend/internal/issuetracker"
	"github.com/fidibeki/app.fidibeki/backend/internal/outbox"
	"github.com/fidibeki/app.fidibeki/backend/internal/privacy"
	"github.com/fidibeki/app.fidibeki/backend/internal/redisconn"
	"github.com/fidibeki/app.fidibeki/backend/internal/replay"
	"github.com/fidibeki/app.fidibeki/backend/internal/testimonial"
	"github.com/hibiken/asynq"
)

type Options struct {
	RedisURL                      string
	RedisCACert                   string
	EmailTaskAuthenticator        email.TaskAuthenticator
	EmailConcurrency              int
	AIConcurrency                 int
	DomainConcurrency             int
	MediaConcurrency              int
	PrivacyConcurrency            int
	Logger                        asynq.Logger
	Sender                        email.Sender
	PipelineHandler               asynq.HandlerFunc
	TextHandler                   asynq.HandlerFunc
	AttachmentHandler             asynq.HandlerFunc
	FileDeleteHandler             asynq.HandlerFunc
	AIErrorHandler                asynq.ErrorHandler
	ReplayErrorHandler            asynq.ErrorHandler
	ReporterHandler               asynq.HandlerFunc
	RecipientHandler              asynq.HandlerFunc
	CaseCommunicationHandler      asynq.HandlerFunc
	ControllerNotificationHandler asynq.HandlerFunc
	LifecycleHandler              asynq.HandlerFunc
	EmailFeedbackHandler          asynq.HandlerFunc
	ReplayMergeHandler            asynq.HandlerFunc
	ReplayVerificationHandler     asynq.HandlerFunc
	AttachmentVerificationHandler asynq.HandlerFunc
	// TestimonialTranscodeHandler is optional: a deployment without ffmpeg simply
	// does not register it, and uploads stay pending rather than failing loudly.
	TestimonialTranscodeHandler asynq.HandlerFunc
	IntegrationHandler          asynq.HandlerFunc
	IssueTrackerHandler         asynq.HandlerFunc
	PrivacyExportHandler        asynq.HandlerFunc
}

type Worker struct {
	emailServer   *asynq.Server
	domainServer  *asynq.Server
	mediaServer   *asynq.Server
	privacyServer *asynq.Server
	aiServer      *asynq.Server
	emailMux      *asynq.ServeMux
	domainMux     *asynq.ServeMux
	mediaMux      *asynq.ServeMux
	privacyMux    *asynq.ServeMux
	aiMux         *asynq.ServeMux
}

func New(options Options) (*Worker, error) {
	if options.EmailConcurrency <= 0 {
		return nil, fmt.Errorf("email worker concurrency must be positive")
	}
	if options.AIConcurrency <= 0 {
		return nil, fmt.Errorf("AI worker concurrency must be positive")
	}
	if options.DomainConcurrency <= 0 || options.MediaConcurrency <= 0 ||
		options.PrivacyConcurrency <= 0 {
		return nil, fmt.Errorf("domain, media, and privacy worker concurrency must be positive")
	}
	if options.PipelineHandler == nil || options.TextHandler == nil ||
		options.AttachmentHandler == nil || options.FileDeleteHandler == nil ||
		options.AIErrorHandler == nil || options.ReplayErrorHandler == nil ||
		options.ReporterHandler == nil ||
		options.RecipientHandler == nil || options.CaseCommunicationHandler == nil ||
		options.ControllerNotificationHandler == nil ||
		options.LifecycleHandler == nil || options.EmailFeedbackHandler == nil ||
		options.ReplayMergeHandler == nil || options.ReplayVerificationHandler == nil ||
		options.AttachmentVerificationHandler == nil ||
		options.IntegrationHandler == nil || options.IssueTrackerHandler == nil ||
		options.PrivacyExportHandler == nil {
		return nil, fmt.Errorf("all domain and AI handlers are required")
	}
	if options.Logger == nil {
		return nil, fmt.Errorf("worker logger is required")
	}
	redisOptions, err := redisconn.AsynqOptions(options.RedisURL, options.RedisCACert)
	if err != nil {
		return nil, fmt.Errorf("parse worker Redis URL: %w", err)
	}
	emailMux := asynq.NewServeMux()
	if err := email.RegisterHandlers(
		emailMux, options.Sender, options.EmailTaskAuthenticator,
	); err != nil {
		return nil, err
	}
	emailMux.HandleFunc(issue.TaskReporterNotify, options.ReporterHandler)
	emailMux.HandleFunc(privacy.TaskRecipientNotify, options.RecipientHandler)
	emailMux.HandleFunc(privacy.TaskCaseCommunicationNotify, options.CaseCommunicationHandler)
	emailMux.HandleFunc(
		breachregister.TaskControllerNotification,
		options.ControllerNotificationHandler,
	)
	emailMux.HandleFunc(email.TaskLifecycleEmail, options.LifecycleHandler)
	emailMux.HandleFunc(emailfeedback.TaskDeliverInvitation, options.EmailFeedbackHandler)
	domainMux := asynq.NewServeMux()
	domainMux.HandleFunc(outbox.TaskFeedbackPipe, options.PipelineHandler)
	domainMux.HandleFunc(integration.TaskDeliver, options.IntegrationHandler)
	domainMux.HandleFunc(issuetracker.TaskAutoCreate, options.IssueTrackerHandler)
	// Keep pre-upgrade domain-pool tasks runnable while new work uses isolated queues.
	domainMux.HandleFunc(replay.TaskMerge, options.ReplayMergeHandler)
	domainMux.HandleFunc(replay.TaskVerifyBatch, options.ReplayVerificationHandler)
	domainMux.HandleFunc(feedback.TaskAttachmentVerify, options.AttachmentVerificationHandler)
	domainMux.HandleFunc(privacy.TaskExport, options.PrivacyExportHandler)
	mediaMux := asynq.NewServeMux()
	mediaMux.HandleFunc(replay.TaskMerge, options.ReplayMergeHandler)
	mediaMux.HandleFunc(replay.TaskVerifyBatch, options.ReplayVerificationHandler)
	mediaMux.HandleFunc(feedback.TaskAttachmentVerify, options.AttachmentVerificationHandler)
	if options.TestimonialTranscodeHandler != nil {
		mediaMux.HandleFunc(testimonial.TaskTranscode, options.TestimonialTranscodeHandler)
	}
	privacyMux := asynq.NewServeMux()
	privacyMux.HandleFunc(privacy.TaskExport, options.PrivacyExportHandler)
	aiMux := asynq.NewServeMux()
	aiMux.HandleFunc(ai.TaskTextProcess, options.TextHandler)
	aiMux.HandleFunc(ai.TaskAttachmentProcess, options.AttachmentHandler)
	aiMux.HandleFunc(ai.TaskGeminiFileDelete, options.FileDeleteHandler)
	return &Worker{
		emailServer: asynq.NewServer(redisOptions, asynq.Config{
			Concurrency: options.EmailConcurrency, Queues: map[string]int{email.QueueName: 1},
			Logger: options.Logger,
		}),
		domainServer: asynq.NewServer(redisOptions, asynq.Config{
			Concurrency: options.DomainConcurrency, Queues: map[string]int{outbox.QueueName: 1},
			ErrorHandler: options.ReplayErrorHandler, Logger: options.Logger,
		}),
		mediaServer: asynq.NewServer(redisOptions, asynq.Config{
			Concurrency: options.MediaConcurrency,
			Queues: map[string]int{
				replay.QueueName: 1, feedback.VerificationQueueName: 1,
				// Weighted below the others: one encode runs for minutes, and
				// nothing a person is waiting on should queue behind it.
				testimonial.TranscodeQueueName: 1,
			},
			ErrorHandler: options.ReplayErrorHandler, Logger: options.Logger,
		}),
		privacyServer: asynq.NewServer(redisOptions, asynq.Config{
			Concurrency: options.PrivacyConcurrency,
			Queues:      map[string]int{privacy.QueueName: 1},
			Logger:      options.Logger,
		}),
		aiServer: asynq.NewServer(redisOptions, asynq.Config{
			Concurrency: options.AIConcurrency, Queues: map[string]int{ai.QueueName: 1},
			ErrorHandler: options.AIErrorHandler, Logger: options.Logger,
		}),
		emailMux: emailMux, domainMux: domainMux, mediaMux: mediaMux,
		privacyMux: privacyMux, aiMux: aiMux,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	started := make([]*asynq.Server, 0, 5)
	servers := []struct {
		name   string
		server *asynq.Server
		mux    *asynq.ServeMux
	}{
		{name: "email", server: w.emailServer, mux: w.emailMux},
		{name: "domain", server: w.domainServer, mux: w.domainMux},
		{name: "media", server: w.mediaServer, mux: w.mediaMux},
		{name: "privacy", server: w.privacyServer, mux: w.privacyMux},
		{name: "AI", server: w.aiServer, mux: w.aiMux},
	}
	for _, item := range servers {
		if err := item.server.Start(item.mux); err != nil {
			for _, server := range started {
				server.Shutdown()
			}
			return fmt.Errorf("start %s worker: %w", item.name, err)
		}
		started = append(started, item.server)
	}
	<-ctx.Done()
	for _, server := range started {
		server.Shutdown()
	}
	return nil
}
