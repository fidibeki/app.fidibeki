package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	identityRequestBytes  = MaxSourceBytes + 512*1024
	identityMutationLimit = 10
	identityCleanupGrace  = 15 * time.Minute
)

type HTTPOptions struct {
	Store         Store
	JWT           *auth.JWTManager
	Storage       ObjectStorage
	Limiter       ratelimit.Limiter
	PublicBaseURL string
	Logger        *slog.Logger
}

type HTTPHandler struct {
	options HTTPOptions
}

func RegisterTenantRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(
		protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionTenantUpdate),
	)...).Put("/v1/organization-profile/logo", handler.putLogo)
	router.With(append(
		protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionTenantUpdate),
	)...).Delete("/v1/organization-profile/logo", handler.deleteLogo)
	router.With(append(protected, appmiddleware.RequireWritable)...).
		Put("/v1/profile/avatar", handler.putAvatar)
	router.With(append(protected, appmiddleware.RequireWritable)...).
		Delete("/v1/profile/avatar", handler.deleteAvatar)
	router.With(protected...).
		Get("/v1/members/{memberID}/avatar/{version}/{size}.png", handler.getAvatar)
}

func (handler *HTTPHandler) putLogo(w http.ResponseWriter, r *http.Request) {
	resolved, query, claims, ok := handler.requestState(w, r)
	if !ok || !handler.requireActiveHuman(w, r, query, resolved.ID, claims.Subject) ||
		!handler.allowMutation(w, r, "logo:"+resolved.ID) {
		return
	}
	variants, ok := handler.readImage(w, r, ImageKindLogo)
	if !ok {
		return
	}
	version := uuid.NewString()
	if err := handler.options.Store.StageCleanup(
		r.Context(), resolved.ID, ImageKindLogo, nil, version, identityCleanupGrace,
	); err != nil {
		handler.internalError(w, r, "stage workspace logo cleanup", err)
		return
	}
	if err := writeVariants(r.Context(), handler.options.Storage, resolved.ID, ImageKindLogo, nil, version, variants); err != nil {
		handler.internalError(w, r, "write workspace logo", err)
		return
	}
	if err := handler.options.Store.ReplaceLogo(
		r.Context(), query, resolved.ID, claims.Subject, version,
	); err != nil {
		removeUploadedVersion(r.Context(), handler.options.Storage, resolved.ID, ImageKindLogo, nil, version)
		handler.internalError(w, r, "replace workspace logo", err)
		return
	}
	logoURL, err := WorkspaceLogoURL(handler.options.PublicBaseURL, resolved.ID, version, 256)
	if err != nil {
		handler.internalError(w, r, "build workspace logo URL", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"logo_version": version,
		"logo_url":     logoURL,
	})
}

func (handler *HTTPHandler) deleteLogo(w http.ResponseWriter, r *http.Request) {
	resolved, query, claims, ok := handler.requestState(w, r)
	if !ok || !handler.requireActiveHuman(w, r, query, resolved.ID, claims.Subject) ||
		!handler.allowMutation(w, r, "logo:"+resolved.ID) {
		return
	}
	removed, err := handler.options.Store.RemoveLogo(
		r.Context(), query, resolved.ID, claims.Subject,
	)
	if err != nil {
		handler.internalError(w, r, "remove workspace logo", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"removed":      removed,
		"logo_version": nil,
		"logo_url":     nil,
	})
}

func (handler *HTTPHandler) putAvatar(w http.ResponseWriter, r *http.Request) {
	resolved, query, claims, ok := handler.requestState(w, r)
	if !ok || !handler.requireActiveHuman(w, r, query, resolved.ID, claims.Subject) ||
		!handler.allowMutation(w, r, "avatar:"+resolved.ID+":"+claims.Subject) {
		return
	}
	variants, ok := handler.readImage(w, r, ImageKindAvatar)
	if !ok {
		return
	}
	version := uuid.NewString()
	if err := handler.options.Store.StageCleanup(
		r.Context(), resolved.ID, ImageKindAvatar, &claims.Subject, version, identityCleanupGrace,
	); err != nil {
		handler.internalError(w, r, "stage member avatar cleanup", err)
		return
	}
	if err := writeVariants(
		r.Context(), handler.options.Storage, resolved.ID, ImageKindAvatar,
		&claims.Subject, version, variants,
	); err != nil {
		handler.internalError(w, r, "write member avatar", err)
		return
	}
	if err := handler.options.Store.ReplaceAvatar(
		r.Context(), query, resolved.ID, claims.Subject, version,
	); err != nil {
		removeUploadedVersion(
			r.Context(), handler.options.Storage, resolved.ID, ImageKindAvatar,
			&claims.Subject, version,
		)
		handler.internalError(w, r, "replace member avatar", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"avatar_version": version,
		"member_id":      claims.Subject,
	})
}

func (handler *HTTPHandler) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	resolved, query, claims, ok := handler.requestState(w, r)
	if !ok || !handler.requireActiveHuman(w, r, query, resolved.ID, claims.Subject) ||
		!handler.allowMutation(w, r, "avatar:"+resolved.ID+":"+claims.Subject) {
		return
	}
	removed, err := handler.options.Store.RemoveAvatar(
		r.Context(), query, resolved.ID, claims.Subject,
	)
	if errors.Is(err, ErrAssetNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "member not found", nil)
		return
	}
	if err != nil {
		handler.internalError(w, r, "remove member avatar", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"removed":        removed,
		"avatar_version": nil,
		"member_id":      claims.Subject,
	})
}

func (handler *HTTPHandler) getAvatar(w http.ResponseWriter, r *http.Request) {
	resolved, query, _, ok := handler.requestState(w, r)
	if !ok {
		return
	}
	memberID := chi.URLParam(r, "memberID")
	version := chi.URLParam(r, "version")
	size, validSize := parseImageSize(chi.URLParam(r, "size"))
	if _, err := uuid.Parse(memberID); err != nil || !validSize {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "avatar not found", nil)
		return
	}
	current, err := handler.options.Store.CurrentAvatarVersion(
		r.Context(), query, resolved.ID, memberID,
	)
	if errors.Is(err, ErrAssetNotFound) || current != version {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "avatar not found", nil)
		return
	}
	if err != nil {
		handler.internalError(w, r, "get member avatar", err)
		return
	}
	key, err := ObjectKey(resolved.ID, ImageKindAvatar, &memberID, version, size)
	if err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "avatar not found", nil)
		return
	}
	handler.streamPNG(w, r, key, "private, max-age=3600")
}

func (handler *HTTPHandler) requestState(
	w http.ResponseWriter,
	r *http.Request,
) (resolved struct {
	ID string
}, query db.Querier, claims auth.Claims, ok bool) {
	tenantValue, tenantOK := appmiddleware.TenantFromContext(r.Context())
	queryValue, queryOK := appmiddleware.QuerierFromContext(r.Context())
	claimsValue, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !tenantOK || !queryOK || !claimsOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return resolved, nil, auth.Claims{}, false
	}
	resolved.ID = tenantValue.ID
	return resolved, queryValue, claimsValue, true
}

func (handler *HTTPHandler) readImage(
	w http.ResponseWriter,
	r *http.Request,
	kind ImageKind,
) (ImageVariants, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, identityRequestBytes)
	if err := r.ParseMultipartForm(identityRequestBytes); err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "validation failed",
			map[string]string{"image": "must be a PNG, JPEG, or WebP file up to 2 MiB"})
		return ImageVariants{}, false
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "validation failed",
			map[string]string{"image": "is required"})
		return ImageVariants{}, false
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		handler.internalError(w, r, "read identity image", err)
		return ImageVariants{}, false
	}
	variants, err := (ImageProcessor{}).Process(
		header.Filename, header.Header.Get("Content-Type"), body, kind,
	)
	if err != nil {
		message := "must be a valid PNG, JPEG, or WebP image between 32 and 4096 pixels and up to 2 MiB"
		if errors.Is(err, ErrImageTooLarge) {
			message = "must be at most 2 MiB"
		}
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "validation failed",
			map[string]string{"image": message})
		return ImageVariants{}, false
	}
	return variants, true
}

func (handler *HTTPHandler) allowMutation(w http.ResponseWriter, r *http.Request, key string) bool {
	if handler.options.Limiter == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return false
	}
	decision, err := handler.options.Limiter.Allow(
		r.Context(), "identity:"+key, ratelimit.Window{Limit: identityMutationLimit, Period: time.Hour},
	)
	if err != nil {
		handler.internalError(w, r, "identity mutation limiter unavailable", err)
		return false
	}
	if decision.Allowed {
		return true
	}
	retrySeconds := max(1, int(math.Ceil(decision.RetryAfter.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
	api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded, "rate limit exceeded", nil)
	return false
}

func (handler *HTTPHandler) requireActiveHuman(
	w http.ResponseWriter,
	r *http.Request,
	query db.Querier,
	tenantID, memberID string,
) bool {
	active, err := handler.options.Store.ActiveMember(r.Context(), query, tenantID, memberID)
	if err != nil {
		handler.internalError(w, r, "check identity member", err)
		return false
	}
	if !active {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "member access required", nil)
		return false
	}
	return true
}

func (handler *HTTPHandler) streamPNG(
	w http.ResponseWriter,
	r *http.Request,
	key, cacheControl string,
) {
	if handler.options.Storage == nil {
		handler.internalError(w, r, "identity storage unavailable", errors.New("storage is unavailable"))
		return
	}
	object, err := handler.options.Storage.OpenObject(r.Context(), key)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "image not found", nil)
			return
		}
		handler.internalError(w, r, "open identity image", err)
		return
	}
	defer object.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, object); err != nil && handler.options.Logger != nil {
		handler.options.Logger.WarnContext(r.Context(), "stream identity image failed", "error", err)
	}
}

func (handler *HTTPHandler) internalError(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	err error,
) {
	if handler.options.Logger != nil {
		handler.options.Logger.ErrorContext(r.Context(), operation, "error", err)
	}
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func writeVariants(
	ctx context.Context,
	objectStorage ObjectStorage,
	tenantID string,
	kind ImageKind,
	memberID *string,
	version string,
	variants ImageVariants,
) error {
	if objectStorage == nil {
		return errors.New("identity storage is unavailable")
	}
	written := make([]string, 0, 2)
	for _, variant := range []struct {
		size int
		body []byte
	}{
		{size: 64, body: variants.Small},
		{size: 256, body: variants.Large},
	} {
		size, body := variant.size, variant.body
		key, err := ObjectKey(tenantID, kind, memberID, version, size)
		if err != nil {
			return err
		}
		if err := objectStorage.PutObject(
			ctx, key, bytes.NewReader(body), int64(len(body)), "image/png",
		); err != nil {
			for _, writtenKey := range written {
				_ = objectStorage.RemoveObject(context.WithoutCancel(ctx), writtenKey)
			}
			return err
		}
		written = append(written, key)
	}
	return nil
}

func removeUploadedVersion(
	ctx context.Context,
	objectStorage ObjectStorage,
	tenantID string,
	kind ImageKind,
	memberID *string,
	version string,
) {
	if objectStorage == nil {
		return
	}
	for _, size := range []int{64, 256} {
		key, err := ObjectKey(tenantID, kind, memberID, version, size)
		if err == nil {
			_ = objectStorage.RemoveObject(context.WithoutCancel(ctx), key)
		}
	}
}

func WorkspaceLogoURL(publicBaseURL, tenantID, version string, size int) (string, error) {
	if _, err := ObjectKey(tenantID, ImageKindLogo, nil, version, size); err != nil {
		return "", err
	}
	base, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", errors.New("public base URL is invalid")
	}
	base.Path = fmt.Sprintf(
		"/v1/public/workspace-logo/%s/%s/%d.png", tenantID, version, size,
	)
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func parseImageSize(value string) (int, bool) {
	size, err := strconv.Atoi(value)
	return size, err == nil && (size == 64 || size == 256)
}
