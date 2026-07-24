package integration

import (
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

func ValidateCreate(input CreateInput, allowInsecureURL ...bool) (CreateValues, []FieldError) {
	values := CreateValues{
		Kind: input.Kind, Name: strings.TrimSpace(input.Name),
		WebhookURL: strings.TrimSpace(input.WebhookURL), Active: true,
	}
	if input.Active != nil {
		values.Active = *input.Active
	}
	fields := make([]FieldError, 0)
	if input.Kind != KindSlack && input.Kind != KindTeams && input.Kind != KindWebhook {
		fields = append(fields, FieldError{Field: "kind", Message: "must be slack, teams, or webhook"})
	}
	if values.Name == "" || utf8.RuneCountInString(values.Name) > 100 {
		fields = append(fields, FieldError{Field: "name", Message: "must contain 1 to 100 characters"})
	}
	if !validWebhookURL(values.WebhookURL, allowInsecureURL...) {
		fields = append(fields, FieldError{Field: "webhook_url", Message: "must be a valid HTTPS URL"})
	}
	if input.Channel != nil {
		channel := strings.TrimSpace(*input.Channel)
		if input.Kind != KindSlack {
			fields = append(fields, FieldError{Field: "channel", Message: "is supported only for Slack"})
		} else if channel == "" || utf8.RuneCountInString(channel) > 100 {
			fields = append(fields, FieldError{Field: "channel", Message: "must contain 1 to 100 characters"})
		} else {
			values.Channel = &channel
		}
	}
	values.Events, fields = validateEvents(input.Events, fields)
	values.CustomHeaders, fields = validateHeaders(input.Kind, input.CustomHeaders, fields)
	values.PrivacyFields, fields = validatePrivacyFields(input.PrivacyFields, fields)
	return values, fields
}

func ValidatePrivacyUpdate(input UpdatePrivacyInput) ([]string, []FieldError) {
	return validatePrivacyFields(input.PrivacyFields, nil)
}

func validatePrivacyFields(values []string, fields []FieldError) ([]string, []FieldError) {
	allowed := make(map[string]struct{}, len(allPrivacyFields))
	for _, field := range allPrivacyFields {
		allowed[field] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	validated := make([]string, 0, len(values))
	for _, field := range values {
		field = strings.TrimSpace(field)
		if _, ok := allowed[field]; !ok {
			return nil, append(fields, FieldError{
				Field: "privacy_fields", Message: "contains an unsupported field group",
			})
		}
		if _, exists := seen[field]; exists {
			continue
		}
		seen[field] = struct{}{}
		validated = append(validated, field)
	}
	sort.Strings(validated)
	return validated, fields
}

func validWebhookURL(value string, allowInsecureURL ...bool) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	if len(allowInsecureURL) == 0 || !allowInsecureURL[0] {
		return false
	}
	host := parsed.Hostname()
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func validateEvents(events []string, fields []FieldError) ([]string, []FieldError) {
	allowed := map[string]struct{}{
		EventFeedbackCreated: {}, EventIssueCreated: {},
		EventIssueUpdated: {}, EventIssueResolved: {},
	}
	seen := make(map[string]struct{}, len(events))
	validated := make([]string, 0, len(events))
	for _, event := range events {
		event = strings.TrimSpace(event)
		if _, ok := allowed[event]; !ok {
			fields = append(fields, FieldError{Field: "events", Message: "contains an unsupported event"})
			return nil, fields
		}
		if _, exists := seen[event]; exists {
			continue
		}
		seen[event] = struct{}{}
		validated = append(validated, event)
	}
	if len(validated) == 0 {
		fields = append(fields, FieldError{Field: "events", Message: "must contain at least one event"})
	}
	sort.Strings(validated)
	return validated, fields
}

func validateHeaders(kind Kind, headers map[string]string, fields []FieldError) (map[string]string, []FieldError) {
	if len(headers) == 0 {
		return nil, fields
	}
	if kind != KindWebhook {
		return nil, append(fields, FieldError{Field: "custom_headers", Message: "is supported only for webhooks"})
	}
	if len(headers) > 20 {
		return nil, append(fields, FieldError{Field: "custom_headers", Message: "must contain at most 20 headers"})
	}
	blocked := map[string]struct{}{
		"Content-Length": {}, "Content-Type": {}, "Host": {},
		"X-Fidibeki-Delivery": {}, "X-Fidibeki-Event": {}, "X-Fidibeki-Signature": {},
	}
	validated := make(map[string]string, len(headers))
	total := 0
	for name, value := range headers {
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(name))
		if canonical == "" || canonical != strings.TrimSpace(name) && !validHeaderName(name) || !validHeaderName(canonical) {
			return nil, append(fields, FieldError{Field: "custom_headers", Message: "contains an invalid header name"})
		}
		if _, denied := blocked[canonical]; denied {
			return nil, append(fields, FieldError{Field: "custom_headers", Message: "contains a reserved header"})
		}
		if strings.ContainsAny(value, "\r\n") || len(value) > 2048 {
			return nil, append(fields, FieldError{Field: "custom_headers", Message: "contains an invalid header value"})
		}
		total += len(canonical) + len(value)
		validated[canonical] = value
	}
	if total > 8192 {
		return nil, append(fields, FieldError{Field: "custom_headers", Message: "must be at most 8192 bytes"})
	}
	return validated, fields
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		digit := character >= '0' && character <= '9'
		if !letter && !digit && !strings.ContainsRune("!#$%&'*+-.^_`|~", character) {
			return false
		}
	}
	return true
}
