package emailfeedback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

func ValidateInvitation(input InvitationInput) (InvitationValues, FieldErrors) {
	values := InvitationValues{
		CampaignID:      strings.TrimSpace(input.CampaignID),
		ExternalEventID: strings.TrimSpace(input.ExternalEventID),
		RecipientEmail:  strings.ToLower(strings.TrimSpace(input.RecipientEmail)),
	}
	fields := FieldErrors{}
	if _, err := uuid.Parse(values.CampaignID); err != nil {
		fields["campaign_id"] = "must identify an email feedback campaign"
	}
	if count := utf8.RuneCountInString(values.ExternalEventID); count < 1 || count > 200 ||
		containsControl(values.ExternalEventID) {
		fields["external_event_id"] = "must contain between 1 and 200 characters without controls"
	}
	parsedEmail, err := mail.ParseAddress(values.RecipientEmail)
	if err != nil || parsedEmail.Address != values.RecipientEmail ||
		utf8.RuneCountInString(values.RecipientEmail) > 320 ||
		containsControl(values.RecipientEmail) {
		fields["recipient_email"] = "must be a valid email address"
	} else {
		values.RecipientDigest = digestString(values.RecipientEmail)
	}
	if input.Locale != nil {
		values.RequestedLocale = normalizeLocale(*input.Locale)
		if !localePattern.MatchString(values.RequestedLocale) ||
			len(values.RequestedLocale) > 35 {
			fields["locale"] = "must identify a valid locale"
		}
	}
	if input.ContextURL != nil {
		contextURL, valid := normalizeInvitationContextURL(*input.ContextURL)
		if !valid {
			fields["context_url"] = "must be an HTTPS URL without credentials, query, or fragment"
		} else {
			values.ContextURL = &contextURL
		}
	}
	if len(fields) == 0 {
		values.RequestDigest = invitationRequestDigest(values)
	}
	return values, fields
}

func invitationRequestDigest(values InvitationValues) string {
	payload, _ := json.Marshal(struct {
		CampaignID      string  `json:"campaign_id"`
		ExternalEventID string  `json:"external_event_id"`
		RecipientEmail  string  `json:"recipient_email"`
		RequestedLocale string  `json:"requested_locale"`
		ContextURL      *string `json:"context_url"`
	}{
		CampaignID: values.CampaignID, ExternalEventID: values.ExternalEventID,
		RecipientEmail: values.RecipientEmail, RequestedLocale: values.RequestedLocale,
		ContextURL: values.ContextURL,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func digestString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func normalizeInvitationContextURL(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 || strings.Contains(value, "#") {
		return "", false
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.ForceQuery || parsed.Opaque != "" {
		return "", false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return "", false
	}
	port := parsed.Port()
	if port == "443" {
		port = ""
	}
	if port == "" {
		if strings.Contains(hostname, ":") {
			parsed.Host = "[" + hostname + "]"
		} else {
			parsed.Host = hostname
		}
	} else {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	parsed.Scheme = "https"
	normalized := parsed.String()
	if len(normalized) > 2048 {
		return "", false
	}
	return normalized, true
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
