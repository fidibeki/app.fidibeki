package testimonial

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

const (
	// videoUploadTTL is how long the author has to finish sending the file.
	// Generous, because they may be on a phone on mobile data.
	videoUploadTTL = 30 * time.Minute
	// maxVideoBytes bounds what one testimonial may cost us. A minute or two of
	// phone video sits far below this; the limit exists to stop an accident, not
	// to ration honest use.
	maxVideoBytes = 200 << 20
)

// videoContentTypes is what a phone or a browser recorder actually produces.
// The type is checked again after upload by the scanner, which reads the bytes
// rather than believing this.
var videoContentTypes = map[string]bool{
	"video/mp4":       true,
	"video/quicktime": true,
	"video/webm":      true,
}

// CollectMediaStore is the write surface the author's upload needs.
type CollectMediaStore interface {
	CreateVideoUpload(context.Context, db.Querier, string, VideoUploadValues) (string, error)
	CompleteVideoUpload(context.Context, db.Querier, string, string, string, time.Time) error
}

// UploadSigner issues the URL the author's browser sends the file to.
type UploadSigner interface {
	PresignPut(context.Context, string, time.Duration) (string, error)
}

// requestVideo hands back a direct-to-storage upload URL.
//
// The bytes never pass through the API. A phone video is tens of megabytes and
// would hold an API connection for the length of a mobile upload, which under a
// small connection pool is a self-inflicted outage.
func (h *CollectHTTPHandler) requestVideo(w http.ResponseWriter, r *http.Request) {
	var submission struct {
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes"`
	}
	if !decodeJSON(w, r, &submission) {
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(submission.ContentType))
	fields := map[string]string{}
	if !videoContentTypes[contentType] {
		fields["content_type"] = "must be an mp4, QuickTime or WebM video"
	}
	if submission.SizeBytes < 1 || submission.SizeBytes > maxVideoBytes {
		fields["size_bytes"] = fmt.Sprintf("must be between 1 and %d bytes", int64(maxVideoBytes))
	}
	if len(fields) > 0 {
		noStore(w)
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"this video cannot be accepted", fields)
		return
	}

	tenantID, request, ok := h.resolve(w, r, true)
	if !ok {
		return
	}
	if !h.videoAllowed(r.Context(), tenantID) {
		noStore(w)
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanFeatureUnavailable,
			"video testimonials are not available on this plan", nil)
		return
	}

	publicID, err := GeneratePublicID(PrefixMedia)
	if err != nil {
		h.writeFailure(w, r, err)
		return
	}
	completeToken, completeHash, err := GenerateToken()
	if err != nil {
		h.writeFailure(w, r, err)
		return
	}
	// Content-addressed later by the transcoder; until then the key only has to
	// be unique and unguessable.
	storageKey := fmt.Sprintf("testimonials/%s/%s/source", tenantID, publicID)

	if err := h.withTenant(r.Context(), tenantID, func(query db.Querier) error {
		_, createErr := h.options.MediaStore.CreateVideoUpload(r.Context(), query, tenantID,
			VideoUploadValues{
				RequestID: request.ID, PublicID: publicID, StorageKey: storageKey,
				ContentType: contentType, SizeBytes: submission.SizeBytes,
				CompleteTokenHash: completeHash,
			})
		return createErr
	}); err != nil {
		h.writeFailure(w, r, err)
		return
	}

	url, err := h.options.Uploads.PresignPut(r.Context(), storageKey, videoUploadTTL)
	if err != nil {
		h.writeFailure(w, r, err)
		return
	}
	noStore(w)
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"media_id":       publicID,
		"upload_url":     url,
		"method":         http.MethodPut,
		"headers":        map[string]string{"Content-Type": contentType},
		"complete_token": completeToken,
		"expires_at":     h.now().Add(videoUploadTTL),
	})
}

// completeVideo records that the upload finished.
//
// The token proves the caller is the one who asked for this slot, and it is
// consumed here so a replay cannot resurrect a file that was later replaced.
func (h *CollectHTTPHandler) completeVideo(w http.ResponseWriter, r *http.Request) {
	var submission struct {
		MediaID       string `json:"media_id"`
		CompleteToken string `json:"complete_token"`
	}
	if !decodeJSON(w, r, &submission) {
		return
	}
	tenantID, _, ok := h.resolve(w, r, true)
	if !ok {
		return
	}
	if err := h.withTenant(r.Context(), tenantID, func(query db.Querier) error {
		return h.options.MediaStore.CompleteVideoUpload(r.Context(), query, tenantID,
			strings.TrimSpace(submission.MediaID), HashToken(submission.CompleteToken), h.now())
	}); err != nil {
		if errors.Is(err, ErrNotFound) {
			noStore(w)
			api.WriteError(w, http.StatusNotFound, api.CodeNotFound,
				"this upload is no longer expecting a file", nil)
			return
		}
		h.writeFailure(w, r, err)
		return
	}
	// Enqueued after the row is committed, so the worker cannot claim an upload
	// the transaction has not finished recording.
	if h.options.Transcodes != nil {
		task, err := NewTranscodeTask(tenantID, strings.TrimSpace(submission.MediaID))
		if err == nil {
			if _, err = h.options.Transcodes.EnqueueContext(r.Context(), task); err != nil &&
				h.options.Logger != nil {
				// The author's upload succeeded; only the follow-up is late. A
				// periodic sweep picks up anything left pending.
				h.options.Logger.WarnContext(r.Context(), "enqueue testimonial transcode failed",
					slog.String("error", err.Error()))
			}
		}
	}
	noStore(w)
	w.WriteHeader(http.StatusNoContent)
}

// videoAllowed reports whether the workspace's plan carries video.
//
// Read here rather than trusted from the page, because this endpoint is
// unauthenticated: the only thing the caller proves is that they hold a
// collection token, which says nothing about what the workspace pays for.
func (h *CollectHTTPHandler) videoAllowed(ctx context.Context, tenantID string) bool {
	var plan string
	if err := h.options.Resolver.QueryRow(ctx, `
		SELECT plan::text FROM tenants WHERE id = $1
	`, tenantID).Scan(&plan); err != nil {
		return false
	}
	return config.CanUseFeature(tenant.Tenant{Plan: tenant.Plan(plan)}, config.FeatureVideo)
}
