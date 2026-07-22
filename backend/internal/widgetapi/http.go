package widgetapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/go-chi/chi/v5"
)

const PublicKeyHeader = "X-Fidibeki-Public-Key"

type ConfigurationStore interface {
	LoadPublicConfiguration(context.Context, db.Querier, string) (project.PublicConfiguration, error)
	MarkWidgetSeen(context.Context, db.Querier, string, time.Time) error
}

type HTTPOptions struct {
	Store     ConfigurationStore
	Database  db.Querier
	Logger    *slog.Logger
	Now       func() time.Time
	SentryDSN string
}

type HTTPHandler struct {
	options    HTTPOptions
	logger     *slog.Logger
	authorizer ProjectAuthorizer
}

type ConfigurationResponse struct {
	ProjectID           string              `json:"project_id"`
	Features            config.PlanFeatures `json:"features"`
	Rules               []PublicWidgetRule  `json:"rules"`
	RefreshAfterSeconds int                 `json:"refresh_after_seconds"`
	ErrorReportingDSN   string              `json:"error_reporting_dsn,omitempty"`
}

type PublicWidgetRule struct {
	ID                       string                              `json:"id"`
	Name                     string                              `json:"name"`
	Priority                 int                                 `json:"priority"`
	MatcherType              project.MatcherType                 `json:"matcher_type"`
	MatcherValue             *string                             `json:"matcher_value"`
	IncludeConditions        []project.WidgetRuleTargetCondition `json:"include_conditions"`
	ExcludeConditions        []project.WidgetRuleTargetCondition `json:"exclude_conditions"`
	TriggerType              project.TriggerType                 `json:"trigger_type"`
	TriggerDelaySeconds      *int                                `json:"trigger_delay_seconds"`
	TriggerScrollPercent     *int                                `json:"trigger_scroll_percent"`
	CanRate                  bool                                `json:"can_rate"`
	CanWriteText             bool                                `json:"can_write_text"`
	CanRecordAudio           bool                                `json:"can_record_audio"`
	CanRecordVideo           bool                                `json:"can_record_video"`
	CanAttachElement         bool                                `json:"can_attach_element"`
	CanAttachScreenshot      bool                                `json:"can_attach_screenshot"`
	Greeting                 *string                             `json:"greeting"`
	Position                 project.WidgetPosition              `json:"position"`
	Theme                    project.WidgetTheme                 `json:"theme"`
	AccentColor              *string                             `json:"accent_color"`
	ReplayMode               project.ReplayMode                  `json:"replay_mode"`
	ReplaySamplingRate       int                                 `json:"replay_sampling_rate"`
	ReplayMaxDurationSeconds int                                 `json:"replay_max_duration_seconds"`
	ReplayMaxEvents          int                                 `json:"replay_max_events"`
	ReplayMaskAllInputs      bool                                `json:"replay_mask_all_inputs"`
	ReplayMaskTextSelector   *string                             `json:"replay_mask_text_selector"`
	RecurrenceMode           project.RecurrenceMode              `json:"recurrence_mode"`
	RecurrenceDays           *int                                `json:"recurrence_days"`
}

func NewHTTPHandler(options HTTPOptions) http.Handler {
	router := chi.NewRouter()
	RegisterRoutes(router, options)
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	})
	return router
}

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &HTTPHandler{
		options: options,
		logger:  logger,
		authorizer: Authorizer{
			Store: options.Store, Database: options.Database, Now: options.Now,
		},
	}
	router.Get("/v1/widget/config", handler.configuration)
	router.Options("/v1/widget/config", handler.preflight)
	router.Post("/v1/widget/connected", handler.connected)
}

func (h *HTTPHandler) preflight(w http.ResponseWriter, r *http.Request) {
	publicKey := r.URL.Query().Get("key")
	configuration, origin, ok := h.authorize(w, r, publicKey)
	if !ok {
		return
	}
	_ = configuration
	SetCORSHeaders(w, origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", PublicKeyHeader)
	w.Header().Set("Access-Control-Max-Age", "300")
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) configuration(w http.ResponseWriter, r *http.Request) {
	publicKey := r.Header.Get(PublicKeyHeader)
	if queryKey := r.URL.Query().Get("key"); queryKey != "" && queryKey != publicKey {
		writeAccessDenied(w)
		return
	}
	configuration, origin, ok := h.authorize(w, r, publicKey)
	if !ok {
		return
	}
	definition, exists := config.PlanFor(configuration.Plan)
	if !exists {
		h.logger.ErrorContext(r.Context(), "public widget project has unknown plan", "project_id", configuration.ProjectID)
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	rules := make([]PublicWidgetRule, len(configuration.Rules))
	for index, rule := range configuration.Rules {
		rules[index] = publicWidgetRule(rule)
	}
	SetCORSHeaders(w, origin)
	w.Header().Set("Cache-Control", "no-store")
	api.WriteJSON(w, http.StatusOK, ConfigurationResponse{
		ProjectID: configuration.ProjectID, Features: definition.Features,
		Rules: rules, RefreshAfterSeconds: 300, ErrorReportingDSN: h.options.SentryDSN,
	})
}

func (h *HTTPHandler) connected(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorize(w, r, r.URL.Query().Get("key"))
	if !ok {
		return
	}
	if configuration.DeletionRequestedAt != nil {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "tenant is pending deletion and is read-only", nil)
		return
	}
	if _, exists := config.PlanFor(configuration.Plan); !exists {
		h.logger.ErrorContext(r.Context(), "widget connection project has unknown plan", "project_id", configuration.ProjectID)
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	ruleID := r.URL.Query().Get("rule_id")
	if !configurationHasRule(configuration, ruleID) {
		writeAccessDenied(w)
		return
	}
	seenAt := time.Now().UTC()
	if h.options.Now != nil {
		seenAt = h.options.Now().UTC()
	}
	if err := h.options.Store.MarkWidgetSeen(r.Context(), h.options.Database, configuration.ProjectID, seenAt); err != nil {
		h.logger.ErrorContext(r.Context(), "record widget activity failed", "project_id", configuration.ProjectID, "error", err)
	}
	SetCORSHeaders(w, origin)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func configurationHasRule(configuration project.PublicConfiguration, ruleID string) bool {
	if ruleID == "" {
		return false
	}
	for _, rule := range configuration.Rules {
		if rule.ID == ruleID {
			return true
		}
	}
	return false
}

func (h *HTTPHandler) authorize(
	w http.ResponseWriter,
	r *http.Request,
	publicKey string,
) (project.PublicConfiguration, string, bool) {
	origin := r.Header.Get("Origin")
	if publicKey == "" || !ValidOrigin(origin) {
		writeAccessDenied(w)
		return project.PublicConfiguration{}, "", false
	}
	configuration, err := h.authorizer.Authorize(r.Context(), publicKey, origin)
	if err == ErrAccessDenied {
		writeAccessDenied(w)
		return project.PublicConfiguration{}, "", false
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "load public widget configuration failed", "error", err)
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return project.PublicConfiguration{}, "", false
	}
	return configuration, origin, true
}

func ValidOrigin(origin string) bool {
	_, ok := originHostname(origin)
	return ok
}

func OriginAllowed(origin string, allowedDomains []string) bool {
	hostname, ok := originHostname(origin)
	if !ok {
		return false
	}
	for _, allowed := range allowedDomains {
		allowed = strings.ToLower(allowed)
		if strings.HasPrefix(allowed, "*.") {
			suffix := strings.TrimPrefix(allowed, "*")
			if strings.HasSuffix(hostname, suffix) && len(hostname) > len(suffix) {
				return true
			}
			continue
		}
		if hostname == allowed {
			return true
		}
	}
	return false
}

func originHostname(origin string) (string, bool) {
	if origin == "" || origin == "null" {
		return "", false
	}
	parsed, err := url.ParseRequestURI(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", false
		}
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || strings.HasSuffix(hostname, ".") {
		return "", false
	}
	return hostname, true
}

func SetCORSHeaders(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Add("Vary", "Origin")
}

func writeAccessDenied(w http.ResponseWriter) {
	api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "widget access denied", nil)
}

func publicWidgetRule(rule project.WidgetRule) PublicWidgetRule {
	return PublicWidgetRule{
		ID: rule.ID, Name: rule.Name, Priority: rule.Priority,
		MatcherType: rule.MatcherType, MatcherValue: rule.MatcherValue,
		IncludeConditions: rule.IncludeConditions, ExcludeConditions: rule.ExcludeConditions,
		TriggerType: rule.TriggerType, TriggerDelaySeconds: rule.TriggerDelaySeconds,
		TriggerScrollPercent: rule.TriggerScrollPercent,
		CanRate:              rule.CanRate, CanWriteText: rule.CanWriteText,
		CanRecordAudio: rule.CanRecordAudio, CanRecordVideo: rule.CanRecordVideo,
		CanAttachElement: rule.CanAttachElement, CanAttachScreenshot: rule.CanAttachScreenshot,
		Greeting: rule.Greeting, Position: rule.Position, Theme: rule.Theme, AccentColor: rule.AccentColor,
		ReplayMode: rule.ReplayMode, ReplaySamplingRate: rule.ReplaySamplingRate,
		ReplayMaxDurationSeconds: rule.ReplayMaxDurationSeconds, ReplayMaxEvents: rule.ReplayMaxEvents,
		ReplayMaskAllInputs: rule.ReplayMaskAllInputs, ReplayMaskTextSelector: rule.ReplayMaskTextSelector,
		RecurrenceMode: rule.RecurrenceMode, RecurrenceDays: rule.RecurrenceDays,
	}
}
