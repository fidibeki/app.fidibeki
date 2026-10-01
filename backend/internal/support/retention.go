package support

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

// Keep conversations for 180 days after the last message. A member-scoped
// privacy hold prevents the scheduled sweep; account deletion remains explicit.
type Maintainer struct {
	Database db.Querier
	Health   retentionhealth.Recorder
	Logger   *slog.Logger
}

func (m Maintainer) MaintainOnce(ctx context.Context, now time.Time) error {
	phase, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var processed int64
	var failure error
	exhausted := false
	for batch := 0; batch < 10; batch++ {
		result, err := m.Database.Exec(phase, `DELETE FROM support_conversations WHERE id IN (
            SELECT c.id FROM support_conversations c WHERE c.updated_at<$1
            AND NOT EXISTS(SELECT 1 FROM privacy_case_legal_holds h
                JOIN privacy_cases p ON p.tenant_id=h.tenant_id AND p.id=h.privacy_case_id
                WHERE h.tenant_id=c.tenant_id AND p.subject_member_id=c.member_id AND h.active)
            ORDER BY c.updated_at,c.id LIMIT 100 FOR UPDATE OF c SKIP LOCKED
        )`, now.Add(-180*24*time.Hour))
		if err != nil {
			failure = err
			break
		}
		processed += result.RowsAffected()
		if result.RowsAffected() < 100 {
			break
		}
		exhausted = batch == 9
	}
	result := retentionhealth.ResultSucceeded
	if failure != nil {
		result = retentionhealth.ResultFailed
	}
	recordErr := m.Health.RecordRun(ctx, retentionhealth.Run{Operation: retentionhealth.OperationSupportConversations, AttemptedAt: now, Result: result, Processed: processed, BudgetExhausted: exhausted})
	return errors.Join(failure, recordErr)
}

func (m Maintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger := m.Logger
	if logger == nil {
		logger = slog.Default()
	}
	for {
		if err := m.MaintainOnce(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "support retention failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
