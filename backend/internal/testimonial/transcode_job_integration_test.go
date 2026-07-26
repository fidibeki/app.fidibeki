package testimonial

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

// fakeEncoder stands in for ffmpeg. What is under test is the job's ordering and
// its refusals, not whether libx264 works.
type fakeEncoder struct {
	probed   Probed
	probeErr error
	encoded  Dimensions
	calls    []string
}

func (f *fakeEncoder) Probe(context.Context, string) (Probed, error) {
	f.calls = append(f.calls, "probe")
	return f.probed, f.probeErr
}

func (f *fakeEncoder) EncodeVideo(_ context.Context, _, destination string, target Dimensions) error {
	f.calls = append(f.calls, "encode")
	f.encoded = target
	return os.WriteFile(destination, []byte("encoded video bytes"), 0o600)
}

func (f *fakeEncoder) ExtractPoster(_ context.Context, _, destination string) error {
	f.calls = append(f.calls, "poster")
	return os.WriteFile(destination, []byte("poster bytes"), 0o600)
}

// memoryStorage keeps rendition bytes in memory so the job can be exercised
// without object storage.
type memoryStorage struct {
	source  []byte
	written map[string][]byte
}

func (m *memoryStorage) OpenObject(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(m.source))), nil
}

func (m *memoryStorage) PutObject(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	payload, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if m.written == nil {
		m.written = map[string][]byte{}
	}
	m.written[key] = payload
	return nil
}

func TestTranscodeProducesServableRenditions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := newTestimonialEnvironment(ctx, t)
	now := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	publicID := seedCompletedUpload(ctx, t, env, "tm_")

	encoder := &fakeEncoder{probed: Probed{
		// Portrait, which is what most people record and the shape most likely to
		// be mangled by a pipeline that assumes landscape.
		Dimensions: Dimensions{Width: 1080, Height: 1920}, DurationSeconds: 42,
	}}
	storage := &memoryStorage{source: []byte("original upload bytes")}
	options := TranscodeOptions{
		Store: Store{}, Storage: storage, Encoder: encoder,
		Begin: db.BeginFromPool(env.pools.App), Now: func() time.Time { return now },
	}

	if err := RunTranscode(ctx, options, TranscodePayload{
		TenantID: env.tenantID, MediaID: publicID,
	}); err != nil {
		t.Fatalf("transcode: %v", err)
	}

	// The file is probed before anything else happens, because everything after
	// depends on knowing what it is.
	if len(encoder.calls) == 0 || encoder.calls[0] != "probe" {
		t.Errorf("calls = %v, want the probe first", encoder.calls)
	}
	if encoder.encoded != (Dimensions{Width: 720, Height: 1280}) {
		t.Errorf("encoded at %v, want 720x1280 — a portrait recording kept portrait",
			encoder.encoded)
	}
	if len(storage.written) != 2 {
		t.Errorf("stored %d renditions, want the video and its poster", len(storage.written))
	}

	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var transcode, safety, hash string
		var width, height, duration int
		if err := tx.QueryRow(ctx, `
			SELECT transcode_status::text, safety_status::text, content_hash, width, height,
			       duration_seconds
			FROM testimonial_media WHERE tenant_id = $1 AND public_id = $2
		`, env.tenantID, publicID).Scan(&transcode, &safety, &hash, &width, &height, &duration); err != nil {
			t.Fatalf("read transcoded media: %v", err)
		}
		if transcode != "ready" || safety != "safe" {
			t.Errorf("status = %s/%s, want ready/safe", transcode, safety)
		}
		if len(hash) != 64 {
			t.Errorf("content hash = %q, want a sha256 the public path can address", hash)
		}
		// The source dimensions are recorded, not the rendition's: the backoffice
		// tells the customer what the author actually sent.
		if width != 1080 || height != 1920 || duration != 42 {
			t.Errorf("recorded %dx%d for %ds, want 1080x1920 for 42s", width, height, duration)
		}

		var kinds []string
		rows, err := tx.Query(ctx, `
			SELECT kind::text FROM testimonial_media_renditions
			WHERE tenant_id = $1 ORDER BY kind::text
		`, env.tenantID)
		if err != nil {
			t.Fatalf("read renditions: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var kind string
			if err := rows.Scan(&kind); err != nil {
				t.Fatalf("scan rendition: %v", err)
			}
			kinds = append(kinds, kind)
		}
		if len(kinds) != 2 || kinds[0] != "mp4_720p" || kinds[1] != "poster" {
			t.Errorf("renditions = %v, want mp4_720p and poster", kinds)
		}
	})

	// Claimed work is not claimable twice, so two workers cannot encode the same
	// upload into two sets of renditions.
	encoder.calls = nil
	if err := RunTranscode(ctx, options, TranscodePayload{
		TenantID: env.tenantID, MediaID: publicID,
	}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(encoder.calls) != 0 {
		t.Errorf("second run did work: %v", encoder.calls)
	}
}

// A file that is not a video will never become one, so it is recorded as failed
// with the reason rather than retried forever.
func TestTranscodeRefusesSomethingThatIsNotAVideo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := newTestimonialEnvironment(ctx, t)
	now := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	publicID := seedCompletedUpload(ctx, t, env, "tm_")

	encoder := &fakeEncoder{probeErr: fmt.Errorf("moov atom not found")}
	if err := RunTranscode(ctx, TranscodeOptions{
		Store: Store{}, Storage: &memoryStorage{source: []byte("not a video")},
		Encoder: encoder, Begin: db.BeginFromPool(env.pools.App),
		Now: func() time.Time { return now },
	}, TranscodePayload{TenantID: env.tenantID, MediaID: publicID}); err != nil {
		t.Fatalf("transcode: %v", err)
	}

	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var status, safety string
		var reason *string
		if err := tx.QueryRow(ctx, `
			SELECT transcode_status::text, safety_status::text, transcode_error
			FROM testimonial_media WHERE tenant_id = $1 AND public_id = $2
		`, env.tenantID, publicID).Scan(&status, &safety, &reason); err != nil {
			t.Fatalf("read failed media: %v", err)
		}
		if status != "failed" || reason == nil || *reason != "not_a_video" {
			t.Errorf("status = %s, reason = %v; want failed with a reason worth showing",
				status, reason)
		}
		// Never promoted: a file nothing could decode is not a file anything
		// verified.
		if safety == "safe" {
			t.Error("an undecodable upload was marked safe")
		}
	})
}

func seedCompletedUpload(
	ctx context.Context,
	t *testing.T,
	env *testimonialEnvironment,
	_ string,
) string {
	t.Helper()
	publicID, err := GeneratePublicID(PrefixMedia)
	if err != nil {
		t.Fatalf("generate media id: %v", err)
	}
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		request, err := Store{}.CreateRequest(ctx, tx, env.tenantID, RequestValues{
			ProjectID: &env.projectID, Kind: RequestCollection,
			TokenHash:      strings.Repeat("d", 64),
			RecipientEmail: "recorder@example.test",
			ExpiresAt:      time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO testimonial_media (
				tenant_id, request_id, public_id, role, source_storage_key, content_type,
				size_bytes, safety_status, transcode_status, completed_at
			) VALUES ($1, $2, $3, 'video', 'uploads/source', 'video/mp4', 4096,
				'quarantined', 'pending', now())
		`, env.tenantID, request.ID, publicID); err != nil {
			t.Fatalf("insert completed upload: %v", err)
		}
	})
	return publicID
}
