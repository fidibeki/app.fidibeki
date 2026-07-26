package testimonial

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
	// requestTTL is how long a collection link stays usable. Long enough that a
	// customer's request does not expire over a holiday, short enough that a
	// forwarded link does not stay live indefinitely.
	requestTTL = 30 * 24 * time.Hour
)

// HTTPOptions wires the backoffice routes.
type HTTPOptions struct {
	Store  Store
	JWT    *auth.JWTManager
	Logger *slog.Logger
	Now    func() time.Time
}

// HTTPHandler serves the tenant-facing testimonial API.
type HTTPHandler struct {
	options HTTPOptions
}

// RegisterRoutes mounts the backoffice API.
//
// The permission split is the point: read and write are ordinary, but publishing
// a wall requires testimonial:publish, because it is the action that puts a
// named person's words on the public internet.
func RegisterRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	read := append(protected, appmiddleware.RequirePermission(auth.PermissionTestimonialRead))
	create := append(protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionTestimonialCreate))
	update := append(protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionTestimonialUpdate))
	publish := append(protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionTestimonialPublish))

	router.With(read...).Get("/v1/testimonial-candidates", handler.listCandidates)
	router.With(create...).Post("/v1/testimonial-candidates/{feedbackID}/dismiss", handler.dismissCandidate)
	router.With(create...).Post("/v1/testimonial-candidates/{feedbackID}/request", handler.requestTestimonial)
	router.With(create...).Post("/v1/testimonial-requests", handler.inviteTestimonial)

	router.With(read...).Get("/v1/testimonials", handler.listTestimonials)
	router.With(read...).Get("/v1/testimonials/{testimonialID}", handler.getTestimonial)
	router.With(update...).Post("/v1/testimonials/{testimonialID}/approve", handler.approve)
	router.With(update...).Patch("/v1/testimonials/{testimonialID}", handler.updateTestimonial)
	router.With(update...).Put("/v1/testimonials/{testimonialID}/tags", handler.setTags)

	router.With(read...).Get("/v1/testimonial-walls", handler.listWalls)
	router.With(create...).Post("/v1/testimonial-walls", handler.createWall)
	router.With(read...).Get("/v1/testimonial-walls/{wallID}", handler.getWall)
	router.With(update...).Put("/v1/testimonial-walls/{wallID}/items", handler.setWallItems)
	router.With(publish...).Post("/v1/testimonial-walls/{wallID}/publish", handler.publishWall)
}

func (h *HTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now()
	}
	return time.Now().UTC()
}

func (h *HTTPHandler) listCandidates(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	candidates, err := h.options.Store.ListCandidates(r.Context(), query, tenantID, listLimit(r))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
}

func (h *HTTPHandler) dismissCandidate(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	feedbackID, ok := uuidParam(w, r, "feedbackID")
	if !ok {
		return
	}
	actor := memberID(r)
	if err := h.options.Store.DismissCandidate(r.Context(), query, tenantID, feedbackID,
		actor, h.now()); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requestTestimonial creates the collection invitation.
//
// The recipient address is supplied by the caller rather than read from the
// feedback record. That is deliberate: the reporter email on a feedback was
// collected under the feedback_updates consent purpose, and soliciting a
// testimonial is a different purpose. Until a testimonial_requests purpose
// exists, the customer confirms the address they already have a relationship
// with, and we do not silently repurpose the one we hold.
// requestTestimonial asks the person behind a piece of feedback for a
// testimonial about it.
//
// The address is read from the feedback record and never taken from the body.
// The customer never saw the reporter's address, so asking them to supply one
// would either invite them to invent it or turn this endpoint into a way to
// send a workspace-branded invitation to any inbox at all.
func (h *HTTPHandler) requestTestimonial(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Prompt *string `json:"prompt"`
		Locale *string `json:"locale"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	feedbackID, ok := uuidParam(w, r, "feedbackID")
	if !ok {
		return
	}
	email, err := h.options.Store.FeedbackRecipient(r.Context(), query, tenantID, feedbackID)
	if errors.Is(err, ErrNotContactable) {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"this feedback has no contactable author", map[string]string{
				"recipient_email": "the reporter left no address, or did not agree to be contacted",
			})
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	token, tokenHash, err := GenerateToken()
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	actor := memberID(r)
	now := h.now()
	created, err := h.options.Store.CreateRequest(r.Context(), query, tenantID, RequestValues{
		FeedbackID:     &feedbackID,
		Kind:           RequestCollection,
		TokenHash:      tokenHash,
		RecipientEmail: email,
		Prompt:         request.Prompt,
		Locale:         request.Locale,
		ExpiresAt:      now.Add(requestTTL),
		SentAt:         &now,
		CreatedBy:      &actor,
	})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	// The plaintext token is returned exactly once, for the message the customer
	// sends. It is never stored and cannot be recovered.
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"request": created,
		"token":   token,
	})
}

// inviteTestimonial asks someone who never left feedback.
//
// Here the caller does supply the address, and that is the honest arrangement:
// there is no record to draw one from, and the relationship justifying the ask
// is the customer's own. The person still writes their own words and gives
// their own permission before anything can be published.
func (h *HTTPHandler) inviteTestimonial(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RecipientEmail string  `json:"recipient_email"`
		ProjectID      *string `json:"project_id"`
		Prompt         *string `json:"prompt"`
		Locale         *string `json:"locale"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	email := strings.TrimSpace(request.RecipientEmail)
	if !validEmail(email) {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"a recipient email is required", map[string]string{
				"recipient_email": "must be an email address",
			})
		return
	}
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	if request.ProjectID != nil {
		if _, err := uuid.Parse(*request.ProjectID); err != nil {
			api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
				"project is invalid", map[string]string{"project_id": "must be a project identifier"})
			return
		}
	}
	token, tokenHash, err := GenerateToken()
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	actor := memberID(r)
	now := h.now()
	created, err := h.options.Store.CreateRequest(r.Context(), query, tenantID, RequestValues{
		ProjectID:      request.ProjectID,
		Kind:           RequestCollection,
		TokenHash:      tokenHash,
		RecipientEmail: email,
		Prompt:         request.Prompt,
		Locale:         request.Locale,
		ExpiresAt:      now.Add(requestTTL),
		SentAt:         &now,
		CreatedBy:      &actor,
	})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"request": created, "token": token})
}

// validEmail is a shape check, not a deliverability check: the address is only
// ever shown back to the customer who typed it.
func validEmail(value string) bool {
	at := strings.Index(value, "@")
	return at > 0 && at < len(value)-1 && len(value) <= 320 && !strings.ContainsAny(value, " \t\n")
}

func (h *HTTPHandler) listTestimonials(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch Status(status) {
	case "", StatusDraft, StatusPendingReview, StatusApproved, StatusArchived:
	default:
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"status filter is invalid", map[string]string{"status": "is not a testimonial status"})
		return
	}
	items, err := h.options.Store.ListTestimonials(r.Context(), query, tenantID, status, listLimit(r))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"testimonials": items})
}

func (h *HTTPHandler) getTestimonial(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "testimonialID")
	if !ok {
		return
	}
	item, err := h.options.Store.GetTestimonial(r.Context(), query, tenantID, id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"testimonial":        item,
		"quote_is_consented": item.QuoteIsConsented(),
	})
}

func (h *HTTPHandler) approve(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "testimonialID")
	if !ok {
		return
	}
	actor := memberID(r)
	if err := h.options.Store.ApproveTestimonial(r.Context(), query, tenantID, id,
		actor, h.now()); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateTestimonial applies a customer edit to the displayed quote.
//
// The response reports the resulting consent state so the UI can say plainly
// that the author has to re-approve, rather than leaving the customer to
// discover it when the wall refuses to publish.
func (h *HTTPHandler) updateTestimonial(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Quote string `json:"quote"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	quote := strings.TrimSpace(request.Quote)
	if quote == "" || len([]rune(quote)) > 2000 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"quote is invalid", map[string]string{"quote": "must be between 1 and 2000 characters"})
		return
	}
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "testimonialID")
	if !ok {
		return
	}
	state, err := h.options.Store.UpdateQuote(r.Context(), query, tenantID, id, quote)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"consent_state":      state,
		"requires_reconsent": state == ConsentPendingReconsent,
	})
}

func (h *HTTPHandler) setTags(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TagIDs []string `json:"tag_ids"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	for _, tagID := range request.TagIDs {
		if _, err := uuid.Parse(tagID); err != nil {
			api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
				"tag id is invalid", map[string]string{"tag_ids": "must be identifiers"})
			return
		}
	}
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "testimonialID")
	if !ok {
		return
	}
	if err := h.options.Store.SetTags(r.Context(), query, tenantID, id, request.TagIDs); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) listWalls(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	walls, err := h.options.Store.ListWalls(r.Context(), query, tenantID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"walls": walls})
}

func (h *HTTPHandler) createWall(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name           string     `json:"name"`
		Slug           string     `json:"slug"`
		Layout         WallLayout `json:"layout"`
		Theme          Theme      `json:"theme"`
		MaxItems       int        `json:"max_items"`
		ShowRating     *bool      `json:"show_rating"`
		ShowAvatar     *bool      `json:"show_avatar"`
		ShowCompany    *bool      `json:"show_company"`
		ShowVideo      *bool      `json:"show_video"`
		AllowedOrigins []string   `json:"allowed_origins"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	fields := map[string]string{}
	name := strings.TrimSpace(request.Name)
	if name == "" || len([]rune(name)) > 120 {
		fields["name"] = "must be between 1 and 120 characters"
	}
	slug := strings.TrimSpace(strings.ToLower(request.Slug))
	if !slugPattern.MatchString(slug) {
		fields["slug"] = "must be lowercase words separated by hyphens"
	}
	if request.Layout == "" {
		request.Layout = LayoutGrid
	}
	if !request.Layout.Valid() {
		fields["layout"] = "is not a supported layout"
	}
	if request.MaxItems == 0 {
		request.MaxItems = 24
	}
	if request.MaxItems < 1 || request.MaxItems > 200 {
		fields["max_items"] = "must be between 1 and 200"
	}
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "wall is invalid", fields)
		return
	}

	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	publicID, err := GeneratePublicID(PrefixWall)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	actor := memberID(r)
	wall, err := h.options.Store.CreateWall(r.Context(), query, tenantID, publicID, WallValues{
		Name: name, Slug: slug, Layout: request.Layout, Theme: request.Theme,
		MaxItems:       request.MaxItems,
		ShowRating:     boolOrDefault(request.ShowRating, true),
		ShowAvatar:     boolOrDefault(request.ShowAvatar, true),
		ShowCompany:    boolOrDefault(request.ShowCompany, true),
		ShowVideo:      boolOrDefault(request.ShowVideo, true),
		AllowedOrigins: request.AllowedOrigins,
		CreatedBy:      &actor,
	})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"wall": wall})
}

func (h *HTTPHandler) getWall(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "wallID")
	if !ok {
		return
	}
	wall, err := h.options.Store.GetWall(r.Context(), query, tenantID, id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	cards, err := h.options.Store.LoadWallCards(r.Context(), query, tenantID, id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"wall": wall, "cards": cards})
}

func (h *HTTPHandler) setWallItems(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Items []WallItem `json:"items"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	seen := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		if _, err := uuid.Parse(item.TestimonialID); err != nil {
			api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
				"wall items are invalid", map[string]string{"items": "must be testimonial identifiers"})
			return
		}
		if _, duplicate := seen[item.TestimonialID]; duplicate {
			api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
				"wall items are invalid", map[string]string{"items": "must not repeat a testimonial"})
			return
		}
		seen[item.TestimonialID] = struct{}{}
	}
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "wallID")
	if !ok {
		return
	}
	if err := h.options.Store.SetWallItems(r.Context(), query, tenantID, id, request.Items); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// publishWall builds and stores the snapshot the public endpoint serves.
//
// Cards that fail the publishability predicate are dropped rather than
// published half-rendered, and the response reports how many were dropped so the
// customer is told rather than left to notice.
func (h *HTTPHandler) publishWall(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := requestState(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "wallID")
	if !ok {
		return
	}
	wall, err := h.options.Store.GetWall(r.Context(), query, tenantID, id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	cards, err := h.options.Store.LoadWallCards(r.Context(), query, tenantID, id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	payload := BuildSnapshotPayload(wall, cards, h.now())
	if len(payload.Cards) == 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"a wall needs at least one publishable testimonial", map[string]string{
				"items": "none of the selected testimonials can be published yet",
			})
		return
	}
	actor := memberID(r)
	snapshot, err := h.options.Store.PublishSnapshot(r.Context(), query, tenantID, id, payload,
		&actor, h.now())
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"version":      snapshot.Version,
		"etag":         snapshot.ETag,
		"published":    len(payload.Cards),
		"skipped":      len(cards) - len(payload.Cards),
		"public_id":    wall.PublicID,
		"published_at": snapshot.PublishedAt,
	})
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func listLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultListLimit
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		return defaultListLimit
	}
	return min(parsed, maxListLimit)
}

func requestState(w http.ResponseWriter, r *http.Request) (string, db.Querier, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal,
			"internal server error", nil)
		return "", nil, false
	}
	return resolved.ID, query, true
}

// memberID is the acting member, taken from the verified token subject rather
// than from anything the caller can set.
func memberID(r *http.Request) string {
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		return ""
	}
	return claims.Subject
}

// slugPattern matches the CHECK constraint on testimonial_walls.slug, so an
// invalid slug is a 422 with a field message rather than a 500 from the database.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func uuidParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := chi.URLParam(r, name)
	if _, err := uuid.Parse(value); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource was not found", nil)
		return "", false
	}
	return value, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeValidation, "request body is invalid", nil)
		return false
	}
	return true
}

func (h *HTTPHandler) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource was not found", nil)
	case errors.Is(err, ErrNotPublishable):
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"this testimonial cannot be published in its current state", nil)
	default:
		if h.options.Logger != nil {
			h.options.Logger.ErrorContext(r.Context(), "testimonial request failed",
				slog.String("error", err.Error()))
		}
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal,
			"the request could not be completed", nil)
	}
}
