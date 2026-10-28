package issuetracker

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/accessaudit"
	"github.com/fidibeki/app.fidibeki/backend/internal/activity"
	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type HTTPOptions struct {
	Store             Store
	JWT               *auth.JWTManager
	Clients           ClientFactory
	AllowInsecureURLs bool
	Now               func() time.Time
}

type HTTPHandler struct {
	options HTTPOptions
}

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIntegrationRead))...).
		Get("/v1/issue-trackers", handler.listConnections)
	router.With(append(protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionIntegrationCreate))...).
		Put("/v1/issue-trackers/{provider}", handler.connect)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIntegrationUpdate))...).
		Get("/v1/issue-trackers/{provider}/destinations", handler.listDestinations)
	router.With(append(protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionIntegrationUpdate))...).
		Patch("/v1/issue-trackers/{provider}", handler.configure)
	router.With(append(protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionIntegrationDelete))...).
		Delete("/v1/issue-trackers/{provider}", handler.disconnect)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIssueRead))...).
		Get("/v1/issues/{issueID}/external", handler.listLinks)
	router.With(append(protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionIssueUpdate))...).
		Post("/v1/issues/{issueID}/external", handler.create)
	router.With(append(protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionIssueUpdate))...).
		Post("/v1/issues/{issueID}/external/{linkID}/retry", handler.retry)
}

func (h *HTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *HTTPHandler) listConnections(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	connections, err := h.options.Store.ListConnections(r.Context(), query, resolved.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"issue_trackers":  connections,
		"providers":       Providers(),
		"plan_entitled":   config.CanUseFeature(resolved, config.FeatureSlack),
		"linear_endpoint": LinearEndpoint,
	})
}

func (h *HTTPHandler) connect(w http.ResponseWriter, r *http.Request) {
	provider, ok := providerParam(w, r)
	if !ok {
		return
	}
	resolved, query, stateOK := requestState(w, r)
	if !stateOK || !requirePlan(w, resolved) {
		return
	}
	var input ConnectInput
	if !decodeJSON(w, r, &input) {
		return
	}
	credentials, fields := ValidateConnect(provider, input, h.options.AllowInsecureURLs)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	client, err := h.options.Clients(provider)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	account, err := client.Validate(r.Context(), credentials)
	if err != nil {
		writeProviderError(w, err, nil)
		return
	}
	connection, err := h.options.Store.Connect(r.Context(), query, resolved.ID, ConnectValues{
		Provider: provider, SiteURL: credentials.SiteURL, AccountLabel: account.Label,
		Email: credentials.Email, APIToken: credentials.APIToken, ValidatedAt: h.now(),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !h.audit(w, r, resolved.ID, query, connection, AuditKindConnected, AuditActionConnect, nil) {
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue_tracker": connection})
}

func (h *HTTPHandler) listDestinations(w http.ResponseWriter, r *http.Request) {
	provider, ok := providerParam(w, r)
	if !ok {
		return
	}
	resolved, query, stateOK := requestState(w, r)
	if !stateOK || !requirePlan(w, resolved) {
		return
	}
	credentials, err := h.options.Store.Credentials(r.Context(), query, resolved.ID, provider)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	client, err := h.options.Clients(provider)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	destinations, err := client.Destinations(r.Context(), credentials)
	if err != nil {
		writeProviderError(w, err, nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"destinations": destinations})
}

func (h *HTTPHandler) configure(w http.ResponseWriter, r *http.Request) {
	provider, ok := providerParam(w, r)
	if !ok {
		return
	}
	resolved, query, stateOK := requestState(w, r)
	if !stateOK || !requirePlan(w, resolved) {
		return
	}
	var input ConfigureInput
	if !decodeJSON(w, r, &input) {
		return
	}
	current, err := h.options.Store.GetConnection(r.Context(), query, resolved.ID, provider)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	values := ConfigureValues{
		DestinationID: current.DestinationID, DestinationKey: current.DestinationKey,
		DestinationName: current.DestinationName, IssueTypeID: current.IssueTypeID,
		IssueTypeName: current.IssueTypeName, SubDestinationID: current.SubDestinationID,
		SubDestinationName: current.SubDestinationName, Enabled: current.Enabled,
		AutoCreate: current.AutoCreate,
	}
	if current.ValidatedAt != nil {
		values.ValidatedAt = *current.ValidatedAt
	}
	selectionChanged := false
	if input.DestinationID != nil && *input.DestinationID != current.DestinationID {
		values.DestinationID = *input.DestinationID
		values.SubDestinationID = ""
		values.SubDestinationName = ""
		values.IssueTypeID = ""
		values.IssueTypeName = ""
		selectionChanged = true
	}
	if input.IssueTypeID != nil && *input.IssueTypeID != values.IssueTypeID {
		values.IssueTypeID = *input.IssueTypeID
		selectionChanged = true
	}
	if input.SubDestinationID != nil && *input.SubDestinationID != values.SubDestinationID {
		values.SubDestinationID = *input.SubDestinationID
		selectionChanged = true
	}
	if input.Enabled != nil {
		values.Enabled = *input.Enabled
	}
	if input.AutoCreate != nil {
		values.AutoCreate = *input.AutoCreate
		if values.AutoCreate && !values.Enabled {
			writeValidation(w, []FieldError{{
				Field: "auto_create", Message: "requires the provider to be enabled",
			}})
			return
		}
	}
	// Switching a provider off also stops automatic creation rather than leaving a flag armed
	// for the next time it is switched on.
	if !values.Enabled {
		values.AutoCreate = false
	}
	enabling := input.Enabled != nil && *input.Enabled && !current.Enabled
	// The destination is re-resolved against the live provider whenever the selection changes or
	// the provider is being switched on, so "enabled" always means "verified to work".
	if selectionChanged || enabling {
		if values.DestinationID == "" {
			writeValidation(w, []FieldError{{
				Field: "destination_id", Message: "must be selected before enabling this provider",
			}})
			return
		}
		credentials, credentialErr := h.options.Store.Credentials(r.Context(), query, resolved.ID, provider)
		if credentialErr != nil {
			writeStoreError(w, credentialErr)
			return
		}
		client, clientErr := h.options.Clients(provider)
		if clientErr != nil {
			api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
			return
		}
		destinations, destinationErr := client.Destinations(r.Context(), credentials)
		if destinationErr != nil {
			writeProviderError(w, destinationErr, nil)
			return
		}
		resolvedValues, fields := resolveDestination(provider, destinations, values)
		if len(fields) > 0 {
			writeValidation(w, fields)
			return
		}
		resolvedValues.ValidatedAt = h.now()
		values = resolvedValues
	}
	updated, err := h.options.Store.Configure(r.Context(), query, resolved.ID, provider, values)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	kind, action := AuditKindConfigured, AuditActionConfigure
	switch {
	case updated.Enabled && !current.Enabled:
		kind, action = AuditKindEnabled, AuditActionEnable
	case !updated.Enabled && current.Enabled:
		kind, action = AuditKindDisabled, AuditActionDisable
	case updated.AutoCreate != current.AutoCreate:
		kind, action = AuditKindAutoCreate, AuditActionAutomate
	}
	if !h.audit(w, r, resolved.ID, query, updated, kind, action,
		map[string]any{"auto_create": updated.AutoCreate}) {
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue_tracker": updated})
}

// resolveDestination checks the selected destination against what the provider actually
// exposes and fills in the display names stored alongside the identifiers.
func resolveDestination(
	provider Provider,
	destinations []Destination,
	values ConfigureValues,
) (ConfigureValues, []FieldError) {
	for _, destination := range destinations {
		if destination.ID != values.DestinationID {
			continue
		}
		values.DestinationKey = destination.Key
		values.DestinationName = destination.Name
		if provider == ProviderJira {
			if values.IssueTypeID == "" {
				return values, []FieldError{{
					Field: "issue_type_id", Message: "must be selected for Jira",
				}}
			}
			for _, issueType := range destination.IssueTypes {
				if issueType.ID == values.IssueTypeID {
					values.IssueTypeName = issueType.Name
					return values, nil
				}
			}
			return values, []FieldError{{
				Field: "issue_type_id", Message: "is not available on the selected project",
			}}
		}
		if values.SubDestinationID == "" {
			values.SubDestinationName = ""
			return values, nil
		}
		for _, project := range destination.Projects {
			if project.ID == values.SubDestinationID {
				values.SubDestinationName = project.Name
				return values, nil
			}
		}
		return values, []FieldError{{
			Field: "sub_destination_id", Message: "is not available on the selected team",
		}}
	}
	return values, []FieldError{{
		Field: "destination_id", Message: "is not available for the connected account",
	}}
}

func (h *HTTPHandler) disconnect(w http.ResponseWriter, r *http.Request) {
	provider, ok := providerParam(w, r)
	if !ok {
		return
	}
	resolved, query, stateOK := requestState(w, r)
	if !stateOK {
		return
	}
	connectionID, err := h.options.Store.Disconnect(r.Context(), query, resolved.ID, provider)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !h.audit(w, r, resolved.ID, query,
		Connection{ID: connectionID, Provider: provider}, AuditKindDisconnect, AuditActionDisconn, nil) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) listLinks(w http.ResponseWriter, r *http.Request) {
	issueID, ok := uuidParam(w, r, "issueID")
	if !ok {
		return
	}
	resolved, query, stateOK := requestState(w, r)
	if !stateOK {
		return
	}
	if _, err := h.options.Store.LoadIssue(r.Context(), query, resolved.ID, issueID); err != nil {
		writeStoreError(w, err)
		return
	}
	links, err := h.options.Store.ListLinks(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Only providers a workspace actually switched on are advertised, so the creation controls
	// stay invisible everywhere else.
	available := make([]Connection, 0, len(Providers()))
	if config.CanUseFeature(resolved, config.FeatureSlack) {
		connections, connectionErr := h.options.Store.ListConnections(r.Context(), query, resolved.ID)
		if connectionErr != nil {
			writeStoreError(w, connectionErr)
			return
		}
		for _, connection := range connections {
			if connection.Enabled && connection.Configured() {
				available = append(available, connection)
			}
		}
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"external_links": links, "available_providers": available,
	})
}

func (h *HTTPHandler) create(w http.ResponseWriter, r *http.Request) {
	issueID, ok := uuidParam(w, r, "issueID")
	if !ok {
		return
	}
	resolved, query, stateOK := requestState(w, r)
	if !stateOK || !requirePlan(w, resolved) {
		return
	}
	var input CreateInput
	if !decodeJSON(w, r, &input) {
		return
	}
	issue, err := h.options.Store.LoadIssue(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	normalized, fields := ValidateCreate(input, issue)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	connection, ok := h.enabledConnection(w, r, query, resolved, normalized.Provider)
	if !ok {
		return
	}
	if normalized.IdempotencyKey == "" {
		normalized.IdempotencyKey = DeriveIdempotencyKey(issueID, normalized)
	}
	memberID := h.memberID(r)
	values := PendingLinkValues{
		IssueID: issueID, Provider: normalized.Provider, IdempotencyKey: normalized.IdempotencyKey,
		Title: normalized.Title, Description: normalized.Description, Priority: normalized.Priority,
		DestinationID: connection.DestinationID, DestinationName: connection.DestinationName,
		IssueTypeID: connection.IssueTypeID, MemberID: memberID,
	}
	link, reuse, ok := h.resolveExistingAttempt(w, r, query, resolved.ID, values)
	if !ok {
		return
	}
	if !reuse {
		created, err := h.options.Store.BeginLink(r.Context(), query, resolved.ID, values)
		if err != nil {
			// Losing the race against a concurrent request is the unique index doing its job.
			// The transaction cannot be queried after the violation, so the caller is told to
			// re-read the issue rather than being handed a possibly stale row.
			api.WriteError(w, http.StatusConflict, api.CodeConflict,
				"an external issue is already being created for this issue", nil)
			return
		}
		link = created
	}
	h.dispatch(w, r, query, resolved, connection, issue, link, memberID, http.StatusCreated)
}

// resolveExistingAttempt answers a repeated request from the attempt that already exists.
// It runs before the insert because a unique violation aborts the surrounding transaction,
// which would leave the handler unable to read anything back.
func (h *HTTPHandler) resolveExistingAttempt(
	w http.ResponseWriter,
	r *http.Request,
	query db.Querier,
	tenantID string,
	values PendingLinkValues,
) (ExternalLink, bool, bool) {
	live, err := h.options.Store.FindLiveLink(
		r.Context(), query, tenantID, values.IssueID, values.Provider,
	)
	switch {
	case err == nil && live.Status == LinkStatusCreated:
		// The external issue already exists, so the repeated request is simply satisfied.
		api.WriteJSON(w, http.StatusOK, map[string]any{"external_link": live})
		return ExternalLink{}, false, false
	case err == nil:
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"an external issue is already being created for this issue",
			map[string]any{"external_link": live})
		return ExternalLink{}, false, false
	case !errors.Is(err, ErrNotFound):
		writeStoreError(w, err)
		return ExternalLink{}, false, false
	}
	prior, err := h.options.Store.FindLinkByIdempotencyKey(
		r.Context(), query, tenantID, values.IdempotencyKey,
	)
	if errors.Is(err, ErrNotFound) {
		return ExternalLink{}, false, true
	}
	if err != nil {
		writeStoreError(w, err)
		return ExternalLink{}, false, false
	}
	if prior.IssueID != values.IssueID || prior.Provider != values.Provider {
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"this idempotency key was already used for a different issue or provider", nil)
		return ExternalLink{}, false, false
	}
	// The same reviewed request failed earlier, so it retries that attempt instead of opening
	// a second one under a key the provider may already have seen.
	retried, err := h.options.Store.RetryLink(
		r.Context(), query, tenantID, prior.IssueID, prior.ID, values,
	)
	if err != nil {
		writeStoreError(w, err)
		return ExternalLink{}, false, false
	}
	return retried, true, true
}

func (h *HTTPHandler) retry(w http.ResponseWriter, r *http.Request) {
	issueID, ok := uuidParam(w, r, "issueID")
	if !ok {
		return
	}
	linkID, ok := uuidParam(w, r, "linkID")
	if !ok {
		return
	}
	resolved, query, stateOK := requestState(w, r)
	if !stateOK || !requirePlan(w, resolved) {
		return
	}
	issue, err := h.options.Store.LoadIssue(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	existing, err := h.options.Store.GetLink(r.Context(), query, resolved.ID, issueID, linkID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	connection, ok := h.enabledConnection(w, r, query, resolved, existing.Provider)
	if !ok {
		return
	}
	// The retry reuses the stored idempotency key, so a provider that silently succeeded on the
	// first attempt cannot be asked to create a second issue by a second row.
	link, err := h.options.Store.RetryLink(r.Context(), query, resolved.ID, issueID, linkID,
		PendingLinkValues{
			DestinationID: connection.DestinationID, DestinationName: connection.DestinationName,
			IssueTypeID: connection.IssueTypeID,
		})
	if errors.Is(err, ErrLinkNotRetryable) {
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"only a failed external issue attempt can be retried",
			map[string]any{"external_link": existing})
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.dispatch(w, r, query, resolved, connection, issue, link, h.memberID(r), http.StatusOK)
}

// dispatch performs the provider call and records the outcome. The Fidibeki issue is never
// modified here: a failure only marks the attempt and is committed so the retry path survives.
func (h *HTTPHandler) dispatch(
	w http.ResponseWriter,
	r *http.Request,
	query db.Querier,
	resolved tenant.Tenant,
	connection Connection,
	issue IssueSnapshot,
	link ExternalLink,
	memberID *string,
	successStatus int,
) {
	credentials, err := h.options.Store.Credentials(r.Context(), query, resolved.ID, connection.Provider)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	client, err := h.options.Clients(connection.Provider)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	result, providerErr := client.CreateIssue(r.Context(), credentials, CreateRequest{
		Title: link.RequestTitle, Description: link.RequestDescription, Priority: link.RequestPriority,
		DestinationID: connection.DestinationID, IssueTypeID: connection.IssueTypeID,
		SubDestination: connection.SubDestinationID, IdempotencyKey: link.ID,
	})
	if providerErr != nil {
		h.recordFailure(w, r, query, resolved.ID, issue, link, memberID, providerErr)
		return
	}
	completed, err := h.options.Store.CompleteLink(r.Context(), query, resolved.ID, link.ID, result)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := activity.Record(r.Context(), query, activity.RecordInput{
		TenantID: resolved.ID, IssueID: issue.ID, ActorMemberID: memberID,
		Kind: ActivityExternalIssueCreated, Data: map[string]any{
			"provider": string(completed.Provider), "external_key": completed.ExternalKey,
			"external_url": completed.ExternalURL, "destination": completed.DestinationName,
		}, CreatedAt: h.now(),
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, successStatus, map[string]any{"external_link": completed})
}

// recordFailure persists the failed attempt, the actionable error, and the timeline entry, then
// forces the transaction to commit despite the error status so the workspace can retry.
func (h *HTTPHandler) recordFailure(
	w http.ResponseWriter,
	r *http.Request,
	query db.Querier,
	tenantID string,
	issue IssueSnapshot,
	link ExternalLink,
	memberID *string,
	providerErr error,
) {
	message := providerErr.Error()
	code := ErrorCodeRejected
	var failure *ProviderError
	if errors.As(providerErr, &failure) {
		message = failure.Message
		code = failure.Code
	}
	failed, err := h.options.Store.FailLink(r.Context(), query, tenantID, link.ID, message)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := h.options.Store.RecordConnectionError(
		r.Context(), query, tenantID, link.Provider, message,
	); err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := activity.Record(r.Context(), query, activity.RecordInput{
		TenantID: tenantID, IssueID: issue.ID, ActorMemberID: memberID,
		Kind: ActivityExternalIssueFailed, Data: map[string]any{
			"provider": string(link.Provider), "error_code": code, "attempt": failed.AttemptCount,
		}, CreatedAt: h.now(),
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	appmiddleware.CommitTenantTransactionOnError(r.Context())
	writeProviderError(w, providerErr, map[string]any{"external_link": failed})
}

func (h *HTTPHandler) enabledConnection(
	w http.ResponseWriter,
	r *http.Request,
	query db.Querier,
	resolved tenant.Tenant,
	provider Provider,
) (Connection, bool) {
	if !provider.Valid() {
		writeValidation(w, []FieldError{{Field: "provider", Message: "must be jira or linear"}})
		return Connection{}, false
	}
	connection, err := h.options.Store.GetConnection(r.Context(), query, resolved.ID, provider)
	if err != nil {
		writeStoreError(w, err)
		return Connection{}, false
	}
	if !connection.Enabled || !connection.Configured() {
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"this provider is not enabled for the workspace", nil)
		return Connection{}, false
	}
	return connection, true
}

func (h *HTTPHandler) memberID(r *http.Request) *string {
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok || claims.IsAPIKey || claims.Subject == "" {
		return nil
	}
	subject := claims.Subject
	return &subject
}

// audit records a configuration change in the workspace access ledger. A failure to record is
// treated as a failure of the change itself, matching the rest of the audited surfaces.
func (h *HTTPHandler) audit(
	w http.ResponseWriter,
	r *http.Request,
	tenantID string,
	query db.Querier,
	connection Connection,
	kind string,
	action string,
	extra map[string]any,
) bool {
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return false
	}
	data := map[string]any{"provider": string(connection.Provider)}
	if connection.DestinationKey != "" {
		data["destination"] = connection.DestinationKey
	}
	maps.Copy(data, extra)
	permission := string(auth.PermissionIntegrationUpdate)
	if action == AuditActionConnect {
		permission = string(auth.PermissionIntegrationCreate)
	}
	if action == AuditActionDisconn {
		permission = string(auth.PermissionIntegrationDelete)
	}
	if err := accessaudit.Record(r.Context(), query, accessaudit.RecordInput{
		TenantID: tenantID, Kind: kind, Actor: accessaudit.ActorFromClaims(claims),
		ResourceType: AuditResourceType, ResourceID: connection.ID, Action: action,
		PermissionPath: permission, RequestID: appmiddleware.RequestIDFromContext(r.Context()),
		Outcome: accessaudit.OutcomeSuccess, Data: data, Now: h.now(),
	}); err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			"service unavailable", nil)
		return false
	}
	return true
}

func requestState(w http.ResponseWriter, r *http.Request) (tenant.Tenant, db.Querier, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return tenant.Tenant{}, nil, false
	}
	return resolved, query, true
}

func requirePlan(w http.ResponseWriter, resolved tenant.Tenant) bool {
	if !config.CanUseFeature(resolved, config.FeatureSlack) {
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanFeatureUnavailable,
			"issue tracker integrations are not available on this plan",
			map[string]string{"upgrade_url": "/v1/billing"})
		return false
	}
	return true
}

func providerParam(w http.ResponseWriter, r *http.Request) (Provider, bool) {
	provider := Provider(chi.URLParam(r, "provider"))
	if !provider.Valid() {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return "", false
	}
	return provider, true
}

func uuidParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := chi.URLParam(r, name)
	if _, err := uuid.Parse(value); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return "", false
	}
	return value, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			// An empty body is a valid "nothing to change" request.
			return true
		}
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest,
			"request body must contain one JSON value", nil)
		return false
	}
	return true
}

func writeValidation(w http.ResponseWriter, fields []FieldError) {
	api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
		"request validation failed", map[string]any{"fields": fields})
}

// writeProviderError maps a provider failure onto a status the client can act on: a
// configuration or credential problem the workspace must fix, or an upstream problem to retry.
func writeProviderError(w http.ResponseWriter, err error, details map[string]any) {
	failure := &ProviderError{Code: ErrorCodeRejected, Message: "the provider rejected the request"}
	if !errors.As(err, &failure) {
		api.WriteError(w, http.StatusBadGateway, api.ErrorCode(ErrorCodeRejected),
			"the provider rejected the request", details)
		return
	}
	status := http.StatusUnprocessableEntity
	if failure.Retryable {
		status = http.StatusBadGateway
	}
	payload := map[string]any{"provider_status": failure.Status, "retryable": failure.Retryable}
	maps.Copy(payload, details)
	api.WriteError(w, status, api.ErrorCode(failure.Code), failure.Message, payload)
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	case errors.Is(err, ErrNotConnected):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound,
			"this provider is not connected", nil)
	case errors.Is(err, ErrNotEnabled), errors.Is(err, ErrLinkExists), errors.Is(err, ErrLinkInFlight):
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"an external issue already exists for this issue", nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			"service unavailable", nil)
	}
}
