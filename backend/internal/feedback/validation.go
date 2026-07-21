package feedback

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/google/uuid"
)

const (
	maxTextLength       = 10000
	maxElementHTMLBytes = 200000
	maxElementCSSBytes  = 100000
)

var attachmentRules = map[AttachmentKind]struct {
	maxBytes     int64
	contentTypes map[string]struct{}
}{
	AttachmentScreenshot: {10 << 20, set("image/png", "image/jpeg", "image/webp")},
	AttachmentAudio:      {25 << 20, set("audio/webm", "audio/ogg", "audio/mpeg", "audio/wav", "audio/mp4")},
	AttachmentVideo:      {100 << 20, set("video/webm", "video/mp4")},
	AttachmentReplay:     {25 << 20, set("application/json", "application/gzip", "application/x-gzip")},
}

func ValidateSubmission(
	input SubmissionRequest,
	configuration project.PublicConfiguration,
) (SubmissionValues, []FieldError) {
	values := normalizeSubmission(input)
	fields := make([]FieldError, 0)
	if _, err := uuid.Parse(values.ClientFeedbackID); err != nil {
		fields = append(fields, field("client_feedback_id", "must be a UUID"))
	}
	if _, err := uuid.Parse(values.WidgetRuleID); err != nil {
		fields = append(fields, field("widget_rule_id", "must identify an enabled widget rule"))
	}
	if values.ReplaySessionID != nil {
		if _, err := uuid.Parse(*values.ReplaySessionID); err != nil {
			fields = append(fields, field("session_id", "must be a UUID"))
		}
	}
	if values.Rating != nil && (*values.Rating < 1 || *values.Rating > 5) {
		fields = append(fields, field("rating", "must be between 1 and 5"))
	}
	if values.Text != nil && len(*values.Text) > maxTextLength {
		fields = append(fields, field("text", fmt.Sprintf("must not exceed %d characters", maxTextLength)))
	}
	if values.ReporterEmail != nil && !validEmail(*values.ReporterEmail) {
		fields = append(fields, field("reporter_email", "must be a valid email address"))
	}
	if values.NotifyReporter && values.ReporterEmail == nil {
		fields = append(fields, field("reporter_email", "is required when notify_reporter is true"))
	}
	if !validHTTPURL(values.PageURL, 4096) {
		fields = append(fields, field("metadata.url", "must be an absolute HTTP or HTTPS URL"))
	}
	if values.Referrer != nil && !validHTTPURL(*values.Referrer, 4096) {
		fields = append(fields, field("metadata.referrer", "must be an absolute HTTP or HTTPS URL"))
	}
	if values.Device != nil && *values.Device != "desktop" && *values.Device != "tablet" && *values.Device != "mobile" {
		fields = append(fields, field("metadata.device", "must be desktop, tablet, or mobile"))
	}
	if values.ViewportWidth == nil || *values.ViewportWidth < 1 || *values.ViewportWidth > 100000 {
		fields = append(fields, field("metadata.viewport_width", "must be between 1 and 100000"))
	}
	if values.ViewportHeight == nil || *values.ViewportHeight < 1 || *values.ViewportHeight > 100000 {
		fields = append(fields, field("metadata.viewport_height", "must be between 1 and 100000"))
	}
	if input.Element != nil {
		if strings.TrimSpace(input.Element.Selector) == "" || len(input.Element.Selector) > 2000 {
			fields = append(fields, field("element_attachment.selector", "must be between 1 and 2000 characters"))
		}
		if len(input.Element.HTML) == 0 || len(input.Element.HTML) > maxElementHTMLBytes {
			fields = append(fields, field("element_attachment.html", "must be between 1 and 200000 bytes"))
		}
		styles, _ := json.Marshal(input.Element.Styles)
		if len(styles) > maxElementCSSBytes {
			fields = append(fields, field("element_attachment.styles", "must not exceed 100000 bytes"))
		}
	}
	fields = append(fields, validateAttachments(values.Attachments)...)
	fields = append(fields, validateRuleCapabilities(input, values, configuration)...)
	return values, fields
}

func normalizeSubmission(input SubmissionRequest) SubmissionValues {
	values := SubmissionValues{
		ClientFeedbackID: strings.TrimSpace(input.ClientFeedbackID),
		WidgetRuleID:     strings.TrimSpace(input.WidgetRuleID), Rating: input.Rating,
		ReplaySessionID: trimStringPointer(input.ReplaySessionID),
		Text:            trimOptional(input.Text), ReporterEmail: lowerOptional(input.ReporterEmail),
		NotifyReporter: input.NotifyReporter, PageURL: strings.TrimSpace(input.Metadata.URL),
		Referrer: trimString(input.Metadata.Referrer), UserAgent: trimString(input.Metadata.UserAgent),
		OS: trimString(input.Metadata.OS), Browser: trimString(input.Metadata.Browser),
		Device: trimString(input.Metadata.Device), Language: trimString(input.Metadata.Language),
		Attachments: make([]AttachmentValues, len(input.Attachments)),
	}
	if input.Metadata.ViewportWidth != 0 {
		values.ViewportWidth = &input.Metadata.ViewportWidth
	}
	if input.Metadata.ViewportHeight != 0 {
		values.ViewportHeight = &input.Metadata.ViewportHeight
	}
	if input.Element != nil {
		html := input.Element.HTML
		styles, _ := json.Marshal(input.Element.Styles)
		stylesJSON := string(styles)
		values.DOMSnapshotHTML = &html
		values.DOMSnapshotStyles = &stylesJSON
	}
	for index, attachment := range input.Attachments {
		values.Attachments[index] = AttachmentValues{
			ClientAttachmentID: strings.TrimSpace(attachment.ClientAttachmentID),
			Kind:               attachment.Kind, ContentType: normalizeContentType(attachment.ContentType),
			SizeBytes: attachment.SizeBytes, DurationSeconds: attachment.DurationSeconds,
		}
	}
	return values
}

func validateAttachments(attachments []AttachmentValues) []FieldError {
	fields := make([]FieldError, 0)
	if len(attachments) > 4 {
		fields = append(fields, field("attachments", "must not contain more than four items"))
	}
	seenIDs := map[string]struct{}{}
	seenKinds := map[AttachmentKind]struct{}{}
	for index, attachment := range attachments {
		prefix := fmt.Sprintf("attachments.%d", index)
		if _, err := uuid.Parse(attachment.ClientAttachmentID); err != nil {
			fields = append(fields, field(prefix+".client_attachment_id", "must be a UUID"))
		} else if _, exists := seenIDs[attachment.ClientAttachmentID]; exists {
			fields = append(fields, field(prefix+".client_attachment_id", "must be unique"))
		}
		seenIDs[attachment.ClientAttachmentID] = struct{}{}
		rule, exists := attachmentRules[attachment.Kind]
		if !exists {
			fields = append(fields, field(prefix+".kind", "must be screenshot, audio, video, or replay"))
			continue
		}
		if _, exists := seenKinds[attachment.Kind]; exists {
			fields = append(fields, field(prefix+".kind", "must be unique"))
		}
		seenKinds[attachment.Kind] = struct{}{}
		if _, exists := rule.contentTypes[attachment.ContentType]; !exists {
			fields = append(fields, field(prefix+".content_type", "is not supported for this attachment kind"))
		}
		if attachment.SizeBytes < 1 || attachment.SizeBytes > rule.maxBytes {
			fields = append(fields, field(prefix+".size_bytes", fmt.Sprintf("must be between 1 and %d", rule.maxBytes)))
		}
		if attachment.DurationSeconds != nil && (*attachment.DurationSeconds < 0 || *attachment.DurationSeconds > 7200) {
			fields = append(fields, field(prefix+".duration_seconds", "must be between 0 and 7200"))
		}
	}
	return fields
}

func validateRuleCapabilities(
	input SubmissionRequest,
	values SubmissionValues,
	configuration project.PublicConfiguration,
) []FieldError {
	fields := make([]FieldError, 0)
	var selected *project.WidgetRule
	for index := range configuration.Rules {
		if configuration.Rules[index].ID == values.WidgetRuleID {
			selected = &configuration.Rules[index]
			break
		}
	}
	if selected == nil {
		return append(fields, field("widget_rule_id", "must identify an enabled widget rule"))
	}
	definition, exists := config.PlanFor(configuration.Plan)
	if !exists {
		return append(fields, field("project", "has an unsupported plan"))
	}
	if values.Rating != nil && !selected.CanRate {
		fields = append(fields, field("rating", "is not enabled by this widget rule"))
	}
	if values.Text != nil && !selected.CanWriteText {
		fields = append(fields, field("text", "is not enabled by this widget rule"))
	}
	if input.Element != nil && (!selected.CanAttachElement || !definition.Features.DOMCapture) {
		fields = append(fields, field("element_attachment", "is not enabled by this widget rule and plan"))
	}
	for index, attachment := range values.Attachments {
		allowed := false
		switch attachment.Kind {
		case AttachmentScreenshot:
			allowed = selected.CanAttachScreenshot && definition.Features.DOMCapture
		case AttachmentAudio:
			allowed = selected.CanRecordAudio && definition.Features.Audio
		case AttachmentVideo:
			allowed = selected.CanRecordVideo && definition.Features.Video
		case AttachmentReplay:
			allowed = selected.ReplayMode != project.ReplayNone && definition.Features.Replay
		}
		if !allowed {
			fields = append(fields, field(fmt.Sprintf("attachments.%d.kind", index),
				"is not enabled by this widget rule and plan"))
		}
	}
	return fields
}

func validEmail(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && len(value) <= 320
}

func validHTTPURL(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func normalizeContentType(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	return trimString(*value)
}

func trimStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return trimString(*value)
}

func lowerOptional(value *string) *string {
	trimmed := trimOptional(value)
	if trimmed == nil {
		return nil
	}
	lower := strings.ToLower(*trimmed)
	return &lower
}

func trimString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func field(name string, message string) FieldError {
	return FieldError{Field: name, Message: message}
}

func set(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
