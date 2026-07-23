package feedback

import "testing"

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
