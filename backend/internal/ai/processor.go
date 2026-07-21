package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/hibiken/asynq"
)

type ObjectSource interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
}

type Processor struct {
	Store    Store
	Analyzer Analyzer
	Storage  ObjectSource
	Limiter  ratelimit.Limiter
	RPM      int
	Now      func() time.Time
	Logger   *slog.Logger
}

func (p Processor) TextHandler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		payload, err := decodeTask(task)
		if err != nil {
			return err
		}
		if payload.Generation <= 0 {
			return fmt.Errorf("text AI task generation is invalid: %w", asynq.SkipRetry)
		}
		work, err := p.Store.ClaimText(
			ctx, payload.TenantID, payload.ResourceID, payload.Generation,
		)
		if errors.Is(err, ErrNoWork) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := p.acquire(ctx); err != nil {
			return p.handleTextFailure(ctx, work, err)
		}
		result, err := p.Analyzer.AnalyzeText(ctx, work.Text)
		if err != nil {
			return p.handleTextFailure(ctx, work, err)
		}
		if err := p.Store.CompleteText(ctx, work, result, p.now()); err != nil {
			return err
		}
		return nil
	}
}

func (p Processor) AttachmentHandler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		payload, err := decodeTask(task)
		if err != nil {
			return err
		}
		if payload.Attempt <= 0 {
			return fmt.Errorf("attachment AI task attempt is invalid: %w", asynq.SkipRetry)
		}
		work, err := p.Store.ClaimAttachment(ctx, payload.TenantID, payload.ResourceID, payload.Attempt)
		if errors.Is(err, ErrNoWork) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := p.acquire(ctx); err != nil {
			return p.handleAttachmentFailure(ctx, work, err)
		}
		reader, err := p.Storage.OpenObject(ctx, work.StorageKey)
		if err != nil {
			return p.handleAttachmentFailure(ctx, work, err)
		}
		result, analysisErr := p.Analyzer.AnalyzeAttachment(ctx, work, reader)
		closeErr := reader.Close()
		if analysisErr != nil {
			return p.handleAttachmentFailure(ctx, work, analysisErr)
		}
		if closeErr != nil {
			return p.handleAttachmentFailure(ctx, work, fmt.Errorf("close attachment stream: %w", closeErr))
		}
		if err := p.Store.CompleteAttachment(ctx, work, result, p.now()); err != nil {
			return err
		}
		return nil
	}
}

func (p Processor) acquire(ctx context.Context) error {
	if p.Analyzer == nil {
		return ErrUnavailable
	}
	if p.Limiter == nil {
		return nil
	}
	decision, err := p.Limiter.Allow(ctx, "gemini", ratelimit.Window{
		Limit: p.RPM, Period: time.Minute,
	})
	if err != nil {
		return fmt.Errorf("Gemini rate limiter: %w", err)
	}
	if !decision.Allowed {
		return fmt.Errorf("Gemini rate limit reached; retry after %s", decision.RetryAfter)
	}
	return nil
}

func (p Processor) handleTextFailure(ctx context.Context, work TextWork, failure error) error {
	permanentFailure := IsPermanent(failure) || retryExhausted(ctx)
	if err := p.Store.FailText(ctx, work, failure, permanentFailure, p.now()); err != nil {
		return errors.Join(failure, err)
	}
	if permanentFailure {
		return fmt.Errorf("text AI failed permanently: %w: %w", failure, asynq.SkipRetry)
	}
	return fmt.Errorf("text AI failed transiently: %w", failure)
}

func (p Processor) handleAttachmentFailure(
	ctx context.Context,
	work AttachmentWork,
	failure error,
) error {
	permanentFailure := IsPermanent(failure) || retryExhausted(ctx)
	if err := p.Store.FailAttachment(ctx, work, failure, permanentFailure, p.now()); err != nil {
		return errors.Join(failure, err)
	}
	if permanentFailure {
		return fmt.Errorf("attachment AI failed permanently: %w: %w", failure, asynq.SkipRetry)
	}
	return fmt.Errorf("attachment AI failed transiently: %w", failure)
}

func (p Processor) ErrorHandler() asynq.ErrorHandler {
	return asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, failure error) {
		if !retryExhausted(ctx) {
			return
		}
		payload, err := decodeTask(task)
		if err != nil {
			return
		}
		var kind TaskKind
		switch task.Type() {
		case TaskTextProcess:
			kind = TaskKindText
		case TaskAttachmentProcess:
			kind = TaskKindAttachment
		default:
			return
		}
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := p.Store.FinalizeExhaustedTask(finalizeCtx, kind, payload, failure, p.now()); err != nil && p.Logger != nil {
			p.Logger.ErrorContext(ctx, "finalize exhausted AI task", "error", err, "task_type", task.Type())
		}
	})
}

func retryExhausted(ctx context.Context) bool {
	retried, retryOK := asynq.GetRetryCount(ctx)
	maximum, maximumOK := asynq.GetMaxRetry(ctx)
	return retryOK && maximumOK && retried >= maximum
}

func decodeTask(task *asynq.Task) (taskPayload, error) {
	var payload taskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return taskPayload{}, fmt.Errorf("decode AI task: %w: %w", err, asynq.SkipRetry)
	}
	if payload.TenantID == "" || payload.ResourceID == "" {
		return taskPayload{}, fmt.Errorf("AI task identifiers are required: %w", asynq.SkipRetry)
	}
	return payload, nil
}

func (p Processor) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}
