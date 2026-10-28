package issue

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/activity"
	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/moderation"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/fidibeki/app.fidibeki/backend/internal/triage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type HTTPOptions struct {
	Store Store
	JWT   *auth.JWTManager
	Now   func() time.Time
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
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIssueRead))...).
		Get("/v1/issues", handler.list)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIssueRead))...).
		Get("/v1/issues/{issueID}", handler.get)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIssueUpdate))...).
		Post("/v1/issues/{issueID}/comments", handler.createComment)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIssueUpdate))...).
		Patch("/v1/issues/{issueID}/reporter-access", handler.setReporterAccess)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIssueRead))...).
		Get("/v1/notifications", handler.listNotifications)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIssueRead))...).
		Patch("/v1/notifications/{notificationID}/read", handler.markNotificationRead)
	createPermissions := append(protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionFeedbackUpdate),
		appmiddleware.RequirePermission(auth.PermissionIssueUpdate),
	)
	router.With(createPermissions...).Post("/v1/feedbacks/{feedbackID}/issue", handler.createFromFeedback)
	router.With(createPermissions...).Post("/v1/issues/from-feedback", handler.createFromFeedbackMany)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIssueUpdate))...).
		Patch("/v1/issues/{issueID}", handler.updateStatus)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIssueMerge))...).
		Post("/v1/issues/{issueID}/merge", handler.merge)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIssueMerge))...).
		Delete("/v1/issues/{issueID}/merge", handler.unmerge)
}

func (h *HTTPHandler) createFromFeedback(w http.ResponseWriter, r *http.Request) {
	feedbackID := chi.URLParam(r, "feedbackID")
	if _, err := uuid.Parse(feedbackID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	var request createFromFeedbackRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.FeedbackIDs != nil {
		writeValidation(w, map[string]string{"feedback_ids": "is only valid on /v1/issues/from-feedback"})
		return
	}
	h.createIssueFromFeedback(w, r, request, []string{feedbackID})
}

func (h *HTTPHandler) createFromFeedbackMany(w http.ResponseWriter, r *http.Request) {
	var request createFromFeedbackRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	h.createIssueFromFeedback(w, r, request, request.FeedbackIDs)
}

func (h *HTTPHandler) createIssueFromFeedback(
	w http.ResponseWriter,
	r *http.Request,
	request createFromFeedbackRequest,
	feedbackIDs []string,
) {
	input, fields := validateCreateFromFeedback(request, feedbackIDs)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	created, err := h.options.Store.CreateFromFeedback(
		r.Context(), query, resolved.ID, resolved.Slug, claims.Subject, input, now,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/issues/"+created.ID)
	api.WriteJSON(w, http.StatusCreated, map[string]any{"issue": created})
}

type createFromFeedbackRequest struct {
	FeedbackIDs []string  `json:"feedback_ids"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Priority    *string   `json:"priority"`
	AssigneeID  *string   `json:"assignee_id"`
	Unassign    bool      `json:"unassign"`
	TagIDs      *[]string `json:"tag_ids"`
}

func validateCreateFromFeedback(
	request createFromFeedbackRequest,
	feedbackIDs []string,
) (CreateFromFeedbackInput, map[string]string) {
	fields := make(map[string]string)
	input := CreateFromFeedbackInput{FeedbackIDs: validateIssueUUIDs("feedback_ids", feedbackIDs, fields)}
	if len(feedbackIDs) < 1 || len(feedbackIDs) > 100 {
		fields["feedback_ids"] = "must contain between 1 and 100 UUIDs"
	}
	input.Title = strings.TrimSpace(request.Title)
	input.Description = strings.TrimSpace(request.Description)
	if length := len([]rune(input.Title)); length < 1 || length > 80 {
		fields["title"] = "must be between 1 and 80 characters"
	}
	if len([]rune(input.Description)) > 10000 {
		fields["description"] = "must be at most 10000 characters"
	}
	if request.Priority != nil {
		input.SetPriority = true
		if *request.Priority != "none" {
			priority := triage.Priority(*request.Priority)
			if !priority.Valid() {
				fields["priority"] = "must be low, medium, high, urgent, or none"
			} else {
				input.Priority = &priority
			}
		}
	}
	if request.AssigneeID != nil && request.Unassign {
		fields["assignee_id"] = "cannot be combined with unassign"
	} else if request.AssigneeID != nil {
		if _, err := uuid.Parse(*request.AssigneeID); err != nil {
			fields["assignee_id"] = "must be a UUID"
		} else {
			input.SetAssignee = true
			input.AssigneeID = request.AssigneeID
		}
	} else if request.Unassign {
		input.SetAssignee = true
	}
	if request.TagIDs != nil {
		input.SetTags = true
		if len(*request.TagIDs) > 100 {
			fields["tag_ids"] = "must contain at most 100 UUIDs"
		}
		input.TagIDs = validateIssueUUIDs("tag_ids", *request.TagIDs, fields)
	}
	return input, fields
}

func validateIssueUUIDs(field string, values []string, fields map[string]string) []string {
	seen := make(map[string]struct{}, len(values))
	validated := make([]string, 0, len(values))
	for _, value := range values {
		if _, err := uuid.Parse(value); err != nil {
			fields[field] = "must contain only UUIDs"
			continue
		}
		if _, exists := seen[value]; exists {
			fields[field] = "must contain unique UUIDs"
			continue
		}
		seen[value] = struct{}{}
		validated = append(validated, value)
	}
	return validated
}

func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	issues, err := h.options.Store.List(r.Context(), query, resolved.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

func (h *HTTPHandler) get(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	found, err := h.options.Store.Get(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	updates, err := h.options.Store.ListUpdates(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	comments, err := h.options.Store.ListComments(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	activities, err := activity.ListIssue(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	sources, err := h.options.Store.ModeratedFeedbackSources(
		r.Context(), query, resolved.ID, issueID,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	moderateIssueActivities(activities, found, sources)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"issue": found, "updates": updates, "comments": comments, "activities": activities,
	})
}

func moderateIssueActivities(events []activity.Event, found Issue, sources []string) {
	if len(sources) == 0 {
		return
	}
	for index := range events {
		if value, exists := events[index].Data["title"].(string); exists &&
			moderation.IsDerived(value, sources...) {
			events[index].Data["title"] = found.Title
		}
		if value, exists := events[index].Data["description"].(string); exists &&
			moderation.IsDerived(value, sources...) {
			events[index].Data["description"] = found.Description
		}
		changes, exists := events[index].Data["changes"].(map[string]any)
		if !exists {
			continue
		}
		for _, field := range []string{"title", "description"} {
			change, exists := changes[field].(map[string]any)
			if !exists {
				continue
			}
			for _, side := range []string{"from", "to"} {
				value, exists := change[side].(string)
				if exists {
					change[side] = moderation.RedactDerived(value, sources...)
				}
			}
		}
	}
}

func (h *HTTPHandler) createComment(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	var request struct {
		Body             string   `json:"body"`
		MentionMemberIDs []string `json:"mention_member_ids"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	fields := make(map[string]string)
	body := strings.TrimSpace(request.Body)
	if length := len([]rune(body)); length < 1 || length > 5000 {
		fields["body"] = "must be between 1 and 5000 characters"
	}
	if len(request.MentionMemberIDs) > 20 {
		fields["mention_member_ids"] = "must contain at most 20 UUIDs"
	}
	mentionMemberIDs := validateIssueUUIDs("mention_member_ids", request.MentionMemberIDs, fields)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	comment, event, err := h.options.Store.CreateComment(
		r.Context(), query, resolved.ID, issueID, claims.Subject,
		body, mentionMemberIDs, h.now(),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"comment": comment, "activity": event})
}

func (h *HTTPHandler) setReporterAccess(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	updated, err := h.options.Store.SetReporterAccess(
		r.Context(), query, resolved.ID, issueID, claims.Subject, request.Enabled, h.now(),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue": updated})
}

func (h *HTTPHandler) listNotifications(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	notifications, unread, err := h.options.Store.ListMemberNotifications(
		r.Context(), query, resolved.ID, claims.Subject,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"notifications": notifications, "unread_count": unread,
	})
}

func (h *HTTPHandler) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	notificationID := chi.URLParam(r, "notificationID")
	if _, err := uuid.Parse(notificationID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	found, err := h.options.Store.MarkMemberNotificationRead(
		r.Context(), query, resolved.ID, claims.Subject, notificationID, h.now(),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !found {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"read": true})
}

func (h *HTTPHandler) updateStatus(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	var request struct {
		Status          *Status  `json:"status"`
		Comment         *string  `json:"comment"`
		ReporterMessage *string  `json:"reporter_message"`
		Title           *string  `json:"title"`
		Description     *string  `json:"description"`
		Priority        *string  `json:"priority"`
		AssigneeID      *string  `json:"assignee_id"`
		Unassign        bool     `json:"unassign"`
		AddTagIDs       []string `json:"add_tag_ids"`
		RemoveTagIDs    []string `json:"remove_tag_ids"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	fields := make(map[string]string)
	mutation := Mutation{Status: request.Status}
	if request.Status != nil && !request.Status.Valid() {
		fields["status"] = "must be open, in_progress, needs_info, resolved, or closed"
	}
	if request.Comment != nil {
		trimmed := strings.TrimSpace(*request.Comment)
		if len(trimmed) > 2000 {
			fields["comment"] = "must be at most 2000 characters"
		} else if request.Status == nil {
			fields["comment"] = "requires a status transition"
		} else if trimmed != "" {
			mutation.Comment = &trimmed
		}
	}
	if request.ReporterMessage != nil {
		trimmed := strings.TrimSpace(*request.ReporterMessage)
		if request.Status == nil || *request.Status != StatusNeedsInfo {
			fields["reporter_message"] = "is only valid when status is needs_info"
		} else if length := len([]rune(trimmed)); length < 1 || length > 2000 {
			fields["reporter_message"] = "must be between 1 and 2000 characters"
		} else {
			mutation.ReporterMessage = &trimmed
		}
	} else if request.Status != nil && *request.Status == StatusNeedsInfo {
		fields["reporter_message"] = "is required when status is needs_info"
	}
	if request.Title != nil {
		trimmed := strings.TrimSpace(*request.Title)
		if length := len([]rune(trimmed)); length < 1 || length > 80 {
			fields["title"] = "must be between 1 and 80 characters"
		} else {
			mutation.Title = &trimmed
		}
	}
	if request.Description != nil {
		trimmed := strings.TrimSpace(*request.Description)
		if len([]rune(trimmed)) > 10000 {
			fields["description"] = "must be at most 10000 characters"
		} else {
			mutation.Description = &trimmed
		}
	}
	if request.Priority != nil {
		mutation.SetPriority = true
		if *request.Priority != "none" {
			priority := triage.Priority(*request.Priority)
			if !priority.Valid() {
				fields["priority"] = "must be low, medium, high, urgent, or none"
			} else {
				mutation.Priority = &priority
			}
		}
	}
	if request.AssigneeID != nil && request.Unassign {
		fields["assignee_id"] = "cannot be combined with unassign"
	} else if request.AssigneeID != nil {
		if _, err := uuid.Parse(*request.AssigneeID); err != nil {
			fields["assignee_id"] = "must be a UUID"
		} else {
			mutation.SetAssignee = true
			mutation.AssigneeID = request.AssigneeID
		}
	} else if request.Unassign {
		mutation.SetAssignee = true
	}
	if len(request.AddTagIDs) > 100 {
		fields["add_tag_ids"] = "must contain at most 100 UUIDs"
	}
	if len(request.RemoveTagIDs) > 100 {
		fields["remove_tag_ids"] = "must contain at most 100 UUIDs"
	}
	mutation.AddTagIDs = validateIssueUUIDs("add_tag_ids", request.AddTagIDs, fields)
	mutation.RemoveTagIDs = validateIssueUUIDs("remove_tag_ids", request.RemoveTagIDs, fields)
	removeSet := make(map[string]struct{}, len(mutation.RemoveTagIDs))
	for _, tagID := range mutation.RemoveTagIDs {
		removeSet[tagID] = struct{}{}
	}
	for _, tagID := range mutation.AddTagIDs {
		if _, exists := removeSet[tagID]; exists {
			fields["add_tag_ids"] = "must be disjoint from remove_tag_ids"
			break
		}
	}
	if mutation.Status == nil && !mutation.HasMetadata() {
		fields["request"] = "must include at least one operation"
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	found, update, err := h.options.Store.UpdateIssue(
		r.Context(), query, resolved.ID, issueID, claims.Subject,
		mutation, now,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue": found, "update": update})
}

func (h *HTTPHandler) merge(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	var request struct {
		TargetIssueID string `json:"target_issue_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if _, err := uuid.Parse(request.TargetIssueID); err != nil {
		writeValidation(w, map[string]string{"target_issue_id": "must be a UUID"})
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	found, err := h.options.Store.Merge(
		r.Context(), query, resolved.ID, issueID, request.TargetIssueID, claims.Subject, now,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue": found})
}

func (h *HTTPHandler) unmerge(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	found, err := h.options.Store.Unmerge(
		r.Context(), query, resolved.ID, issueID, claims.Subject, h.now(),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue": found})
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

func (h *HTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

func issuePathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := chi.URLParam(r, "issueID")
	if _, err := uuid.Parse(value); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return "", false
	}
	return value, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body must contain one JSON value", nil)
		return false
	}
	return true
}

func writeValidation(w http.ResponseWriter, fields map[string]string) {
	api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "request validation failed", map[string]any{
		"fields": fields,
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	case errors.Is(err, ErrNoStatusChange):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "issue already has that status", nil)
	case errors.Is(err, ErrInvalidMerge):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "issues cannot be merged", nil)
	case errors.Is(err, ErrFeedbackLinked):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "feedback is already linked to an issue", nil)
	case errors.Is(err, ErrProjectMismatch):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "feedback projects do not match", nil)
	case errors.Is(err, ErrReporterAccessUnavailable):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "reporter progress access is unavailable", nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}
