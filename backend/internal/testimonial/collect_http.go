package testimonial

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
)

// CollectStore is the write surface the author-facing form needs.
type CollectStore interface {
	ResolveRequestTenant(context.Context, db.Querier, string) (string, error)
	LoadRequestByToken(context.Context, db.Querier, string, string) (Request, error)
	MarkRequestOpened(context.Context, db.Querier, string, string, time.Time) error
	SubmitCollection(context.Context, db.Querier, string, string, string, SubmissionValues, time.Time) (Testimonial, error)
}

// CollectHTTPOptions wires the public collection form.
type CollectHTTPOptions struct {
	Store CollectStore
	// MediaStore and Uploads are only needed where video is offered. Left nil the
	// video endpoints are not mounted at all, rather than mounted and failing.
	MediaStore CollectMediaStore
	Uploads    UploadSigner
	Transcodes TranscodeEnqueuer
	// Resolver runs on the admin pool: the token has to be traced to a workspace
	// before a tenant context can exist.
	Resolver db.Querier
	Begin    db.BeginFunc
	Logger   *slog.Logger
	Now      func() time.Time
}

// CollectHTTPHandler serves the page an author lands on from their email.
type CollectHTTPHandler struct {
	options CollectHTTPOptions
}

// RegisterCollectRoutes mounts the author-facing collection endpoints.
//
// These are unauthenticated by necessity: the person answering is not a member
// and never will be. The token is the only credential, so it is compared by
// digest and every failure answers identically.
func RegisterCollectRoutes(router chi.Router, options CollectHTTPOptions) {
	handler := &CollectHTTPHandler{options: options}
	router.Get("/v1/public/testimonial-requests/{token}", handler.load)
	router.Post("/v1/public/testimonial-requests/{token}", handler.submit)
	if options.MediaStore != nil && options.Uploads != nil {
		router.Post("/v1/public/testimonial-requests/{token}/video", handler.requestVideo)
		router.Post("/v1/public/testimonial-requests/{token}/video/complete", handler.completeVideo)
	}
}

func (h *CollectHTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now()
	}
	return time.Now().UTC()
}

// CollectView is what the author's page renders. It deliberately carries no
// workspace internals: no identifiers, no member names, no feedback metadata
// beyond the words the author themselves wrote.
type CollectView struct {
	Kind          RequestKind `json:"kind"`
	Prompt        *string     `json:"prompt"`
	Locale        *string     `json:"locale"`
	OriginalText  string      `json:"original_text"`
	ProposedQuote *string     `json:"proposed_quote"`
	// Rating is the author's own stars, echoed back because they are published
	// with the quote. Asking for permission to publish something without showing
	// it is not asking.
	Rating *int `json:"rating"`
	// Whether this workspace can accept a recording at all. The page asks rather
	// than assuming, so a plan without video never shows a field that would be
	// refused after the author had already chosen a file.
	VideoAllowed bool `json:"video_allowed"`
	Submitted    bool `json:"submitted"`
}

func (h *CollectHTTPHandler) load(w http.ResponseWriter, r *http.Request) {
	tenantID, request, ok := h.resolve(w, r, false)
	if !ok {
		return
	}
	view := CollectView{
		Kind: request.Kind, Prompt: request.Prompt, Locale: request.Locale,
		ProposedQuote: request.ProposedQuote,
		VideoAllowed:  h.options.MediaStore != nil && h.videoAllowed(r.Context(), tenantID),
		Submitted:     request.Status == RequestSubmitted,
	}
	if err := h.withTenant(r.Context(), tenantID, func(query db.Querier) error {
		if request.FeedbackID != nil {
			var text *string
			if err := query.QueryRow(r.Context(), `
				SELECT text, rating FROM feedbacks WHERE tenant_id = $1 AND id = $2
			`, tenantID, *request.FeedbackID).Scan(&text, &view.Rating); err != nil {
				return fmt.Errorf("load originating feedback: %w", err)
			}
			if text != nil {
				view.OriginalText = *text
			}
		}
		return h.options.Store.MarkRequestOpened(r.Context(), query, tenantID, request.ID, h.now())
	}); err != nil {
		h.writeFailure(w, r, err)
		return
	}
	noStore(w)
	api.WriteJSON(w, http.StatusOK, map[string]any{"request": view})
}

type collectSubmission struct {
	Quote      string  `json:"quote"`
	Headline   *string `json:"headline"`
	AuthorName string  `json:"author_name"`
	AuthorRole string  `json:"author_role"`
	Company    string  `json:"author_company"`
	CompanyURL string  `json:"author_company_url"`
	Consent    struct {
		Quote    bool `json:"quote"`
		Identity bool `json:"identity"`
		Company  bool `json:"company"`
		Avatar   bool `json:"avatar"`
		Video    bool `json:"video"`
	} `json:"consent"`
	NoticeVersion string `json:"notice_version"`
	Locale        string `json:"locale"`
}

func (h *CollectHTTPHandler) submit(w http.ResponseWriter, r *http.Request) {
	var submission collectSubmission
	if !decodeJSON(w, r, &submission) {
		return
	}
	quote := strings.TrimSpace(submission.Quote)
	fields := map[string]string{}
	if quote == "" || utf8.RuneCountInString(quote) > 2000 {
		fields["quote"] = "must be between 1 and 2000 characters"
	}
	// Publishing without permission for the words themselves is not a testimonial,
	// so this is refused at the edge rather than filtered out later.
	if !submission.Consent.Quote {
		fields["consent.quote"] = "permission to publish the quote is required"
	}
	if submission.Consent.Identity && strings.TrimSpace(submission.AuthorName) == "" {
		fields["author_name"] = "a name is required to publish an identity"
	}
	if len(fields) > 0 {
		noStore(w)
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"the testimonial is incomplete", fields)
		return
	}

	tenantID, request, ok := h.resolve(w, r, true)
	if !ok {
		return
	}
	publicID, err := GeneratePublicID(PrefixTestimonial)
	if err != nil {
		h.writeFailure(w, r, err)
		return
	}
	// The author keeps a permanent way out. The plaintext goes in the confirmation
	// email; only the digest is stored.
	revocationToken, revocationHash, err := GenerateToken()
	if err != nil {
		h.writeFailure(w, r, err)
		return
	}

	values := SubmissionValues{
		ProjectID:  request.ProjectID,
		FeedbackID: request.FeedbackID,
		RequestID:  request.ID,
		Source:     SourceFeedback,
		Quote:      quote,
		Headline:   submission.Headline,
		Author: AuthorDetails{
			Name:       strings.TrimSpace(submission.AuthorName),
			Role:       strings.TrimSpace(submission.AuthorRole),
			Company:    strings.TrimSpace(submission.Company),
			CompanyURL: strings.TrimSpace(submission.CompanyURL),
		},
		Scopes: ConsentScopes{
			Quote:    submission.Consent.Quote,
			Identity: submission.Consent.Identity,
			Company:  submission.Consent.Company,
			Avatar:   submission.Consent.Avatar,
			Video:    submission.Consent.Video,
		},
		Locale: request.Locale,
		Consent: ConsentEvidence{
			NoticeVersion: submission.NoticeVersion,
			Locale:        submission.Locale,
			// A user-agent family and nothing more. A raw IP address would be
			// stronger evidence and far more personal data than proving consent
			// requires.
			UserAgentFamily: userAgentFamily(r.UserAgent()),
		},
	}
	if request.FeedbackID == nil {
		values.Source = SourceForm
	}

	if err := h.withTenant(r.Context(), tenantID, func(query db.Querier) error {
		_, err := h.options.Store.SubmitCollection(r.Context(), query, tenantID, publicID,
			revocationHash, values, h.now())
		return err
	}); err != nil {
		h.writeFailure(w, r, err)
		return
	}

	noStore(w)
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"revocation_token": revocationToken,
	})
}

// resolve turns a token into a workspace and a usable request, answering every
// failure identically. Telling an anonymous caller that a token exists but has
// expired is more than they are owed.
func (h *CollectHTTPHandler) resolve(
	w http.ResponseWriter,
	r *http.Request,
	forSubmission bool,
) (string, Request, bool) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		h.writeUnusable(w)
		return "", Request{}, false
	}
	hash := HashToken(token)
	tenantID, err := h.options.Store.ResolveRequestTenant(r.Context(), h.options.Resolver, hash)
	if err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			h.writeUnusable(w)
		} else {
			h.writeFailure(w, r, err)
		}
		return "", Request{}, false
	}

	var request Request
	if err := h.withTenant(r.Context(), tenantID, func(query db.Querier) error {
		loaded, loadErr := h.options.Store.LoadRequestByToken(r.Context(), query, tenantID, hash)
		request = loaded
		return loadErr
	}); err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			h.writeUnusable(w)
		} else {
			h.writeFailure(w, r, err)
		}
		return "", Request{}, false
	}

	if request.Usable(h.now()) {
		return tenantID, request, true
	}
	// A request that was already submitted stays readable, so someone revisiting
	// their own link sees the confirmation and their withdrawal instructions
	// rather than an error suggesting something went wrong.
	if !forSubmission && request.Status == RequestSubmitted {
		return tenantID, request, true
	}
	h.writeUnusable(w)
	return "", Request{}, false
}

// withTenant runs work inside a transaction that pins app.tenant_id, so an
// unauthenticated request still reads and writes under row-level security.
func (h *CollectHTTPHandler) withTenant(
	ctx context.Context,
	tenantID string,
	work func(db.Querier) error,
) error {
	tx, err := h.options.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin collection transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	var configured string
	if err := tx.QueryRow(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID).
		Scan(&configured); err != nil || configured != tenantID {
		return errors.New("configure collection tenant context")
	}
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit collection transaction: %w", err)
	}
	committed = true
	return nil
}

func (h *CollectHTTPHandler) writeUnusable(w http.ResponseWriter) {
	noStore(w)
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound,
		"this testimonial link is no longer usable", nil)
}

func (h *CollectHTTPHandler) writeFailure(w http.ResponseWriter, r *http.Request, err error) {
	if h.options.Logger != nil {
		h.options.Logger.ErrorContext(r.Context(), "testimonial collection failed",
			slog.String("error", err.Error()))
	}
	noStore(w)
	api.WriteError(w, http.StatusInternalServerError, api.CodeInternal,
		"the request could not be completed", nil)
}

// noStore keeps an author's draft and their consent record out of every cache.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

// userAgentFamily reduces a user-agent string to a coarse browser family.
//
// The point of the evidence is to show a human agent submitted the form, not to
// fingerprint them, so the full string is deliberately discarded.
func userAgentFamily(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "firefox"):
		return "firefox"
	case strings.Contains(lower, "edg/"):
		return "edge"
	case strings.Contains(lower, "chrome"):
		return "chrome"
	case strings.Contains(lower, "safari"):
		return "safari"
	case value == "":
		return "unknown"
	default:
		return "other"
	}
}
