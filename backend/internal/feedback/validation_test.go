package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
)

func TestValidateSubmissionAcceptsEnabledGrowthFeatures(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	text := "Checkout was clear"
	request := SubmissionRequest{
		ClientFeedbackID: "57e3ee52-4ebf-43ce-9e31-9f909628634d",
		WidgetRuleID:     "1341b36b-d02c-4398-ab29-ca935e73539a",
		Rating:           intPointer(5), Text: &text,
		Element: &ElementAttachmentInput{
			Selector: "#checkout", HTML: "<section>Checkout</section>",
			Styles: map[string]string{"display": "grid"},
		},
		Attachments: []AttachmentInput{{
			ClientAttachmentID: "61c18d72-2296-43fa-bd21-a1356aa5da1c",
			Kind:               AttachmentAudio, ContentType: "audio/webm;codecs=opus", SizeBytes: 2048,
		}},
		Metadata: MetadataInput{
			URL: "https://example.com/checkout", ViewportWidth: 1280, ViewportHeight: 720,
			Device: "desktop",
		},
		Analytics: &SubmissionAnalyticsInput{
			EventID:   "00000000-0000-4000-8000-000000000001",
			VisitorID: "00000000-0000-4000-8000-000000000002", PageViewID: "00000000-0000-4000-8000-000000000003",
			RuleID: "1341b36b-d02c-4398-ab29-ca935e73539a", PageURL: "https://example.com/checkout/?step=2",
			OccurredAt: now,
		},
	}
	configuration := project.PublicConfiguration{
		TenantID: "00000000-0000-4000-8000-000000000010", ProjectID: "00000000-0000-4000-8000-000000000020",
		Plan: tenant.PlanGrowth,
		Rules: []project.WidgetRule{{
			ID: request.WidgetRuleID, CanRate: true, CanWriteText: true,
			CanAttachElement: true, CanRecordAudio: true,
		}},
	}
	values, fields := ValidateSubmissionAt(request, configuration, now)
	if len(fields) != 0 {
		t.Fatalf("validation fields = %#v", fields)
	}
	if values.Attachments[0].ContentType != "audio/webm" {
		t.Errorf("content type = %q", values.Attachments[0].ContentType)
	}
	if values.DOMSnapshotStyles == nil || *values.DOMSnapshotStyles != `{"display":"grid"}` {
		t.Errorf("DOM styles = %#v", values.DOMSnapshotStyles)
	}
	if values.Analytics == nil || values.Analytics.EventType != "submitted" ||
		values.Analytics.PageURL != "https://example.com/checkout" {
		t.Errorf("analytics = %#v", values.Analytics)
	}
}

func TestValidateSubmissionNormalizesPublishedResponseBlocks(t *testing.T) {
	t.Parallel()
	comment := "The labels could be clearer"
	email := "CUSTOMER@EXAMPLE.COM"
	request := SubmissionRequest{
		ClientFeedbackID: "57e3ee52-4ebf-43ce-9e31-9f909628634d",
		WidgetRuleID:     "1341b36b-d02c-4398-ab29-ca935e73539a",
		Responses: map[string]json.RawMessage{
			"score":    json.RawMessage(`4`),
			"reaction": json.RawMessage(`"helpful"`),
			"reason":   json.RawMessage(`"fast"`),
			"details":  json.RawMessage(`"The checkout was clear"`),
		},
		Comment: &comment, ReporterEmail: &email, NotifyReporter: true,
		Consent: &ReporterConsentInput{
			Purpose: ReporterConsentPurposeFeedbackUpdates, NoticeVersion: "2026-07",
			Locale: "en", Granted: true,
		},
		Metadata: MetadataInput{
			URL: "https://example.com/checkout", ViewportWidth: 1280, ViewportHeight: 720,
		},
	}
	configuration := project.PublicConfiguration{
		Plan: tenant.PlanGrowth,
		Rules: []project.WidgetRule{{
			ID: request.WidgetRuleID, DefinitionVersion: 3, AllowComment: true, CollectContact: true,
			CanRate: true, CanWriteText: true,
			ResponseBlocks: []project.WidgetResponseBlock{
				{Key: "score", Type: project.ResponseRating, Required: true},
				{Key: "reaction", Type: project.ResponseReaction, Required: true, Options: []project.WidgetResponseOption{{Value: "helpful"}, {Value: "not_helpful"}}},
				{Key: "reason", Type: project.ResponseSingleChoice, Options: []project.WidgetResponseOption{{Value: "fast"}, {Value: "clear"}}},
				{Key: "details", Type: project.ResponseFreeText},
			},
		}},
	}
	values, fields := ValidateSubmission(request, configuration)
	if len(fields) != 0 {
		t.Fatalf("validation fields = %#v", fields)
	}
	if values.Rating == nil || *values.Rating != 4 || values.Text == nil || *values.Text != "The checkout was clear" {
		t.Errorf("legacy response values = rating %v text %v", values.Rating, values.Text)
	}
	if values.DefinitionVersion == nil || *values.DefinitionVersion != 3 || values.Comment == nil || *values.Comment != comment {
		t.Errorf("definition context = version %v comment %v", values.DefinitionVersion, values.Comment)
	}
	if values.ReporterEmail == nil || *values.ReporterEmail != "customer@example.com" {
		t.Errorf("reporter email = %v", values.ReporterEmail)
	}
	if values.ReporterConsentPurpose == nil || *values.ReporterConsentPurpose != ReporterConsentPurposeFeedbackUpdates ||
		values.ReporterConsentNoticeVersion == nil || *values.ReporterConsentNoticeVersion != "2026-07" ||
		values.ReporterConsentLocale == nil || *values.ReporterConsentLocale != "en" ||
		values.ReporterConsentSource == nil || *values.ReporterConsentSource != ReporterConsentSourceWidget {
		t.Errorf("consent evidence = purpose %v version %v locale %v source %v",
			values.ReporterConsentPurpose, values.ReporterConsentNoticeVersion,
			values.ReporterConsentLocale, values.ReporterConsentSource)
	}
	var responses map[string]any
	if err := json.Unmarshal([]byte(values.Responses), &responses); err != nil {
		t.Fatalf("decode normalized responses: %v", err)
	}
	if len(responses) != 4 || responses["reaction"] != "helpful" || responses["reason"] != "fast" {
		t.Errorf("normalized responses = %#v", responses)
	}
}

func TestValidateReporterConsentRejectsContradictoryAndUnknownPayloads(t *testing.T) {
	t.Parallel()
	baseConfiguration := project.PublicConfiguration{
		Plan: tenant.PlanGrowth,
		Rules: []project.WidgetRule{{
			ID: "1341b36b-d02c-4398-ab29-ca935e73539a", CollectContact: true,
		}},
	}
	validConsent := ReporterConsentInput{
		Purpose: ReporterConsentPurposeFeedbackUpdates, NoticeVersion: "2026-07",
		Locale: "en", Granted: true,
	}
	email := "customer@example.com"
	baseRequest := func() SubmissionRequest {
		return SubmissionRequest{
			ClientFeedbackID: "57e3ee52-4ebf-43ce-9e31-9f909628634d",
			WidgetRuleID:     "1341b36b-d02c-4398-ab29-ca935e73539a",
			Metadata: MetadataInput{
				URL: "https://example.com/checkout", ViewportWidth: 1280, ViewportHeight: 720,
			},
		}
	}
	cases := []struct {
		name    string
		mutate  func(*SubmissionRequest)
		fields  []string
		wantNil []string
	}{
		{
			name: "email without granted consent",
			mutate: func(r *SubmissionRequest) {
				r.ReporterEmail = &email
			},
			fields: []string{"reporter_email"},
		},
		{
			name: "consent without notify_reporter",
			mutate: func(r *SubmissionRequest) {
				consent := validConsent
				r.Consent = &consent
			},
			fields: []string{"consent"},
		},
		{
			name: "notify_reporter true without any evidence",
			mutate: func(r *SubmissionRequest) {
				r.NotifyReporter = true
				r.ReporterEmail = &email
			},
			fields: []string{"consent"},
		},
		{
			name: "notify_reporter true with granted false",
			mutate: func(r *SubmissionRequest) {
				r.NotifyReporter = true
				r.ReporterEmail = &email
				consent := validConsent
				consent.Granted = false
				r.Consent = &consent
			},
			fields: []string{"consent.granted"},
		},
		{
			name: "unknown consent purpose",
			mutate: func(r *SubmissionRequest) {
				r.NotifyReporter = true
				r.ReporterEmail = &email
				consent := validConsent
				consent.Purpose = "marketing"
				r.Consent = &consent
			},
			fields: []string{"consent.purpose"},
		},
		{
			name: "unknown notice version",
			mutate: func(r *SubmissionRequest) {
				r.NotifyReporter = true
				r.ReporterEmail = &email
				consent := validConsent
				consent.NoticeVersion = "1999-01"
				r.Consent = &consent
			},
			fields: []string{"consent.notice_version"},
		},
		{
			name: "invalid locale",
			mutate: func(r *SubmissionRequest) {
				r.NotifyReporter = true
				r.ReporterEmail = &email
				consent := validConsent
				consent.Locale = "not a locale!"
				r.Consent = &consent
			},
			fields: []string{"consent.locale"},
		},
		{
			name: "unsupported locale",
			mutate: func(r *SubmissionRequest) {
				r.NotifyReporter = true
				r.ReporterEmail = &email
				consent := validConsent
				consent.Locale = "fr"
				r.Consent = &consent
			},
			fields: []string{"consent.locale"},
		},
		{
			name: "notify_reporter true without an email",
			mutate: func(r *SubmissionRequest) {
				r.NotifyReporter = true
				consent := validConsent
				r.Consent = &consent
			},
			fields: []string{"reporter_email"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			request := baseRequest()
			testCase.mutate(&request)
			values, fields := ValidateSubmission(request, baseConfiguration)
			for _, expected := range testCase.fields {
				if !hasField(fields, expected) {
					t.Errorf("fields = %#v, missing %q", fields, expected)
				}
			}
			if values.ReporterConsentPurpose != nil || values.ReporterConsentNoticeVersion != nil ||
				values.ReporterConsentLocale != nil || values.ReporterConsentSource != nil {
				t.Errorf("rejected payload must not populate consent evidence, got %#v", values)
			}
		})
	}
}

func TestValidateSubmissionAcceptsUncheckedConsentEvenWithPriorEmailInState(t *testing.T) {
	t.Parallel()
	request := SubmissionRequest{
		ClientFeedbackID: "57e3ee52-4ebf-43ce-9e31-9f909628634d",
		WidgetRuleID:     "1341b36b-d02c-4398-ab29-ca935e73539a",
		NotifyReporter:   false, ReporterEmail: nil, Consent: nil,
		Metadata: MetadataInput{
			URL: "https://example.com/checkout", ViewportWidth: 1280, ViewportHeight: 720,
		},
	}
	configuration := project.PublicConfiguration{
		Plan: tenant.PlanGrowth,
		Rules: []project.WidgetRule{{
			ID: request.WidgetRuleID, CollectContact: true,
		}},
	}
	values, fields := ValidateSubmission(request, configuration)
	if len(fields) != 0 {
		t.Fatalf("anonymous submission fields = %#v", fields)
	}
	if values.ReporterEmail != nil || values.NotifyReporter ||
		values.ReporterConsentPurpose != nil || values.ReporterConsentSource != nil {
		t.Errorf("anonymous submission values = %#v", values)
	}
}

func TestValidateSubmissionRejectsResponsesOutsidePublishedDefinition(t *testing.T) {
	t.Parallel()
	comment := "Unexpected comment"
	email := "customer@example.com"
	request := SubmissionRequest{
		ClientFeedbackID: "57e3ee52-4ebf-43ce-9e31-9f909628634d",
		WidgetRuleID:     "1341b36b-d02c-4398-ab29-ca935e73539a",
		Responses: map[string]json.RawMessage{
			"reaction": json.RawMessage(`"unknown"`),
			"extra":    json.RawMessage(`"value"`),
		},
		Comment: &comment, ReporterEmail: &email,
		Metadata: MetadataInput{
			URL: "https://example.com/checkout", ViewportWidth: 1280, ViewportHeight: 720,
		},
	}
	_, fields := ValidateSubmission(request, project.PublicConfiguration{
		Plan: tenant.PlanGrowth,
		Rules: []project.WidgetRule{{
			ID: request.WidgetRuleID,
			ResponseBlocks: []project.WidgetResponseBlock{
				{Key: "reaction", Type: project.ResponseReaction, Required: true, Options: []project.WidgetResponseOption{{Value: "helpful"}, {Value: "not_helpful"}}},
				{Key: "details", Type: project.ResponseFreeText, Required: true},
			},
		}},
	})
	for _, expected := range []string{"responses.reaction", "responses.details", "responses.extra", "comment", "reporter_email"} {
		if !hasField(fields, expected) {
			t.Errorf("fields = %#v, missing %q", fields, expected)
		}
	}
}

func TestValidateSubmissionRejectsMismatchedAnalyticsContext(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	ruleID := "1341b36b-d02c-4398-ab29-ca935e73539a"
	request := SubmissionRequest{
		ClientFeedbackID: "57e3ee52-4ebf-43ce-9e31-9f909628634d", WidgetRuleID: ruleID,
		Metadata: MetadataInput{URL: "https://example.com/checkout", ViewportWidth: 1280, ViewportHeight: 720},
		Analytics: &SubmissionAnalyticsInput{
			EventID:   "00000000-0000-4000-8000-000000000001",
			VisitorID: "00000000-0000-4000-8000-000000000002", PageViewID: "00000000-0000-4000-8000-000000000003",
			RuleID: "00000000-0000-4000-8000-000000000099", PageURL: "https://example.com/other",
			OccurredAt: now,
		},
	}
	_, fields := ValidateSubmissionAt(request, project.PublicConfiguration{
		TenantID: "00000000-0000-4000-8000-000000000010", ProjectID: "00000000-0000-4000-8000-000000000020",
		Plan: tenant.PlanGrowth, Rules: []project.WidgetRule{{ID: ruleID}},
	}, now)
	for _, expected := range []string{"analytics.rule_id", "analytics.page_url"} {
		if !hasField(fields, expected) {
			t.Fatalf("fields = %#v, missing %s", fields, expected)
		}
	}
}

func TestFeedbackPreflightAuthorizesOriginAndHeaders(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	RegisterRoutes(router, HTTPOptions{Authorizer: authorizerFunc(func(
		_ context.Context,
		key string,
		origin string,
	) (project.PublicConfiguration, error) {
		if key != "pk_live_test" || origin != "https://example.com" {
			return project.PublicConfiguration{}, widgetapi.ErrAccessDenied
		}
		return project.PublicConfiguration{ProjectID: "project-id"}, nil
	})})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions,
		"http://widget.localhost/v1/feedback?key=pk_live_test", nil)
	request.Header.Set("Origin", "https://example.com")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent ||
		recorder.Header().Get("Access-Control-Allow-Origin") != "https://example.com" ||
		!regexp.MustCompile(`Content-Type`).MatchString(recorder.Header().Get("Access-Control-Allow-Headers")) {
		t.Errorf("preflight status = %d, headers = %#v", recorder.Code, recorder.Header())
	}
}

func TestAttachmentHelpersCoverSupportedFormats(t *testing.T) {
	t.Parallel()
	formats := map[string]string{
		"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp",
		"audio/ogg": "ogg", "audio/mpeg": "mp3", "audio/wav": "wav",
		"audio/mp4": "mp4", "video/mp4": "mp4", "application/json": "json",
		"application/gzip": "json.gz", "application/x-gzip": "json.gz", "audio/webm": "webm",
	}
	for contentType, want := range formats {
		if got := attachmentExtension(AttachmentValues{ContentType: contentType}); got != want {
			t.Errorf("attachmentExtension(%q) = %q, want %q", contentType, got, want)
		}
	}
	short := "Short title. More context belongs in the description."
	if got := feedbackTitle(nil); got != "Feedback without written detail" {
		t.Errorf("feedbackTitle(nil) = %q", got)
	}
	if got := feedbackTitle(&short); got != "Short title." {
		t.Errorf("feedbackTitle(short) = %q", got)
	}
	long := strings.Repeat("a", 81)
	if got := feedbackTitle(&long); len([]rune(got)) != 80 {
		t.Errorf("feedbackTitle(long) rune count = %d", len([]rune(got)))
	}
	multiline := "The checkout froze\nI had already entered my card details"
	if got := feedbackTitle(&multiline); got != "The checkout froze" {
		t.Errorf("feedbackTitle(multiline) = %q", got)
	}
}

func TestAttachmentValidationBoundsCountIdentityKindsAndAggregateSize(t *testing.T) {
	t.Parallel()

	attachments := []AttachmentValues{
		{ClientAttachmentID: "00000000-0000-4000-8000-000000000001", Kind: AttachmentVideo, ContentType: "video/mp4", SizeBytes: 100 << 20},
		{ClientAttachmentID: "00000000-0000-4000-8000-000000000002", Kind: AttachmentAudio, ContentType: "audio/webm", SizeBytes: 25 << 20},
		{ClientAttachmentID: "00000000-0000-4000-8000-000000000003", Kind: AttachmentScreenshot, ContentType: "image/png", SizeBytes: 1},
		{ClientAttachmentID: "00000000-0000-4000-8000-000000000003", Kind: AttachmentScreenshot, ContentType: "image/png", SizeBytes: 1},
		{ClientAttachmentID: "00000000-0000-4000-8000-000000000005", Kind: AttachmentReplay, ContentType: "application/json", SizeBytes: 1},
	}
	fields := validateAttachments(attachments)
	for _, expected := range []string{
		"attachments",
		"attachments.3.client_attachment_id",
		"attachments.3.kind",
	} {
		if !hasField(fields, expected) {
			t.Errorf("fields = %#v, missing %q", fields, expected)
		}
	}
}

func TestValidateSubmissionReportsFieldLevelAndCapabilityErrors(t *testing.T) {
	t.Parallel()
	invalidEmail := "not-an-email"
	request := SubmissionRequest{
		ClientFeedbackID: "invalid", WidgetRuleID: "missing", Rating: intPointer(9),
		ReporterEmail: &invalidEmail, NotifyReporter: true,
		Attachments: []AttachmentInput{{
			ClientAttachmentID: "invalid", Kind: AttachmentVideo,
			ContentType: "video/avi", SizeBytes: 200 << 20,
		}},
		Metadata: MetadataInput{URL: "relative", ViewportWidth: -1},
	}
	_, fields := ValidateSubmission(request, project.PublicConfiguration{Plan: tenant.PlanStarter})
	for _, expected := range []string{
		"client_feedback_id", "widget_rule_id", "rating", "reporter_email",
		"metadata.url", "metadata.viewport_width", "metadata.viewport_height",
		"attachments.0.client_attachment_id", "attachments.0.content_type", "attachments.0.size_bytes",
	} {
		if !hasField(fields, expected) {
			t.Errorf("fields = %#v, missing %q", fields, expected)
		}
	}
}

func TestGeneratePublicIDUsesContractFormat(t *testing.T) {
	t.Parallel()
	value, err := GeneratePublicID()
	if err != nil {
		t.Fatalf("GeneratePublicID() error = %v", err)
	}
	if !regexp.MustCompile(`^fb_[A-Za-z0-9]{8}$`).MatchString(value) {
		t.Errorf("public ID = %q", value)
	}
}

func hasField(fields []FieldError, name string) bool {
	for _, candidate := range fields {
		if candidate.Field == name {
			return true
		}
	}
	return false
}

func intPointer(value int) *int {
	return &value
}

type authorizerFunc func(context.Context, string, string) (project.PublicConfiguration, error)

func (f authorizerFunc) Authorize(
	ctx context.Context,
	key string,
	origin string,
) (project.PublicConfiguration, error) {
	if f == nil {
		return project.PublicConfiguration{}, errors.New("authorizer is nil")
	}
	return f(ctx, key, origin)
}
