package websitechat

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

// Keep conversations for 180 days after the last message. Anonymous visitors
// cannot be matched to a privacy case, so any active tenant hold stops cleanup.
type Maintainer struct {
	Database db.Querier
	Health   retentionhealth.Recorder
	Logger   *slog.Logger
}

func (m Maintainer) MaintainOnce(ctx context.Context, now time.Time) error {
	_, err := m.maintainOnce(ctx, now)
	return err
}

func (m Maintainer) maintainOnce(ctx context.Context, now time.Time) (bool, error) {
	phase, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var processed int64
	var failure error
	exhausted := false
	for batch := 0; batch < 10; batch++ {
		result, err := m.Database.Exec(phase, `DELETE FROM website_chat_visitors WHERE id IN (
            SELECT c.id FROM website_chat_visitors c WHERE ((c.last_sequence>0 AND c.updated_at<$1) OR (c.last_sequence=0 AND c.expires_at<$2))
            AND NOT EXISTS(SELECT 1 FROM privacy_case_legal_holds h
                WHERE h.tenant_id=c.tenant_id AND h.active)
            ORDER BY c.updated_at,c.id LIMIT 100 FOR UPDATE OF c SKIP LOCKED
        )`, now.Add(-180*24*time.Hour), now.Add(-24*time.Hour))
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
	recordErr := m.Health.RecordRun(ctx, retentionhealth.Run{Operation: retentionhealth.OperationWebsiteChatConversations, AttemptedAt: now, Result: result, Processed: processed, BudgetExhausted: exhausted})
	return exhausted, errors.Join(failure, recordErr)
}

func (m Maintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	logger := m.Logger
	if logger == nil {
		logger = slog.Default()
	}
	for {
		exhausted, err := m.maintainOnce(ctx, time.Now().UTC())
		if err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "website chat retention failed", "error", err)
		}
		// Continue a full sweep promptly so traffic cannot outgrow a daily cap.
		delay := interval
		if exhausted && delay > time.Second {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
