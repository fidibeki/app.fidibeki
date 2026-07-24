package platformadmin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

const (
	magicLinkRetentionBatchSize    = 200
	magicLinkRetentionMaxBatches   = 20
	magicLinkRetentionPhaseTimeout = time.Minute
)

type MaintenanceOperations interface {
	ExpiredMagicLinks(ctx context.Context, now time.Time, limit int) (int64, error)
}

// Maintainer bounds and retries the expired platform-admin magic-link sweep
// (issue #62's platform-admin authentication-artifact tracer bullet) using
// the same batched-drain, ticker-driven shape as tenantmember.Maintainer.
type Maintainer struct {
	Store        MaintenanceOperations
	Health       retentionhealth.Recorder
	Logger       *slog.Logger
	Now          func() time.Time
	BatchSize    int
	MaxBatches   int
	PhaseTimeout time.Duration
}

func (maintainer Maintainer) MaintainOnce(ctx context.Context) error {
	if maintainer.Store == nil {
		return errors.New("platform admin retention store is unavailable")
	}
	if maintainer.Health == nil {
		return errors.New("platform admin retention health recorder is unavailable")
	}
	now := maintainer.now()
	batchSize := maintainer.BatchSize
	if batchSize <= 0 {
		batchSize = magicLinkRetentionBatchSize
	}
	maxBatches := maintainer.MaxBatches
	if maxBatches <= 0 {
		maxBatches = magicLinkRetentionMaxBatches
	}
	phaseCtx, cancel := maintainer.phaseContext(ctx)
	defer cancel()
	deleted, budgetExhausted, err := drainMagicLinkRetention(
		phaseCtx, batchSize, maxBatches,
		func(expireCtx context.Context, limit int) (int64, error) {
			return maintainer.Store.ExpiredMagicLinks(expireCtx, now, limit)
		},
	)
	log := maintainer.logger()
	log.InfoContext(ctx, "platform admin magic link retention completed",
		"magic_links_deleted", deleted, "failed", err != nil)
	if budgetExhausted {
		log.WarnContext(ctx, "platform admin magic link retention budget exhausted",
			"maximum_batches", maxBatches)
	}
	result := retentionhealth.ResultSucceeded
	if err != nil {
		result = retentionhealth.ResultFailed
	}
	recordErr := maintainer.Health.RecordRun(ctx, retentionhealth.Run{
		Operation:   retentionhealth.OperationPlatformAdminMagicLinks,
		AttemptedAt: now, Result: result, Processed: deleted,
		BudgetExhausted: budgetExhausted,
	})
	if recordErr != nil {
		recordErr = fmt.Errorf("record platform admin magic link retention health: %w", recordErr)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("expire platform admin magic links: %w", err), recordErr)
	}
	return recordErr
}

func (maintainer Maintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := maintainer.MaintainOnce(ctx); err != nil && ctx.Err() == nil {
			maintainer.logger().ErrorContext(ctx, "platform admin magic link retention failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func drainMagicLinkRetention(
	ctx context.Context,
	batchSize int,
	maxBatches int,
	expire func(context.Context, int) (int64, error),
) (int64, bool, error) {
	var total int64
	for batch := 0; batch < maxBatches; batch++ {
		deleted, err := expire(ctx, batchSize)
		total += deleted
		if err != nil {
			return total, false, err
		}
		if deleted < int64(batchSize) {
			return total, false, nil
		}
	}
	return total, true, nil
}

func (maintainer Maintainer) phaseContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := maintainer.PhaseTimeout
	if timeout <= 0 {
		timeout = magicLinkRetentionPhaseTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (maintainer Maintainer) now() time.Time {
	if maintainer.Now != nil {
		return maintainer.Now().UTC()
	}
	return time.Now().UTC()
}

func (maintainer Maintainer) logger() *slog.Logger {
	if maintainer.Logger != nil {
		return maintainer.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
