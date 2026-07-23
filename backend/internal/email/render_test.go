package email

import (
	"strings"
	"testing"
)

func TestRenderTransactionalEmailHasTextParityAndEscapesDynamicContent(t *testing.T) {
	t.Parallel()
	content := Content{
		Preheader: "A useful preview", Heading: `Welcome <script>alert(1)</script>`,
		Workspace: `Acme & Sons`, Body: []string{`Hello <img src=x onerror=alert(1)>`, "Your trial is ready."},
		Metadata:      []Metadata{{Label: "Plan", Value: "Growth"}},
		Action:        &Link{Label: "Open workspace", URL: "https://acme.fidibeki.com/getting-started"},
		ShowActionURL: true, Footer: "You received this because you created the workspace.",
		FooterLinks: []Link{{Label: "Contact support", URL: "mailto:support@fidibeki.com"}},
		LogoURL:     "https://fidibeki.com/brand/fidibeki-color.png",
	}
	textBody, htmlBody, err := Render(content)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, expected := range []string{
		"Welcome <script>alert(1)</script>", "Workspace: Acme & Sons", "Plan: Growth",
		"Open workspace: https://acme.fidibeki.com/getting-started", "Contact support: mailto:support@fidibeki.com",
	} {
		if !strings.Contains(textBody, expected) {
			t.Errorf("text missing %q: %s", expected, textBody)
		}
	}
	if strings.Contains(htmlBody, "<script>") || strings.Contains(htmlBody, "<img src=x") {
		t.Errorf("HTML contains unescaped content: %s", htmlBody)
	}
	for _, expected := range []string{
		`alt="Fidibeki"`, `width="111" height="28"`, "A useful preview", "Acme &amp; Sons", "Open workspace",
		"https://acme.fidibeki.com/getting-started", "If the button does not work",
	} {
		if !strings.Contains(htmlBody, expected) {
			t.Errorf("HTML missing %q", expected)
		}
	}
	if strings.Contains(htmlBody, ">Fidibeki</strong>") {
		t.Errorf("HTML duplicates the wordmark with adjacent brand text")
	}
}

func TestRenderTransactionalEmailValidatesStructuredLinksAndOptionalSections(t *testing.T) {
	t.Parallel()
	base := Content{Heading: "Sign in", LogoURL: "https://fidibeki.com/brand/fidibeki-color.png"}
	textBody, htmlBody, err := Render(base)
	if err != nil || strings.Contains(textBody, "Workspace:") || strings.Contains(htmlBody, "If the button") {
		t.Fatalf("optional Render() = text %q HTML %q error %v", textBody, htmlBody, err)
	}
	for _, content := range []Content{
		{LogoURL: base.LogoURL},
		{Heading: "Sign in", LogoURL: "/logo.png"},
		{Heading: "Sign in", LogoURL: base.LogoURL, Action: &Link{Label: "Open", URL: "/relative"}},
		{Heading: "Sign in", LogoURL: base.LogoURL, FooterLinks: []Link{{Label: "Bad", URL: "javascript:alert(1)"}}},
	} {
		if _, _, err := Render(content); err == nil {
			t.Errorf("Render(%#v) error = nil", content)
		}
	}
}

func TestComposeAndBrandLogoURL(t *testing.T) {
	t.Parallel()
	logo, err := BrandLogoURL("http://api.fidibeki.localhost:8088", "fidibeki.localhost")
	if err != nil || logo != "http://fidibeki.localhost:8088/brand/fidibeki-color.png" {
		t.Fatalf("BrandLogoURL() = %q, %v", logo, err)
	}
	message, err := Compose(Message{
		From: "Fidibeki <auth@fidibeki.com>", To: "owner@example.com", Subject: "Welcome",
	}, Content{Heading: "Welcome", LogoURL: logo})
	if err != nil || message.Text == "" || message.HTML == "" {
		t.Fatalf("Compose() = %#v, %v", message, err)
	}
}
