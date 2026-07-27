package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const seatSyncFailureMessage = "Seat quantity could not be synchronized with Stripe."

type SeatSyncCandidate struct {
	TenantID          string
	SubscriptionID    string
	ConfirmedQuantity int
	DesiredQuantity   int
}

type SeatSyncStore interface {
	ListSeatSyncCandidates(context.Context, int) ([]SeatSyncCandidate, error)
	RecordSeatSyncSuccess(context.Context, string, string, int) error
	RecordSeatSyncFailure(context.Context, string, string, string) error
}

type SeatReconciler struct {
	Store     SeatSyncStore
	Provider  Provider
	Logger    *slog.Logger
	BatchSize int
}

func (reconciler SeatReconciler) ReconcileOnce(ctx context.Context) error {
	if reconciler.Store == nil || reconciler.Provider == nil {
		return errors.New("seat reconciliation collaborators are unavailable")
	}
	batchSize := reconciler.BatchSize
	if batchSize <= 0 || batchSize > 100 {
		batchSize = 100
	}
	candidates, err := reconciler.Store.ListSeatSyncCandidates(ctx, batchSize)
	if err != nil {
		return err
	}
	var joined error
	for _, candidate := range candidates {
		if err := reconciler.reconcile(ctx, candidate); err != nil {
			joined = errors.Join(joined, fmt.Errorf(
				"reconcile tenant %s seat quantity: %w",
				candidate.TenantID,
				err,
			))
		}
	}
	return joined
}

func (reconciler SeatReconciler) reconcile(
	ctx context.Context,
	candidate SeatSyncCandidate,
) error {
	remote, err := reconciler.Provider.RetrieveSubscription(ctx, candidate.SubscriptionID)
	if err != nil {
		return reconciler.fail(ctx, candidate, err)
	}
	desired := max(1, candidate.DesiredQuantity)
	if remote.Quantity != desired {
		key := fmt.Sprintf(
			"seat-sync:%s:%d:%d",
			candidate.TenantID,
			remote.Quantity,
			desired,
		)
		if err := reconciler.Provider.UpdateSubscriptionQuantity(
			ctx,
			remote.ItemID,
			desired,
			key,
		); err != nil {
			return reconciler.fail(ctx, candidate, err)
		}
	}
	if err := reconciler.Store.RecordSeatSyncSuccess(
		ctx,
		candidate.TenantID,
		candidate.SubscriptionID,
		desired,
	); err != nil {
		return err
	}
	return nil
}

func (reconciler SeatReconciler) fail(
	ctx context.Context,
	candidate SeatSyncCandidate,
	cause error,
) error {
	if reconciler.Logger != nil {
		reconciler.Logger.ErrorContext(
			ctx,
			"seat quantity synchronization failed",
			"tenant_id",
			candidate.TenantID,
			"subscription_id",
			candidate.SubscriptionID,
			"error",
			cause,
		)
	}
	if err := reconciler.Store.RecordSeatSyncFailure(
		ctx,
		candidate.TenantID,
		candidate.SubscriptionID,
		seatSyncFailureMessage,
	); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (reconciler SeatReconciler) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Minute
	}
	if err := reconciler.ReconcileOnce(ctx); err != nil && reconciler.Logger != nil {
		reconciler.Logger.ErrorContext(ctx, "seat reconciliation sweep failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := reconciler.ReconcileOnce(ctx); err != nil && reconciler.Logger != nil {
				reconciler.Logger.ErrorContext(ctx, "seat reconciliation sweep failed", "error", err)
			}
		}
	}
}
