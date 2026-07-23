package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/hibiken/asynq"
)

type VerificationStorage interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
	PutObject(context.Context, string, io.Reader, int64, string) error
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
	MarkSafe(context.Context, VerificationWork, string, string, time.Time) error
	CompleteSafeCleanup(context.Context, VerificationWork, time.Time) error
	MarkRejected(context.Context, VerificationWork, string, *string, time.Time) error
	CompleteRejectedCleanup(context.Context, VerificationWork, time.Time) error
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
	seeker, cleanup, err := verificationReadSeeker(reader, work.SizeBytes)
	if err != nil {
		return "", err
	}
	if cleanup != nil {
		defer cleanup()
	}
	if work.Kind == AttachmentReplay && normalizeContentType(work.ContentType) == "application/json" {
		if !validJSONStream(seeker, work.SizeBytes) {
			return "", safetyFailure{reason: "invalid_json"}
		}
		return "application/json", nil
	}

	headerSize := min(work.SizeBytes, 512)
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(seeker, header); err != nil {
		return "", fmt.Errorf("read attachment header: %w", err)
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind attachment header: %w", err)
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
	if !validMediaStructureReader(seeker, work.SizeBytes, detected) {
		return "", safetyFailure{reason: "invalid_structure", detected: detected}
	}
	return detected, nil
}

type verificationReader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

func verificationReadSeeker(
	reader io.Reader,
	declaredSize int64,
) (verificationReader, func(), error) {
	if declaredSize < 1 || declaredSize > maxAttachmentBytes {
		return nil, nil, safetyFailure{reason: "size_mismatch"}
	}
	if seeker, ok := reader.(verificationReader); ok {
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, nil, fmt.Errorf("measure attachment body: %w", err)
		}
		if size != declaredSize {
			return nil, nil, safetyFailure{reason: "size_mismatch"}
		}
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return nil, nil, fmt.Errorf("rewind attachment body: %w", err)
		}
		return seeker, nil, nil
	}
	temporary, err := stageVerificationObject(reader, declaredSize)
	if err != nil {
		return nil, nil, err
	}
	return temporary, func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}, nil
}

func validJSONStream(reader io.ReadSeeker, size int64) bool {
	if size < 2 || size > attachmentRules[AttachmentReplay].maxBytes {
		return false
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return false
	}
	decoder := json.NewDecoder(io.LimitReader(reader, size+1))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return false
	}
	for decoder.More() {
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return false
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim(']') {
		return false
	}
	var trailing json.RawMessage
	return errors.Is(decoder.Decode(&trailing), io.EOF)
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
		if work.SafetyStatus == AttachmentSafetySafe {
			stagingKey, err := stagingAttachmentKey(work.StorageKey)
			if err != nil {
				return err
			}
			if err := processor.Storage.RemoveObject(ctx, stagingKey); err != nil {
				return err
			}
			if processor.now().Before(work.UploadExpiresAt.Add(uploadCleanupGrace)) {
				return nil
			}
			return processor.Store.CompleteSafeCleanup(ctx, work, processor.now())
		}
		if work.SafetyStatus == AttachmentSafetyRejected {
			if err := processor.Storage.RemoveObject(ctx, work.StorageKey); err != nil {
				return err
			}
			if processor.now().Before(work.UploadExpiresAt.Add(uploadCleanupGrace)) {
				return nil
			}
			return processor.Store.CompleteRejectedCleanup(ctx, work, processor.now())
		}
		if processor.Scanner == nil {
			return errors.New("attachment safety scanner is unavailable")
		}
		reader, err := processor.Storage.OpenObject(ctx, work.StorageKey)
		if err != nil {
			return err
		}
		temporary, stageErr := stageVerificationObject(reader, work.SizeBytes)
		closeErr := reader.Close()
		if closeErr != nil && stageErr == nil {
			stageErr = fmt.Errorf("close attachment verification stream: %w", closeErr)
		}
		if stageErr != nil {
			return processor.rejectUnsafe(ctx, work, stageErr)
		}
		defer func() {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}()
		detected, scanErr := processor.Scanner.Scan(ctx, work, temporary)
		if scanErr != nil {
			return processor.rejectUnsafe(ctx, work, scanErr)
		}
		if _, err := temporary.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("rewind verified attachment: %w", err)
		}
		verifiedKey := verifiedAttachmentKey(work.StorageKey, work.Generation, work.Attempt)
		if err := processor.Storage.PutObject(
			ctx, verifiedKey, temporary, work.SizeBytes, detected,
		); err != nil {
			return err
		}
		if err := processor.Store.MarkSafe(
			ctx, work, detected, verifiedKey, processor.now(),
		); err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			cleanupErr := processor.Storage.RemoveObject(cleanupCtx, verifiedKey)
			cancel()
			return errors.Join(err, cleanupErr)
		}
		if err := processor.Storage.RemoveObject(ctx, work.StorageKey); err != nil {
			return err
		}
		if !processor.now().Before(work.UploadExpiresAt.Add(uploadCleanupGrace)) {
			if err := processor.Store.CompleteSafeCleanup(ctx, work, processor.now()); err != nil {
				return err
			}
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

const uploadCleanupGrace = time.Minute

func stageVerificationObject(reader io.Reader, declaredSize int64) (*os.File, error) {
	if declaredSize < 1 || declaredSize > maxAttachmentBytes {
		return nil, safetyFailure{reason: "size_mismatch"}
	}
	temporary, err := os.CreateTemp("", "fidibeki-attachment-verification-*")
	if err != nil {
		return nil, fmt.Errorf("create attachment verification file: %w", err)
	}
	written, copyErr := io.Copy(temporary, io.LimitReader(reader, declaredSize+1))
	if copyErr != nil {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return nil, fmt.Errorf("stage attachment verification body: %w", copyErr)
	}
	if written != declaredSize {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return nil, safetyFailure{reason: "size_mismatch"}
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return nil, fmt.Errorf("rewind attachment verification body: %w", err)
	}
	return temporary, nil
}

func verifiedAttachmentKey(stagingKey string, generation int, attempt int) string {
	return fmt.Sprintf("%s.verified-%d-%d", stagingKey, generation, attempt)
}

func stagingAttachmentKey(verifiedKey string) (string, error) {
	marker := strings.LastIndex(verifiedKey, ".verified-")
	if marker < 1 {
		return "", errors.New("verified attachment storage key is invalid")
	}
	parts := strings.Split(verifiedKey[marker+len(".verified-"):], "-")
	if len(parts) < 1 || len(parts) > 2 {
		return "", errors.New("verified attachment storage key is invalid")
	}
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 1 {
			return "", errors.New("verified attachment storage key is invalid")
		}
	}
	return verifiedKey[:marker], nil
}

func (processor VerificationProcessor) rejectUnsafe(
	ctx context.Context,
	work VerificationWork,
	scanErr error,
) error {
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
	if !processor.now().Before(work.UploadExpiresAt.Add(uploadCleanupGrace)) {
		if err := processor.Store.CompleteRejectedCleanup(ctx, work, processor.now()); err != nil {
			return err
		}
	}
	processor.recordRejected(ctx, work, unsafe.reason)
	return nil
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
