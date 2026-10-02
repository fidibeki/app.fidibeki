package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/aiprovider"
	"github.com/hibiken/asynq"
)

func (f *ProviderFactory) FileDeleteHandler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		payload, err := decodeTask(task)
		if err != nil {
			return err
		}
		work, err := f.Ledger.ClaimGeminiFile(ctx, payload.TenantID, payload.ResourceID, payload.Attempt)
		if errors.Is(err, ErrNoWork) {
			return nil
		}
		if err != nil {
			return err
		}
		key := f.ManagedKey
		if len(work.CredentialEnvelope) > 0 {
			connection := work.ConnectionID
			if connection == "" {
				connection = "managed"
			}
			key, err = f.Connections.Keys.Decrypt(work.CredentialEnvelope, aiprovider.CredentialAAD(work.TenantID, connection, work.CredentialVersion))
		} else if work.ConnectionID != "" {
			err = aiprovider.ErrNotFound
		}
		var deletion error
		if err != nil {
			deletion = permanent(&aiprovider.Failure{Code: "cleanup_credential_unavailable"})
		} else {
			a, initErr := newGeminiAnalyzerWithClient(ctx, key, "cleanup", f.Ledger, f.Logger, f.client(nil))
			if initErr != nil {
				deletion = safeProviderError(initErr)
			} else {
				_, deletion = a.deleteFileOnce(ctx, work.FileName)
				deletion = safeProviderError(deletion)
			}
		}
		if deletion == nil {
			return f.Ledger.CompleteGeminiFileDeletion(ctx, work, time.Now().UTC())
		}
		permanentFailure := IsPermanent(deletion)
		requeue := retryExhausted(ctx) && !permanentFailure
		if err := f.Ledger.FailGeminiFileDeletion(ctx, work, deletion, requeue, permanentFailure, time.Now().UTC()); err != nil {
			return err
		}
		if permanentFailure || requeue {
			return fmt.Errorf("provider cleanup stopped: %w", asynq.SkipRetry)
		}
		return deletion
	}
}
