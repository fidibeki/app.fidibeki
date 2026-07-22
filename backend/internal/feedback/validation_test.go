package feedback

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
)

func TestValidateSubmissionAcceptsEnabledGrowthFeatures(t *testing.T) {
	t.Parallel()
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
	}
	configuration := project.PublicConfiguration{
		Plan: tenant.PlanGrowth,
		Rules: []project.WidgetRule{{
			ID: request.WidgetRuleID, CanRate: true, CanWriteText: true,
			CanAttachElement: true, CanRecordAudio: true,
		}},
	}
	values, fields := ValidateSubmission(request, configuration)
	if len(fields) != 0 {
		t.Fatalf("validation fields = %#v", fields)
	}
	if values.Attachments[0].ContentType != "audio/webm" {
		t.Errorf("content type = %q", values.Attachments[0].ContentType)
	}
	if values.DOMSnapshotStyles == nil || *values.DOMSnapshotStyles != `{"display":"grid"}` {
		t.Errorf("DOM styles = %#v", values.DOMSnapshotStyles)
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
