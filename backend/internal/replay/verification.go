package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/hibiken/asynq"
)

const TaskVerifyBatch = "replay:verify-batch"

var ErrNoBatchVerificationWork = errors.New("no replay batch verification work")

type BatchVerificationTask struct {
	TenantID   string `json:"tenant_id"`
	BatchID    string `json:"batch_id"`
	Generation int    `json:"generation"`
}

type BatchVerificationWork struct {
	Batch
}

type BatchVerificationStore interface {
	Claim(context.Context, BatchVerificationTask) (BatchVerificationWork, error)
	MarkSafe(context.Context, BatchVerificationWork, time.Time) error
	MarkRejected(context.Context, BatchVerificationWork, string, time.Time) error
	CompleteRejectedCleanup(context.Context, BatchVerificationWork) error
}

type BatchVerificationStorage interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
	RemoveObject(context.Context, string) error
}

type BatchDiagnosticRecorder interface {
	Record(context.Context, abuse.DiagnosticEvent) error
}

type BatchVerificationProcessor struct {
	Store       BatchVerificationStore
	Storage     BatchVerificationStorage
	Diagnostics BatchDiagnosticRecorder
	Now         func() time.Time
	Logger      *slog.Logger
}

func (processor BatchVerificationProcessor) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		payload, err := decodeBatchVerificationTask(task.Payload())
		if err != nil {
			return fmt.Errorf("decode replay batch verification: %w: %w", err, asynq.SkipRetry)
		}
		work, err := processor.Store.Claim(ctx, payload)
		if errors.Is(err, ErrNoBatchVerificationWork) {
			return nil
		}
		if err != nil {
			return err
		}
		if work.SafetyStatus == BatchSafetyRejected {
			if err := processor.Storage.RemoveObject(ctx, work.StorageKey); err != nil {
				return err
			}
			return processor.Store.CompleteRejectedCleanup(ctx, work)
		}
		reader, err := processor.Storage.OpenObject(ctx, work.StorageKey)
		if err != nil {
			return err
		}
		_, verifyErr := decodeVerifiedBatch(reader, work.Batch, nil)
		closeErr := reader.Close()
		if closeErr != nil && verifyErr == nil {
			verifyErr = fmt.Errorf("close replay batch verification stream: %w", closeErr)
		}
		if verifyErr != nil {
			var unsafe unsafeReplayBatch
			if !errors.As(verifyErr, &unsafe) {
				return verifyErr
			}
			if err := processor.Store.MarkRejected(ctx, work, unsafe.reason, processor.now()); err != nil {
				return err
			}
			if err := processor.Storage.RemoveObject(ctx, work.StorageKey); err != nil {
				return err
			}
			if err := processor.Store.CompleteRejectedCleanup(ctx, work); err != nil {
				return err
			}
			processor.recordRejected(ctx, work, unsafe.reason)
			return nil
		}
		return processor.Store.MarkSafe(ctx, work, processor.now())
	}
}

func decodeBatchVerificationTask(payload []byte) (BatchVerificationTask, error) {
	var task BatchVerificationTask
	if err := json.Unmarshal(payload, &task); err != nil {
		return BatchVerificationTask{}, err
	}
	if strings.TrimSpace(task.TenantID) == "" || strings.TrimSpace(task.BatchID) == "" ||
		task.Generation < 1 {
		return BatchVerificationTask{}, errors.New("replay batch verification task is invalid")
	}
	return task, nil
}

func (processor BatchVerificationProcessor) recordRejected(
	ctx context.Context,
	work BatchVerificationWork,
	reason string,
) {
	if processor.Diagnostics == nil {
		return
	}
	if err := processor.Diagnostics.Record(ctx, abuse.DiagnosticEvent{
		TenantID: work.TenantID, ProjectID: work.ProjectID,
		Category: abuse.CategoryUploadSafety, Reason: "replay_" + reason,
		OccurredAt: processor.now(),
	}); err != nil {
		processor.logger().WarnContext(ctx, "record rejected replay diagnostic", "error", err)
	}
}

func (processor BatchVerificationProcessor) now() time.Time {
	if processor.Now != nil {
		return processor.Now().UTC()
	}
	return time.Now().UTC()
}

func (processor BatchVerificationProcessor) logger() *slog.Logger {
	if processor.Logger != nil {
		return processor.Logger
	}
	return slog.Default()
}

type unsafeReplayBatch struct {
	reason string
}

func (failure unsafeReplayBatch) Error() string {
	return failure.reason
}

func decodeVerifiedBatch(
	reader io.Reader,
	batch Batch,
	consume func([]byte) error,
) (int, error) {
	if batch.SizeBytes < 1 || batch.SizeBytes > maximumBatchSize {
		return 0, unsafeReplayBatch{reason: "size_mismatch"}
	}
	body, err := io.ReadAll(io.LimitReader(reader, batch.SizeBytes+1))
	if err != nil {
		return 0, fmt.Errorf("read replay batch %d: %w", batch.Index, err)
	}
	if int64(len(body)) != batch.SizeBytes {
		return 0, unsafeReplayBatch{reason: "size_mismatch"}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return 0, unsafeReplayBatch{reason: "expected_array"}
	}
	count := 0
	for decoder.More() {
		var event json.RawMessage
		if err := decoder.Decode(&event); err != nil {
			return 0, unsafeReplayBatch{reason: "invalid_json"}
		}
		if !validRRWebEvent(event) {
			return 0, unsafeReplayBatch{reason: "invalid_event"}
		}
		if consume != nil {
			if err := consume(event); err != nil {
				return 0, err
			}
		}
		count++
		if count > 50 {
			return 0, unsafeReplayBatch{reason: "event_count_mismatch"}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return 0, unsafeReplayBatch{reason: "invalid_json"}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return 0, unsafeReplayBatch{reason: "trailing_json"}
	}
	if count != batch.EventsCount {
		return 0, unsafeReplayBatch{reason: "event_count_mismatch"}
	}
	return count, nil
}

func validRRWebEvent(raw json.RawMessage) bool {
	var event struct {
		Type      *int            `json:"type"`
		Timestamp *float64        `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return false
	}
	if event.Type == nil || *event.Type < 0 || *event.Type > 6 ||
		event.Timestamp == nil || math.IsNaN(*event.Timestamp) ||
		math.IsInf(*event.Timestamp, 0) || *event.Timestamp <= 0 {
		return false
	}
	trimmed := bytes.TrimSpace(event.Data)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}
