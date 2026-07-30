package feedback

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/triage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TenantReadStore interface {
	ListTenantFeedback(context.Context, db.Querier, string, ListOptions) ([]Item, error)
	GetTenantFeedback(context.Context, db.Querier, string, string) (Detail, error)
	UpdateTenantFeedback(context.Context, db.Querier, string, string, TenantMutation) (Detail, error)
	BulkUpdateTenantFeedback(context.Context, db.Querier, string, []string, TenantMutation) ([]Item, error)
	LinkTenantFeedback(context.Context, db.Querier, string, []string, string, string, time.Time) ([]Item, error)
	UnlinkTenantFeedback(context.Context, db.Querier, string, []string, string, time.Time) ([]Item, error)
	TenantDashboard(context.Context, db.Querier, string, string, int, time.Time) (Dashboard, error)
}

type TenantHTTPOptions struct {
	Store TenantReadStore
	JWT   *auth.JWTManager
	Now   func() time.Time
}

type TenantHTTPHandler struct {
	options TenantHTTPOptions
}

func RegisterTenantRoutes(router chi.Router, options TenantHTTPOptions) {
	handler := &TenantHTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(protected,
		appmiddleware.RequirePermission(auth.PermissionFeedbackRead),
		appmiddleware.RequirePermission(auth.PermissionIssueRead),
	)...).Get("/v1/dashboard", handler.dashboard)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionFeedbackRead))...).
		Get("/v1/feedbacks", handler.list)
	router.With(append(protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionFeedbackUpdate),
	)...).Patch("/v1/feedbacks/bulk", handler.bulkUpdate)
	linkPermissions := append(protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionFeedbackUpdate),
		appmiddleware.RequirePermission(auth.PermissionIssueUpdate),
	)
	router.With(linkPermissions...).Post("/v1/feedbacks/link", handler.link)
	router.With(linkPermissions...).Post("/v1/feedbacks/unlink", handler.unlink)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionFeedbackRead))...).
		Get("/v1/feedbacks/{feedbackID}", handler.get)
	router.With(append(protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionFeedbackUpdate),
	)...).Patch("/v1/feedbacks/{feedbackID}", handler.update)
}

func (h *TenantHTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	options, fields := parseTenantFeedbackOptions(r)
	if len(fields) > 0 {
		writeTenantValidation(w, fields)
		return
	}
	resolved, query, ok := tenantFeedbackRequestState(w, r)
	if !ok {
		return
	}
	items, err := h.options.Store.ListTenantFeedback(r.Context(), query, resolved.ID, options)
	if err != nil {
		writeTenantStoreError(w, err)
		return
	}
	hasMore := len(items) > options.Limit
	if hasMore {
		items = items[:options.Limit]
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		encoded, err := encodeTenantFeedbackCursor(items[len(items)-1], options.Sort)
		if err != nil {
			writeTenantStoreError(w, err)
			return
		}
		nextCursor = &encoded
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"feedbacks": items,
		"pagination": map[string]any{
			"next_cursor": nextCursor,
			"has_more":    hasMore,
			"limit":       options.Limit,
		},
	})
}

func (h *TenantHTTPHandler) get(w http.ResponseWriter, r *http.Request) {
	feedbackID, ok := tenantFeedbackPathID(w, r)
	if !ok {
		return
	}
	resolved, query, ok := tenantFeedbackRequestState(w, r)
	if !ok {
		return
	}
	found, err := h.options.Store.GetTenantFeedback(r.Context(), query, resolved.ID, feedbackID)
	if err != nil {
		writeTenantStoreError(w, err)
		return
	}
	for index := range found.Attachments {
		if found.Attachments[index].CompletedAt == nil ||
			found.Attachments[index].RetentionDeletedAt != nil ||
			found.Attachments[index].SafetyStatus != AttachmentSafetySafe {
			continue
		}
		value := "/v1/feedbacks/" + feedbackID + "/attachments/" + found.Attachments[index].ID + "/content"
		found.Attachments[index].ContentURL = &value
	}
	if found.Session != nil && found.Session.ReplayReady {
		value := "/v1/sessions/" + found.Session.ID + "/replay"
		found.Session.ReplayURL = &value
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"feedback": found})
}

func (h *TenantHTTPHandler) update(w http.ResponseWriter, r *http.Request) {
	feedbackID, ok := tenantFeedbackPathID(w, r)
	if !ok {
		return
	}
	var request tenantFeedbackMutationRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.FeedbackIDs != nil {
		writeTenantValidation(w, map[string]string{"feedback_ids": "is only valid for bulk updates"})
		return
	}
	mutation, fields := validateTenantFeedbackMutation(request, true)
	if len(fields) > 0 {
		writeTenantValidation(w, fields)
		return
	}
	resolved, query, ok := tenantFeedbackRequestState(w, r)
	if !ok {
		return
	}
	updated, err := h.options.Store.UpdateTenantFeedback(
		r.Context(), query, resolved.ID, feedbackID, mutation,
	)
	if err != nil {
		writeTenantStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"feedback": updated})
}

func (h *TenantHTTPHandler) bulkUpdate(w http.ResponseWriter, r *http.Request) {
	var request tenantFeedbackMutationRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	fields := validateTenantFeedbackIDs(request.FeedbackIDs)
	if request.Title != nil {
		fields["title"] = "is not supported for bulk updates"
	}
	mutation, mutationFields := validateTenantFeedbackMutation(request, false)
	for field, message := range mutationFields {
		fields[field] = message
	}
	if len(fields) > 0 {
		writeTenantValidation(w, fields)
		return
	}
	resolved, query, ok := tenantFeedbackRequestState(w, r)
	if !ok {
		return
	}
	updated, err := h.options.Store.BulkUpdateTenantFeedback(
		r.Context(), query, resolved.ID, request.FeedbackIDs, mutation,
	)
	if err != nil {
		writeTenantStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"updated_count": len(updated),
		"feedbacks":     updated,
	})
}

func (h *TenantHTTPHandler) link(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FeedbackIDs []string `json:"feedback_ids"`
		IssueID     string   `json:"issue_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	fields := validateTenantFeedbackIDs(request.FeedbackIDs)
	if _, err := uuid.Parse(request.IssueID); err != nil {
		fields["issue_id"] = "must be a UUID"
	}
	if len(fields) > 0 {
		writeTenantValidation(w, fields)
		return
	}
	resolved, query, ok := tenantFeedbackRequestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	updated, err := h.options.Store.LinkTenantFeedback(
		r.Context(), query, resolved.ID, request.FeedbackIDs, request.IssueID, claims.Subject, h.now(),
	)
	if err != nil {
		writeTenantStoreError(w, err)
		return
	}
	writeTenantFeedbackMutationResult(w, updated)
}

func (h *TenantHTTPHandler) unlink(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FeedbackIDs []string `json:"feedback_ids"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if fields := validateTenantFeedbackIDs(request.FeedbackIDs); len(fields) > 0 {
		writeTenantValidation(w, fields)
		return
	}
	resolved, query, ok := tenantFeedbackRequestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	updated, err := h.options.Store.UnlinkTenantFeedback(
		r.Context(), query, resolved.ID, request.FeedbackIDs, claims.Subject, h.now(),
	)
	if err != nil {
		writeTenantStoreError(w, err)
		return
	}
	writeTenantFeedbackMutationResult(w, updated)
}

func writeTenantFeedbackMutationResult(w http.ResponseWriter, updated []Item) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"updated_count": len(updated),
		"feedbacks":     updated,
	})
}

func (h *TenantHTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

type tenantFeedbackMutationRequest struct {
	FeedbackIDs  []string `json:"feedback_ids"`
	Status       *Status  `json:"status"`
	Title        *string  `json:"title"`
	Priority     *string  `json:"priority"`
	AssigneeID   *string  `json:"assignee_id"`
	Unassign     bool     `json:"unassign"`
	AddTagIDs    []string `json:"add_tag_ids"`
	RemoveTagIDs []string `json:"remove_tag_ids"`
}

func validateTenantFeedbackMutation(
	request tenantFeedbackMutationRequest,
	allowTitle bool,
) (TenantMutation, map[string]string) {
	fields := make(map[string]string)
	mutation := TenantMutation{Status: request.Status}
	if request.Status != nil && !request.Status.Valid() {
		fields["status"] = "must be open, spam, or archived"
	}
	if allowTitle && request.Title != nil {
		trimmed := strings.TrimSpace(*request.Title)
		if length := len([]rune(trimmed)); length < 1 || length > 80 {
			fields["title"] = "must be between 1 and 80 characters"
		} else {
			mutation.Title = &trimmed
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
	mutation.AddTagIDs = validateTenantMutationUUIDs("add_tag_ids", request.AddTagIDs, fields)
	mutation.RemoveTagIDs = validateTenantMutationUUIDs("remove_tag_ids", request.RemoveTagIDs, fields)
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
	if request.Status == nil && mutation.Title == nil && !mutation.SetPriority &&
		!mutation.SetAssignee && len(mutation.AddTagIDs) == 0 && len(mutation.RemoveTagIDs) == 0 {
		fields["request"] = "must include at least one operation"
	}
	return mutation, fields
}

func validateTenantFeedbackIDs(values []string) map[string]string {
	fields := make(map[string]string)
	if len(values) < 1 || len(values) > 100 {
		fields["feedback_ids"] = "must contain between 1 and 100 UUIDs"
		return fields
	}
	validateTenantMutationUUIDs("feedback_ids", values, fields)
	return fields
}

func validateTenantMutationUUIDs(
	field string,
	values []string,
	fields map[string]string,
) []string {
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

func (h *TenantHTTPHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	rangeDays := 30
	if raw := r.URL.Query().Get("range"); raw != "" {
		switch raw {
		case "7d":
			rangeDays = 7
		case "30d":
			rangeDays = 30
		case "90d":
			rangeDays = 90
		default:
			writeTenantValidation(w, map[string]string{"range": "must be 7d, 30d, or 90d"})
			return
		}
	}
	projectID := r.URL.Query().Get("project_id")
	if projectID != "" {
		if _, err := uuid.Parse(projectID); err != nil {
			writeTenantValidation(w, map[string]string{"project_id": "must be a UUID"})
			return
		}
	}
	resolved, query, ok := tenantFeedbackRequestState(w, r)
	if !ok {
		return
	}
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	dashboard, err := h.options.Store.TenantDashboard(
		r.Context(), query, resolved.ID, projectID, rangeDays, now,
	)
	if err != nil {
		writeTenantStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"dashboard": dashboard})
}

func parseTenantFeedbackOptions(r *http.Request) (ListOptions, map[string]string) {
	query := r.URL.Query()
	options := ListOptions{Sort: SortNewest, Limit: 50}
	fields := make(map[string]string)

	options.Query = strings.TrimSpace(query.Get("q"))
	if len(options.Query) > 200 {
		fields["q"] = "must be at most 200 characters"
	}
	options.ProjectID = query.Get("project_id")
	if options.ProjectID != "" {
		if _, err := uuid.Parse(options.ProjectID); err != nil {
			fields["project_id"] = "must be a UUID"
		}
	}
	if raw := query.Get("source"); raw != "" {
		switch raw {
		case "web", "ios", "android", "email":
			options.Source = raw
		default:
			fields["source"] = "must be web, ios, android, or email"
		}
	}
	if raw := query.Get("status"); raw != "" {
		options.Status = Status(raw)
		if !options.Status.Valid() {
			fields["status"] = "must be open, spam, or archived"
		}
	}
	if raw := query.Get("rating"); raw != "" {
		rating, err := strconv.Atoi(raw)
		if err != nil || rating < 1 || rating > 5 {
			fields["rating"] = "must be an integer from 1 to 5"
		} else {
			options.Rating = rating
		}
	}
	if raw := query.Get("attachment"); raw != "" {
		allowed := map[string]bool{
			"any": true, "none": true, "screenshot": true, "audio": true,
			"video": true, "replay": true,
		}
		if !allowed[raw] {
			fields["attachment"] = "must be any, none, screenshot, audio, video, or replay"
		} else {
			options.Attachment = raw
		}
	}
	if raw := query.Get("processing"); raw != "" {
		allowed := map[string]bool{
			"pending": true, "processing": true, "deferred": true,
			"completed": true, "failed": true, "skipped": true,
		}
		if !allowed[raw] {
			fields["processing"] = "must be pending, processing, deferred, completed, failed, or skipped"
		} else {
			options.Processing = raw
		}
	}
	if raw := query.Get("moderation"); raw != "" {
		if raw != "flagged" && raw != "clean" {
			fields["moderation"] = "must be flagged or clean"
		} else {
			options.Moderation = raw
		}
	}
	if raw := query.Get("issue"); raw != "" {
		if raw != "linked" && raw != "unlinked" {
			fields["issue"] = "must be linked or unlinked"
		} else {
			options.Issue = raw
		}
	}
	if raw := query.Get("date_from"); raw != "" {
		parsed, err := parseTenantFeedbackDate(raw, false)
		if err != nil {
			fields["date_from"] = "must be an RFC3339 timestamp or YYYY-MM-DD date"
		} else {
			options.DateFrom = &parsed
		}
	}
	if raw := query.Get("date_to"); raw != "" {
		parsed, err := parseTenantFeedbackDate(raw, true)
		if err != nil {
			fields["date_to"] = "must be an RFC3339 timestamp or YYYY-MM-DD date"
		} else {
			options.DateTo = &parsed
		}
	}
	if options.DateFrom != nil && options.DateTo != nil && !options.DateFrom.Before(*options.DateTo) {
		fields["date_to"] = "must be after date_from"
	}
	if raw := query.Get("sentiment"); raw != "" {
		if raw != "positive" && raw != "neutral" && raw != "negative" {
			fields["sentiment"] = "must be positive, neutral, or negative"
		} else {
			options.Sentiment = raw
		}
	}
	if raw := query.Get("sort"); raw != "" {
		options.Sort = SortOrder(strings.ReplaceAll(raw, "-", "_"))
		if !options.Sort.Valid() {
			fields["sort"] = "must be newest, oldest, rating_low, or rating_high"
		}
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			fields["limit"] = "must be an integer from 1 to 100"
		} else {
			options.Limit = limit
		}
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := decodeTenantFeedbackCursor(raw)
		if err != nil || cursor.Sort != options.Sort {
			fields["cursor"] = "is invalid for the selected sort order"
		} else {
			options.Cursor = &cursor
		}
	}
	return options, fields
}

func parseTenantFeedbackDate(value string, endOfDay bool) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return parsed.UTC(), nil
}

type tenantFeedbackCursorEnvelope struct {
	Version   int       `json:"v"`
	Sort      SortOrder `json:"sort"`
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
	RatingKey int       `json:"rating_key"`
}

func encodeTenantFeedbackCursor(item Item, sort SortOrder) (string, error) {
	ratingKey := 0
	if item.Rating != nil {
		ratingKey = *item.Rating
	} else if sort == SortRatingLow {
		ratingKey = 6
	}
	payload, err := json.Marshal(tenantFeedbackCursorEnvelope{
		Version: 1, Sort: sort, CreatedAt: item.CreatedAt.UTC(), ID: item.ID, RatingKey: ratingKey,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeTenantFeedbackCursor(value string) (ListCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ListCursor{}, err
	}
	var envelope tenantFeedbackCursorEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return ListCursor{}, err
	}
	if envelope.Version != 1 || !envelope.Sort.Valid() || envelope.CreatedAt.IsZero() {
		return ListCursor{}, errors.New("invalid feedback cursor")
	}
	if _, err := uuid.Parse(envelope.ID); err != nil {
		return ListCursor{}, errors.New("invalid feedback cursor")
	}
	if envelope.RatingKey < 0 || envelope.RatingKey > 6 {
		return ListCursor{}, errors.New("invalid feedback cursor")
	}
	return ListCursor{
		Sort: envelope.Sort, CreatedAt: envelope.CreatedAt.UTC(),
		ID: envelope.ID, RatingKey: envelope.RatingKey,
	}, nil
}

func tenantFeedbackRequestState(w http.ResponseWriter, r *http.Request) (tenant.Tenant, db.Querier, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return tenant.Tenant{}, nil, false
	}
	return resolved, query, true
}

func tenantFeedbackPathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := chi.URLParam(r, "feedbackID")
	if _, err := uuid.Parse(value); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return "", false
	}
	return value, true
}

func writeTenantValidation(w http.ResponseWriter, fields map[string]string) {
	api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "request validation failed", map[string]any{
		"fields": fields,
	})
}

func writeTenantStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	case errors.Is(err, ErrLinkConflict):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "feedback is linked to another issue", nil)
	case errors.Is(err, ErrMergedIssue):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "merged issue cannot receive feedback", nil)
	case errors.Is(err, ErrProjectMismatch):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "feedback and issue projects do not match", nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}
