package issuetracker

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// allowedPriorities are the values an external issue may carry. "none" and "" both mean the
// provider's own default.
var allowedPriorities = map[string]struct{}{
	"": {}, "none": {}, "low": {}, "medium": {}, "high": {}, "urgent": {},
}

// ValidateConnect checks the credential shape before anything is sent to the provider. Jira
// needs a site plus an account email; Linear needs only a workspace API key.
func ValidateConnect(
	provider Provider,
	input ConnectInput,
	allowInsecureURLs bool,
) (Credentials, []FieldError) {
	fields := make([]FieldError, 0)
	credentials := Credentials{
		Email:    strings.TrimSpace(input.Email),
		APIToken: strings.TrimSpace(input.APIToken),
	}
	if credentials.APIToken == "" || len(credentials.APIToken) > 500 {
		fields = append(fields, FieldError{
			Field: "api_token", Message: "must contain 1 to 500 characters",
		})
	}
	switch provider {
	case ProviderJira:
		site, ok := ValidateSiteURL(input.SiteURL, allowInsecureURLs)
		if !ok {
			fields = append(fields, FieldError{
				Field: "site_url", Message: "must be the HTTPS URL of a Jira site",
			})
		}
		credentials.SiteURL = site
		if credentials.Email == "" || !strings.Contains(credentials.Email, "@") ||
			utf8.RuneCountInString(credentials.Email) > 200 {
			fields = append(fields, FieldError{
				Field: "email", Message: "must be the Atlassian account email for the API token",
			})
		}
	case ProviderLinear:
		if strings.TrimSpace(input.SiteURL) != "" {
			fields = append(fields, FieldError{
				Field: "site_url", Message: "is not used by Linear",
			})
		}
		if credentials.Email != "" {
			fields = append(fields, FieldError{
				Field: "email", Message: "is not used by Linear",
			})
		}
	default:
		fields = append(fields, FieldError{Field: "provider", Message: "must be jira or linear"})
	}
	return credentials, fields
}

// ValidateCreate normalizes the reviewed external issue. An empty title falls back to the
// Fidibeki issue title so the caller can send only what the user edited.
func ValidateCreate(input CreateInput, issue IssueSnapshot) (CreateInput, []FieldError) {
	fields := make([]FieldError, 0)
	normalized := CreateInput{
		Provider:       input.Provider,
		Title:          strings.TrimSpace(input.Title),
		Description:    strings.TrimSpace(input.Description),
		Priority:       strings.TrimSpace(strings.ToLower(input.Priority)),
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
	}
	if !normalized.Provider.Valid() {
		fields = append(fields, FieldError{Field: "provider", Message: "must be jira or linear"})
	}
	if normalized.Title == "" {
		normalized.Title = strings.TrimSpace(issue.Title)
	}
	if normalized.Title == "" || utf8.RuneCountInString(normalized.Title) > 255 {
		fields = append(fields, FieldError{Field: "title", Message: "must contain 1 to 255 characters"})
	}
	if utf8.RuneCountInString(normalized.Description) > 10000 {
		fields = append(fields, FieldError{
			Field: "description", Message: "must contain at most 10000 characters",
		})
	}
	if _, ok := allowedPriorities[normalized.Priority]; !ok {
		fields = append(fields, FieldError{
			Field: "priority", Message: "must be none, low, medium, high, or urgent",
		})
	}
	if normalized.IdempotencyKey != "" {
		length := utf8.RuneCountInString(normalized.IdempotencyKey)
		if length < 8 || length > 200 {
			fields = append(fields, FieldError{
				Field: "idempotency_key", Message: "must contain 8 to 200 characters",
			})
		}
	}
	return normalized, fields
}

// DeriveIdempotencyKey builds the fallback key for a client that did not send one. It is
// derived from the issue, the provider, and the reviewed content, so resubmitting the same
// request converges on the same attempt while an edited request is a genuinely new one.
func DeriveIdempotencyKey(issueID string, input CreateInput) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		issueID, string(input.Provider), input.Title, input.Description, input.Priority,
	}, "\x00")))
	return "derived:" + hex.EncodeToString(digest[:16])
}
