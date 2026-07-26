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
		LogoURL:     "https://api.fidibeki.com" + BrandLogoPath,
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
		`alt="Fidibeki"`, `width="111"`, "A useful preview", "Acme &amp; Sons", "Open workspace",
		"https://acme.fidibeki.com/getting-started", "If the button does not work",
	} {
		if !strings.Contains(htmlBody, expected) {
			t.Errorf("HTML missing %q", expected)
		}
	}
	if strings.Contains(htmlBody, ">Fidibeki</strong>") {
		t.Errorf("HTML duplicates the wordmark with adjacent brand text")
	}
	// A recipient whose client blocks or fails to load the logo sees the alt text
	// instead, and only sees it in full if the image is not pinned to the artwork's
	// height. Phone clients that ignore alt text entirely then fall back to the
	// heading rather than to a clipped line of nothing.
	logo := htmlBody[strings.Index(htmlBody, "<img"):]
	logo = logo[:strings.Index(logo, ">")+1]
	if strings.Contains(logo, `height="`) {
		t.Errorf("logo pins the alt-text fallback to the artwork height: %s", logo)
	}
	for _, expected := range []string{";height:auto;", "font-family:", "font-weight:bold"} {
		if !strings.Contains(logo, expected) {
			t.Errorf("logo missing %q: %s", expected, logo)
		}
	}
	// Phone clients that repaint light emails for dark mode invert the white card
	// out from under a dark wordmark. Declaring the scheme opts the message out.
	for _, expected := range []string{`name="color-scheme" content="light"`, "color-scheme:light"} {
		if !strings.Contains(htmlBody, expected) {
			t.Errorf("HTML missing %q", expected)
		}
	}
}

func TestRenderTransactionalEmailValidatesStructuredLinksAndOptionalSections(t *testing.T) {
	t.Parallel()
	base := Content{Heading: "Sign in", LogoURL: "https://api.fidibeki.com" + BrandLogoPath}
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
	// The API host is kept as given. Trading it for the base domain reaches whichever
	// tier owns the apex, and that is the marketing site in production and the app in
	// staging — only one of them has ever served the wordmark.
	logo, err := BrandLogoURL("http://api.fidibeki.localhost:8088")
	if err != nil || logo != "http://api.fidibeki.localhost:8088/brand/wordmark.png" {
		t.Fatalf("BrandLogoURL() = %q, %v", logo, err)
	}
	for _, invalid := range []string{"", "fidibeki.com", "/brand", "://nope"} {
		if _, err := BrandLogoURL(invalid); err == nil {
			t.Errorf("BrandLogoURL(%q) error = nil", invalid)
		}
	}
	message, err := Compose(Message{
		From: "Fidibeki <auth@fidibeki.com>", To: "owner@example.com", Subject: "Welcome",
	}, Content{Heading: "Welcome", LogoURL: logo})
	if err != nil || message.Text == "" || message.HTML == "" {
		t.Fatalf("Compose() = %#v, %v", message, err)
	}
}
