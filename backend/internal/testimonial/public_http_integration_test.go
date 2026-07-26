package testimonial

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestTestimonialLifecycle walks the whole path against real PostgreSQL:
// a scored candidate becomes a collection request, the author submits and
// consents, the customer approves and publishes a wall, an anonymous caller
// reads the snapshot, and a revocation takes effect on the very next read.
//
// The two assertions that matter most are the last two. They are the fixes for
// the defects the audit found: revocation must block a read before any rebuild
// runs, and a suspended workspace must stop serving immediately.
func TestTestimonialLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := newTestimonialEnvironment(ctx, t)
	store := Store{}

	// A collection request, as the customer would send it from the Suggested queue.
	token, tokenHash, err := GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	var request Request
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		request, err = store.CreateRequest(ctx, tx, env.tenantID, RequestValues{
			ProjectID:      &env.projectID,
			FeedbackID:     &env.feedbackID,
			Kind:           RequestCollection,
			TokenHash:      tokenHash,
			RecipientEmail: "author@example.test",
			ExpiresAt:      now.Add(30 * 24 * time.Hour),
			SentAt:         &now,
		})
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
	})

	// The token resolves without a tenant context, exactly as the public form
	// must resolve it before it can set one.
	resolvedTenant, err := store.ResolveRequestTenant(ctx, env.pools.Admin, HashToken(token))
	if err != nil || resolvedTenant != env.tenantID {
		t.Fatalf("resolve request tenant = %q, %v; want %q", resolvedTenant, err, env.tenantID)
	}
	if _, err := store.ResolveRequestTenant(ctx, env.pools.Admin, HashToken("not-a-real-token")); err == nil {
		t.Fatal("an unknown token resolved to a workspace")
	}

	const consented = "We cut our triage time from 3 days to 20 minutes in the first week."
	var item Testimonial
	// Kept, because the whole point of the next section is that the author can
	// use it. It is shown to them once and never stored in plaintext.
	var revocationToken string
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		publicID, err := GeneratePublicID(PrefixTestimonial)
		if err != nil {
			t.Fatalf("generate public id: %v", err)
		}
		var revocationHash string
		revocationToken, revocationHash, err = GenerateToken()
		if err != nil {
			t.Fatalf("generate revocation token: %v", err)
		}
		item, err = store.SubmitCollection(ctx, tx, env.tenantID, publicID, revocationHash,
			SubmissionValues{
				ProjectID:  &env.projectID,
				FeedbackID: &env.feedbackID,
				RequestID:  request.ID,
				Source:     SourceFeedback,
				Quote:      consented,
				Author: AuthorDetails{
					Name: "Ana Marques", Role: "Head of Support", Company: "Acme",
				},
				Scopes:  ConsentScopes{Quote: true, Identity: true, Company: true},
				Consent: ConsentEvidence{NoticeVersion: "v1", Locale: "pt-PT"},
			}, now)
		if err != nil {
			t.Fatalf("submit collection: %v", err)
		}
	})
	if item.ConsentState != ConsentGranted || item.Status != StatusPendingReview {
		t.Fatalf("submitted testimonial = %s/%s, want granted/pending_review",
			item.ConsentState, item.Status)
	}
	if item.ConsentedText != item.Quote {
		t.Fatal("submission must record the consented text as the displayed quote")
	}
	// The stars the author already gave with their feedback ride along without the
	// caller having to remember them, because the rating is a fact about the
	// source feedback rather than something the publication form collects again.
	if item.Rating == nil || *item.Rating != 5 {
		t.Errorf("rating = %v, want the source feedback's 5", item.Rating)
	}

	// A wall, published by the customer.
	var wall Wall
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if err := store.ApproveTestimonial(ctx, tx, env.tenantID, item.ID, env.memberID, now); err != nil {
			t.Fatalf("approve: %v", err)
		}
		publicID, err := GeneratePublicID(PrefixWall)
		if err != nil {
			t.Fatalf("generate wall id: %v", err)
		}
		wall, err = store.CreateWall(ctx, tx, env.tenantID, publicID, WallValues{
			Name: "Homepage", Slug: "homepage", Layout: LayoutGrid, MaxItems: 24,
			ShowRating: true, ShowAvatar: true, ShowCompany: true, ShowVideo: true,
		})
		if err != nil {
			t.Fatalf("create wall: %v", err)
		}
		if err := store.SetWallItems(ctx, tx, env.tenantID, wall.ID,
			[]WallItem{{TestimonialID: item.ID}}); err != nil {
			t.Fatalf("set wall items: %v", err)
		}
		cards, err := store.LoadWallCards(ctx, tx, env.tenantID, wall.ID)
		if err != nil {
			t.Fatalf("load wall cards: %v", err)
		}
		payload := BuildSnapshotPayload(wall, cards, now)
		if len(payload.Cards) != 1 {
			t.Fatalf("snapshot cards = %d, want 1 (%#v)", len(payload.Cards), payload)
		}
		if _, err := store.PublishSnapshot(ctx, tx, env.tenantID, wall.ID, payload, &env.memberID, now); err != nil {
			t.Fatalf("publish snapshot: %v", err)
		}
	})

	server := env.publicServer(t)
	defer server.Close()

	// An anonymous caller reads the wall.
	response, body := getWall(t, server.URL, wall.PublicID, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("public wall status = %d, want 200 (%s)", response.StatusCode, body)
	}
	var payload SnapshotPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode payload: %v (%s)", err, body)
	}
	if len(payload.Cards) != 1 || payload.Cards[0].Quote != consented {
		t.Fatalf("payload = %#v, want one card carrying the consented quote", payload)
	}
	if payload.Cards[0].Author.Name != "Ana Marques" {
		t.Fatalf("identity was consented but did not survive: %#v", payload.Cards[0].Author)
	}
	if payload.Cards[0].Rating == nil || *payload.Cards[0].Rating != 5 {
		t.Errorf("served rating = %v, want the 5 stars the author gave", payload.Cards[0].Rating)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("the embed endpoint must be readable cross-origin")
	}
	etag := response.Header.Get("ETag")
	if etag == "" {
		t.Fatal("a cacheable snapshot must carry an ETag")
	}

	// Conditional requests are what keep a popular wall cheap.
	notModified, _ := getWall(t, server.URL, wall.PublicID, etag)
	if notModified.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional request status = %d, want 304", notModified.StatusCode)
	}

	// Before revoking, while the testimonial is still publishable on every other
	// count: footage of its own has to be watched before it goes out.
	exerciseVideoReviewGate(ctx, t, env, store, wall, item, now)
	exerciseMediaServing(ctx, t, env, store, server.URL, item, now)
	exerciseVideoUpload(ctx, t, env)

	// The author withdraws through the link they were given, not through anything
	// the workspace does for them. The consent notice promises this page exists,
	// so the test opens it exactly as they would.
	revokeURL := server.URL + "/v1/public/testimonial-revocations/" + revocationToken
	preview, previewBody := getJSON(t, revokeURL)
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("open withdrawal link = %d, want 200 (%s)", preview.StatusCode, previewBody)
	}
	var view struct {
		Testimonial RevokeView `json:"testimonial"`
	}
	if err := json.Unmarshal([]byte(previewBody), &view); err != nil {
		t.Fatalf("decode withdrawal view: %v (%s)", err, previewBody)
	}
	if view.Testimonial.Quote != consented || view.Testimonial.Revoked {
		t.Fatalf("withdrawal view = %#v, want their own words and not yet revoked", view.Testimonial)
	}
	if unknown, _ := getJSON(t, server.URL+"/v1/public/testimonial-revocations/not-a-token"); unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown withdrawal token = %d, want 404", unknown.StatusCode)
	}

	// Revocation must take effect on the very next read, without any rebuild.
	withdrawn, withdrawnBody := postJSON(t, revokeURL, `{"reason":"author changed their mind"}`)
	if withdrawn.StatusCode != http.StatusOK {
		t.Fatalf("withdraw = %d, want 200 (%s)", withdrawn.StatusCode, withdrawnBody)
	}
	// Clicking twice reassures rather than errors: someone returning to an old
	// email should be told it is already withdrawn.
	if again, _ := postJSON(t, revokeURL, `{}`); again.StatusCode != http.StatusOK {
		t.Fatalf("second withdrawal = %d, want 200", again.StatusCode)
	}
	afterRevoke, revokedBody := getWall(t, server.URL, wall.PublicID, "")
	if afterRevoke.StatusCode != http.StatusOK {
		t.Fatalf("post-revocation status = %d, want 200 with an empty wall", afterRevoke.StatusCode)
	}
	var revokedPayload SnapshotPayload
	if err := json.Unmarshal([]byte(revokedBody), &revokedPayload); err != nil {
		t.Fatalf("decode post-revocation payload: %v", err)
	}
	if len(revokedPayload.Cards) != 0 {
		t.Fatalf("a revoked testimonial was still served: %#v", revokedPayload.Cards)
	}
	if cache := afterRevoke.Header.Get("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store while a snapshot is stale", cache)
	}

	exerciseStoreSurface(ctx, t, env, store, wall, item, now)

	// A suspended workspace stops serving entirely.
	if _, err := env.root.Exec(ctx, `UPDATE tenants SET status = 'suspended' WHERE id = $1`,
		env.tenantID); err != nil {
		t.Fatalf("suspend tenant: %v", err)
	}
	suspended, _ := getWall(t, server.URL, wall.PublicID, "")
	if suspended.StatusCode != http.StatusNotFound {
		t.Fatalf("suspended workspace status = %d, want 404", suspended.StatusCode)
	}
}

// exerciseStoreSurface covers the reads and writes the backoffice depends on:
// the Suggested queue, dismissal, the re-consent trigger, tagging, and the
// wall-membership cleanup that stops archived testimonials leaving ghost rows.
// exerciseVideoReviewGate proves that footage is held back until a member has
// watched it, and that the gate is the review rather than the approval: the
// testimonial here is already approved and consented, and still cannot publish.
func exerciseVideoReviewGate(
	ctx context.Context,
	t *testing.T,
	env *testimonialEnvironment,
	store Store,
	wall Wall,
	item Testimonial,
	now time.Time,
) {
	t.Helper()
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		publicID, err := GeneratePublicID(PrefixMedia)
		if err != nil {
			t.Fatalf("generate media id: %v", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO testimonial_media (
				tenant_id, testimonial_id, public_id, role, source_storage_key, content_type,
				size_bytes, width, height, safety_status, transcode_status, content_hash
			) VALUES ($1, $2, $3, 'video', 'uploads/clip.mp4', 'video/mp4', 4096, 1080, 1920,
				'safe', 'ready', repeat('a', 64))
		`, env.tenantID, item.ID, publicID); err != nil {
			t.Fatalf("insert video media: %v", err)
		}

		cards, err := store.LoadWallCards(ctx, tx, env.tenantID, wall.ID)
		if err != nil {
			t.Fatalf("load cards with video: %v", err)
		}
		if len(cards) != 1 || !cards[0].HasVideo {
			t.Fatalf("cards = %#v, want one carrying video", cards)
		}
		if cards[0].VideoReviewed {
			t.Error("video was cleared for publication without anyone watching it")
		}
		if payload := BuildSnapshotPayload(wall, cards, now); len(payload.Cards) != 0 {
			t.Errorf("unreviewed footage reached a snapshot: %#v", payload.Cards)
		}

		if err := store.ReviewMedia(ctx, tx, env.tenantID, item.ID, env.memberID, now); err != nil {
			t.Fatalf("review media: %v", err)
		}
		reviewed, err := store.LoadWallCards(ctx, tx, env.tenantID, wall.ID)
		if err != nil {
			t.Fatalf("reload cards: %v", err)
		}
		if !reviewed[0].VideoReviewed {
			t.Fatal("the review was recorded but did not reach the card")
		}
		if payload := BuildSnapshotPayload(wall, reviewed, now); len(payload.Cards) != 1 {
			t.Errorf("reviewed footage was still held back: %#v", payload)
		}

		// Reviewing a testimonial with no video is refused rather than silently
		// succeeding, so a stray call cannot look like a cleared asset.
		if _, err := tx.Exec(ctx, `DELETE FROM testimonial_media WHERE tenant_id = $1`,
			env.tenantID); err != nil {
			t.Fatalf("clear media: %v", err)
		}
		if err := store.ReviewMedia(ctx, tx, env.tenantID, item.ID, env.memberID, now); !errors.Is(err, ErrNotFound) {
			t.Errorf("review with no video = %v, want ErrNotFound", err)
		}
	})
}

// exerciseVideoUpload covers the author's side: asking for somewhere to send a
// recording, and confirming it arrived.
func exerciseVideoUpload(ctx context.Context, t *testing.T, env *testimonialEnvironment) {
	t.Helper()
	collect := env.collectServer(t)
	defer collect.Close()

	token, tokenHash, err := GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	now := time.Date(2026, 7, 26, 13, 0, 0, 0, time.UTC)
	var request Request
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		request, err = Store{}.CreateRequest(ctx, tx, env.tenantID, RequestValues{
			ProjectID: &env.projectID, Kind: RequestCollection, TokenHash: tokenHash,
			RecipientEmail: "recorder@example.test", ExpiresAt: now.Add(24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("create request for upload: %v", err)
		}
	})
	base := collect.URL + "/v1/public/testimonial-requests/" + token

	// What the browser sends is checked before anything is reserved, so a
	// mistyped type never leaves a half-made row behind.
	if got, _ := postJSON(t, base+"/video", `{"content_type":"application/zip","size_bytes":10}`); got.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("non-video type = %d, want 422", got.StatusCode)
	}
	if got, _ := postJSON(t, base+"/video", `{"content_type":"video/mp4","size_bytes":999999999}`); got.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("oversized upload = %d, want 422", got.StatusCode)
	}

	created, body := postJSON(t, base+"/video", `{"content_type":"video/mp4","size_bytes":4096}`)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("request upload = %d, want 201 (%s)", created.StatusCode, body)
	}
	var upload struct {
		MediaID       string `json:"media_id"`
		UploadURL     string `json:"upload_url"`
		CompleteToken string `json:"complete_token"`
	}
	if err := json.Unmarshal([]byte(body), &upload); err != nil {
		t.Fatalf("decode upload: %v (%s)", err, body)
	}
	if upload.MediaID == "" || upload.UploadURL == "" || upload.CompleteToken == "" {
		t.Fatalf("upload = %#v, want an id, a destination and a token", upload)
	}

	// A wrong token cannot confirm someone else's slot.
	if got, _ := postJSON(t, base+"/video/complete",
		`{"media_id":"`+upload.MediaID+`","complete_token":"wrong"}`); got.StatusCode != http.StatusNotFound {
		t.Errorf("wrong complete token = %d, want 404", got.StatusCode)
	}
	if got, completeBody := postJSON(t, base+"/video/complete",
		`{"media_id":"`+upload.MediaID+`","complete_token":"`+upload.CompleteToken+`"}`); got.StatusCode != http.StatusNoContent {
		t.Fatalf("complete upload = %d, want 204 (%s)", got.StatusCode, completeBody)
	}
	// The token is consumed, so a replay cannot resurrect a replaced file.
	if got, _ := postJSON(t, base+"/video/complete",
		`{"media_id":"`+upload.MediaID+`","complete_token":"`+upload.CompleteToken+`"}`); got.StatusCode != http.StatusNotFound {
		t.Errorf("replayed completion = %d, want 404", got.StatusCode)
	}

	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var safety string
		var completed *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT safety_status::text, completed_at FROM testimonial_media
			WHERE tenant_id = $1 AND public_id = $2
		`, env.tenantID, upload.MediaID).Scan(&safety, &completed); err != nil {
			t.Fatalf("read completed media: %v", err)
		}
		// Quarantined, never safe: nothing has looked at the file yet, and a
		// status saying otherwise is how unscanned uploads reach people.
		if safety != "quarantined" || completed == nil {
			t.Errorf("after completion safety = %q, completed = %v; want quarantined and stamped",
				safety, completed)
		}
	})

	// Recording a second take replaces the first rather than leaving a wall to
	// pick whichever the join found.
	second, secondBody := postJSON(t, base+"/video", `{"content_type":"video/webm","size_bytes":2048}`)
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("second take = %d (%s)", second.StatusCode, secondBody)
	}
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM testimonial_media WHERE tenant_id = $1 AND request_id = $2
		`, env.tenantID, request.ID).Scan(&count); err != nil {
			t.Fatalf("count media: %v", err)
		}
		if count != 1 {
			t.Errorf("media rows after a second take = %d, want 1", count)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM testimonial_media WHERE tenant_id = $1`,
			env.tenantID); err != nil {
			t.Fatalf("clear upload media: %v", err)
		}
	})
}

// exerciseMediaServing covers the endpoint the snapshot's media paths point at.
//
// The conditions are re-checked per request rather than trusted from the
// published payload, so the interesting case is the last one: an asset whose
// consent has since been withdrawn must stop resolving even though the snapshot
// that named it is unchanged.
func exerciseMediaServing(
	ctx context.Context,
	t *testing.T,
	env *testimonialEnvironment,
	store Store,
	base string,
	item Testimonial,
	now time.Time,
) {
	t.Helper()
	const hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	publicID, err := GeneratePublicID(PrefixMedia)
	if err != nil {
		t.Fatalf("generate media id: %v", err)
	}
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var mediaID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO testimonial_media (
				tenant_id, testimonial_id, public_id, role, source_storage_key, content_type,
				size_bytes, width, height, safety_status, transcode_status, content_hash,
				reviewed_at, reviewed_by
			) VALUES ($1, $2, $3, 'video', 'uploads/clip.mov', 'video/quicktime', 8192, 1080, 1920,
				'safe', 'ready', $4, $5, $6)
			RETURNING id
		`, env.tenantID, item.ID, publicID, hash, now, env.memberID).Scan(&mediaID); err != nil {
			t.Fatalf("insert servable media: %v", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO testimonial_media_renditions (
				tenant_id, media_id, kind, storage_key, content_type, size_bytes,
				width, height, content_hash
			) VALUES ($1, $2, 'mp4_720p', 'renditions/clip-720.mp4', 'video/mp4', 4096, 720, 1280, $3)
		`, env.tenantID, mediaID, hash); err != nil {
			t.Fatalf("insert rendition: %v", err)
		}
	})

	assetURL := base + MediaPath(publicID, hash, "mp4")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// This author consented to their words, name and company — never to video.
	// Everything else about the asset is in order, and that is the point: the
	// scope covering it is missing, so it does not serve.
	withoutScope, err := client.Get(assetURL)
	if err != nil {
		t.Fatalf("fetch media without video consent: %v", err)
	}
	_ = withoutScope.Body.Close()
	if withoutScope.StatusCode != http.StatusNotFound {
		t.Fatalf("media without the video scope = %d, want 404", withoutScope.StatusCode)
	}

	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `
			UPDATE testimonials SET consent_scopes = consent_scopes || '{"video":true}'::jsonb
			WHERE tenant_id = $1 AND id = $2
		`, env.tenantID, item.ID); err != nil {
			t.Fatalf("grant video scope: %v", err)
		}
	})

	response, err := client.Get(assetURL)
	if err != nil {
		t.Fatalf("fetch media: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("media status = %d, want 302", response.StatusCode)
	}
	if location := response.Header.Get("Location"); !strings.Contains(location, "renditions/clip-720.mp4") {
		t.Errorf("redirect = %q, want the transcoded rendition, never the source upload", location)
	}

	// A wrong hash is a different asset, not this one with a typo.
	wrong := base + MediaPath(publicID, strings.Repeat("c", 64), "mp4")
	if got, _ := getJSON(t, wrong); got.StatusCode != http.StatusNotFound {
		t.Errorf("mismatched content hash = %d, want 404", got.StatusCode)
	}

	// Withdrawal has to reach the footage, not only the words.
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if err := store.Revoke(ctx, tx, env.tenantID, item.ID, "", now); err != nil {
			t.Fatalf("revoke for media check: %v", err)
		}
	})
	afterRevoke, err := client.Get(assetURL)
	if err != nil {
		t.Fatalf("fetch media after revocation: %v", err)
	}
	_ = afterRevoke.Body.Close()
	if afterRevoke.StatusCode != http.StatusNotFound {
		t.Errorf("media after revocation = %d, want 404", afterRevoke.StatusCode)
	}

	// Put the world back, and prove the gate is evaluated per request rather
	// than latched: the same URL resolves again once consent does.
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `
			UPDATE testimonials SET consent_state = 'granted', revoked_at = NULL,
			       revocation_reason = NULL
			WHERE tenant_id = $1 AND id = $2
		`, env.tenantID, item.ID); err != nil {
			t.Fatalf("restore consent: %v", err)
		}
	})
	restored, err := client.Get(assetURL)
	if err != nil {
		t.Fatalf("fetch media after restoring consent: %v", err)
	}
	_ = restored.Body.Close()
	if restored.StatusCode != http.StatusFound {
		t.Errorf("media after restoring consent = %d, want 302", restored.StatusCode)
	}

	// Leave no media behind: the sections after this one assert on wall contents.
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `DELETE FROM testimonial_media WHERE tenant_id = $1`,
			env.tenantID); err != nil {
			t.Fatalf("clear served media: %v", err)
		}
	})
}

func exerciseStoreSurface(
	ctx context.Context,
	t *testing.T,
	env *testimonialEnvironment,
	store Store,
	wall Wall,
	item Testimonial,
	now time.Time,
) {
	t.Helper()

	env.withTenant(ctx, t, func(tx pgx.Tx) {
		assessment := Score(Input{
			Text:             "We cut our triage time from 3 days to 20 minutes in the first week.",
			Rating:           intPointer(5),
			FeedbackStatus:   "open",
			ModerationStatus: ModerationCompleted,
			SentimentScore:   floatPointer(0.9),
			Actionability:    "not_actionable",
			AuthorReachable:  true,
			Language:         "en",
			SupportedLocales: []string{"en", "pt"},
		})
		if assessment.Eligibility != Eligible {
			t.Fatalf("seed feedback must be an eligible candidate: %#v", assessment)
		}
		if err := store.UpsertCandidate(ctx, tx, env.tenantID, env.feedbackID, env.projectID,
			assessment, assessment.BaseScore, now); err != nil {
			t.Fatalf("upsert candidate: %v", err)
		}
		// Upserting twice must not duplicate: the scoring job runs repeatedly.
		if err := store.UpsertCandidate(ctx, tx, env.tenantID, env.feedbackID, env.projectID,
			assessment, assessment.BaseScore, now); err != nil {
			t.Fatalf("re-upsert candidate: %v", err)
		}
		candidates, err := store.ListCandidates(ctx, tx, env.tenantID, 20)
		if err != nil {
			t.Fatalf("list candidates: %v", err)
		}
		if len(candidates) != 1 {
			t.Fatalf("candidates = %d, want exactly one after a repeated upsert", len(candidates))
		}
		if len(candidates[0].Reasons) == 0 {
			t.Error("a suggested candidate must carry the reasons that explain its rank")
		}
		if err := store.DismissCandidate(ctx, tx, env.tenantID, env.feedbackID, env.memberID, now); err != nil {
			t.Fatalf("dismiss candidate: %v", err)
		}
		dismissed, err := store.ListCandidates(ctx, tx, env.tenantID, 20)
		if err != nil {
			t.Fatalf("list candidates after dismissal: %v", err)
		}
		if len(dismissed) != 0 {
			t.Fatalf("a dismissed candidate was still suggested: %#v", dismissed)
		}
		if err := store.DismissCandidate(ctx, tx, env.tenantID, env.feedbackID, env.memberID, now); err == nil {
			t.Error("dismissing twice must report that nothing changed")
		}
	})

	// Editing a quote away from what the author agreed to must suspend consent.
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		state, err := store.UpdateQuote(ctx, tx, env.tenantID, item.ID,
			"We cut our triage time from 3 days to 20 minutes in the first week.")
		if err != nil {
			t.Fatalf("update quote to the same wording: %v", err)
		}
		if state == ConsentPendingReconsent {
			t.Error("re-saving identical wording must not demand re-consent")
		}
		if _, err := store.UpdateQuote(ctx, tx, env.tenantID, item.ID, "We cut triage a lot."); err != nil {
			t.Fatalf("update quote to new wording: %v", err)
		}
		reloaded, err := store.GetTestimonial(ctx, tx, env.tenantID, item.ID)
		if err != nil {
			t.Fatalf("reload testimonial: %v", err)
		}
		if reloaded.QuoteIsConsented() {
			t.Error("an edited quote must no longer count as consented")
		}
		if _, err := store.UpdateQuote(ctx, tx, env.tenantID, "00000000-0000-0000-0000-000000000000", "x"); err == nil {
			t.Error("editing an unknown testimonial must fail")
		}
	})

	// Tags reuse the workspace catalogue that feedback and issues already share.
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var tagID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO tags (tenant_id, name, color) VALUES ($1, 'onboarding', '#087F73')
			RETURNING id
		`, env.tenantID).Scan(&tagID); err != nil {
			t.Fatalf("create tag: %v", err)
		}
		if err := store.SetTags(ctx, tx, env.tenantID, item.ID, []string{tagID}); err != nil {
			t.Fatalf("set tags: %v", err)
		}
		tagged, err := store.GetTestimonial(ctx, tx, env.tenantID, item.ID)
		if err != nil {
			t.Fatalf("reload tagged testimonial: %v", err)
		}
		if len(tagged.TagIDs) != 1 || tagged.TagIDs[0] != tagID {
			t.Fatalf("tags = %#v, want the one just applied", tagged.TagIDs)
		}
		if err := store.SetTags(ctx, tx, env.tenantID, item.ID, nil); err != nil {
			t.Fatalf("clear tags: %v", err)
		}
	})

	env.withTenant(ctx, t, func(tx pgx.Tx) {
		items, err := store.ListTestimonials(ctx, tx, env.tenantID, "", 20)
		if err != nil {
			t.Fatalf("list testimonials: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("testimonials = %d, want 1", len(items))
		}
		if items[0].Author.Name != "Ana Marques" {
			t.Errorf("author did not survive the list scan: %#v", items[0].Author)
		}
		approvedOnly, err := store.ListTestimonials(ctx, tx, env.tenantID, string(StatusApproved), 20)
		if err != nil {
			t.Fatalf("list approved testimonials: %v", err)
		}
		if len(approvedOnly) != 1 {
			t.Fatalf("approved testimonials = %d, want 1", len(approvedOnly))
		}

		walls, err := store.ListWalls(ctx, tx, env.tenantID)
		if err != nil {
			t.Fatalf("list walls: %v", err)
		}
		if len(walls) != 1 || walls[0].ItemCount != 1 {
			t.Fatalf("walls = %#v, want one wall holding one item", walls)
		}
		loaded, err := store.GetWall(ctx, tx, env.tenantID, wall.ID)
		if err != nil || loaded.PublicID != wall.PublicID {
			t.Fatalf("get wall = %#v, %v", loaded, err)
		}
		if _, err := store.GetWall(ctx, tx, env.tenantID, "00000000-0000-0000-0000-000000000000"); err == nil {
			t.Error("an unknown wall must not resolve")
		}
	})

	// Removing a testimonial must not leave a ghost row behind in the editor.
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if err := store.RemoveTestimonialFromWalls(ctx, tx, env.tenantID, item.ID); err != nil {
			t.Fatalf("remove from walls: %v", err)
		}
		walls, err := store.ListWalls(ctx, tx, env.tenantID)
		if err != nil {
			t.Fatalf("list walls after removal: %v", err)
		}
		if len(walls) != 1 || walls[0].ItemCount != 0 {
			t.Fatalf("walls = %#v, want the item gone", walls)
		}
	})
}

func intPointer(value int) *int { return &value }

func floatPointer(value float64) *float64 { return &value }

// getJSON and postJSON drive the author-facing endpoints over a real socket, so
// routing is exercised rather than assumed.
func getJSON(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	return callJSON(t, http.MethodGet, url, "")
}

func postJSON(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	return callJSON(t, http.MethodPost, url, body)
}

func callJSON(t *testing.T, method, url, body string) (*http.Response, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("call %s %s: %v", method, url, err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return response, string(payload)
}

func getWall(t *testing.T, base, publicID, etag string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, base+"/v1/public/walls/"+publicID, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("call public wall: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return response, string(body)
}

type testimonialEnvironment struct {
	pools      *db.Pools
	root       *pgx.Conn
	tenantID   string
	projectID  string
	feedbackID string
	memberID   string
}

func (e *testimonialEnvironment) withTenant(ctx context.Context, t *testing.T, fn func(pgx.Tx)) {
	t.Helper()
	tx, err := e.pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var configured string
	if err := tx.QueryRow(ctx, `SELECT set_config('app.tenant_id', $1, true)`, e.tenantID).
		Scan(&configured); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}
	fn(tx)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func (e *testimonialEnvironment) publicServer(t *testing.T) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	RegisterPublicRoutes(router, PublicHTTPOptions{
		Store:      Store{},
		Authorizer: PublicAuthorizer{Database: e.pools.Admin},
		Begin:      db.BeginFromPool(e.pools.App),
	})
	// Mounted on the same router the author's link reaches, rather than calling
	// the handler directly: routing and middleware are where the last two fatal
	// defects in this feature lived.
	RegisterRevokeRoutes(router, RevokeHTTPOptions{
		Store: Store{}, Resolver: e.pools.Admin, Begin: db.BeginFromPool(e.pools.App),
	})
	RegisterMediaRoutes(router, MediaHTTPOptions{
		Store: Store{}, Resolver: e.pools.Admin, Begin: db.BeginFromPool(e.pools.App),
		Signer: stubSigner{},
	})
	return httptest.NewServer(router)
}

// stubSigner stands in for object storage. What is under test is which requests
// reach a signature at all, not what the signature looks like.
type stubSigner struct{}

func (stubSigner) PresignInline(_ context.Context, key, contentType string, _ time.Duration) (string, error) {
	return "https://storage.test/" + key + "?type=" + contentType, nil
}

func (stubSigner) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://storage.test/put/" + key, nil
}

// collectServer mounts the author-facing endpoints on a real router, because
// routing and middleware are where this feature's fatal defects have lived.
func (e *testimonialEnvironment) collectServer(t *testing.T) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	RegisterCollectRoutes(router, CollectHTTPOptions{
		Store: Store{}, MediaStore: Store{}, Uploads: stubSigner{},
		Resolver: e.pools.Admin, Begin: db.BeginFromPool(e.pools.App),
	})
	return httptest.NewServer(router)
}

func newTestimonialEnvironment(ctx context.Context, t *testing.T) *testimonialEnvironment {
	t.Helper()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, testimonialMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}

	env := &testimonialEnvironment{root: root}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status) VALUES ('Acme', 'acme', 'scale', 'active')
		RETURNING id
	`).Scan(&env.tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2) RETURNING id
	`, env.tenantID, auth.TenantPermissionStrings()).Scan(&env.memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.com}') RETURNING id
	`, env.tenantID).Scan(&env.projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, client_feedback_id, rating, text, page_url,
			text_process_status, reporter_email, notify_reporter
		) VALUES ($1, $2, 'fb_ABCD1234', gen_random_uuid(), 5,
			'We cut our triage time from 3 days to 20 minutes in the first week.',
			'https://example.com/pricing', 'completed', 'reporter@example.test', true)
		RETURNING id
	`, env.tenantID, env.projectID).Scan(&env.feedbackID); err != nil {
		t.Fatalf("create feedback: %v", err)
	}

	pools, err := db.OpenPools(ctx, testimonialRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	env.pools = pools
	return env
}

func testimonialRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}

func testimonialMigrationsPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller for migrations path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "migrations")
}
