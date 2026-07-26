package testimonial

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// TestTestimonialAPI drives the backoffice API the way the UI will: list the
// Suggested queue, send a collection request, approve, build a wall and publish
// it.
//
// The permission assertions are the point of the second half. Publishing is a
// separate grant from editing because it is the step that puts a named person's
// words on the public internet, and a test is the only thing that keeps those
// two from quietly collapsing into one.
func TestTestimonialAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := newTestimonialEnvironment(ctx, t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	jwtManager, err := auth.NewJWTManager(strings.Repeat("t", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	fullToken, _, _ := jwtManager.IssueTenant(env.memberID, env.tenantID, auth.TenantPermissionStrings())

	// A member who may edit everything but may not publish.
	var editorID string
	if err := env.root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'editor@acme.test', ARRAY['testimonial:read','testimonial:create','testimonial:update'])
		RETURNING id
	`, env.tenantID).Scan(&editorID); err != nil {
		t.Fatalf("create editor: %v", err)
	}
	editorToken, _, _ := jwtManager.IssueTenant(editorID, env.tenantID, []string{
		string(auth.PermissionTestimonialRead),
		string(auth.PermissionTestimonialCreate),
		string(auth.PermissionTestimonialUpdate),
	})

	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Store: Store{}, JWT: jwtManager,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:    func() time.Time { return now },
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: env.pools.Admin},
		Begin: db.BeginFromPool(env.pools.App), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

	store := Store{}
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
			SupportedLocales: []string{"en"},
		})
		if err := store.UpsertCandidate(ctx, tx, env.tenantID, env.feedbackID, env.projectID,
			assessment, assessment.BaseScore, now); err != nil {
			t.Fatalf("seed candidate: %v", err)
		}
	})

	// The Suggested queue, with the reasons that explain each rank.
	listed := apiCall(handler, http.MethodGet, "/v1/testimonial-candidates", "", fullToken)
	if listed.Code != http.StatusOK {
		t.Fatalf("list candidates status = %d, body = %s", listed.Code, listed.Body.String())
	}
	var candidates struct {
		Candidates []Candidate `json:"candidates"`
	}
	decodeAPI(t, listed, &candidates)
	if len(candidates.Candidates) != 1 {
		t.Fatalf("candidates = %#v, want one", candidates.Candidates)
	}
	if len(candidates.Candidates[0].Reasons) == 0 {
		t.Error("a suggested candidate must explain itself")
	}

	// Requesting a testimonial. The address comes from the feedback record, which
	// is the only address anyone here can be said to hold: the customer never saw
	// it, and inviting them to type one would be inviting them to invent one.
	// Naming a recipient is refused rather than ignored, so a caller expecting to
	// choose one is told, not quietly overruled.
	chosenRecipient := apiCall(handler, http.MethodPost,
		"/v1/testimonial-candidates/"+env.feedbackID+"/request",
		`{"recipient_email":"attacker@example.test"}`, fullToken)
	if chosenRecipient.Code != http.StatusBadRequest {
		t.Fatalf("caller-chosen recipient status = %d, want 400 (%s)",
			chosenRecipient.Code, chosenRecipient.Body.String())
	}
	requested := apiCall(handler, http.MethodPost,
		"/v1/testimonial-candidates/"+env.feedbackID+"/request",
		`{"prompt":"Would you be happy for us to publish this?"}`, fullToken)
	if requested.Code != http.StatusCreated {
		t.Fatalf("request status = %d, body = %s", requested.Code, requested.Body.String())
	}
	var created struct {
		Token   string  `json:"token"`
		Request Request `json:"request"`
	}
	decodeAPI(t, requested, &created)
	if created.Token == "" {
		t.Fatal("the plaintext token must be returned once, for the message the customer sends")
	}
	// An address in the body is ignored rather than honoured: the endpoint is not
	// a way to send a workspace-branded invitation to an arbitrary inbox.
	if created.Request.RecipientEmail != "reporter@example.test" {
		t.Errorf("recipient = %q, want the address on the feedback record",
			created.Request.RecipientEmail)
	}

	// A feedback with no reporter address, or whose reporter did not agree to be
	// contacted, cannot be asked at all.
	var anonymousID string
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if err := tx.QueryRow(ctx, `
			INSERT INTO feedbacks (
				tenant_id, project_id, public_id, client_feedback_id, rating, text, page_url,
				text_process_status
			) VALUES ($1, $2, 'fb_ANON0001', gen_random_uuid(), 5, 'Anonymous but glowing.',
				'https://example.com/', 'completed')
			RETURNING id
		`, env.tenantID, env.projectID).Scan(&anonymousID); err != nil {
			t.Fatalf("create anonymous feedback: %v", err)
		}
	})
	anonymous := apiCall(handler, http.MethodPost,
		"/v1/testimonial-candidates/"+anonymousID+"/request", `{}`, fullToken)
	if anonymous.Code != http.StatusUnprocessableEntity {
		t.Fatalf("anonymous request status = %d, want 422 (%s)", anonymous.Code, anonymous.Body.String())
	}

	// Inviting someone who never left feedback is a different act with a
	// different justification: there is no record to draw an address from, so the
	// customer supplies one out of a relationship only they have.
	invited := apiCall(handler, http.MethodPost, "/v1/testimonial-requests",
		`{"recipient_email":"customer@example.test","prompt":"Would you write a line for us?"}`,
		fullToken)
	if invited.Code != http.StatusCreated {
		t.Fatalf("invite status = %d, body = %s", invited.Code, invited.Body.String())
	}
	var invite struct {
		Token   string  `json:"token"`
		Request Request `json:"request"`
	}
	decodeAPI(t, invited, &invite)
	if invite.Token == "" || invite.Request.FeedbackID != nil {
		t.Fatalf("invite = %#v, want a token and no feedback behind it", invite.Request)
	}
	if invite.Request.RecipientEmail != "customer@example.test" {
		t.Errorf("invite recipient = %q, want the supplied address", invite.Request.RecipientEmail)
	}
	badInvite := apiCall(handler, http.MethodPost, "/v1/testimonial-requests",
		`{"recipient_email":"not-an-address"}`, fullToken)
	if badInvite.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid invite status = %d, want 422", badInvite.Code)
	}
	// What we stored must be the digest, never the token itself.
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var stored string
		if err := tx.QueryRow(ctx, `SELECT token_hash FROM testimonial_requests WHERE id = $1`,
			created.Request.ID).Scan(&stored); err != nil {
			t.Fatalf("read stored token: %v", err)
		}
		if stored == created.Token {
			t.Fatal("the plaintext token was stored")
		}
		if stored != HashToken(created.Token) {
			t.Fatal("the stored digest does not match the issued token")
		}
	})

	// The author's side, over the real public endpoints rather than the store.
	collectRoutes := chi.NewRouter()
	RegisterCollectRoutes(collectRoutes, CollectHTTPOptions{
		Store: Store{}, Resolver: env.pools.Admin, Begin: db.BeginFromPool(env.pools.App),
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:    func() time.Time { return now },
	})
	collectServer := httptest.NewServer(collectRoutes)
	defer collectServer.Close()
	collectPath := "/v1/public/testimonial-requests/" + created.Token

	// An unknown token is indistinguishable from an expired one: an anonymous
	// caller is not owed the difference.
	unknown := publicCall(t, collectServer.URL, http.MethodGet,
		"/v1/public/testimonial-requests/not-a-real-token", "")
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown token status = %d, want 404", unknown.StatusCode)
	}

	loaded := publicCall(t, collectServer.URL, http.MethodGet, collectPath, "")
	if loaded.StatusCode != http.StatusOK {
		t.Fatalf("load collect page status = %d", loaded.StatusCode)
	}
	if cache := loaded.Header.Get("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store on an author's draft", cache)
	}
	if robots := loaded.Header.Get("X-Robots-Tag"); !strings.Contains(robots, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want the collection page kept out of indexes", robots)
	}

	// Publishing without permission for the words is not a testimonial.
	noQuoteConsent := publicCall(t, collectServer.URL, http.MethodPost, collectPath,
		`{"quote":"Anything at all here","consent":{"quote":false,"identity":true}}`)
	if noQuoteConsent.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("submission without quote consent = %d, want 422", noQuoteConsent.StatusCode)
	}
	// Publishing an identity with no name to publish is incoherent.
	namelessIdentity := publicCall(t, collectServer.URL, http.MethodPost, collectPath,
		`{"quote":"Anything at all here","consent":{"quote":true,"identity":true}}`)
	if namelessIdentity.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("identity consent without a name = %d, want 422", namelessIdentity.StatusCode)
	}

	submitted := publicCall(t, collectServer.URL, http.MethodPost, collectPath,
		`{"quote":"We cut our triage time from 3 days to 20 minutes in the first week.",`+
			`"author_name":"Ana Marques","author_role":"Head of Support",`+
			`"consent":{"quote":true,"identity":true},"notice_version":"v1","locale":"pt-PT"}`)
	if submitted.StatusCode != http.StatusCreated {
		t.Fatalf("submit status = %d", submitted.StatusCode)
	}

	// A second submission on the same token must not create a second testimonial.
	replay := publicCall(t, collectServer.URL, http.MethodPost, collectPath,
		`{"quote":"A different quote entirely","consent":{"quote":true}}`)
	if replay.StatusCode == http.StatusCreated {
		t.Fatal("a spent collection token created a second testimonial")
	}
	// Revisiting the link still works, so the author sees their confirmation.
	revisited := publicCall(t, collectServer.URL, http.MethodGet, collectPath, "")
	if revisited.StatusCode != http.StatusOK {
		t.Fatalf("revisit status = %d, want the confirmation to remain readable", revisited.StatusCode)
	}

	var item Testimonial
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		items, err := store.ListTestimonials(ctx, tx, env.tenantID, "", 10)
		if err != nil {
			t.Fatalf("list after submission: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("testimonials = %d, want exactly one after a replayed submit", len(items))
		}
		item = items[0]
	})
	if item.ConsentState != ConsentGranted || !item.Scopes.Identity {
		t.Fatalf("collected testimonial = %#v, want granted consent including identity", item)
	}

	if got := apiCall(handler, http.MethodPost, "/v1/testimonials/"+item.ID+"/approve", "", fullToken); got.Code != http.StatusNoContent {
		t.Fatalf("approve status = %d, body = %s", got.Code, got.Body.String())
	}

	// Editing the quote must suspend consent and say so, rather than letting the
	// customer discover it when publishing fails.
	edited := apiCall(handler, http.MethodPatch, "/v1/testimonials/"+item.ID,
		`{"quote":"We cut triage a lot."}`, fullToken)
	if edited.Code != http.StatusOK {
		t.Fatalf("edit status = %d, body = %s", edited.Code, edited.Body.String())
	}
	var editResult struct {
		ConsentState      ConsentState `json:"consent_state"`
		RequiresReconsent bool         `json:"requires_reconsent"`
	}
	decodeAPI(t, edited, &editResult)
	if !editResult.RequiresReconsent || editResult.ConsentState != ConsentPendingReconsent {
		t.Fatalf("edit result = %#v, want a re-consent requirement", editResult)
	}

	// Put the consented wording back so the wall can be published.
	if got := apiCall(handler, http.MethodPatch, "/v1/testimonials/"+item.ID,
		`{"quote":"We cut our triage time from 3 days to 20 minutes in the first week."}`,
		fullToken); got.Code != http.StatusOK {
		t.Fatalf("restore quote status = %d", got.Code)
	}
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `
			UPDATE testimonials SET consent_state = 'granted' WHERE tenant_id = $1 AND id = $2
		`, env.tenantID, item.ID); err != nil {
			t.Fatalf("restore consent: %v", err)
		}
	})

	// Wall creation validates before the database has to.
	if got := apiCall(handler, http.MethodPost, "/v1/testimonial-walls",
		`{"name":"Homepage","slug":"Not A Slug"}`, fullToken); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid slug status = %d, want 422", got.Code)
	}
	wallResponse := apiCall(handler, http.MethodPost, "/v1/testimonial-walls",
		`{"name":"Homepage","slug":"homepage","layout":"grid"}`, fullToken)
	if wallResponse.Code != http.StatusCreated {
		t.Fatalf("create wall status = %d, body = %s", wallResponse.Code, wallResponse.Body.String())
	}
	var wallResult struct {
		Wall Wall `json:"wall"`
	}
	decodeAPI(t, wallResponse, &wallResult)
	if wallResult.Wall.Status != WallDraft {
		t.Fatalf("a new wall must start as a draft, got %q", wallResult.Wall.Status)
	}

	itemsBody := `{"items":[{"testimonial_id":"` + item.ID + `","position":0,"pinned":false}]}`
	if got := apiCall(handler, http.MethodPut,
		"/v1/testimonial-walls/"+wallResult.Wall.ID+"/items", itemsBody, fullToken); got.Code != http.StatusNoContent {
		t.Fatalf("set items status = %d, body = %s", got.Code, got.Body.String())
	}
	duplicate := `{"items":[{"testimonial_id":"` + item.ID + `"},{"testimonial_id":"` + item.ID + `"}]}`
	if got := apiCall(handler, http.MethodPut,
		"/v1/testimonial-walls/"+wallResult.Wall.ID+"/items", duplicate, fullToken); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate item status = %d, want 422", got.Code)
	}

	// Editing is not publishing.
	forbidden := apiCall(handler, http.MethodPost,
		"/v1/testimonial-walls/"+wallResult.Wall.ID+"/publish", "", editorToken)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("publish without testimonial:publish = %d, want 403", forbidden.Code)
	}
	// ...but that member can still stage the wall.
	if got := apiCall(handler, http.MethodPut,
		"/v1/testimonial-walls/"+wallResult.Wall.ID+"/items", itemsBody, editorToken); got.Code != http.StatusNoContent {
		t.Fatalf("editor could not stage a wall: %d", got.Code)
	}

	published := apiCall(handler, http.MethodPost,
		"/v1/testimonial-walls/"+wallResult.Wall.ID+"/publish", "", fullToken)
	if published.Code != http.StatusOK {
		t.Fatalf("publish status = %d, body = %s", published.Code, published.Body.String())
	}
	var publishResult struct {
		Version   int    `json:"version"`
		Published int    `json:"published"`
		Skipped   int    `json:"skipped"`
		PublicID  string `json:"public_id"`
		ETag      string `json:"etag"`
	}
	decodeAPI(t, published, &publishResult)
	if publishResult.Published != 1 || publishResult.Version != 1 || publishResult.ETag == "" {
		t.Fatalf("publish result = %#v", publishResult)
	}

	// The wall detail view feeds the editor.
	detail := apiCall(handler, http.MethodGet, "/v1/testimonial-walls/"+wallResult.Wall.ID, "", fullToken)
	if detail.Code != http.StatusOK {
		t.Fatalf("wall detail status = %d", detail.Code)
	}

	if got := apiCall(handler, http.MethodGet, "/v1/testimonials?status=nonsense", "", fullToken); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status filter = %d, want 422", got.Code)
	}
	if got := apiCall(handler, http.MethodGet, "/v1/testimonials/not-a-uuid", "", fullToken); got.Code != http.StatusNotFound {
		t.Fatalf("malformed identifier = %d, want 404", got.Code)
	}
	if got := apiCall(handler, http.MethodGet, "/v1/testimonials", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", got.Code)
	}

	dismissed := apiCall(handler, http.MethodPost,
		"/v1/testimonial-candidates/"+env.feedbackID+"/dismiss", "", fullToken)
	if dismissed.Code != http.StatusNoContent {
		t.Fatalf("dismiss status = %d, body = %s", dismissed.Code, dismissed.Body.String())
	}

	// Tagging reuses the workspace catalogue that feedback and issues share.
	var tagID string
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if err := tx.QueryRow(ctx, `
			INSERT INTO tags (tenant_id, name, color) VALUES ($1, 'launch', '#2B63C6') RETURNING id
		`, env.tenantID).Scan(&tagID); err != nil {
			t.Fatalf("create tag: %v", err)
		}
	})
	if got := apiCall(handler, http.MethodPut, "/v1/testimonials/"+item.ID+"/tags",
		`{"tag_ids":["`+tagID+`"]}`, fullToken); got.Code != http.StatusNoContent {
		t.Fatalf("set tags status = %d, body = %s", got.Code, got.Body.String())
	}
	if got := apiCall(handler, http.MethodPut, "/v1/testimonials/"+item.ID+"/tags",
		`{"tag_ids":["not-a-uuid"]}`, fullToken); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("malformed tag id = %d, want 422", got.Code)
	}
	tagged := apiCall(handler, http.MethodGet, "/v1/testimonials/"+item.ID, "", fullToken)
	if tagged.Code != http.StatusOK {
		t.Fatalf("get testimonial status = %d", tagged.Code)
	}
	var taggedDetail struct {
		Testimonial      Testimonial `json:"testimonial"`
		QuoteIsConsented bool        `json:"quote_is_consented"`
	}
	decodeAPI(t, tagged, &taggedDetail)
	if len(taggedDetail.Testimonial.TagIDs) != 1 || !taggedDetail.QuoteIsConsented {
		t.Fatalf("testimonial detail = %#v (consented=%t)", taggedDetail.Testimonial, taggedDetail.QuoteIsConsented)
	}

	walls := apiCall(handler, http.MethodGet, "/v1/testimonial-walls", "", fullToken)
	if walls.Code != http.StatusOK {
		t.Fatalf("list walls status = %d", walls.Code)
	}
	var wallList struct {
		Walls []TestimonialWallView `json:"walls"`
	}
	decodeAPI(t, walls, &wallList)
	if len(wallList.Walls) != 1 || wallList.Walls[0].Status != string(WallPublished) {
		t.Fatalf("walls = %#v, want one published wall", wallList.Walls)
	}

	// A malformed body must be a 400, not a 500 from a nil dereference deeper in.
	if got := apiCall(handler, http.MethodPatch, "/v1/testimonials/"+item.ID,
		`{"quote":`, fullToken); got.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON = %d, want 400", got.Code)
	}
	// An unknown field is refused rather than silently ignored, so a client
	// typo never looks like a successful save.
	if got := apiCall(handler, http.MethodPatch, "/v1/testimonials/"+item.ID,
		`{"quotes":"typo"}`, fullToken); got.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, want 400", got.Code)
	}
	if got := apiCall(handler, http.MethodPatch, "/v1/testimonials/"+item.ID,
		`{"quote":"   "}`, fullToken); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank quote = %d, want 422", got.Code)
	}
	// Approving something already approved is a conflict, not a silent success.
	if got := apiCall(handler, http.MethodPost, "/v1/testimonials/"+item.ID+"/approve",
		"", fullToken); got.Code != http.StatusConflict {
		t.Fatalf("re-approve = %d, want 409", got.Code)
	}
	if got := apiCall(handler, http.MethodGet,
		"/v1/testimonial-walls/00000000-0000-0000-0000-000000000000", "", fullToken); got.Code != http.StatusNotFound {
		t.Fatalf("unknown wall = %d, want 404", got.Code)
	}
}

// TestimonialWallView mirrors the wall JSON the backoffice consumes, so a field
// rename in the API surfaces here rather than in the browser.
type TestimonialWallView struct {
	ID        string `json:"id"`
	PublicID  string `json:"public_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	ItemCount int    `json:"item_count"`
}

// publicCall drives an unauthenticated endpoint over a real socket, so the
// headers a browser would see are the ones under test.
func publicCall(t *testing.T, base, method, path, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatalf("build public request: %v", err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("call public endpoint: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func apiCall(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, "http://acme.localhost"+path, reader)
	request.Host = "acme.localhost"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeAPI(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v (%s)", err, response.Body.String())
	}
}
