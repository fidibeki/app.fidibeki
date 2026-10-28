package tenantmember

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/fidibeki/app.fidibeki/backend/internal/widgetconfig"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"
)

func NormalizeMemberName(input *string) (*string, string) {
	name := optionalTrim(input)
	if name != nil && utf8.RuneCountInString(*name) > 120 {
		return nil, "must be at most 120 characters"
	}
	return name, ""
}

func NormalizeMemberLocale(input *string) (*string, string) {
	locale := optionalTrim(input)
	if locale == nil {
		return nil, ""
	}
	normalized := strings.ToLower(strings.ReplaceAll(*locale, "_", "-"))
	value, supported := map[string]string{
		"en": "en", "pt": "pt", "pt-ao": "pt-AO", "pt-pt": "pt-PT", "en-xa": "en-XA",
	}[normalized]
	if !supported {
		return nil, "must be a supported English or Portuguese locale"
	}
	return &value, ""
}

func NormalizeMemberTimezone(input *string) (*string, string) {
	timezone := optionalTrim(input)
	if timezone == nil {
		return nil, ""
	}
	if len(*timezone) > 100 || *timezone == "Local" {
		return nil, "must be a valid IANA timezone"
	}
	if _, err := time.LoadLocation(*timezone); err != nil {
		return nil, "must be a valid IANA timezone"
	}
	return timezone, ""
}

func (s Store) UpdateOwnProfile(
	ctx context.Context,
	query db.Querier,
	tenantID, memberID string,
	patch MemberProfilePatch,
) (Member, error) {
	var previousName, previousLocale, previousTimezone *string
	if err := query.QueryRow(ctx, `
		SELECT name, locale, timezone
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE
	`, tenantID, memberID).Scan(
		&previousName, &previousLocale, &previousTimezone,
	); errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrMemberActorInvalid
	} else if err != nil {
		return Member{}, fmt.Errorf("lock member profile: %w", err)
	}
	name, locale, timezone := previousName, previousLocale, previousTimezone
	if patch.SetName {
		name = patch.Name
	}
	if patch.SetLocale {
		locale = patch.Locale
	}
	if patch.SetTimezone {
		timezone = patch.Timezone
	}
	if optionalStringsEqual(previousName, name) &&
		optionalStringsEqual(previousLocale, locale) &&
		optionalStringsEqual(previousTimezone, timezone) {
		member, err := scanMemberWithRole(query.QueryRow(ctx, `
			SELECT member.id, member.tenant_id, member.email, member.name,
			       member.avatar_version, member.locale, member.timezone, member.permissions,
			       member.status, member.authorization_version,
			       member.id = tenant.owner_member_id, member.last_login_at IS NOT NULL
			FROM tenant_members AS member
			JOIN tenants AS tenant ON tenant.id = member.tenant_id
			WHERE member.tenant_id = $1 AND member.id = $2 AND member.status = 'active'
		`, tenantID, memberID))
		if err != nil {
			return Member{}, fmt.Errorf("read unchanged member profile: %w", err)
		}
		return member, nil
	}
	member, err := scanMemberWithRole(query.QueryRow(ctx, `
		WITH updated AS (
			UPDATE tenant_members
			SET name = $3, locale = $4, timezone = $5
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'
			RETURNING *
		)
		SELECT updated.id, updated.tenant_id, updated.email, updated.name,
		       updated.avatar_version, updated.locale, updated.timezone,
		       updated.permissions, updated.status,
		       updated.authorization_version, updated.id = tenant.owner_member_id,
		       updated.last_login_at IS NOT NULL
		FROM updated
		JOIN tenants AS tenant ON tenant.id = updated.tenant_id
	`, tenantID, memberID, name, locale, timezone))
	if err != nil {
		return Member{}, fmt.Errorf("update member profile: %w", err)
	}
	if err := recordTenantAccessEvent(
		ctx, query, tenantID, "member.profile_updated", &memberID, &memberID,
		map[string]any{
			"previous_name": previousName, "name": member.Name,
			"previous_locale": previousLocale, "locale": member.Locale,
			"previous_timezone": previousTimezone, "timezone": member.Timezone,
		},
	); err != nil {
		return Member{}, err
	}
	return member, nil
}

func ValidateOrganizationProfile(
	input tenant.OrganizationProfile,
) (tenant.OrganizationProfile, map[string]string) {
	fields := map[string]string{}
	profile := tenant.OrganizationProfile{
		Name:                strings.TrimSpace(input.Name),
		ShortName:           optionalTrim(input.ShortName),
		WebsiteURL:          optionalTrim(input.WebsiteURL),
		SupportEmail:        optionalLower(input.SupportEmail),
		AccentColor:         optionalLower(input.AccentColor),
		DefaultLocale:       optionalTrim(input.DefaultLocale),
		DefaultTimezone:     optionalTrim(input.DefaultTimezone),
		PrivacyPolicyURL:    optionalTrim(input.PrivacyPolicyURL),
		PrivacyContactEmail: optionalLower(input.PrivacyContactEmail),
		PrivacyRightsEmail:  optionalLower(input.PrivacyRightsEmail),
		TermsURL:            optionalTrim(input.TermsURL),
	}
	if count := utf8.RuneCountInString(profile.Name); count < 1 || count > 120 {
		fields["name"] = "must be between 1 and 120 characters"
	}
	if profile.ShortName != nil && utf8.RuneCountInString(*profile.ShortName) > 32 {
		fields["short_name"] = "must be at most 32 characters"
	}
	for name, value := range map[string]*string{
		"website_url": profile.WebsiteURL, "terms_url": profile.TermsURL,
	} {
		if value != nil && !validOrganizationURL(*value) {
			fields[name] = "must be an absolute HTTP or HTTPS URL"
		}
	}
	if profile.PrivacyPolicyURL != nil && !validPrivacyPolicyURL(*profile.PrivacyPolicyURL) {
		fields["privacy_policy_url"] = "must be an absolute HTTPS URL without credentials, query parameters, or a fragment"
	}
	for name, value := range map[string]*string{
		"support_email":         profile.SupportEmail,
		"privacy_contact_email": profile.PrivacyContactEmail,
		"privacy_rights_email":  profile.PrivacyRightsEmail,
	} {
		if value != nil && !validOrganizationEmail(*value) {
			fields[name] = "must be a valid email address"
		}
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

func validPrivacyPolicyURL(candidate string) bool {
	if utf8.RuneCountInString(candidate) > 2048 {
		return false
	}
	parsed, err := url.ParseRequestURI(candidate)
	return err == nil && parsed.IsAbs() && parsed.Host != "" && parsed.User == nil &&
		parsed.Scheme == "https" && parsed.RawQuery == "" && parsed.Fragment == ""
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
	if err := requireActiveMemberActor(ctx, query, tenantID, actorMemberID); err != nil {
		return tenant.OrganizationProfile{}, err
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
		    privacy_contact_email = $9::citext,
		    privacy_rights_email = $10::citext,
		    privacy_profile_version = privacy_profile_version + CASE
		        WHEN ROW(
		            name, privacy_policy_url, privacy_contact_email,
		            privacy_rights_email
		        ) IS DISTINCT FROM ROW($2::text, $8::text, $9::citext, $10::citext)
		        THEN 1 ELSE 0
		    END,
		    terms_url = $11,
		    widget_appearance_defaults = CASE
		        WHEN $12::text IS NULL THEN widget_appearance_defaults - 'accent_color'
		        ELSE jsonb_set(
		            widget_appearance_defaults, '{accent_color}', to_jsonb($12::text), true
		        )
		    END
		WHERE id = $1
	RETURNING name, short_name, website_url, support_email::text,
		          widget_appearance_defaults->>'accent_color', default_locale, default_timezone,
		          privacy_policy_url, privacy_contact_email::text, privacy_rights_email::text,
		          privacy_profile_version, terms_url, logo_version::text
	`, tenantID, input.Name, input.ShortName, input.WebsiteURL, input.SupportEmail,
		input.DefaultLocale, input.DefaultTimezone, input.PrivacyPolicyURL,
		input.PrivacyContactEmail, input.PrivacyRightsEmail, input.TermsURL,
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
		       tenant.default_timezone, tenant.privacy_policy_url,
		       tenant.privacy_contact_email::text, tenant.privacy_rights_email::text,
		       tenant.privacy_profile_version, tenant.terms_url,
		       tenant.logo_version::text
	FROM tenants AS tenant
`

func scanOrganizationProfile(row rowScanner) (tenant.OrganizationProfile, error) {
	var profile tenant.OrganizationProfile
	err := row.Scan(
		&profile.Name, &profile.ShortName, &profile.WebsiteURL, &profile.SupportEmail,
		&profile.AccentColor, &profile.DefaultLocale, &profile.DefaultTimezone,
		&profile.PrivacyPolicyURL, &profile.PrivacyContactEmail, &profile.PrivacyRightsEmail,
		&profile.PrivacyProfileVersion, &profile.TermsURL, &profile.LogoVersion,
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
		optionalStringsEqual(left.PrivacyContactEmail, right.PrivacyContactEmail) &&
		optionalStringsEqual(left.PrivacyRightsEmail, right.PrivacyRightsEmail) &&
		left.PrivacyProfileVersion == right.PrivacyProfileVersion &&
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
		"privacy_policy_url":      profile.PrivacyPolicyURL,
		"privacy_contact_email":   profile.PrivacyContactEmail,
		"privacy_rights_email":    profile.PrivacyRightsEmail,
		"privacy_profile_version": profile.PrivacyProfileVersion,
		"terms_url":               profile.TermsURL,
	}
}
