package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/privacy"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/hibiken/asynq"
)

type MergeStorage interface {
	Stat(context.Context, string) (storage.ObjectInfo, error)
	OpenObject(context.Context, string) (io.ReadCloser, error)
	PutObject(context.Context, string, io.Reader, int64, string) error
	RemoveObject(context.Context, string) error
}

type Processor struct {
	Store   Store
	Storage MergeStorage
	Now     func() time.Time
}

func (p Processor) MergeHandler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.SessionID == "" {
			return fmt.Errorf("decode replay merge task: %w", asynq.SkipRetry)
		}
		session, batches, err := p.Store.LoadMerge(ctx, payload.SessionID)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("replay merge session no longer exists: %w", asynq.SkipRetry)
		}
		if errors.Is(err, ErrConsentRequired) {
			if markErr := p.Store.MarkUnavailable(ctx, payload.SessionID); markErr != nil {
				return errors.Join(err, markErr)
			}
			return fmt.Errorf("replay merge consent evidence is unavailable: %w", asynq.SkipRetry)
		}
		if errors.Is(err, privacy.ErrProcessingRestricted) {
			if markErr := p.Store.MarkUnavailable(ctx, payload.SessionID); markErr != nil {
				return errors.Join(err, markErr)
			}
			return fmt.Errorf("replay merge suppressed by privacy restriction: %w", asynq.SkipRetry)
		}
		if err != nil {
			return err
		}
		if session.MergedAt == nil {
			if len(batches) == 0 {
				if err := p.Store.MarkUnavailable(ctx, session.ID); err != nil {
					return err
				}
				return fmt.Errorf("replay unavailable: %w", asynq.SkipRetry)
			}
			key := fmt.Sprintf("tenants/%s/sessions/%s/replay.json", session.TenantID, session.ID)
			pageURL := ""
			if session.PageURL != nil {
				pageURL = *session.PageURL
			}
			eventsCount, mergeErr := p.mergeBatches(ctx, key, pageURL, batches)
			if mergeErr != nil {
				markErr := p.Store.MarkMergeFailed(ctx, session.ID, mergeErr.Error())
				return errors.Join(mergeErr, markErr)
			}
			if err := p.Store.MarkMerged(ctx, session.ID, key, eventsCount, p.now()); err != nil {
				return err
			}
		}
		for _, batch := range batches {
			if err := p.Storage.RemoveObject(ctx, batch.StorageKey); err != nil {
				return fmt.Errorf("remove merged replay batch: %w", err)
			}
			if err := p.Store.DeleteBatch(ctx, batch.ID); err != nil {
				return err
			}
		}
		return nil
	}
}

func (p Processor) mergeBatches(
	ctx context.Context,
	key string,
	expectedPageURL string,
	batches []Batch,
) (int, error) {
	temporary, err := os.CreateTemp("", "fidibeki-replay-*.json")
	if err != nil {
		return 0, fmt.Errorf("create replay merge file: %w", err)
	}
	path := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(path)
	}()
	writer := bufio.NewWriter(temporary)
	if _, err := writer.WriteString("["); err != nil {
		return 0, fmt.Errorf("start replay merge file: %w", err)
	}
	total := 0
	for _, batch := range batches {
		count, err := p.appendBatch(ctx, writer, batch, expectedPageURL, total > 0)
		if err != nil {
			return 0, err
		}
		total += count
	}
	if _, err := writer.WriteString("]"); err != nil {
		return 0, fmt.Errorf("finish replay merge file: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return 0, fmt.Errorf("flush replay merge file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return 0, fmt.Errorf("sync replay merge file: %w", err)
	}
	info, err := temporary.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat replay merge file: %w", err)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("rewind replay merge file: %w", err)
	}
	if err := p.Storage.PutObject(ctx, key, temporary, info.Size(), "application/json"); err != nil {
		return 0, err
	}
	return total, nil
}

func (p Processor) appendBatch(
	ctx context.Context,
	writer *bufio.Writer,
	batch Batch,
	expectedPageURL string,
	hasPrevious bool,
) (int, error) {
	object, err := p.Storage.OpenObject(ctx, batch.StorageKey)
	if err != nil {
		return 0, fmt.Errorf("open replay batch %d: %w", batch.Index, err)
	}
	defer func() { _ = object.Close() }()
	count := 0
	decoded, err := decodeVerifiedBatch(object, batch, expectedPageURL, func(event []byte) error {
		if hasPrevious || count > 0 {
			if err := writer.WriteByte(','); err != nil {
				return fmt.Errorf("separate replay events: %w", err)
			}
		}
		if _, err := writer.Write(event); err != nil {
			return fmt.Errorf("write replay event: %w", err)
		}
		count++
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("verify replay batch %d: %w", batch.Index, err)
	}
	return decoded, nil
}

func (p Processor) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}
