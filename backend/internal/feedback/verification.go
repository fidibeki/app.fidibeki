package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/hibiken/asynq"
)

type VerificationStorage interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
	RemoveObject(context.Context, string) error
}

type AttachmentSafetyScanner interface {
	Scan(context.Context, VerificationWork, io.Reader) (string, error)
}

type VerifiedAttachmentScheduler interface {
	ScheduleAttachment(context.Context, string, string) error
}

type AttachmentVerificationStore interface {
	Claim(context.Context, VerificationTask) (VerificationWork, error)
	MarkSafe(context.Context, VerificationWork, string, time.Time) error
	MarkRejected(context.Context, VerificationWork, string, *string, time.Time) error
	CompleteRejectedCleanup(context.Context, VerificationWork) error
}

type safetyFailure struct {
	reason   string
	detected string
}

func (failure safetyFailure) Error() string {
	return failure.reason
}

type SignatureScanner struct{}

func (SignatureScanner) Scan(
	_ context.Context,
	work VerificationWork,
	reader io.Reader,
) (string, error) {
	if work.SizeBytes < 1 {
		return "", safetyFailure{reason: "empty_upload"}
	}
	if work.Kind == AttachmentReplay && normalizeContentType(work.ContentType) == "application/json" {
		body, err := io.ReadAll(io.LimitReader(reader, work.SizeBytes+1))
		if err != nil {
			return "", fmt.Errorf("read replay attachment: %w", err)
		}
		if int64(len(body)) != work.SizeBytes {
			return "", safetyFailure{reason: "size_mismatch"}
		}
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) == 0 || trimmed[0] != '[' || !json.Valid(trimmed) {
			return "", safetyFailure{reason: "invalid_json"}
		}
		return "application/json", nil
	}

	header, err := readVerificationHeader(reader, work.SizeBytes)
	if err != nil {
		return "", err
	}
	detected := detectContentType(header)
	if detected == "" {
		return "", safetyFailure{reason: "unknown_signature"}
	}
	declared := normalizeContentType(work.ContentType)
	if !signatureMatches(declared, detected) {
		return "", safetyFailure{reason: "signature_mismatch", detected: detected}
	}
	if !kindMatches(work.Kind, detected) {
		return "", safetyFailure{reason: "kind_mismatch", detected: detected}
	}
	return detected, nil
}

func readVerificationHeader(reader io.Reader, declaredSize int64) ([]byte, error) {
	headerCapacity := int64(512)
	if declaredSize+1 < headerCapacity {
		headerCapacity = declaredSize + 1
	}
	header := make([]byte, headerCapacity)
	read, readErr := io.ReadFull(io.LimitReader(reader, declaredSize+1), header)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("read attachment body: %w", readErr)
	}
	header = header[:read]
	total := int64(read)
	if total <= declaredSize {
		copied, err := io.Copy(io.Discard, io.LimitReader(reader, declaredSize-total+1))
		if err != nil {
			return nil, fmt.Errorf("read attachment body: %w", err)
		}
		total += copied
	}
	if total != declaredSize {
		return nil, safetyFailure{reason: "size_mismatch"}
	}
	return header, nil
}

func detectContentType(header []byte) string {
	switch {
	case len(header) >= 8 && bytes.Equal(header[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png"
	case len(header) >= 3 && header[0] == 0xff && header[1] == 0xd8 && header[2] == 0xff:
		return "image/jpeg"
	case len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) &&
		bytes.Equal(header[8:12], []byte("WEBP")):
		return "image/webp"
	case len(header) >= 4 && bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return "video/webm"
	case len(header) >= 4 && bytes.Equal(header[:4], []byte("OggS")):
		return "audio/ogg"
	case len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) &&
		bytes.Equal(header[8:12], []byte("WAVE")):
		return "audio/wav"
	case len(header) >= 3 && bytes.Equal(header[:3], []byte("ID3")):
		return "audio/mpeg"
	case len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0:
		return "audio/mpeg"
	case len(header) >= 12 && bytes.Equal(header[4:8], []byte("ftyp")):
		return "video/mp4"
	case len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b:
		return "application/gzip"
	default:
		trimmed := bytes.TrimLeftFunc(header, unicode.IsSpace)
		if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
			return "application/json"
		}
		return ""
	}
}

func signatureMatches(declared, detected string) bool {
	if declared == detected {
		return true
	}
	if (declared == "audio/mp4" || declared == "video/mp4") && detected == "video/mp4" {
		return true
	}
	if (declared == "audio/webm" || declared == "video/webm") && detected == "video/webm" {
		return true
	}
	if declared == "application/x-gzip" && detected == "application/gzip" {
		return true
	}
	return false
}

func kindMatches(kind AttachmentKind, detected string) bool {
	switch kind {
	case AttachmentScreenshot:
		return strings.HasPrefix(detected, "image/")
	case AttachmentAudio:
		return strings.HasPrefix(detected, "audio/") ||
			detected == "video/mp4" || detected == "video/webm"
	case AttachmentVideo:
		return strings.HasPrefix(detected, "video/")
	case AttachmentReplay:
		return detected == "application/json" || detected == "application/gzip"
	default:
		return false
	}
}

type VerificationProcessor struct {
	Store       AttachmentVerificationStore
	Storage     VerificationStorage
	Scanner     AttachmentSafetyScanner
	AI          VerifiedAttachmentScheduler
	Diagnostics AbuseDiagnosticRecorder
	Now         func() time.Time
	Logger      *slog.Logger
}

func (processor VerificationProcessor) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		payload, err := decodeVerificationTask(task.Payload())
		if err != nil {
			return fmt.Errorf("decode attachment verification: %w: %w", err, asynq.SkipRetry)
		}
		work, err := processor.Store.Claim(ctx, payload)
		if errors.Is(err, ErrNoVerificationWork) {
			return nil
		}
		if err != nil {
			return err
		}
		if work.SafetyStatus == AttachmentSafetyRejected {
			if err := processor.Storage.RemoveObject(ctx, work.StorageKey); err != nil {
				return err
			}
			return processor.Store.CompleteRejectedCleanup(ctx, work)
		}
		if processor.Scanner == nil {
			return errors.New("attachment safety scanner is unavailable")
		}
		reader, err := processor.Storage.OpenObject(ctx, work.StorageKey)
		if err != nil {
			return err
		}
		detected, scanErr := processor.Scanner.Scan(ctx, work, reader)
		closeErr := reader.Close()
		if closeErr != nil && scanErr == nil {
			scanErr = fmt.Errorf("close attachment verification stream: %w", closeErr)
		}
		if scanErr != nil {
			var unsafe safetyFailure
			if !errors.As(scanErr, &unsafe) {
				return scanErr
			}
			var detectedType *string
			if unsafe.detected != "" {
				detectedType = &unsafe.detected
			}
			if err := processor.Store.MarkRejected(
				ctx, work, unsafe.reason, detectedType, processor.now(),
			); err != nil {
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
		if err := processor.Store.MarkSafe(ctx, work, detected, processor.now()); err != nil {
			return err
		}
		if processor.AI != nil {
			if err := processor.AI.ScheduleAttachment(
				context.WithoutCancel(ctx), work.AttachmentID, work.FeedbackID,
			); err != nil {
				processor.logger().WarnContext(ctx, "schedule verified attachment AI",
					"error", err, "attachment_id", work.AttachmentID)
			}
		}
		return nil
	}
}

func decodeVerificationTask(payload []byte) (VerificationTask, error) {
	var task VerificationTask
	if err := json.Unmarshal(payload, &task); err != nil {
		return VerificationTask{}, err
	}
	if strings.TrimSpace(task.TenantID) == "" || strings.TrimSpace(task.AttachmentID) == "" ||
		task.Generation < 1 {
		return VerificationTask{}, errors.New("attachment verification task is invalid")
	}
	return task, nil
}

func (processor VerificationProcessor) recordRejected(
	ctx context.Context,
	work VerificationWork,
	reason string,
) {
	if processor.Diagnostics == nil {
		return
	}
	if err := processor.Diagnostics.Record(ctx, abuse.DiagnosticEvent{
		TenantID: work.TenantID, ProjectID: work.ProjectID,
		Category: abuse.CategoryUploadSafety, Reason: reason, OccurredAt: processor.now(),
	}); err != nil {
		processor.logger().WarnContext(ctx, "record rejected attachment diagnostic", "error", err)
	}
}

func (processor VerificationProcessor) now() time.Time {
	if processor.Now != nil {
		return processor.Now().UTC()
	}
	return time.Now().UTC()
}

func (processor VerificationProcessor) logger() *slog.Logger {
	if processor.Logger != nil {
		return processor.Logger
	}
	return slog.Default()
}
