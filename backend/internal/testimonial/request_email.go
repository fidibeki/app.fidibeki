package testimonial

import (
	"fmt"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

type requestMessageInput struct {
	From             string
	To               string
	TenantName       string
	TenantSlug       string
	Token            string
	Prompt           *string
	Locale           *string
	ExpiresAt        time.Time
	RequestID        string
	PublicBaseURL    string
	WebPublicBaseURL string
	BaseDomain       string
}

type requestMessageCopy struct {
	subject   string
	preheader string
	heading   string
	intro     string
	expiry    string
	action    string
	footer    string
}

func composeRequestMessage(input requestMessageInput) (email.Message, error) {
	collectionURL, err := tenant.PublicURL(
		input.WebPublicBaseURL,
		input.BaseDomain,
		input.TenantSlug,
		"/testimonials/collect/"+input.Token,
	)
	if err != nil {
		return email.Message{}, fmt.Errorf("build testimonial collection URL: %w", err)
	}
	logoURL, err := email.BrandLogoURL(input.PublicBaseURL)
	if err != nil {
		return email.Message{}, fmt.Errorf("build testimonial email logo URL: %w", err)
	}

	copy := englishRequestMessageCopy(input.TenantName)
	if input.Locale != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(*input.Locale)), "pt") {
		copy = portugueseRequestMessageCopy(input.TenantName)
	}
	body := []string{copy.intro}
	if input.Prompt != nil {
		if prompt := strings.TrimSpace(*input.Prompt); prompt != "" {
			body = append(body, prompt)
		}
	}
	body = append(body, copy.footer)

	return email.Compose(email.Message{
		From:           input.From,
		To:             input.To,
		Subject:        copy.subject,
		IdempotencyKey: "testimonial-request:" + input.RequestID,
	}, email.Content{
		Preheader: copy.preheader,
		Heading:   copy.heading,
		Workspace: input.TenantName,
		Body:      body,
		Metadata: []email.Metadata{{
			Label: copy.expiry,
			Value: localizedRequestExpiry(input.Locale, input.ExpiresAt),
		}},
		Action: &email.Link{
			Label: copy.action,
			URL:   collectionURL.String(),
		},
		ShowActionURL: true,
		LogoURL:       logoURL,
	})
}

func englishRequestMessageCopy(tenantName string) requestMessageCopy {
	return requestMessageCopy{
		subject:   tenantName + " asked for your testimonial",
		preheader: tenantName + " invited you to share your experience.",
		heading:   "Share your experience",
		intro:     tenantName + " invited you to share a testimonial.",
		expiry:    "Expires",
		action:    "Give your testimonial",
		footer:    "Nothing will be published until you review the details and give your consent.",
	}
}

func portugueseRequestMessageCopy(tenantName string) requestMessageCopy {
	return requestMessageCopy{
		subject:   tenantName + " pediu o seu testemunho",
		preheader: tenantName + " convidou-o a partilhar a sua experiência.",
		heading:   "Partilhe a sua experiência",
		intro:     tenantName + " convidou-o a partilhar um testemunho.",
		expiry:    "Expira em",
		action:    "Dar o meu testemunho",
		footer:    "Nada será publicado antes de rever os dados e dar o seu consentimento.",
	}
}

func localizedRequestExpiry(locale *string, expiresAt time.Time) string {
	if locale != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(*locale)), "pt") {
		return expiresAt.Format("02/01/2006")
	}
	return expiresAt.Format("Jan 2, 2006")
}
