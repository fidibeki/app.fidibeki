package testimonial

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/hibiken/asynq"
)

// TaskTranscode is the asynq task name for turning an upload into renditions.
const TaskTranscode = "testimonial:transcode"

// TranscodeQueueName keeps this work off the queues that serve people waiting.
// Encoding a video takes seconds to minutes; nothing else should queue behind it.
const TranscodeQueueName = "testimonial_media"

// transcodeTimeout bounds one encode. A two-minute phone recording finishes far
// inside this; anything that does not is stuck rather than slow.
const transcodeTimeout = 10 * time.Minute

// TranscodeEnqueuer schedules the work. Left nil the upload still succeeds and
// a sweep picks it up later, because losing the author's recording because a
// queue was briefly unreachable would be the worse failure.
type TranscodeEnqueuer interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

// TranscodePayload names the asset to work on.
type TranscodePayload struct {
	TenantID string `json:"tenant_id"`
	MediaID  string `json:"media_id"`
}

// RenditionValues is one derived file ready to be recorded.
type RenditionValues struct {
	Kind        string
	StorageKey  string
	ContentType string
	SizeBytes   int64
	Width       int
	Height      int
	ContentHash string
}

// TranscodeWork is everything the job needs about one asset.
type TranscodeWork struct {
	MediaID     string
	StorageKey  string
	ContentType string
	SizeBytes   int64
}

// TranscodeStore is the surface the job needs.
type TranscodeStore interface {
	ClaimTranscode(context.Context, db.Querier, string, string, time.Time) (TranscodeWork, error)
	CompleteTranscode(context.Context, db.Querier, string, string, string, Probed, []RenditionValues, time.Time) error
	FailTranscode(context.Context, db.Querier, string, string, string, time.Time) error
	MarkSourceDeleted(context.Context, db.Querier, string, string, string, time.Time) error
}

// TranscodeStorage is the object storage the job reads from and writes to.
type TranscodeStorage interface {
	Stat(context.Context, string) (storage.ObjectInfo, error)
	OpenObject(context.Context, string) (io.ReadCloser, error)
	PutObject(context.Context, string, io.Reader, int64, string) error
	RemoveObject(context.Context, string) error
}

// TranscodeOptions wires the job.
type TranscodeOptions struct {
	Store   TranscodeStore
	Storage TranscodeStorage
	Encoder Encoder
	Begin   db.BeginFunc
	Logger  *slog.Logger
	Now     func() time.Time
	// MaxSourceBytes is lowered by tests. The zero value uses the product
	// limit, keeping production configuration non-optional.
	MaxSourceBytes int64
}

// NewTranscodeTask builds the task enqueued when an upload completes.
func NewTranscodeTask(tenantID, mediaID string) (*asynq.Task, error) {
	payload, err := json.Marshal(TranscodePayload{TenantID: tenantID, MediaID: mediaID})
	if err != nil {
		return nil, fmt.Errorf("encode transcode payload: %w", err)
	}
	return asynq.NewTask(TaskTranscode, payload, asynq.Queue(TranscodeQueueName)), nil
}

// TranscodeHandler returns the asynq handler.
func TranscodeHandler(options TranscodeOptions) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload TranscodePayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			// Undecodable payloads never become decodable. Retrying is noise.
			return fmt.Errorf("decode transcode payload: %w: %w", err, asynq.SkipRetry)
		}
		return RunTranscode(ctx, options, payload)
	}
}

// RunTranscode turns one upload into the derivatives a wall can serve.
//
// The order matters: the file is probed before anything else happens, and a
// probe that fails is the job's safety check. It does not prove the contents are
// wholesome — nothing here watches the video — but it does prove the bytes are a
// decodable media file rather than something wearing a video's name, which is
// the class of upload that hurts whoever opens it. What a human should see is
// the separate review gate, and that stays unaffected by anything decided here.
func RunTranscode(ctx context.Context, options TranscodeOptions, payload TranscodePayload) error {
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now()
	}
	ctx, cancel := context.WithTimeout(ctx, transcodeTimeout)
	defer cancel()

	var work TranscodeWork
	if err := withTranscodeTenant(ctx, options, payload.TenantID, func(query db.Querier) error {
		claimed, err := options.Store.ClaimTranscode(ctx, query, payload.TenantID, payload.MediaID, now)
		work = claimed
		return err
	}); err != nil {
		// Already claimed, already done, or gone. None of those are failures.
		return nil
	}
	maxSourceBytes := options.MaxSourceBytes
	if maxSourceBytes <= 0 || maxSourceBytes > maxVideoBytes {
		maxSourceBytes = maxVideoBytes
	}
	object, err := options.Storage.Stat(ctx, work.StorageKey)
	if err != nil {
		return options.fail(ctx, payload, work.StorageKey, "source_unavailable", now, err)
	}
	if object.Size < 1 || object.Size > maxSourceBytes {
		return options.fail(ctx, payload, work.StorageKey, "source_too_large", now,
			fmt.Errorf("source size %d exceeds %d", object.Size, maxSourceBytes))
	}
	if object.Size != work.SizeBytes ||
		strings.ToLower(strings.TrimSpace(object.ContentType)) != work.ContentType {
		return options.fail(ctx, payload, work.StorageKey, "source_mismatch", now,
			fmt.Errorf("source metadata changed after completion"))
	}

	directory, err := os.MkdirTemp("", "fidibeki-transcode-")
	if err != nil {
		return fmt.Errorf("create transcode workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()

	sourcePath := filepath.Join(directory, "source")
	sourceHash, err := download(
		ctx, options.Storage, work.StorageKey, sourcePath, work.SizeBytes, maxSourceBytes,
	)
	if err != nil {
		reason := "download_failed"
		if errors.Is(err, errSourceTooLarge) {
			reason = "source_too_large"
		}
		return options.fail(ctx, payload, work.StorageKey, reason, now, err)
	}

	probed, err := options.Encoder.Probe(ctx, sourcePath)
	if err != nil {
		return options.fail(ctx, payload, work.StorageKey, "not_a_video", now, err)
	}
	target := Fit(probed.Dimensions)
	if target == (Dimensions{}) {
		return options.fail(ctx, payload, work.StorageKey, "unusable_dimensions", now,
			fmt.Errorf("probed %dx%d", probed.Width, probed.Height))
	}

	videoPath := filepath.Join(directory, "video.mp4")
	if err := options.Encoder.EncodeVideo(ctx, sourcePath, videoPath, target); err != nil {
		return options.fail(ctx, payload, work.StorageKey, "encode_failed", now, err)
	}
	posterPath := filepath.Join(directory, "poster.jpg")
	if err := options.Encoder.ExtractPoster(ctx, videoPath, posterPath); err != nil {
		return options.fail(ctx, payload, work.StorageKey, "poster_failed", now, err)
	}

	renditions := []struct {
		kind        string
		path        string
		contentType string
		dimensions  Dimensions
	}{
		{"mp4_720p", videoPath, "video/mp4", target},
		{"poster", posterPath, "image/jpeg", target},
	}
	values := make([]RenditionValues, 0, len(renditions))
	for _, rendition := range renditions {
		stored, err := upload(ctx, options.Storage, payload, rendition.kind,
			rendition.path, rendition.contentType)
		if err != nil {
			return options.fail(ctx, payload, work.StorageKey, "upload_failed", now, err)
		}
		stored.Kind = rendition.kind
		stored.Width = rendition.dimensions.Width
		stored.Height = rendition.dimensions.Height
		values = append(values, stored)
	}

	if err := withTranscodeTenant(ctx, options, payload.TenantID, func(query db.Querier) error {
		return options.Store.CompleteTranscode(ctx, query, payload.TenantID, payload.MediaID,
			sourceHash, probed, values, now)
	}); err != nil {
		return err
	}
	return options.removeSource(ctx, payload, work.StorageKey, now)
}

var errSourceTooLarge = errors.New("testimonial video exceeds the source limit")

// download streams the object to disk and hashes it on the way past, so the
// bytes are read once rather than twice.
func download(
	ctx context.Context,
	storage TranscodeStorage,
	key, destination string,
	expectedBytes, maxBytes int64,
) (string, error) {
	reader, err := storage.OpenObject(ctx, key)
	if err != nil {
		return "", fmt.Errorf("open source video: %w", err)
	}
	defer func() { _ = reader.Close() }()

	file, err := os.Create(destination)
	if err != nil {
		return "", fmt.Errorf("create local source: %w", err)
	}
	defer func() { _ = file.Close() }()

	digest := sha256.New()
	copied, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("stream source video: %w", err)
	}
	if copied > maxBytes {
		return "", fmt.Errorf("%w: copied more than %d bytes", errSourceTooLarge, maxBytes)
	}
	if copied != expectedBytes {
		return "", fmt.Errorf("source size changed: copied %d bytes, expected %d", copied, expectedBytes)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func upload(
	ctx context.Context,
	storage TranscodeStorage,
	payload TranscodePayload,
	kind, path, contentType string,
) (RenditionValues, error) {
	file, err := os.Open(path)
	if err != nil {
		return RenditionValues{}, fmt.Errorf("open rendition: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return RenditionValues{}, fmt.Errorf("stat rendition: %w", err)
	}
	if info.Size() < 1 || info.Size() > maxVideoBytes {
		return RenditionValues{}, fmt.Errorf(
			"rendition size %d exceeds the %d byte budget", info.Size(), int64(maxVideoBytes),
		)
	}
	digest := sha256.New()
	if _, err := io.CopyN(digest, file, info.Size()); err != nil {
		return RenditionValues{}, fmt.Errorf("hash rendition: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return RenditionValues{}, fmt.Errorf("rewind rendition: %w", err)
	}
	hash := hex.EncodeToString(digest.Sum(nil))
	key := fmt.Sprintf("testimonials/%s/%s/%s-%s", payload.TenantID, payload.MediaID, kind, hash[:16])
	if err := storage.PutObject(ctx, key, file, info.Size(), contentType); err != nil {
		return RenditionValues{}, fmt.Errorf("store rendition: %w", err)
	}
	return RenditionValues{
		StorageKey: key, ContentType: contentType, SizeBytes: info.Size(), ContentHash: hash,
	}, nil
}

// fail records why, and does not retry.
//
// A file that is not a video will not become one, and an encode that failed on
// its content fails the same way every time. Retrying would burn a worker slot
// repeatedly for the same answer; the customer is told instead.
func (o TranscodeOptions) fail(
	ctx context.Context,
	payload TranscodePayload,
	storageKey string,
	reason string,
	now time.Time,
	cause error,
) error {
	if o.Logger != nil {
		o.Logger.ErrorContext(ctx, "testimonial transcode failed",
			"media_id", payload.MediaID, "reason", reason, "error", cause)
	}
	if err := withTranscodeTenant(ctx, o, payload.TenantID, func(query db.Querier) error {
		return o.Store.FailTranscode(ctx, query, payload.TenantID, payload.MediaID, reason, now)
	}); err != nil {
		return err
	}
	return o.removeSource(ctx, payload, storageKey, now)
}

func (o TranscodeOptions) removeSource(
	ctx context.Context,
	payload TranscodePayload,
	storageKey string,
	now time.Time,
) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := o.Storage.RemoveObject(cleanupCtx, storageKey); err != nil {
		return fmt.Errorf("remove testimonial source video: %w", err)
	}
	return withTranscodeTenant(cleanupCtx, o, payload.TenantID, func(query db.Querier) error {
		return o.Store.MarkSourceDeleted(
			cleanupCtx, query, payload.TenantID, payload.MediaID, storageKey, now,
		)
	})
}

func withTranscodeTenant(
	ctx context.Context,
	options TranscodeOptions,
	tenantID string,
	work func(db.Querier) error,
) error {
	tx, err := options.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	var configured string
	if err := tx.QueryRow(ctx, `SELECT set_config('app.tenant_id', $1, true)`,
		tenantID).Scan(&configured); err != nil || configured != tenantID {
		return fmt.Errorf("configure transcode tenant context")
	}
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}
