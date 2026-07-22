package feedback

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestAttachmentStreamTenantResolutionAllowsDeletionGraceReads(t *testing.T) {
	t.Parallel()
	now := time.Now()
	expired := now.Add(-time.Hour)
	deletionRequestedAt := now.Add(-24 * time.Hour)
	for _, test := range []struct {
		name     string
		status   tenant.Status
		trialEnd *time.Time
	}{
		{name: "suspended", status: tenant.StatusSuspended},
		{name: "expired trial", status: tenant.StatusTrial, trialEnd: &expired},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &AttachmentStreamHTTPHandler{
				options: AttachmentStreamHTTPOptions{
					BaseDomain: "localhost",
					Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
						return tenant.Tenant{
							ID: "tenant-id", Slug: "acme", Status: test.status,
							TrialEndsAt: test.trialEnd, DeletionRequestedAt: &deletionRequestedAt,
						}, nil
					}),
					Now: func() time.Time { return now },
				},
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			request := httptest.NewRequest(http.MethodGet, "http://acme.localhost/content", nil)
			request.Host = "acme.localhost"
			recorder := httptest.NewRecorder()
			resolved, ok := handler.resolveTenant(recorder, request)
			if !ok || resolved.ID != "tenant-id" {
				t.Fatalf("resolveTenant() = %#v/%t, want pending-deletion tenant", resolved, ok)
			}
		})
	}
}

func TestParseTenantFeedbackOptions(t *testing.T) {
	request := httptest.NewRequest("GET", "/v1/feedbacks?"+url.Values{
		"q":          {"checkout"},
		"project_id": {"945f0358-7dbb-4108-ab24-0157e24591ca"},
		"status":     {"open"},
		"rating":     {"2"},
		"attachment": {"replay"},
		"processing": {"failed"},
		"moderation": {"flagged"},
		"issue":      {"linked"},
		"date_from":  {"2026-07-01"},
		"date_to":    {"2026-07-21"},
		"sentiment":  {"negative"},
		"sort":       {"rating-low"},
		"limit":      {"25"},
	}.Encode(), nil)
	options, fields := parseTenantFeedbackOptions(request)
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if options.Sort != SortRatingLow || options.Limit != 25 || options.Rating != 2 ||
		options.Query != "checkout" || options.Attachment != "replay" || options.Processing != "failed" ||
		options.Moderation != "flagged" ||
		options.Issue != "linked" || options.Sentiment != "negative" {
		t.Fatalf("options = %#v", options)
	}
	if options.DateFrom == nil || options.DateTo == nil ||
		options.DateFrom.Format("2006-01-02") != "2026-07-01" ||
		options.DateTo.Format("2006-01-02") != "2026-07-22" {
		t.Fatalf("date range = %v - %v", options.DateFrom, options.DateTo)
	}
}

func TestParseTenantFeedbackOptionsRejectsInvalidFilters(t *testing.T) {
	request := httptest.NewRequest("GET", "/v1/feedbacks?"+url.Values{
		"project_id": {"nope"},
		"status":     {"closed"},
		"rating":     {"6"},
		"attachment": {"pdf"},
		"moderation": {"maybe"},
		"issue":      {"missing"},
		"date_from":  {"tomorrow"},
		"date_to":    {"yesterday"},
		"sentiment":  {"mixed"},
		"sort":       {"popular"},
		"limit":      {"101"},
		"cursor":     {"broken"},
	}.Encode(), nil)
	_, fields := parseTenantFeedbackOptions(request)
	want := []string{
		"project_id", "status", "rating", "attachment", "moderation", "issue",
		"date_from", "date_to", "sentiment", "sort", "limit", "cursor",
	}
	for _, field := range want {
		if _, exists := fields[field]; !exists {
			t.Errorf("missing validation error for %s: %#v", field, fields)
		}
	}
}

func TestTenantFeedbackCursorRoundTrip(t *testing.T) {
	rating := 3
	item := Item{
		ID: "86ec3ccf-a45b-4a73-a821-e502504ad97f", Rating: &rating,
		CreatedAt: time.Date(2026, 7, 21, 11, 12, 13, 14, time.UTC),
	}
	encoded, err := encodeTenantFeedbackCursor(item, SortRatingHigh)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	decoded, err := decodeTenantFeedbackCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded.Sort != SortRatingHigh || decoded.ID != item.ID ||
		decoded.RatingKey != rating || !decoded.CreatedAt.Equal(item.CreatedAt) {
		t.Fatalf("decoded cursor = %#v", decoded)
	}

	request := httptest.NewRequest("GET", "/v1/feedbacks?sort=newest&cursor="+url.QueryEscape(encoded), nil)
	_, fields := parseTenantFeedbackOptions(request)
	if fields["cursor"] == "" {
		t.Fatalf("cursor from another sort was accepted: %#v", fields)
	}
}
