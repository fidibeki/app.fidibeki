package feedback

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetanalytics"
	"github.com/google/uuid"
)

const (
	maxTextLength       = 10000
	maxElementHTMLBytes = 200000
	maxElementCSSBytes  = 100000
	maxAttachmentBytes  = 125 << 20
	maxLocaleLength     = 35
)

// ReporterConsentPurposeFeedbackUpdates is the only supported consent purpose for reporter emails.
// Any other purpose value is an unknown/contradictory payload and is rejected.
const ReporterConsentPurposeFeedbackUpdates = "feedback_updates"

// ReporterConsentNoticeVersion is the consent notice revision currently accepted by the API.
const ReporterConsentNoticeVersion = "2026-07"

// ReporterConsentSourceWidget identifies the widget submission flow as the origin of a consent
// grant. It is assigned by the server, never accepted from the client.
const ReporterConsentSourceWidget = "widget_submission"

// reporterConsentNoticeVersions are the notice/copy revisions the backend currently recognizes.
// Submissions asserting any other version are rejected as unknown so a future notice-copy change
// can be rolled out without silently accepting evidence for a notice the reporter never saw.
var reporterConsentNoticeVersions = set(ReporterConsentNoticeVersion)

var localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

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
	return ValidateSubmissionAt(input, configuration, time.Now().UTC())
}

func ValidateSubmissionAt(
	input SubmissionRequest,
	configuration project.PublicConfiguration,
	now time.Time,
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
	fields = append(fields, validateReporterConsent(input, &values)...)
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
	fields = append(fields, validateAttachments(values.Attachments)...)
	fields = append(fields, validateResponses(input, &values, configuration)...)
	fields = append(fields, validateRuleCapabilities(input, values, configuration)...)
	if input.Analytics != nil {
		analytics, analyticsFields := widgetanalytics.ValidateSubmissionEvent(
			widgetanalytics.EventInput{
				EventID: input.Analytics.EventID, VisitorID: input.Analytics.VisitorID,
				PageViewID: input.Analytics.PageViewID, RuleID: input.Analytics.RuleID,
				PageURL: input.Analytics.PageURL, OccurredAt: input.Analytics.OccurredAt,
			},
			configuration, values.WidgetRuleID, values.PageURL, now,
		)
		for name, message := range analyticsFields {
			fields = append(fields, field("analytics."+name, message))
		}
		if len(analyticsFields) == 0 {
			values.Analytics = &analytics
		}
	}
	return values, fields
}

// validateReporterConsent enforces the explicit, auditable consent contract: an unchecked/absent
// opt-in must carry no email and no consent assertion, and an opt-in must carry a valid email plus
// complete, recognized consent evidence. Contradictory or unknown payloads are rejected outright
// rather than silently normalized, so the backend never has to guess reporter intent. Evidence
// fields on values are only populated once the payload is fully valid.
func validateReporterConsent(input SubmissionRequest, values *SubmissionValues) []FieldError {
	fields := make([]FieldError, 0)
	if values.ReporterEmail != nil && !validEmail(*values.ReporterEmail) {
		fields = append(fields, field("reporter_email", "must be a valid email address"))
	}
	if !input.NotifyReporter {
		if values.ReporterEmail != nil {
			fields = append(fields, field("reporter_email", "must not be submitted without granted consent"))
		}
		if input.Consent != nil {
			fields = append(fields, field("consent", "must not be submitted when notify_reporter is false"))
		}
		return fields
	}
	if values.ReporterEmail == nil {
		fields = append(fields, field("reporter_email", "is required when notify_reporter is true"))
	}
	if input.Consent == nil {
		return append(fields, field("consent", "is required when notify_reporter is true"))
	}
	if !input.Consent.Granted {
		fields = append(fields, field("consent.granted", "must be true when notify_reporter is true"))
	}
	purpose := strings.TrimSpace(input.Consent.Purpose)
	if purpose != ReporterConsentPurposeFeedbackUpdates {
		fields = append(fields, field("consent.purpose", "must be feedback_updates"))
	}
	noticeVersion := strings.TrimSpace(input.Consent.NoticeVersion)
	if _, supported := reporterConsentNoticeVersions[noticeVersion]; !supported {
		fields = append(fields, field("consent.notice_version", "is not a supported notice version"))
	}
	locale := strings.TrimSpace(input.Consent.Locale)
	if !SupportedReporterConsentLocale(locale) {
		fields = append(fields, field("consent.locale", "must identify a supported locale"))
	}
	if len(fields) == 0 {
		source := ReporterConsentSourceWidget
		values.ReporterConsentPurpose = &purpose
		values.ReporterConsentNoticeVersion = &noticeVersion
		values.ReporterConsentLocale = &locale
		values.ReporterConsentSource = &source
	}
	return fields
}

// SupportedReporterConsentLocale reports whether the consent notice exists for this locale.
func SupportedReporterConsentLocale(locale string) bool {
	locale = strings.TrimSpace(locale)
	if locale == "" || len(locale) > maxLocaleLength || !localePattern.MatchString(locale) {
		return false
	}
	switch strings.ToLower(strings.SplitN(locale, "-", 2)[0]) {
	case "en", "pt":
		return true
	default:
		return false
	}
}

func normalizeSubmission(input SubmissionRequest) SubmissionValues {
	values := SubmissionValues{
		ClientFeedbackID: strings.TrimSpace(input.ClientFeedbackID),
		WidgetRuleID:     strings.TrimSpace(input.WidgetRuleID), Rating: input.Rating,
		ReplaySessionID: trimStringPointer(input.ReplaySessionID),
		Text:            trimOptional(input.Text), Comment: trimOptional(input.Comment),
		Responses: "{}", ReporterEmail: lowerOptional(input.ReporterEmail),
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
		html, styles, ok := sanitizeElementAttachment(*input.Element)
		if ok {
			values.DOMSnapshotHTML = &html
			values.DOMSnapshotStyles = &styles
		} else {
			values.ElementContextOmitted = true
		}
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

func validateResponses(
	input SubmissionRequest,
	values *SubmissionValues,
	configuration project.PublicConfiguration,
) []FieldError {
	var selected *project.WidgetRule
	for index := range configuration.Rules {
		if configuration.Rules[index].ID == values.WidgetRuleID {
			selected = &configuration.Rules[index]
			break
		}
	}
	if selected == nil {
		return nil
	}
	blocks := selected.ResponseBlocks
	if len(blocks) == 0 {
		blocks = legacyResponseBlocks(selected.CanRate, selected.CanWriteText)
	}
	if selected.DefinitionVersion >= 1 {
		version := selected.DefinitionVersion
		values.DefinitionVersion = &version
	}

	fields := make([]FieldError, 0)
	responses := make(map[string]any, len(blocks))
	known := make(map[string]project.WidgetResponseBlock, len(blocks))
	for _, block := range blocks {
		known[block.Key] = block
		raw, provided := input.Responses[block.Key]
		if !provided {
			switch block.Type {
			case project.ResponseRating:
				if values.Rating != nil {
					responses[block.Key] = *values.Rating
					provided = true
				}
			case project.ResponseFreeText:
				if values.Text != nil {
					responses[block.Key] = *values.Text
					provided = true
				}
			}
		} else {
			value, valid := validateResponseValue(block, raw)
			if !valid {
				fields = append(fields, field("responses."+block.Key, responseValueMessage(block.Type)))
			} else if value != nil {
				responses[block.Key] = value
				provided = true
				synchronizeLegacyResponse(values, block.Type, value, &fields, block.Key)
			} else {
				provided = false
			}
		}
		if block.Required && !provided {
			fields = append(fields, field("responses."+block.Key, "is required"))
		}
	}
	for key := range input.Responses {
		if _, exists := known[key]; !exists {
			fields = append(fields, field("responses."+key, "is not defined by this widget"))
		}
	}
	if values.Comment != nil && !selected.AllowComment {
		fields = append(fields, field("comment", "is not enabled by this widget"))
	}
	if values.Comment != nil && len(*values.Comment) > maxTextLength {
		fields = append(fields, field("comment", fmt.Sprintf("must not exceed %d characters", maxTextLength)))
	}
	if (values.ReporterEmail != nil || values.NotifyReporter) && !selected.CollectContact {
		fields = append(fields, field("reporter_email", "is not enabled by this widget"))
	}
	encoded, err := json.Marshal(responses)
	if err != nil {
		fields = append(fields, field("responses", "could not be encoded"))
	} else {
		values.Responses = string(encoded)
	}
	return fields
}

func legacyResponseBlocks(canRate, canWriteText bool) []project.WidgetResponseBlock {
	blocks := make([]project.WidgetResponseBlock, 0, 2)
	if canRate {
		blocks = append(blocks, project.WidgetResponseBlock{Key: "rating", Type: project.ResponseRating})
	}
	if canWriteText {
		blocks = append(blocks, project.WidgetResponseBlock{Key: "feedback", Type: project.ResponseFreeText})
	}
	return blocks
}

func validateResponseValue(block project.WidgetResponseBlock, raw json.RawMessage) (any, bool) {
	switch block.Type {
	case project.ResponseRating:
		var value int
		if err := json.Unmarshal(raw, &value); err != nil || value < 1 || value > 5 {
			return nil, false
		}
		return value, true
	case project.ResponseFreeText:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, false
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, true
		}
		if len(value) > maxTextLength {
			return nil, false
		}
		return value, true
	case project.ResponseReaction, project.ResponseSingleChoice:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, false
		}
		for _, option := range block.Options {
			if value == option.Value {
				return value, true
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

func synchronizeLegacyResponse(
	values *SubmissionValues,
	typeOf project.WidgetResponseBlockType,
	value any,
	fields *[]FieldError,
	key string,
) {
	switch typeOf {
	case project.ResponseRating:
		rating := value.(int)
		if values.Rating != nil && *values.Rating != rating {
			*fields = append(*fields, field("responses."+key, "must match rating"))
			return
		}
		values.Rating = &rating
	case project.ResponseFreeText:
		text := value.(string)
		if values.Text != nil && *values.Text != text {
			*fields = append(*fields, field("responses."+key, "must match text"))
			return
		}
		values.Text = &text
	}
}

func responseValueMessage(blockType project.WidgetResponseBlockType) string {
	switch blockType {
	case project.ResponseRating:
		return "must be an integer between 1 and 5"
	case project.ResponseFreeText:
		return fmt.Sprintf("must be text not exceeding %d characters", maxTextLength)
	default:
		return "must be one of the configured options"
	}
}

func validateAttachments(attachments []AttachmentValues) []FieldError {
	fields := make([]FieldError, 0)
	if len(attachments) > 4 {
		fields = append(fields, field("attachments", "must not contain more than four items"))
	}
	seenIDs := map[string]struct{}{}
	seenKinds := map[AttachmentKind]struct{}{}
	var totalBytes int64
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
		} else {
			totalBytes += attachment.SizeBytes
		}
		if attachment.DurationSeconds != nil && (*attachment.DurationSeconds < 0 || *attachment.DurationSeconds > 7200) {
			fields = append(fields, field(prefix+".duration_seconds", "must be between 0 and 7200"))
		}
	}
	if totalBytes > maxAttachmentBytes {
		fields = append(fields, field(
			"attachments",
			fmt.Sprintf("total size must not exceed %d bytes", maxAttachmentBytes),
		))
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
