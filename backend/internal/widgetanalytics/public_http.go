package widgetanalytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/httpx"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
)

const maximumBodySize int64 = 64 << 10

type EventStore interface {
	InsertEvents(rctx context.Context, query db.Querier, events []EventValues) (IngestResult, error)
}

type PublicHTTPOptions struct {
	Authorizer        widgetapi.ProjectAuthorizer
	Begin             db.BeginFunc
	Store             EventStore
	Consent           ConsentAuthorizer
	Limiter           ratelimit.Limiter
	TrustedProxyCIDRs []netip.Prefix
	Logger            *slog.Logger
	Now               func() time.Time
}

type PublicHTTPHandler struct {
	options PublicHTTPOptions
	logger  *slog.Logger
}

type ingestRequest struct {
	Events []EventInput `json:"events"`
}

func RegisterPublicRoutes(router chi.Router, options PublicHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &PublicHTTPHandler{options: options, logger: logger}
	router.Post("/v1/widget/events", handler.ingest)
	router.Options("/v1/widget/events", handler.preflight)
	router.Post("/v1/widget/analytics-consents", handler.recordConsent)
	router.Options("/v1/widget/analytics-consents", handler.preflight)
	router.Post("/v1/widget/analytics-consents/revoke", handler.revokeConsent)
	router.Options("/v1/widget/analytics-consents/revoke", handler.preflight)
}

func (h *PublicHTTPHandler) preflight(w http.ResponseWriter, r *http.Request) {
	_, origin, ok := h.authorize(w, r)
	if !ok {
		return
	}
	widgetapi.SetCORSHeaders(w, origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+widgetapi.PublicKeyHeader)
	w.Header().Set("Access-Control-Max-Age", "300")
	w.WriteHeader(http.StatusNoContent)
}

func (h *PublicHTTPHandler) ingest(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorize(w, r)
	if !ok {
		return
	}
	widgetapi.SetCORSHeaders(w, origin)
	if configuration.DeletionRequestedAt != nil {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden,
			"tenant is pending deletion and is read-only", nil)
		return
	}
	var request ingestRequest
	if !decodeIngestRequest(w, r, &request) {
		return
	}
	if !h.allow(w, r, configuration.ProjectID, len(request.Events)) {
		return
	}
	values, fields := ValidateEvents(request.Events, configuration, origin, h.now())
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"widget analytics events are invalid", fields)
		return
	}
	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeStoreError(w, r, fmt.Errorf("begin widget analytics transaction: %w", err))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	var configuredTenant string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID).
		Scan(&configuredTenant); err != nil || configuredTenant != configuration.TenantID {
		h.writeStoreError(w, r, errors.New("configure widget analytics tenant context"))
		return
	}
	ok, shouldCommit := h.authorizeEventConsent(w, r, tx, configuration, values)
	if !ok {
		if shouldCommit {
			if err := tx.Commit(r.Context()); err != nil {
				h.writeStoreError(w, r, fmt.Errorf("commit widget analytics consent expiry: %w", err))
				return
			}
			committed = true
		}
		return
	}
	result, err := h.options.Store.InsertEvents(r.Context(), tx, values)
	var outOfOrder *MatchedEventRequiredError
	if errors.As(err, &outOfOrder) {
		// A funnel event whose "matched" opener never arrived is bad request data, not a failing
		// dependency. Reporting it as 5xx made the widget outbox classify the batch as retryable
		// and replay it every five seconds for as long as the events stayed within their 24-hour
		// retention window, so it must stay a non-retryable validation response.
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"widget analytics events are invalid", []map[string]string{{
				"field":   fmt.Sprintf("events[%d].event_type", outOfOrder.Index),
				"message": "must follow a matched event for the same rule, visitor, and page view",
			}})
		return
	}
	var conflicting *EventIDConflictError
	if errors.As(err, &conflicting) {
		// The ID is already taken by different content — a stored row that disagrees, another
		// tenant's event, or a blocked insert with no lifecycle row behind it. Resending cannot
		// change any of those, so this must stay outside the retryable 5xx range for exactly the
		// same reason MatchedEventRequiredError does.
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"widget analytics event conflicts with an existing event", []map[string]string{{
				"field":   fmt.Sprintf("events[%d].event_id", conflicting.Index),
				"message": "must not reuse the ID of a different event",
			}})
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if result.AcceptedEventIDs == nil {
		result.AcceptedEventIDs = []string{}
	}
	if result.DuplicateEventIDs == nil {
		result.DuplicateEventIDs = []string{}
	}
	if result.SuppressedEventIDs == nil {
		result.SuppressedEventIDs = []string{}
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit widget analytics transaction: %w", err))
		return
	}
	committed = true
	w.Header().Set("Cache-Control", "no-store")
	api.WriteJSON(w, http.StatusAccepted, result)
}

// authorizeEventConsent fail-closes the dedicated funnel-event endpoint: every event must
// identify an analytics-enabled rule and carry a valid, current, non-expired widget_analytics
// grant token scoped to that rule. The embedded "submitted" event is authorized separately by
// Store.InsertSubmittedEvent so missing or stale analytics evidence suppresses only measurement
// and never blocks the strictly necessary feedback submission flow.
// authorizeEventConsent returns (ok, shouldCommit). shouldCommit is true when a rejection was
// caused by AuthorizeConsent transitioning an expired grant to the "expired" state: that write
// must still be committed even though the ingest request itself is rejected, exactly like the
// equivalent session_replay expiry path in internal/replay.
func (h *PublicHTTPHandler) authorizeEventConsent(
	w http.ResponseWriter,
	r *http.Request,
	tx db.Transaction,
	configuration project.PublicConfiguration,
	values []EventValues,
) (bool, bool) {
	if h.options.Consent == nil {
		h.writeStoreError(w, r, errors.New("widget analytics consent store is not configured"))
		return false, false
	}
	rules := make(map[string]project.WidgetRule, len(configuration.Rules))
	for _, rule := range configuration.Rules {
		rules[rule.ID] = rule
	}
	now := h.now()
	authorized := make(map[string]struct{}, len(values))
	fields := make([]map[string]string, 0)
	shouldCommit := false
	for index, value := range values {
		rule, ruleExists := rules[value.RuleID]
		if !ruleExists || rule.AnalyticsConsent.Mode == project.AnalyticsConsentNone {
			fields = append(fields, map[string]string{
				"field":   fmt.Sprintf("events[%d].rule_id", index),
				"message": "must identify an analytics-enabled widget rule",
			})
			continue
		}
		if !validConsentTokenFormat(value.ConsentToken) {
			fields = append(fields, map[string]string{
				"field":   fmt.Sprintf("events[%d].consent_token", index),
				"message": "must be a 32-byte opaque token",
			})
			continue
		}
		cacheKey := value.RuleID + ":" + value.ConsentToken
		if _, ok := authorized[cacheKey]; ok {
			continue
		}
		_, err := h.options.Consent.AuthorizeConsent(
			r.Context(), tx, configuration.TenantID, configuration.ProjectID, value.RuleID,
			rule.AnalyticsConsent.NoticeVersion, string(rule.AnalyticsConsent.Mode),
			auth.HashToken(value.ConsentToken), now,
		)
		if err != nil {
			if errors.Is(err, ErrConsentExpired) {
				shouldCommit = true
			}
			fields = append(fields, map[string]string{
				"field":   fmt.Sprintf("events[%d].consent_token", index),
				"message": "must identify an active widget analytics consent grant",
			})
			continue
		}
		authorized[cacheKey] = struct{}{}
	}
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"widget analytics events are invalid", fields)
		return false, shouldCommit
	}
	return true, false
}

func (h *PublicHTTPHandler) authorize(
	w http.ResponseWriter,
	r *http.Request,
) (project.PublicConfiguration, string, bool) {
	headerKey := r.Header.Get(widgetapi.PublicKeyHeader)
	queryKey := r.URL.Query().Get("key")
	if headerKey != "" && queryKey != "" && headerKey != queryKey {
		writeAccessDenied(w)
		return project.PublicConfiguration{}, "", false
	}
	publicKey := headerKey
	if publicKey == "" {
		publicKey = queryKey
	}
	origin := r.Header.Get("Origin")
	if publicKey == "" || !widgetapi.ValidOrigin(origin) {
		writeAccessDenied(w)
		return project.PublicConfiguration{}, "", false
	}
	configuration, err := h.options.Authorizer.Authorize(r.Context(), publicKey, origin)
	if errors.Is(err, widgetapi.ErrAccessDenied) {
		writeAccessDenied(w)
		return project.PublicConfiguration{}, "", false
	}
	if err != nil {
		h.writeStoreError(w, r, fmt.Errorf("authorize widget analytics: %w", err))
		return project.PublicConfiguration{}, "", false
	}
	return configuration, origin, true
}

func decodeIngestRequest(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maximumBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeDecodeError(w, err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeDecodeError(w, err)
		return false
	}
	return true
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var maximum *http.MaxBytesError
	if errors.As(err, &maximum) {
		api.WriteError(w, http.StatusRequestEntityTooLarge, api.CodeBadRequest, "request body is too large", nil)
		return
	}
	api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "invalid JSON request body", nil)
}

func (h *PublicHTTPHandler) allow(w http.ResponseWriter, r *http.Request, projectID string, eventCount int) bool {
	const limit = 300
	window := ratelimit.Window{Limit: limit, Period: time.Minute}
	w.Header().Set("X-RateLimit-Limit", "300;w=60")
	if h.options.Limiter == nil {
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(limit))
		return true
	}
	cost := eventCount
	if cost < 1 {
		cost = 1
	}
	if cost > maximumBatchEvents {
		cost = maximumBatchEvents
	}
	key := "widget:analytics:ip:" + projectID + ":" + httpx.ClientIPFromRequest(r, h.options.TrustedProxyCIDRs)
	for range cost {
		decision, err := h.options.Limiter.Allow(r.Context(), key, window)
		if err != nil {
			h.logger.WarnContext(r.Context(), "widget analytics rate limiter unavailable; allowing request", "error", err)
			w.Header().Set("X-RateLimit-Remaining", "unknown")
			return true
		}
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
		if !decision.Allowed {
			retrySeconds := int((decision.RetryAfter + time.Second - 1) / time.Second)
			if retrySeconds < 1 {
				retrySeconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded,
				"rate limit exceeded", nil)
			return false
		}
	}
	return true
}

func (h *PublicHTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *PublicHTTPHandler) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "widget analytics request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func writeAccessDenied(w http.ResponseWriter) {
	api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "widget access denied", nil)
}
