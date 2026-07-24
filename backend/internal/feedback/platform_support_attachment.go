package feedback

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PlatformSupportTokenVerifier verifies a platform-admin bearer token. It is
// satisfied by *auth.JWTManager.
type PlatformSupportTokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

// PlatformSupportAttachmentHTTPOptions configures the platform-admin/support
// access path to a tenant's feedback attachment. It reuses the same object
// store and storage backend as the tenant-facing stream
// (tenant_attachment_stream.go) but requires platform-admin authentication
// (auth.PermissionPlatformTenantSupportAccess) plus an explicit, validated
// support reason and case reference, and records access as
// actor_type=platform_admin so support access stays separately identifiable
// from ordinary tenant-member/API-key reads.
type PlatformSupportAttachmentHTTPOptions struct {
	Begin   db.BeginFunc
	Store   AttachmentObjectStore
	JWT     PlatformSupportTokenVerifier
	Storage AttachmentStreamStorage
	Logger  *slog.Logger
}

type platformSupportAttachmentHandler struct {
	options PlatformSupportAttachmentHTTPOptions
	logger  *slog.Logger
}

// RegisterPlatformSupportRoutes mounts the platform-admin/support attachment
// access path on the platform-host router (see backend/cmd/fidibeki-api).
func RegisterPlatformSupportRoutes(router chi.Router, options PlatformSupportAttachmentHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &platformSupportAttachmentHandler{options: options, logger: logger}
	router.With(
		appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantSupportAccess),
	).Get(
		"/admin/tenants/{tenantID}/feedbacks/{feedbackID}/attachments/{attachmentID}/support-access",
		handler.serveHTTP,
	)
}

func (h *platformSupportAttachmentHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	feedbackID := chi.URLParam(r, "feedbackID")
	attachmentID := chi.URLParam(r, "attachmentID")
	if _, err := uuid.Parse(tenantID); err != nil {
		writeAttachmentStreamNotFound(w)
		return
	}
	if _, err := uuid.Parse(feedbackID); err != nil {
		writeAttachmentStreamNotFound(w)
		return
	}
	if _, err := uuid.Parse(attachmentID); err != nil {
		writeAttachmentStreamNotFound(w)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	supportRequest, requestErr := accessaudit.ValidateSupportRequest(accessaudit.SupportRequest{
		Reason:        r.Header.Get(accessaudit.SupportReasonHeader),
		CaseReference: r.Header.Get(accessaudit.SupportCaseReferenceHeader),
	})
	mode := "preview"
	if r.URL.Query().Get("mode") == "download" {
		mode = "download"
	}

	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	var configured string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, tenantID).Scan(&configured); err != nil || configured != tenantID {
		h.writeError(w, r, fmt.Errorf("set support attachment stream tenant context: %w", err))
		return
	}
	recordAccess := func(outcome string, data map[string]any) error {
		return accessaudit.Record(r.Context(), tx, accessaudit.RecordInput{
			TenantID: tenantID, Kind: "feedback_attachment.support_accessed",
			Actor:          accessaudit.PlatformAdminActor(claims.Subject),
			ResourceType:   accessaudit.ResourceFeedbackAttachment,
			ResourceID:     attachmentID,
			Action:         mode,
			PermissionPath: accessaudit.PermissionPathSupportAccess,
			RequestID:      appmiddleware.RequestIDFromContext(r.Context()),
			Outcome:        outcome,
			Data:           data,
			DedupWindow:    accessaudit.DefaultDedupWindow,
		})
	}
	// Fail closed: a platform admin without a valid, explicit support reason
	// and case reference never reaches the object lookup below, and the
	// rejection itself is still audited as a denied support-access attempt.
	if requestErr != nil {
		if err := recordAccess(accessaudit.OutcomeDenied, map[string]any{"denial_reason": "support_request_invalid"}); err != nil {
			h.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			h.writeError(w, r, err)
			return
		}
		committed = true
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "support reason and case reference are required", nil)
		return
	}
	supportData := map[string]any{
		"support_reason": supportRequest.Reason, "support_case_reference": supportRequest.CaseReference,
	}
	object, err := h.options.Store.GetTenantAttachmentObject(r.Context(), tx, tenantID, feedbackID, attachmentID)
	if errors.Is(err, ErrNotFound) {
		// Audit recording and the authorization decision commit atomically:
		// if persistence fails we fail closed and never disclose whether the
		// attachment exists.
		if err := recordAccess(accessaudit.OutcomeNotFound, supportData); err != nil {
			h.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			h.writeError(w, r, err)
			return
		}
		committed = true
		writeAttachmentStreamNotFound(w)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.options.Storage == nil {
		h.writeError(w, r, errors.New("attachment storage is unavailable"))
		return
	}
	reader, err := h.options.Storage.OpenObject(r.Context(), object.StorageKey)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	streamOwnsObject := false
	defer func() {
		if !streamOwnsObject {
			closeAttachmentObject(r, reader, object.ID, h.logger)
		}
	}()
	successData := map[string]any{"content_type": object.ContentType}
	maps.Copy(successData, supportData)
	if err := recordAccess(accessaudit.OutcomeSuccess, successData); err != nil {
		h.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeError(w, r, err)
		return
	}
	committed = true
	streamOwnsObject = true
	writeAttachmentStreamContent(w, r, reader, object, feedbackID, mode, h.logger)
}

func (h *platformSupportAttachmentHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "platform support attachment stream request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
