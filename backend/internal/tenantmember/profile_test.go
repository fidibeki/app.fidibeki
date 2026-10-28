package tenantmember

import (
	"testing"

	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

func TestNormalizeMemberLocale(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		input   *string
		want    *string
		wantErr bool
	}{
		"inherit nil":    {input: nil, want: nil},
		"inherit blank":  {input: stringPointer("  "), want: nil},
		"English":        {input: stringPointer(" EN "), want: stringPointer("en")},
		"Portuguese":     {input: stringPointer("pt"), want: stringPointer("pt")},
		"Angola":         {input: stringPointer(" pt_ao "), want: stringPointer("pt-AO")},
		"Portugal":       {input: stringPointer("PT-pt"), want: stringPointer("pt-PT")},
		"pseudo":         {input: stringPointer("en_xa"), want: stringPointer("en-XA")},
		"unsupported":    {input: stringPointer("fr-FR"), wantErr: true},
		"invalid syntax": {input: stringPointer("not a locale"), wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, message := NormalizeMemberLocale(test.input)
			if test.wantErr {
				if message == "" {
					t.Fatalf("NormalizeMemberLocale(%v) = %v, want error", test.input, got)
				}
				return
			}
			if message != "" || !optionalStringsEqual(got, test.want) {
				t.Fatalf("NormalizeMemberLocale(%v) = %v/%q, want %v", test.input, got, message, test.want)
			}
		})
	}
}

func TestNormalizeMemberTimezone(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		input   *string
		want    *string
		wantErr bool
	}{
		"inherit nil":   {input: nil, want: nil},
		"inherit blank": {input: stringPointer("  "), want: nil},
		"UTC":           {input: stringPointer(" UTC "), want: stringPointer("UTC")},
		"Angola":        {input: stringPointer(" Africa/Luanda "), want: stringPointer("Africa/Luanda")},
		"Portugal":      {input: stringPointer("Europe/Lisbon"), want: stringPointer("Europe/Lisbon")},
		"local alias":   {input: stringPointer("Local"), wantErr: true},
		"unknown":       {input: stringPointer("Nowhere/Unknown"), wantErr: true},
		"unsafe":        {input: stringPointer("../UTC"), wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, message := NormalizeMemberTimezone(test.input)
			if test.wantErr {
				if message == "" {
					t.Fatalf("NormalizeMemberTimezone(%v) = %v, want error", test.input, got)
				}
				return
			}
			if message != "" || !optionalStringsEqual(got, test.want) {
				t.Fatalf("NormalizeMemberTimezone(%v) = %v/%q, want %v", test.input, got, message, test.want)
			}
		})
	}
}

func TestValidateOrganizationProfileNormalizesSupportedValues(t *testing.T) {
	t.Parallel()

	shortName := "  ACME  "
	website := " https://acme.test/about "
	email := " SUPPORT@ACME.TEST "
	accent := " #005F56 "
	locale := " pt-ao "
	timezone := " Africa/Luanda "
	privacy := " https://acme.test/privacy "
	privacyContact := " PRIVACY@ACME.TEST "
	privacyRights := " RIGHTS@ACME.TEST "
	terms := " https://acme.test/terms "
	profile, fields := ValidateOrganizationProfile(tenant.OrganizationProfile{
		Name: "  Acme Studio  ", ShortName: &shortName, WebsiteURL: &website,
		SupportEmail: &email, AccentColor: &accent, DefaultLocale: &locale,
		DefaultTimezone: &timezone, PrivacyPolicyURL: &privacy,
		PrivacyContactEmail: &privacyContact, PrivacyRightsEmail: &privacyRights,
		TermsURL: &terms,
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
		profile.PrivacyContactEmail == nil || *profile.PrivacyContactEmail != "privacy@acme.test" ||
		profile.PrivacyRightsEmail == nil || *profile.PrivacyRightsEmail != "rights@acme.test" ||
		profile.TermsURL == nil || *profile.TermsURL != "https://acme.test/terms" {
		t.Fatalf("profile = %#v", profile)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestValidateOrganizationProfileClearsEmptyOptionalValues(t *testing.T) {
	t.Parallel()

	empty := "  "
	profile, fields := ValidateOrganizationProfile(tenant.OrganizationProfile{
		Name: "Acme", ShortName: &empty, WebsiteURL: &empty, SupportEmail: &empty,
		AccentColor: &empty, DefaultLocale: &empty, DefaultTimezone: &empty,
		PrivacyPolicyURL: &empty, PrivacyContactEmail: &empty,
		PrivacyRightsEmail: &empty, TermsURL: &empty,
	})
	if len(fields) > 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if profile.ShortName != nil || profile.WebsiteURL != nil || profile.SupportEmail != nil ||
		profile.AccentColor != nil || profile.DefaultLocale != nil ||
		profile.DefaultTimezone != nil || profile.PrivacyPolicyURL != nil ||
		profile.PrivacyContactEmail != nil || profile.PrivacyRightsEmail != nil ||
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
	invalidAccent := "#fffff"
	unsupportedLocale := "fr-FR"
	invalidTimezone := "Europe/Nowhere"
	_, fields := ValidateOrganizationProfile(tenant.OrganizationProfile{
		Name: "", ShortName: &tooLongShortName, WebsiteURL: &javascriptURL,
		SupportEmail: &invalidEmail, AccentColor: &invalidAccent,
		DefaultLocale: &unsupportedLocale, DefaultTimezone: &invalidTimezone,
		PrivacyPolicyURL: &credentialURL, TermsURL: &javascriptURL,
		PrivacyContactEmail: &invalidEmail, PrivacyRightsEmail: &invalidEmail,
	})
	for _, field := range []string{
		"name", "short_name", "website_url", "support_email", "accent_color",
		"default_locale", "default_timezone", "privacy_policy_url", "terms_url",
		"privacy_contact_email", "privacy_rights_email",
	} {
		if fields[field] == "" {
			t.Errorf("missing validation error for %s in %#v", field, fields)
		}
	}
}
