package feedback

import (
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestNormalizeSubmissionDiscardsRawReferrerAndUserAgent(t *testing.T) {
	t.Parallel()
	values := normalizeSubmission(SubmissionRequest{
		Metadata: MetadataInput{
			URL:       "https://example.com/account/jane.private?token=SECRET#profile",
			Referrer:  "https://external.example/search?q=jane.private@example.test",
			UserAgent: "CustomerBrowser/1.0 account-48291",
			Browser:   "Firefox",
			OS:        "Linux",
			Device:    "desktop",
		},
	})
	if values.Referrer != nil || values.UserAgent != nil {
		t.Fatalf("raw context retained: referrer=%v user_agent=%v", values.Referrer, values.UserAgent)
	}
	if values.Browser == nil || *values.Browser != "Firefox" ||
		values.OS == nil || *values.OS != "Linux" ||
		values.Device == nil || *values.Device != "desktop" {
		t.Fatalf("coarse context was not retained: %#v", values)
	}
}

func TestValidateSubmissionProjectsPageAgainstAuthorizedRuleAndOrigin(t *testing.T) {
	t.Parallel()
	ruleID := "1341b36b-d02c-4398-ab29-ca935e73539a"
	prefix := "https://example.com/accounts"
	base := SubmissionRequest{
		ClientFeedbackID: "57e3ee52-4ebf-43ce-9e31-9f909628634d",
		WidgetRuleID:     ruleID,
		Metadata: MetadataInput{
			URL:           "https://user:password@example.com/accounts/jane.private@example.test/48291?token=SECRET#profile",
			ViewportWidth: 1280, ViewportHeight: 720,
		},
	}
	configuration := project.PublicConfiguration{
		Plan: tenant.PlanGrowth,
		Rules: []project.WidgetRule{{
			ID: ruleID, MatcherType: project.MatcherURLPrefix, MatcherValue: &prefix,
		}},
	}
	values, fields := ValidateSubmissionForOriginAt(
		base, configuration, "https://example.com", time.Now().UTC(),
	)
	if len(fields) != 0 {
		t.Fatalf("validation fields = %#v", fields)
	}
	if values.PageURL != prefix {
		t.Fatalf("safe page = %q, want %q", values.PageURL, prefix)
	}
	for _, sentinel := range []string{"jane.private", "48291", "SECRET", "password"} {
		if strings.Contains(values.PageURL, sentinel) {
			t.Errorf("safe page contains sentinel %q: %s", sentinel, values.PageURL)
		}
	}

	crossOrigin := base
	crossOrigin.Metadata.URL = "https://other.example/accounts/jane"
	_, fields = ValidateSubmissionForOriginAt(
		crossOrigin, configuration, "https://example.com", time.Now().UTC(),
	)
	if !hasField(fields, "metadata.url") {
		t.Fatalf("cross-origin fields = %#v", fields)
	}
}
