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

	"github.com/hibiken/asynq"
)

type MergeStorage interface {
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
		if err != nil {
			return err
		}
		if session.MergedAt == nil {
			if len(batches) == 0 {
				err = errors.New("replay merge has no completed batches")
				_ = p.Store.MarkMergeFailed(ctx, session.ID, err.Error())
				return err
			}
			key := fmt.Sprintf("tenants/%s/sessions/%s/replay.json", session.TenantID, session.ID)
			eventsCount, mergeErr := p.mergeBatches(ctx, key, batches)
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

func (p Processor) mergeBatches(ctx context.Context, key string, batches []Batch) (int, error) {
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
		count, err := p.appendBatch(ctx, writer, batch, total > 0)
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
	hasPrevious bool,
) (int, error) {
	object, err := p.Storage.OpenObject(ctx, batch.StorageKey)
	if err != nil {
		return 0, fmt.Errorf("open replay batch %d: %w", batch.Index, err)
	}
	defer object.Close()
	decoder := json.NewDecoder(io.LimitReader(object, batch.SizeBytes+1))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return 0, fmt.Errorf("decode replay batch %d: expected JSON array", batch.Index)
	}
	count := 0
	for decoder.More() {
		var event json.RawMessage
		if err := decoder.Decode(&event); err != nil {
			return 0, fmt.Errorf("decode replay event in batch %d: %w", batch.Index, err)
		}
		if hasPrevious || count > 0 {
			if err := writer.WriteByte(','); err != nil {
				return 0, fmt.Errorf("separate replay events: %w", err)
			}
		}
		if _, err := writer.Write(event); err != nil {
			return 0, fmt.Errorf("write replay event: %w", err)
		}
		count++
	}
	if _, err := decoder.Token(); err != nil {
		return 0, fmt.Errorf("finish replay batch %d: %w", batch.Index, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("finish replay batch %d: unexpected trailing JSON", batch.Index)
	}
	if count != batch.EventsCount {
		return 0, fmt.Errorf(
			"replay batch %d declared %d events but contained %d",
			batch.Index, batch.EventsCount, count,
		)
	}
	return count, nil
}

func (p Processor) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}
