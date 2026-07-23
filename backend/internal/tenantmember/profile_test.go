package tenantmember

import (
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestValidateOrganizationProfileNormalizesSupportedValues(t *testing.T) {
	t.Parallel()

	shortName := "  ACME  "
	website := " https://acme.test/about "
	email := " SUPPORT@ACME.TEST "
	accent := " #005F56 "
	locale := " pt-ao "
	timezone := " Africa/Luanda "
	privacy := " https://acme.test/privacy "
	terms := " https://acme.test/terms "
	profile, fields := ValidateOrganizationProfile(tenant.OrganizationProfile{
		Name: "  Acme Studio  ", ShortName: &shortName, WebsiteURL: &website,
		SupportEmail: &email, AccentColor: &accent, DefaultLocale: &locale,
		DefaultTimezone: &timezone, PrivacyPolicyURL: &privacy, TermsURL: &terms,
	})
	if len(fields) > 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if profile.Name != "Acme Studio" || profile.ShortName == nil || *profile.ShortName != "ACME" ||
		profile.WebsiteURL == nil || *profile.WebsiteURL != "https://acme.test/about" ||
		profile.SupportEmail == nil || *profile.SupportEmail != "support@acme.test" ||
		profile.AccentColor == nil || *profile.AccentColor != "#005f56" ||
		profile.DefaultLocale == nil || *profile.DefaultLocale != "pt-AO" ||
		profile.DefaultTimezone == nil || *profile.DefaultTimezone != "Africa/Luanda" ||
		profile.PrivacyPolicyURL == nil || *profile.PrivacyPolicyURL != "https://acme.test/privacy" ||
		profile.TermsURL == nil || *profile.TermsURL != "https://acme.test/terms" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestValidateOrganizationProfileClearsEmptyOptionalValues(t *testing.T) {
	t.Parallel()

	empty := "  "
	profile, fields := ValidateOrganizationProfile(tenant.OrganizationProfile{
		Name: "Acme", ShortName: &empty, WebsiteURL: &empty, SupportEmail: &empty,
		AccentColor: &empty, DefaultLocale: &empty, DefaultTimezone: &empty,
		PrivacyPolicyURL: &empty, TermsURL: &empty,
	})
	if len(fields) > 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if profile.ShortName != nil || profile.WebsiteURL != nil || profile.SupportEmail != nil ||
		profile.AccentColor != nil || profile.DefaultLocale != nil ||
		profile.DefaultTimezone != nil || profile.PrivacyPolicyURL != nil ||
		profile.TermsURL != nil {
		t.Fatalf("optional profile values = %#v, want nil", profile)
	}
}

func TestValidateOrganizationProfileRejectsUnsafeOrUnsupportedValues(t *testing.T) {
	t.Parallel()

	tooLongShortName := "123456789012345678901234567890123"
	javascriptURL := "javascript:alert(1)"
	credentialURL := "https://user:secret@acme.test"
	invalidEmail := "Acme Support <support@acme.test>"
	lowContrastAccent := "#ffffff"
	unsupportedLocale := "fr-FR"
	invalidTimezone := "Europe/Nowhere"
	_, fields := ValidateOrganizationProfile(tenant.OrganizationProfile{
		Name: "", ShortName: &tooLongShortName, WebsiteURL: &javascriptURL,
		SupportEmail: &invalidEmail, AccentColor: &lowContrastAccent,
		DefaultLocale: &unsupportedLocale, DefaultTimezone: &invalidTimezone,
		PrivacyPolicyURL: &credentialURL, TermsURL: &javascriptURL,
	})
	for _, field := range []string{
		"name", "short_name", "website_url", "support_email", "accent_color",
		"default_locale", "default_timezone", "privacy_policy_url", "terms_url",
	} {
		if fields[field] == "" {
			t.Errorf("missing validation error for %s in %#v", field, fields)
		}
	}
}
