package emailfeedback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

type PublicHTTPOptions struct {
	Store        Store
	Resolver     db.Querier
	Begin        db.BeginFunc
	BaseDomain   string
	Logger       *slog.Logger
	Now          func() time.Time
	FeedbackCore feedback.SubmissionCore
}

type publicHTTPHandler struct {
	options PublicHTTPOptions
}

type InvitationOptOutView struct {
	ControllerName   string `json:"controller_name"`
	Locale           string `json:"locale"`
	PrivacyPolicyURL string `json:"privacy_policy_url"`
	OptedOut         bool   `json:"opted_out"`
}

func RegisterPublicRoutes(router chi.Router, options PublicHTTPOptions) {
	handler := &publicHTTPHandler{options: options}
	router.Get("/v1/public/email-feedback/{token}", handler.loadCollection)
	router.Post("/v1/public/email-feedback/{token}", handler.submitCollection)
	router.Get("/v1/public/email-feedback/{token}/opt-out", handler.loadOptOut)
	router.Post("/v1/public/email-feedback/{token}/opt-out", handler.submitOptOut)
}

func (handler *publicHTTPHandler) loadOptOut(w http.ResponseWriter, r *http.Request) {
	resolved, tokenHash, ok := handler.resolvePublicInvitation(w, r)
	if !ok {
		return
	}
	var optOut InvitationOptOut
	if err := handler.withTenant(r.Context(), resolved.TenantID, func(query db.Querier) error {
		loaded, err := handler.options.Store.LoadInvitationOptOut(
			r.Context(), query, resolved, tokenHash, false,
		)
		optOut = loaded
		return err
	}); err != nil {
		handler.writePublicError(w, r, err)
		return
	}
	writeInvitationNoStore(w)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"opt_out": InvitationOptOutView{
			ControllerName:   optOut.ControllerName,
			Locale:           optOut.Locale,
			PrivacyPolicyURL: optOut.PrivacyPolicyURL,
			OptedOut: optOut.Suppressed || optOut.Status == InvitationSuppressed ||
				optOut.Status == InvitationBounced || optOut.Status == InvitationComplained,
		},
	})
}

func (handler *publicHTTPHandler) submitOptOut(w http.ResponseWriter, r *http.Request) {
	if !decodeOptionalEmptyJSON(w, r) {
		return
	}
	resolved, tokenHash, ok := handler.resolvePublicInvitation(w, r)
	if !ok {
		return
	}
	if err := handler.withTenant(r.Context(), resolved.TenantID, func(query db.Querier) error {
		optOut, err := handler.options.Store.LoadInvitationOptOut(
			r.Context(), query, resolved, tokenHash, true,
		)
		if err != nil {
			return err
		}
		return handler.options.Store.SuppressInvitationRecipient(
			r.Context(), query, optOut, handler.now(),
		)
	}); err != nil {
		handler.writePublicError(w, r, err)
		return
	}
	writeInvitationNoStore(w)
	api.WriteJSON(w, http.StatusOK, map[string]bool{"opted_out": true})
}

func (handler *publicHTTPHandler) resolvePublicInvitation(
	w http.ResponseWriter,
	r *http.Request,
) (PublicInvitationResolution, string, bool) {
	rawToken := strings.TrimSpace(chi.URLParam(r, "token"))
	if !validInvitationToken(rawToken) || handler.options.Resolver == nil {
		handler.writePublicUnavailable(w)
		return PublicInvitationResolution{}, "", false
	}
	tokenHash := auth.HashToken(rawToken)
	resolved, err := handler.options.Store.ResolvePublicInvitation(
		r.Context(), handler.options.Resolver, tokenHash,
	)
	if err != nil {
		handler.writePublicError(w, r, err)
		return PublicInvitationResolution{}, "", false
	}
	hostSlug, err := tenant.SlugFromHost(r.Host, handler.options.BaseDomain)
	if err != nil || hostSlug != resolved.TenantSlug {
		handler.writePublicUnavailable(w)
		return PublicInvitationResolution{}, "", false
	}
	return resolved, tokenHash, true
}

func (handler *publicHTTPHandler) withTenant(
	ctx context.Context,
	tenantID string,
	work func(db.Querier) error,
) error {
	if handler.options.Begin == nil {
		return errors.New("email feedback public transaction is unavailable")
	}
	transaction, err := handler.options.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin email feedback public transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback(ctx)
		}
	}()
	var configured string
	if err := transaction.QueryRow(
		ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID,
	).Scan(&configured); err != nil || configured != tenantID {
		return errors.New("configure email feedback public tenant context")
	}
	if err := work(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit email feedback public transaction: %w", err)
	}
	committed = true
	return nil
}

func decodeOptionalEmptyJSON(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return false
	}
	value := strings.TrimSpace(string(payload))
	if value != "" && value != "{}" {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body must be empty", nil)
		return false
	}
	return true
}

func validInvitationToken(value string) bool {
	if len(value) != 47 || !strings.HasPrefix(value, "efi_") {
		return false
	}
	for _, character := range value[len("efi_"):] {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (handler *publicHTTPHandler) writePublicError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrInvitationPublicToken) {
		handler.writePublicUnavailable(w)
		return
	}
	if handler.options.Logger != nil {
		handler.options.Logger.ErrorContext(
			r.Context(), "email feedback public request failed",
			slog.String("error", err.Error()),
		)
	}
	writeInvitationNoStore(w)
	api.WriteError(
		w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
		"service unavailable", nil,
	)
}

func (handler *publicHTTPHandler) writePublicUnavailable(w http.ResponseWriter) {
	writeInvitationNoStore(w)
	api.WriteError(
		w, http.StatusNotFound, api.CodeNotFound,
		"this email feedback link is unavailable", nil,
	)
}

func writeInvitationNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

func (handler *publicHTTPHandler) now() time.Time {
	if handler.options.Now != nil {
		return handler.options.Now().UTC()
	}
	return time.Now().UTC()
}
