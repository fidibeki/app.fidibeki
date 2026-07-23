package feedback

import (
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestSanitizeElementAttachmentRemovesSourceContentAndAttributes(t *testing.T) {
	t.Parallel()
	input := ElementAttachmentInput{
		Selector: `#account-48291.customer-jane[data-email="jane.private@example.test"]`,
		HTML: `<section id="account-48291" class="customer-jane"
			data-email="jane.private@example.test" aria-label="Jane Private"
			title="reset-token-SECRET" onclick="fetch('/private')">
			Jane Private jane.private@example.test
			<!-- bearer SECRET_COMMENT -->
			<script>window.secret = "SECRET_SCRIPT"</script>
			<div><span>Account 48291</span>
				<input name="email" value="jane.private@example.test">
				<img src="https://private.example/avatar/jane.png" alt="Jane Private">
				<a href="https://private.example/reset?token=SECRET_LINK">Reset</a>
			</div>
			<ul><li data-token="SECRET_DATA">Private item</li></ul>
		</section>`,
		Styles: map[string]string{
			"display":          "block",
			"position":         "relative",
			"color":            "rgb(18, 52, 86)",
			"background-color": "url(https://private.example/avatar.png)",
			"font-family":      `"Jane Private"`,
			"width":            "640px",
		},
	}

	html, styles, ok := sanitizeElementAttachment(input)
	if !ok {
		t.Fatal("sanitizeElementAttachment() omitted a safely reducible element")
	}
	if html != `<section><div><span></span></div><ul><li></li></ul></section>` {
		t.Errorf("sanitized HTML = %q", html)
	}
	if styles != `{"color":"rgb(18, 52, 86)","display":"block","position":"relative","width":"640px"}` {
		t.Errorf("sanitized styles = %q", styles)
	}
	serialized := html + styles
	for _, sentinel := range []string{
		"Jane", "jane.private", "48291", "SECRET", "private.example",
		"onclick", "data-", "aria-", "title=", "href=", "src=", "class=", "id=",
	} {
		if strings.Contains(serialized, sentinel) {
			t.Errorf("sanitized element contains sentinel %q: %s", sentinel, serialized)
		}
	}
}

func TestSanitizeElementAttachmentFailsClosedAtStructuralBounds(t *testing.T) {
	t.Parallel()
	if _, _, ok := sanitizeElementAttachment(ElementAttachmentInput{
		Selector: "input", HTML: `<input value="jane.private@example.test">`,
	}); ok {
		t.Fatal("unsafe root element was retained")
	}

	var oversized strings.Builder
	oversized.WriteString("<div>")
	for range 140 {
		oversized.WriteString("<span></span>")
	}
	oversized.WriteString("</div>")
	if _, _, ok := sanitizeElementAttachment(ElementAttachmentInput{
		Selector: "div", HTML: oversized.String(),
	}); ok {
		t.Fatal("oversized element structure was retained")
	}

	if _, _, ok := sanitizeElementAttachment(ElementAttachmentInput{
		Selector: "div", HTML: `<div><span>`,
	}); !ok {
		t.Fatal("safely parseable legacy fragment should be reduced")
	}
}

func TestSanitizeElementStyleValueMatchesWidgetPolicy(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"12px": true, "rgb(1, 2, 3)": true, "solid": true,
		"10000px": true,
		"48291px": false,
		"url(https://private.example/avatar.png)":            false,
		"var(--customer-secret)":                             false,
		`"Jane Private"`:                                     false,
		"calc(100% - 1px)":                                   false,
		"red; background-image:url(https://private.example)": false,
	}
	for input, valid := range tests {
		if _, ok := sanitizeElementStyleValue(input); ok != valid {
			t.Errorf("sanitizeElementStyleValue(%q) valid = %t, want %t", input, ok, valid)
		}
	}
}

func TestValidateSubmissionOmitsUnsafeElementWithoutRejectingFeedback(t *testing.T) {
	t.Parallel()
	request := SubmissionRequest{
		ClientFeedbackID: "57e3ee52-4ebf-43ce-9e31-9f909628634d",
		WidgetRuleID:     "1341b36b-d02c-4398-ab29-ca935e73539a",
		Element: &ElementAttachmentInput{
			Selector: "input#customer-private",
			HTML:     `<input value="jane.private@example.test">`,
			Styles:   map[string]string{"background-image": "url(https://private.example/avatar.png)"},
		},
		Metadata: MetadataInput{
			URL: "https://example.com/checkout", ViewportWidth: 1280, ViewportHeight: 720,
		},
	}
	values, fields := ValidateSubmission(request, project.PublicConfiguration{
		Plan: tenant.PlanGrowth,
		Rules: []project.WidgetRule{{
			ID: request.WidgetRuleID, CanAttachElement: true,
		}},
	})
	if len(fields) != 0 {
		t.Fatalf("validation fields = %#v", fields)
	}
	if !values.ElementContextOmitted || values.DOMSnapshotHTML != nil ||
		values.DOMSnapshotStyles != nil {
		t.Fatalf("unsafe element values = %#v", values)
	}
	warning := elementContextOmittedWarning()
	if warning.Code != "context_omitted" || warning.Reason != "element_privacy_policy" ||
		strings.Contains(warning.Message, "jane.private") {
		t.Fatalf("element warning = %#v", warning)
	}
}
