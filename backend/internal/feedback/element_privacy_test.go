package feedback

import (
	"strings"
	"testing"

	"github.com/fidibeki/app.fidibeki/backend/internal/project"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

func TestSanitizeElementAttachmentAcceptsOnlyCanonicalStructure(t *testing.T) {
	t.Parallel()
	input := ElementAttachmentInput{
		Selector: `main > section:nth-of-type(2)`,
		HTML:     `<section><div><span></span></div><ul><li></li></ul></section>`,
		Styles: map[string]string{
			"display": "block", "position": "relative",
			"color": "rgb(18, 52, 86)", "width": "640px",
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
}

func TestSanitizeElementAttachmentRejectsNonCanonicalSourceValues(t *testing.T) {
	t.Parallel()
	input := ElementAttachmentInput{
		Selector: `#account-48291.customer-jane[data-email="jane.private@example.test"]`,
		HTML: `<section id="account-48291" class="customer-jane"
			data-email="jane.private@example.test" aria-label="Jane Private"
			title="reset-token-SECRET" onclick="fetch('/private')">
			Jane Private jane.private@example.test
			<div><span>Account 48291</span></div>
		</section>`,
		Styles: map[string]string{"display": "block"},
	}
	if _, _, ok := sanitizeElementAttachment(input); ok {
		t.Fatal("non-canonical selector and source DOM were accepted")
	}
	input.Selector = "main > section"
	if _, _, ok := sanitizeElementAttachment(input); ok {
		t.Fatal("source DOM carrying private values was accepted")
	}
	input.HTML = `<section><div><span></span></div></section>`
	input.Styles["background-image"] = "url(https://private.example/avatar.png)"
	if _, _, ok := sanitizeElementAttachment(input); ok {
		t.Fatal("unexpected or unsafe style was silently retained")
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

	var tooDeep strings.Builder
	for range maximumElementDepth + 1 {
		tooDeep.WriteString("<div>")
	}
	for range maximumElementDepth + 1 {
		tooDeep.WriteString("</div>")
	}
	if _, _, ok := sanitizeElementAttachment(ElementAttachmentInput{
		Selector: "div", HTML: tooDeep.String(),
	}); ok {
		t.Fatal("element structure beyond the depth limit was retained")
	}

	if _, _, ok := sanitizeElementAttachment(ElementAttachmentInput{
		Selector: "div", HTML: `<div><span>`,
	}); ok {
		t.Fatal("non-canonical legacy fragment was accepted")
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
