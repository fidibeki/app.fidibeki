package testimonial

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
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
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		publicID, err := GeneratePublicID(PrefixTestimonial)
		if err != nil {
			t.Fatalf("generate public id: %v", err)
		}
		_, revocationHash, err := GenerateToken()
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

	// Revocation must take effect on the very next read, without any rebuild.
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if err := store.Revoke(ctx, tx, env.tenantID, item.ID, "author changed their mind", now); err != nil {
			t.Fatalf("revoke: %v", err)
		}
	})
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
