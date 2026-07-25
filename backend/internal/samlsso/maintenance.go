package samlsso

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

const samlMaintenanceTimeout = time.Minute

// MaintenanceStore sweeps expired SAML authentication artifacts across every
// tenant, so it runs on the admin pool rather than inside a tenant transaction.
type MaintenanceStore struct {
	Database db.Querier
}

// ExpiredArtifacts hard-deletes correlation and replay rows whose authoritative
// expiry has passed. Both boundaries are the ones the validator already
// enforces — an AuthnRequest past expires_at can no longer correlate, and an
// assertion past not_on_or_after can no longer be accepted — so a swept row
// never had remaining function. Nothing about the sign-in is retained here; the
// audit trail lives in tenant_access_events.
func (store MaintenanceStore) ExpiredArtifacts(ctx context.Context, now time.Time) (int64, error) {
	return Store{}.PurgeExpired(ctx, store.Database, now.UTC())
}

// Maintainer runs the sweep on a ticker and reports to the bounded
// retention-health ledger, matching tenantmember.Maintainer's shape.
type Maintainer struct {
	Store        MaintenanceStore
	Health       retentionhealth.Recorder
	Logger       *slog.Logger
	Now          func() time.Time
	PhaseTimeout time.Duration
}

func (maintainer Maintainer) MaintainOnce(ctx context.Context) error {
	if maintainer.Store.Database == nil {
		return errors.New("SAML retention store is unavailable")
	}
	if maintainer.Health == nil {
		return errors.New("SAML retention health recorder is unavailable")
	}
	now := maintainer.now()
	timeout := maintainer.PhaseTimeout
	if timeout <= 0 {
		timeout = samlMaintenanceTimeout
	}
	phaseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deleted, err := maintainer.Store.ExpiredArtifacts(phaseCtx, now)
	maintainer.logger().InfoContext(ctx, "SAML authentication artifact retention completed",
		"artifacts_deleted", deleted, "failed", err != nil)
	result := retentionhealth.ResultSucceeded
	if err != nil {
		result = retentionhealth.ResultFailed
	}
	recordErr := maintainer.Health.RecordRun(ctx, retentionhealth.Run{
		Operation:   retentionhealth.OperationSAMLAuthArtifacts,
		AttemptedAt: now, Result: result, Processed: deleted,
	})
	if recordErr != nil {
		recordErr = fmt.Errorf("record SAML retention health: %w", recordErr)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("expire SAML authentication artifacts: %w", err), recordErr)
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
			maintainer.logger().ErrorContext(ctx, "SAML retention failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
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
