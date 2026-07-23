package tenantmember

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetconfig"
	"golang.org/x/text/language"
)

func ValidateOrganizationProfile(
	input tenant.OrganizationProfile,
) (tenant.OrganizationProfile, map[string]string) {
	fields := map[string]string{}
	profile := tenant.OrganizationProfile{
		Name:             strings.TrimSpace(input.Name),
		ShortName:        optionalTrim(input.ShortName),
		WebsiteURL:       optionalTrim(input.WebsiteURL),
		SupportEmail:     optionalLower(input.SupportEmail),
		AccentColor:      optionalLower(input.AccentColor),
		DefaultLocale:    optionalTrim(input.DefaultLocale),
		DefaultTimezone:  optionalTrim(input.DefaultTimezone),
		PrivacyPolicyURL: optionalTrim(input.PrivacyPolicyURL),
		TermsURL:         optionalTrim(input.TermsURL),
	}
	if count := utf8.RuneCountInString(profile.Name); count < 1 || count > 120 {
		fields["name"] = "must be between 1 and 120 characters"
	}
	if profile.ShortName != nil && utf8.RuneCountInString(*profile.ShortName) > 32 {
		fields["short_name"] = "must be at most 32 characters"
	}
	for name, value := range map[string]*string{
		"website_url": profile.WebsiteURL, "privacy_policy_url": profile.PrivacyPolicyURL,
		"terms_url": profile.TermsURL,
	} {
		if value != nil && !validOrganizationURL(*value) {
			fields[name] = "must be an absolute HTTP or HTTPS URL"
		}
	}
	if profile.SupportEmail != nil && !validOrganizationEmail(*profile.SupportEmail) {
		fields["support_email"] = "must be a valid email address"
	}
	if profile.DefaultLocale != nil {
		normalized, ok := normalizeOrganizationLocale(*profile.DefaultLocale)
		if !ok {
			fields["default_locale"] = "must be a supported English or Portuguese locale"
		} else {
			profile.DefaultLocale = &normalized
		}
	}
	if profile.DefaultTimezone != nil {
		if *profile.DefaultTimezone == "Local" {
			fields["default_timezone"] = "must be a valid IANA timezone"
		} else if _, err := time.LoadLocation(*profile.DefaultTimezone); err != nil {
			fields["default_timezone"] = "must be a valid IANA timezone"
		}
	}
	appearance := widgetconfig.AppearanceOverrides{AccentColor: profile.AccentColor}
	for name, message := range widgetconfig.Validate(appearance) {
		fields[name] = message
	}
	return profile, fields
}

func optionalTrim(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func optionalLower(value *string) *string {
	normalized := optionalTrim(value)
	if normalized == nil {
		return nil
	}
	lower := strings.ToLower(*normalized)
	return &lower
}

func validOrganizationURL(candidate string) bool {
	if utf8.RuneCountInString(candidate) > 2048 {
		return false
	}
	parsed, err := url.ParseRequestURI(candidate)
	return err == nil && parsed.IsAbs() && parsed.Host != "" && parsed.User == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https")
}

func validOrganizationEmail(candidate string) bool {
	if utf8.RuneCountInString(candidate) > 320 || strings.ContainsAny(candidate, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(candidate)
	return err == nil && address.Name == "" && address.Address == candidate &&
		strings.Contains(candidate, "@")
}

func normalizeOrganizationLocale(candidate string) (string, bool) {
	tag, err := language.Parse(candidate)
	if err != nil {
		return "", false
	}
	base, _ := tag.Base()
	if base.String() != "en" && base.String() != "pt" {
		return "", false
	}
	return tag.String(), true
}

func (s Store) GetOrganizationProfile(
	ctx context.Context,
	query db.Querier,
	tenantID string,
) (tenant.OrganizationProfile, error) {
	profile, err := scanOrganizationProfile(query.QueryRow(ctx, organizationProfileSelect+`
		WHERE tenant.id = $1
	`, tenantID))
	if err != nil {
		return tenant.OrganizationProfile{}, fmt.Errorf("get organization profile: %w", err)
	}
	return profile, nil
}

func (s Store) UpdateOrganizationProfile(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	input tenant.OrganizationProfile,
) (tenant.OrganizationProfile, error) {
	before, err := scanOrganizationProfile(query.QueryRow(ctx, organizationProfileSelect+`
		WHERE tenant.id = $1
		FOR UPDATE
	`, tenantID))
	if err != nil {
		return tenant.OrganizationProfile{}, fmt.Errorf("lock organization profile: %w", err)
	}
	after, err := scanOrganizationProfile(query.QueryRow(ctx, `
		UPDATE tenants
		SET name = $2,
		    short_name = $3,
		    website_url = $4,
		    support_email = $5,
		    default_locale = $6,
		    default_timezone = $7,
		    privacy_policy_url = $8,
		    terms_url = $9,
		    widget_appearance_defaults = CASE
		        WHEN $10::text IS NULL THEN widget_appearance_defaults - 'accent_color'
		        ELSE jsonb_set(
		            widget_appearance_defaults, '{accent_color}', to_jsonb($10::text), true
		        )
		    END
		WHERE id = $1
		RETURNING name, short_name, website_url, support_email::text,
		          widget_appearance_defaults->>'accent_color', default_locale, default_timezone,
		          privacy_policy_url, terms_url, logo_version::text
	`, tenantID, input.Name, input.ShortName, input.WebsiteURL, input.SupportEmail,
		input.DefaultLocale, input.DefaultTimezone, input.PrivacyPolicyURL, input.TermsURL,
		input.AccentColor))
	if err != nil {
		return tenant.OrganizationProfile{}, fmt.Errorf("update organization profile: %w", err)
	}
	if organizationProfilesEqual(before, after) {
		return after, nil
	}
	if err := recordTenantAccessEvent(
		ctx, query, tenantID, "organization.profile_updated", &actorMemberID, nil,
		map[string]any{
			"before": organizationProfileAuditData(before),
			"after":  organizationProfileAuditData(after),
		},
	); err != nil {
		return tenant.OrganizationProfile{}, err
	}
	return after, nil
}

const organizationProfileSelect = `
	SELECT tenant.name, tenant.short_name, tenant.website_url, tenant.support_email::text,
	       tenant.widget_appearance_defaults->>'accent_color', tenant.default_locale,
	       tenant.default_timezone, tenant.privacy_policy_url, tenant.terms_url,
	       tenant.logo_version::text
	FROM tenants AS tenant
`

func scanOrganizationProfile(row rowScanner) (tenant.OrganizationProfile, error) {
	var profile tenant.OrganizationProfile
	err := row.Scan(
		&profile.Name, &profile.ShortName, &profile.WebsiteURL, &profile.SupportEmail,
		&profile.AccentColor, &profile.DefaultLocale, &profile.DefaultTimezone,
		&profile.PrivacyPolicyURL, &profile.TermsURL, &profile.LogoVersion,
	)
	return profile, err
}

func organizationProfilesEqual(left, right tenant.OrganizationProfile) bool {
	return left.Name == right.Name &&
		optionalStringsEqual(left.ShortName, right.ShortName) &&
		optionalStringsEqual(left.WebsiteURL, right.WebsiteURL) &&
		optionalStringsEqual(left.SupportEmail, right.SupportEmail) &&
		optionalStringsEqual(left.AccentColor, right.AccentColor) &&
		optionalStringsEqual(left.DefaultLocale, right.DefaultLocale) &&
		optionalStringsEqual(left.DefaultTimezone, right.DefaultTimezone) &&
		optionalStringsEqual(left.PrivacyPolicyURL, right.PrivacyPolicyURL) &&
		optionalStringsEqual(left.TermsURL, right.TermsURL)
}

func optionalStringsEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func organizationProfileAuditData(profile tenant.OrganizationProfile) map[string]any {
	return map[string]any{
		"name": profile.Name, "short_name": profile.ShortName, "website_url": profile.WebsiteURL,
		"support_email": profile.SupportEmail, "accent_color": profile.AccentColor,
		"default_locale": profile.DefaultLocale, "default_timezone": profile.DefaultTimezone,
		"privacy_policy_url": profile.PrivacyPolicyURL, "terms_url": profile.TermsURL,
	}
}
