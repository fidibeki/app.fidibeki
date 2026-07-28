package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/hibiken/asynq"
	"google.golang.org/genai"
)

type ObjectSource interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
}

type Processor struct {
	Store       Store
	Analyzer    Analyzer
	Storage     ObjectSource
	Limiter     ratelimit.Limiter
	RPM         int
	Now         func() time.Time
	Logger      *slog.Logger
	Diagnostics DiagnosticRecorder
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
		authorized, err := p.Store.AuthorizeTextForProvider(ctx, work, p.now())
		if err != nil {
			return err
		}
		if !authorized {
			return nil
		}
		result, err := p.Analyzer.AnalyzeText(ctx, work.Text)
		if err != nil {
			return p.handleTextFailure(ctx, work, err)
		}
		result, err = ProtectResult(result)
		if err != nil {
			return p.handleTextFailure(ctx, work, err)
		}
		if err := p.Store.CompleteText(ctx, work, result, p.now()); errors.Is(err, ErrNoWork) {
			return nil
		} else if err != nil {
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
		authorized, err := p.Store.AuthorizeAttachmentForProvider(ctx, work, p.now())
		if err != nil {
			return err
		}
		if !authorized {
			return nil
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
		result, err = ProtectResult(result)
		if err != nil {
			return p.handleAttachmentFailure(ctx, work, err)
		}
		if err := p.Store.CompleteAttachment(ctx, work, result, p.now()); errors.Is(err, ErrNoWork) {
			return nil
		} else if err != nil {
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
		return fmt.Errorf("gemini rate limiter: %w", err)
	}
	if !decision.Allowed {
		return fmt.Errorf("gemini rate limit reached; retry after %s", decision.RetryAfter)
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

type retryMetadata struct {
	Count      int
	CountKnown bool
	Maximum    int
	MaxKnown   bool
}

func safeFailureLogAttrs(failure error, retry retryMetadata) (slog.Level, bool, []slog.Attr) {
	classification := "internal_error"
	providerCode := 0

	var apiError genai.APIError
	if errors.As(failure, &apiError) {
		providerCode = apiError.Code
		switch {
		case apiError.Code == 401:
			classification = "provider_unauthenticated"
		case apiError.Code == 403:
			classification = "provider_forbidden"
		case apiError.Code == 404:
			classification = "provider_not_found"
		case apiError.Code == 408 || apiError.Code == 504:
			classification = "provider_timeout"
		case apiError.Code == 409:
			classification = "provider_conflict"
		case apiError.Code == 429:
			classification = "provider_rate_limited"
		case apiError.Code >= 500 && apiError.Code <= 599:
			classification = "provider_unavailable"
		case apiError.Code >= 400 && apiError.Code <= 499:
			classification = "provider_rejected"
		default:
			classification = "provider_error"
		}
	} else if errors.Is(failure, ErrUnavailable) {
		classification = "analyzer_unavailable"
	} else if errors.Is(failure, context.DeadlineExceeded) {
		classification = "deadline_exceeded"
	} else if errors.Is(failure, context.Canceled) {
		classification = "canceled"
	} else {
		var networkError net.Error
		switch {
		case errors.As(failure, &networkError) && networkError.Timeout():
			classification = "network_timeout"
		case errors.As(failure, &networkError):
			classification = "network_error"
		case IsPermanent(failure):
			classification = "permanent_failure"
		}
	}

	attributes := []slog.Attr{
		slog.String("failure_class", classification),
		slog.Bool("failure_permanent", IsPermanent(failure)),
	}
	if providerCode >= 400 && providerCode <= 599 {
		attributes = append(attributes, slog.Int("provider_code", providerCode))
	}
	if retry.CountKnown {
		attributes = append(attributes, slog.Int("retry_count", retry.Count))
	}
	if retry.MaxKnown {
		attributes = append(attributes, slog.Int("max_retry", retry.Maximum))
	}

	retryKnown := retry.CountKnown && retry.MaxKnown
	exhausted := retryKnown && retry.Count >= retry.Maximum
	skipRetry := errors.Is(failure, asynq.SkipRetry)
	level := slog.LevelWarn
	disposition := "unknown"
	switch {
	case exhausted:
		level = slog.LevelError
		disposition = "exhausted"
	case skipRetry:
		level = slog.LevelError
		disposition = "skip_retry"
	case retryKnown:
		disposition = "retry"
	}
	attributes = append(attributes, slog.String("retry_disposition", disposition))
	if retryKnown {
		attributes = append(attributes,
			slog.Bool("retry_exhausted", exhausted),
			slog.Bool("will_retry", !exhausted && !skipRetry),
		)
	} else if skipRetry {
		attributes = append(attributes, slog.Bool("will_retry", false))
	}
	return level, exhausted, attributes
}

func (p Processor) ErrorHandler() asynq.ErrorHandler {
	return asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, failure error) {
		payload, decodeErr := decodeTask(task)
		retryCount, retryCountOK := asynq.GetRetryCount(ctx)
		maxRetry, maxRetryOK := asynq.GetMaxRetry(ctx)
		level, exhausted, failureAttributes := safeFailureLogAttrs(failure, retryMetadata{
			Count: retryCount, CountKnown: retryCountOK, Maximum: maxRetry, MaxKnown: maxRetryOK,
		})
		if p.Logger != nil {
			attributes := []slog.Attr{
				slog.String("task_type", task.Type()),
				slog.Bool("task_payload_decoded", decodeErr == nil),
			}
			attributes = append(attributes, failureAttributes...)
			if decodeErr == nil {
				attributes = append(attributes,
					slog.String("tenant_id", payload.TenantID),
					slog.String("project_id", payload.ProjectID),
					slog.String("resource_id", payload.ResourceID),
				)
			}
			p.Logger.LogAttrs(ctx, level, "AI task failed", attributes...)
		}
		if decodeErr == nil {
			p.recordProviderDiagnostic(ctx, payload, failure)
		}
		if !exhausted || decodeErr != nil {
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

func (p Processor) recordProviderDiagnostic(
	ctx context.Context,
	payload taskPayload,
	failure error,
) {
	if p.Diagnostics == nil || payload.ProjectID == "" {
		return
	}
	reason := providerDiagnosticReason(failure)
	if err := p.Diagnostics.Record(ctx, abuse.DiagnosticEvent{
		TenantID: payload.TenantID, ProjectID: payload.ProjectID,
		Category: abuse.CategoryProvider, Reason: reason, OccurredAt: p.now(),
	}); err != nil && p.Logger != nil {
		p.Logger.WarnContext(ctx, "record AI provider diagnostic", "error", err)
	}
}

func providerDiagnosticReason(failure error) string {
	var apiError genai.APIError
	if errors.As(failure, &apiError) {
		switch {
		case apiError.Code == 401:
			return "provider_unauthenticated"
		case apiError.Code == 403:
			return "provider_forbidden"
		case apiError.Code == 404:
			return "provider_not_found"
		case apiError.Code == 408 || apiError.Code == 504:
			return "provider_timeout"
		case apiError.Code == 409:
			return "provider_conflict"
		case apiError.Code == 429:
			return "provider_rate_limited"
		case apiError.Code >= 500 && apiError.Code <= 599:
			return "provider_unavailable"
		case apiError.Code >= 400 && apiError.Code <= 499:
			return "provider_rejected"
		default:
			return "provider_error"
		}
	}
	switch {
	case errors.Is(failure, ErrUnavailable):
		return "analyzer_unavailable"
	case errors.Is(failure, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(failure, context.Canceled):
		return "canceled"
	}
	var networkError net.Error
	switch {
	case errors.As(failure, &networkError) && networkError.Timeout():
		return "network_timeout"
	case errors.As(failure, &networkError):
		return "network_error"
	case IsPermanent(failure):
		return "permanent_failure"
	default:
		return "internal_error"
	}
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
