package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/fidibeki/app.fidibeki/backend/internal/accessaudit"
	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SlackCallbackPath is fixed and lives on the apex API host: Slack matches a redirect URL's
// host and port exactly against the app's registered list, and tenants live on per-workspace
// subdomains, so there is no tenant host that could be registered.
const SlackCallbackPath = "/v1/integrations/slack/callback"

// Access-audit vocabulary for one-click installs, recorded in tenant_access_events so the
// Settings audit tab shows who connected and disconnected a workspace.
const (
	AuditResourceType   = "integration"
	AuditKindInstalled  = "integration.slack_installed"
	AuditKindUninstall  = "integration.slack_uninstalled"
	AuditActionInstall  = "install"
	AuditActionUninstal = "uninstall"
)

// settingsPath is where the browser lands after the round trip, success or failure.
const settingsPath = "/settings"

// slackInstall records the install intent and hands the browser the Slack URL to visit. The
// events and privacy fields chosen here ride in the server-side state, so the integration
// arrives fully configured instead of needing a second edit after the round trip.
func (h *HTTPHandler) slackInstall(w http.ResponseWriter, r *http.Request) {
	resolved, _, ok := integrationRequestState(w, r)
	if !ok || !requireIntegrationPlan(w, resolved) {
		return
	}
	if !h.options.Slack.Configured() {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			"slack one-click install is not configured for this deployment", nil)
		return
	}
	var input SlackInstallInput
	if !decodeIntegrationJSON(w, r, &input) {
		return
	}
	events, fields := validateEvents(input.Events, nil)
	privacyFields, fields := validatePrivacyFields(input.PrivacyFields, fields)
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"request validation failed", map[string]any{"fields": fields})
		return
	}
	claims, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !claimsOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	if claims.IsAPIKey {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden,
			"Slack installation requires a tenant member", nil)
		return
	}
	authorizationVersion := 0
	if claims.AuthorizationVersion != nil {
		authorizationVersion = *claims.AuthorizationVersion
	}
	authorizeURL, err := h.options.Slack.Begin(r.Context(), InstallState{
		TenantID: resolved.ID, TenantSlug: resolved.Slug, MemberID: claims.Subject,
		AuthorizationVersion: &authorizationVersion,
		Events:               events,
		PrivacyFields:        privacyFields,
	})
	if err != nil {
		writeIntegrationError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"authorize_url": authorizeURL})
}

// SlackCallbackOptions carries what the apex callback needs. It runs outside the tenant
// transaction middleware — there is no tenant host to derive a tenant from — so it opens its
// own transaction and scopes it to the tenant named by the state it just consumed.
type SlackCallbackOptions struct {
	Store            Store
	Slack            SlackOAuth
	Begin            db.BeginFunc
	BaseDomain       string
	WebPublicBaseURL string
	Logger           *slog.Logger
}

type slackCallbackHandler struct{ options SlackCallbackOptions }

// RegisterSlackCallbackRoute mounts the callback on a router that is NOT tenant-scoped.
func RegisterSlackCallbackRoute(router chi.Router, options SlackCallbackOptions) {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &slackCallbackHandler{options: options}
	router.Get(SlackCallbackPath, handler.serve)
}

func (h *slackCallbackHandler) serve(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// The state token is the only thing tying this request to a workspace. Consuming it is
	// atomic and single-use, so a replayed callback URL fails here rather than installing
	// twice. Until it resolves there is no tenant to redirect to, so failure is a plain 400.
	state, err := h.options.Slack.States.Consume(r.Context(), query.Get("state"))
	if err != nil {
		if !errors.Is(err, ErrInstallStateInvalid) {
			h.options.Logger.ErrorContext(r.Context(), "slack install state lookup failed", "error", err)
		}
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest,
			"this Slack install link is invalid or has already been used", nil)
		return
	}

	// Slack reports a cancelled install as an error parameter, not an HTTP failure.
	if query.Get("error") != "" {
		h.redirect(w, r, state, "denied")
		return
	}
	code := query.Get("code")
	if code == "" {
		// Slack sends a code or an error. Neither means the callback is malformed, which is
		// a failure rather than a decision the user made.
		h.options.Logger.WarnContext(r.Context(), "slack callback carried neither code nor error",
			"tenant_id", state.TenantID)
		h.redirect(w, r, state, "failed")
		return
	}

	if err := h.preflightAuthority(r.Context(), state); err != nil {
		if errors.Is(err, ErrInstallAuthorityInvalid) {
			h.options.Logger.WarnContext(r.Context(), "slack install authority expired before exchange",
				"tenant_id", state.TenantID, "member_id", state.MemberID)
		} else {
			h.options.Logger.ErrorContext(r.Context(), "slack install authority lookup failed",
				"tenant_id", state.TenantID, "error", err)
		}
		h.redirect(w, r, state, "failed")
		return
	}

	installation, err := h.options.Slack.Exchange(r.Context(), code)
	if err != nil {
		var slackErr *SlackError
		if errors.As(err, &slackErr) {
			h.options.Logger.WarnContext(r.Context(), "slack install exchange rejected",
				"tenant_id", state.TenantID, "slack_error", slackErr.Code)
		} else {
			h.options.Logger.ErrorContext(r.Context(), "slack install exchange failed",
				"tenant_id", state.TenantID, "error", err)
		}
		h.redirect(w, r, state, "failed")
		return
	}

	if err := h.persist(r, state, installation); err != nil {
		if revokeErr := h.options.Slack.Revoke(r.Context(), installation.AccessToken); revokeErr != nil {
			h.options.Logger.ErrorContext(r.Context(), "slack rejected install cleanup failed",
				"tenant_id", state.TenantID, "error", revokeErr)
		}
		if errors.Is(err, ErrInstallAuthorityInvalid) {
			h.options.Logger.WarnContext(r.Context(), "slack install authority expired before persistence",
				"tenant_id", state.TenantID, "member_id", state.MemberID)
		} else {
			h.options.Logger.ErrorContext(r.Context(), "slack install persistence failed",
				"tenant_id", state.TenantID, "error", err)
		}
		h.redirect(w, r, state, "failed")
		return
	}
	h.redirect(w, r, state, "installed")
}

func (h *slackCallbackHandler) preflightAuthority(ctx context.Context, state InstallState) error {
	if state.AuthorizationVersion == nil {
		return ErrInstallAuthorityInvalid
	}
	tx, err := h.beginTenantTransaction(ctx, state.TenantID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return lockSlackInstallAuthority(ctx, tx, state)
}

// persist writes the install inside one transaction scoped to the state's tenant. The audit
// record commits with the integration, so an install can never appear without its trail.
func (h *slackCallbackHandler) persist(
	r *http.Request, state InstallState, installation SlackInstallation,
) error {
	ctx := r.Context()
	tx, err := h.beginTenantTransaction(ctx, state.TenantID)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				h.options.Logger.WarnContext(ctx, "slack install rollback failed", "error", rollbackErr)
			}
		}
	}()
	if err := lockSlackInstallAuthority(ctx, tx, state); err != nil {
		return err
	}

	// integrations.name is NOT NULL with a 1-character floor, so fall back through everything
	// Slack might have left blank rather than failing the install on a nameless channel.
	channel := installation.ChannelName
	name := firstNonEmpty(installation.ChannelName, installation.TeamName, installation.ChannelID)
	integration, err := h.options.Store.UpsertSlackInstall(ctx, tx, state.TenantID, SlackInstallValues{
		ID: uuid.NewString(), Name: truncateIntegrationText(name, 100),
		WebhookURL: installation.WebhookURL, Channel: channel,
		TeamID: installation.TeamID, TeamName: installation.TeamName,
		ChannelID: installation.ChannelID, AccessToken: installation.AccessToken,
		Events: state.Events, PrivacyFields: state.PrivacyFields,
	})
	if err != nil {
		return err
	}
	if err := accessaudit.Record(ctx, tx, accessaudit.RecordInput{
		TenantID: state.TenantID, Kind: AuditKindInstalled,
		Actor:        accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: state.MemberID},
		ResourceType: AuditResourceType, ResourceID: integration.ID,
		Action:         AuditActionInstall,
		PermissionPath: string(auth.PermissionIntegrationCreate),
		RequestID:      appmiddleware.RequestIDFromContext(ctx),
		Outcome:        accessaudit.OutcomeSuccess,
		Data: map[string]any{
			"kind": string(KindSlack), "install_source": string(InstallSourceOAuth),
			"slack_team_id": installation.TeamID,
		},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (h *slackCallbackHandler) beginTenantTransaction(
	ctx context.Context, tenantID string,
) (db.Transaction, error) {
	tx, err := h.options.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var configured string
	if err := tx.QueryRow(ctx,
		`SELECT set_config('app.tenant_id', $1, true)`, tenantID,
	).Scan(&configured); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if configured != tenantID {
		_ = tx.Rollback(ctx)
		return nil, errors.New("slack install tenant scope was not applied")
	}
	return tx, nil
}

// lockSlackInstallAuthority serializes the callback with tenant lifecycle,
// plan, and member authorization mutations. Those mutations use the same
// tenant-then-member lock order, so whichever transaction commits first
// defines whether this install remains authorized.
func lockSlackInstallAuthority(
	ctx context.Context, query db.Querier, state InstallState,
) error {
	if state.AuthorizationVersion == nil {
		return ErrInstallAuthorityInvalid
	}
	var plan tenant.Plan
	var available, deletionPending bool
	if err := query.QueryRow(ctx, `
		SELECT plan::text,
		       status = 'active'
		         OR (status = 'trial' AND trial_ends_at IS NOT NULL AND trial_ends_at > now()),
		       deletion_requested_at IS NOT NULL
		FROM tenants
		WHERE id = $1
		FOR UPDATE
	`, state.TenantID).Scan(&plan, &available, &deletionPending); errors.Is(err, pgx.ErrNoRows) {
		return ErrInstallAuthorityInvalid
	} else if err != nil {
		return fmt.Errorf("lock Slack install tenant authority: %w", err)
	}
	if !available || deletionPending ||
		!config.CanUseFeature(tenant.Tenant{Plan: plan}, config.FeatureSlack) {
		return ErrInstallAuthorityInvalid
	}

	var status string
	var authorizationVersion int
	var permissions []string
	if err := query.QueryRow(ctx, `
		SELECT status::text, authorization_version, permissions
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, state.TenantID, state.MemberID).Scan(
		&status, &authorizationVersion, &permissions,
	); errors.Is(err, pgx.ErrNoRows) {
		return ErrInstallAuthorityInvalid
	} else if err != nil {
		return fmt.Errorf("lock Slack installer authority: %w", err)
	}
	if status != "active" || authorizationVersion != *state.AuthorizationVersion ||
		!auth.HasPermission(permissions, auth.PermissionIntegrationCreate) {
		return ErrInstallAuthorityInvalid
	}
	return nil
}

// redirect sends the browser back to the workspace that started the install, carrying a
// result the Settings page turns into a message. Building the URL from the validated slug
// rather than anything in the request keeps this from becoming an open redirect.
func (h *slackCallbackHandler) redirect(
	w http.ResponseWriter, r *http.Request, state InstallState, result string,
) {
	target, err := tenant.PublicURL(
		h.options.WebPublicBaseURL, h.options.BaseDomain, state.TenantSlug, settingsPath,
	)
	if err != nil {
		h.options.Logger.ErrorContext(r.Context(), "slack install redirect target failed",
			"error", err, "tenant_id", state.TenantID)
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	query := url.Values{}
	query.Set("tab", "integrations")
	query.Set("slack", result)
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// scheduleSlackRevoke reads the bot token while the row still exists, then arranges for the
// revoke to run after the delete commits. Slack is never called with the transaction open,
// and a revoke failure never blocks the delete: the workspace asked for the destination to
// stop receiving data, and the local row is what controls that.
// It reports whether the integration was a one-click install, which is also what decides
// whether the removal is worth an audit entry.
func (h *HTTPHandler) scheduleSlackRevoke(
	ctx context.Context, query db.Querier, tenantID, integrationID string,
) bool {
	if !h.options.Slack.Configured() {
		return false
	}
	token, err := h.options.Store.SlackAccessToken(ctx, query, tenantID, integrationID)
	if err != nil {
		h.options.Logger.WarnContext(ctx, "slack token lookup before delete failed",
			"tenant_id", tenantID, "integration_id", integrationID, "error", err)
		return false
	}
	if token == "" {
		return false
	}
	appmiddleware.AfterTenantCommit(ctx, func(hookCtx context.Context) error {
		if revokeErr := h.options.Slack.Revoke(hookCtx, token); revokeErr != nil {
			h.options.Logger.WarnContext(hookCtx, "slack token revoke failed",
				"tenant_id", tenantID, "integration_id", integrationID, "error", revokeErr)
		}
		return nil
	})
	return true
}

// auditSlackUninstall records the removal in the same transaction as the delete, so the row
// and its trail commit together. It reports whether the caller may continue.
func (h *HTTPHandler) auditSlackUninstall(
	w http.ResponseWriter, r *http.Request, query db.Querier, tenantID, integrationID string,
) bool {
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return false
	}
	if err := accessaudit.Record(r.Context(), query, accessaudit.RecordInput{
		TenantID: tenantID, Kind: AuditKindUninstall, Actor: accessaudit.ActorFromClaims(claims),
		ResourceType: AuditResourceType, ResourceID: integrationID, Action: AuditActionUninstal,
		PermissionPath: string(auth.PermissionIntegrationDelete),
		RequestID:      appmiddleware.RequestIDFromContext(r.Context()),
		Outcome:        accessaudit.OutcomeSuccess,
		Data:           map[string]any{"kind": string(KindSlack)},
	}); err != nil {
		h.options.Logger.ErrorContext(r.Context(), "slack uninstall audit failed", "error", err)
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return false
	}
	return true
}
