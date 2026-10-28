package emailfeedback

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/fidibeki/app.fidibeki/backend/internal/outbox"
	"github.com/hibiken/asynq"
)

type OutboxResolver struct{}

func (OutboxResolver) Resolve(
	_ context.Context,
	event outbox.Event,
) ([]outbox.Destination, error) {
	if event.Event != EventInvitationCreated ||
		event.ResourceType != "email_feedback_invitation" {
		return nil, nil
	}
	return []outbox.Destination{{
		Kind:     "email-feedback",
		ID:       event.ResourceID,
		TaskType: TaskDeliverInvitation,
		Queue:    email.QueueName,
	}}, nil
}

type InvitationDeliveryHandler struct {
	Store            Store
	Begin            db.BeginFunc
	Sender           email.Sender
	Tokens           *InvitationTokenManager
	From             string
	PublicBaseURL    string
	WebPublicBaseURL string
	BaseDomain       string
	Now              func() time.Time
}

func (handler InvitationDeliveryHandler) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload DeliveryTask
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.OutboxEventID == "" || payload.TenantID == "" ||
			payload.ResourceID == "" {
			return fmt.Errorf("decode email feedback delivery task: %w", asynq.SkipRetry)
		}
		retryCount, _ := asynq.GetRetryCount(ctx)
		maxRetry, hasMaxRetry := asynq.GetMaxRetry(ctx)
		return handler.deliver(ctx, payload, hasMaxRetry && retryCount >= maxRetry)
	}
}

func (handler InvitationDeliveryHandler) deliver(
	ctx context.Context,
	task DeliveryTask,
	finalAttempt bool,
) error {
	if handler.Begin == nil || handler.Sender == nil || handler.Tokens == nil {
		return errors.New("email feedback delivery handler is incomplete")
	}
	transaction, err := handler.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin email feedback delivery: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback(ctx)
		}
	}()

	delivery, err := handler.Store.LoadInvitationDelivery(ctx, transaction, task)
	if errors.Is(err, ErrInvitationDeliveryNotFound) {
		return fmt.Errorf("email feedback invitation no longer exists: %w", asynq.SkipRetry)
	}
	if err != nil {
		return err
	}
	if delivery.Status != InvitationQueued {
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit completed email feedback delivery: %w", err)
		}
		committed = true
		return nil
	}
	now := handler.now()
	if !delivery.ExpiresAt.After(now) {
		if err := handler.Store.MarkInvitationExpired(ctx, transaction, delivery, now); err != nil {
			return err
		}
		return commitInvitationDelivery(ctx, transaction, &committed)
	}
	if delivery.Suppressed {
		if err := handler.Store.MarkInvitationSuppressed(ctx, transaction, delivery, now); err != nil {
			return err
		}
		return commitInvitationDelivery(ctx, transaction, &committed)
	}
	if delivery.RecipientEmail == nil || delivery.TokenHash == nil {
		return handler.finishDeliveryFailure(
			ctx, transaction, &committed, delivery,
			errors.New("email feedback invitation delivery data is incomplete"), true, now,
		)
	}
	rawToken, tokenHash, err := handler.Tokens.Token(delivery.InvitationID)
	if err != nil || subtle.ConstantTimeCompare(
		[]byte(tokenHash), []byte(*delivery.TokenHash),
	) != 1 {
		if err == nil {
			err = errors.New("stored email feedback invitation token does not match")
		}
		return handler.finishDeliveryFailure(
			ctx, transaction, &committed, delivery, err, true, now,
		)
	}
	message, err := composeInvitationMessage(delivery, rawToken, invitationMessageOptions{
		From:             handler.From,
		PublicBaseURL:    handler.PublicBaseURL,
		WebPublicBaseURL: handler.WebPublicBaseURL,
		BaseDomain:       handler.BaseDomain,
	})
	if err != nil {
		return handler.finishDeliveryFailure(
			ctx, transaction, &committed, delivery, err, true, now,
		)
	}
	providerMessageID, err := handler.Sender.Send(ctx, message)
	if err != nil {
		return handler.finishDeliveryFailure(
			ctx, transaction, &committed, delivery, err, finalAttempt, now,
		)
	}
	if err := handler.Store.MarkInvitationSent(
		ctx, transaction, delivery, providerMessageID, now,
	); err != nil {
		return err
	}
	return commitInvitationDelivery(ctx, transaction, &committed)
}

func (handler InvitationDeliveryHandler) finishDeliveryFailure(
	ctx context.Context,
	transaction db.Transaction,
	committed *bool,
	delivery InvitationDelivery,
	cause error,
	terminal bool,
	now time.Time,
) error {
	if err := handler.Store.RecordInvitationDeliveryFailure(
		ctx, transaction, delivery, cause.Error(), terminal, now,
	); err != nil {
		return errors.Join(cause, err)
	}
	if err := commitInvitationDelivery(ctx, transaction, committed); err != nil {
		return errors.Join(cause, err)
	}
	if terminal {
		return fmt.Errorf("email feedback invitation delivery failed permanently: %w", cause)
	}
	return fmt.Errorf("send email feedback invitation: %w", cause)
}

func commitInvitationDelivery(
	ctx context.Context,
	transaction db.Transaction,
	committed *bool,
) error {
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit email feedback invitation delivery: %w", err)
	}
	*committed = true
	return nil
}

func (handler InvitationDeliveryHandler) now() time.Time {
	if handler.Now != nil {
		return handler.Now().UTC()
	}
	return time.Now().UTC()
}
