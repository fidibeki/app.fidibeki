package testimonial

import (
	"strings"
	"testing"
	"time"
)

func TestComposeRequestMessageUsesTenantURLAndLocale(t *testing.T) {
	t.Parallel()
	expiresAt := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	prompt := `Tell us what changed <after launch>.`
	portuguese := "pt-PT"

	tests := []struct {
		name        string
		locale      *string
		wantSubject string
		wantText    []string
	}{
		{
			name:        "english fallback",
			wantSubject: "Acme Research asked for your testimonial",
			wantText: []string{
				"Share your experience",
				"Acme Research invited you to share a testimonial.",
				"Tell us what changed <after launch>.",
				"Give your testimonial: https://acme.fidibeki.test/testimonials/collect/secret-token",
				"Expires: Aug 26, 2026",
			},
		},
		{
			name:        "portuguese",
			locale:      &portuguese,
			wantSubject: "Acme Research pediu o seu testemunho",
			wantText: []string{
				"Partilhe a sua experiência",
				"Acme Research convidou-o a partilhar um testemunho.",
				"Dar o meu testemunho: https://acme.fidibeki.test/testimonials/collect/secret-token",
				"Expira em: 26/08/2026",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			message, err := composeRequestMessage(requestMessageInput{
				From:             "Fidibeki <auth@fidibeki.test>",
				To:               "customer@example.test",
				TenantName:       "Acme Research",
				TenantSlug:       "acme",
				Token:            "secret-token",
				Prompt:           &prompt,
				Locale:           test.locale,
				ExpiresAt:        expiresAt,
				RequestID:        "request-id",
				PublicBaseURL:    "https://api.fidibeki.test",
				WebPublicBaseURL: "https://fidibeki.test",
				BaseDomain:       "fidibeki.test",
			})
			if err != nil {
				t.Fatalf("composeRequestMessage() error = %v", err)
			}
			if message.From != "Fidibeki <auth@fidibeki.test>" ||
				message.To != "customer@example.test" {
				t.Fatalf("message envelope = from %q to %q", message.From, message.To)
			}
			if message.Subject != test.wantSubject {
				t.Errorf("subject = %q, want %q", message.Subject, test.wantSubject)
			}
			if message.IdempotencyKey != "testimonial-request:request-id" {
				t.Errorf("idempotency key = %q", message.IdempotencyKey)
			}
			for _, want := range test.wantText {
				if !strings.Contains(message.Text, want) {
					t.Errorf("text does not contain %q:\n%s", want, message.Text)
				}
			}
			if !strings.Contains(message.HTML, "&lt;after launch&gt;") ||
				strings.Contains(message.HTML, "<after launch>") {
				t.Errorf("customer prompt is not HTML-escaped:\n%s", message.HTML)
			}
		})
	}
}
