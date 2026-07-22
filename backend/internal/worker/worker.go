package worker

import (
	"context"
	"fmt"

	"github.com/acidiney/app.fidibeki/backend/internal/ai"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/integration"
	"github.com/acidiney/app.fidibeki/backend/internal/issue"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/acidiney/app.fidibeki/backend/internal/replay"
	"github.com/hibiken/asynq"
)

type Options struct {
	RedisURL           string
	EmailConcurrency   int
	AIConcurrency      int
	Logger             asynq.Logger
	Sender             email.Sender
	PipelineHandler    asynq.HandlerFunc
	TextHandler        asynq.HandlerFunc
	AttachmentHandler  asynq.HandlerFunc
	FileDeleteHandler  asynq.HandlerFunc
	AIErrorHandler     asynq.ErrorHandler
	ReporterHandler    asynq.HandlerFunc
	ReplayMergeHandler asynq.HandlerFunc
	IntegrationHandler asynq.HandlerFunc
}

type Worker struct {
	emailServer  *asynq.Server
	domainServer *asynq.Server
	aiServer     *asynq.Server
	emailMux     *asynq.ServeMux
	domainMux    *asynq.ServeMux
	aiMux        *asynq.ServeMux
}

func New(options Options) (*Worker, error) {
	if options.EmailConcurrency <= 0 {
		return nil, fmt.Errorf("email worker concurrency must be positive")
	}
	if options.AIConcurrency <= 0 {
		return nil, fmt.Errorf("AI worker concurrency must be positive")
	}
	if options.PipelineHandler == nil || options.TextHandler == nil ||
		options.AttachmentHandler == nil || options.FileDeleteHandler == nil ||
		options.AIErrorHandler == nil || options.ReporterHandler == nil || options.ReplayMergeHandler == nil ||
		options.IntegrationHandler == nil {
		return nil, fmt.Errorf("all domain and AI handlers are required")
	}
	if options.Logger == nil {
		return nil, fmt.Errorf("worker logger is required")
	}
	redisOptions, err := asynq.ParseRedisURI(options.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse worker Redis URL: %w", err)
	}
	emailMux := asynq.NewServeMux()
	if err := email.RegisterHandlers(emailMux, options.Sender); err != nil {
		return nil, err
	}
	emailMux.HandleFunc(issue.TaskReporterNotify, options.ReporterHandler)
	domainMux := asynq.NewServeMux()
	domainMux.HandleFunc(outbox.TaskFeedbackPipe, options.PipelineHandler)
	domainMux.HandleFunc(replay.TaskMerge, options.ReplayMergeHandler)
	domainMux.HandleFunc(integration.TaskDeliver, options.IntegrationHandler)
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
			Concurrency: 1, Queues: map[string]int{outbox.QueueName: 1},
			Logger: options.Logger,
		}),
		aiServer: asynq.NewServer(redisOptions, asynq.Config{
			Concurrency: options.AIConcurrency, Queues: map[string]int{ai.QueueName: 1},
			ErrorHandler: options.AIErrorHandler, Logger: options.Logger,
		}),
		emailMux: emailMux, domainMux: domainMux, aiMux: aiMux,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	started := make([]*asynq.Server, 0, 3)
	servers := []struct {
		name   string
		server *asynq.Server
		mux    *asynq.ServeMux
	}{
		{name: "email", server: w.emailServer, mux: w.emailMux},
		{name: "domain", server: w.domainServer, mux: w.domainMux},
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
