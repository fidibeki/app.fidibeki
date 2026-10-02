package mobilecapture

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/google/uuid"
)

var (
	definitionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}[a-z0-9]$`)
	applicationIDPattern = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_-]*)+$`,
	)
	localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
	screenPattern = regexp.MustCompile(`^/[A-Za-z0-9._~!$&'()*+,;=:@%/-]*$`)
)

func ValidateCreateSession(input CreateSessionRequest) (CreateSessionValues, FieldErrors) {
	values := CreateSessionValues{
		ProjectID:           strings.TrimSpace(input.ProjectID),
		WidgetDefinitionKey: strings.TrimSpace(input.WidgetDefinitionKey),
		Platform:            input.Platform,
		ApplicationID:       strings.TrimSpace(input.ApplicationID),
		DisplayLocale:       normalizeLocale(input.DisplayLocale),
	}
	fields := FieldErrors{}
	if _, err := uuid.Parse(values.ProjectID); err != nil {
		fields["project_id"] = "must be a UUID"
	}
	if !definitionKeyPattern.MatchString(values.WidgetDefinitionKey) {
		fields["widget_definition_key"] = "must identify a valid widget definition"
	}
	if !values.Platform.Valid() {
		fields["platform"] = "must be ios or android"
	}
	if len(values.ApplicationID) < 3 || len(values.ApplicationID) > 255 ||
		!applicationIDPattern.MatchString(values.ApplicationID) {
		fields["application_id"] = "must be a reverse-DNS application identifier"
	}
	screen, err := normalizeScreen(input.Screen)
	if err != nil {
		fields["screen"] = err.Error()
	} else {
		values.Screen = screen
	}
	if values.DisplayLocale == "" || len(values.DisplayLocale) > 35 ||
		!localePattern.MatchString(values.DisplayLocale) {
		fields["display_locale"] = "must be a valid locale"
	}
	if input.ContextURL != nil {
		contextURL, err := canonicalHTTPSURL(*input.ContextURL)
		if err != nil {
			fields["context_url"] = err.Error()
		} else {
			values.ContextURL = &contextURL
		}
	}
	return values, fields
}

func ResolveConfiguration(
	configuration project.PublicConfiguration,
	values CreateSessionValues,
) (ResolvedConfiguration, FieldErrors) {
	fields := FieldErrors{}
	if configuration.ProjectID != values.ProjectID {
		fields["project_id"] = "does not identify an available project"
		return ResolvedConfiguration{}, fields
	}
	var definition *project.WidgetRule
	for index := range configuration.Rules {
		candidate := &configuration.Rules[index]
		if candidate.DefinitionKey == values.WidgetDefinitionKey {
			definition = candidate
			break
		}
	}
	if definition == nil || !definition.Enabled ||
		definition.DefinitionStatus != project.DefinitionActive {
		fields["widget_definition_key"] = "does not identify an active feedback definition"
		return ResolvedConfiguration{}, fields
	}
	if values.ContextURL != nil {
		origin, err := project.CanonicalOrigin(*values.ContextURL)
		if err != nil || !widgetapi.OriginAllowed(origin, configuration.AllowedDomains) {
			fields["context_url"] = "host must be allowed by the project"
		}
	}
	displayLocale, localization, ok := resolveLocalization(
		definition.Localizations, values.DisplayLocale, configuration.DefaultLocale,
	)
	if !ok {
		fields["display_locale"] = "does not resolve to a published localization"
	}
	if len(fields) > 0 {
		return ResolvedConfiguration{}, fields
	}
	return ResolvedConfiguration{
		Configuration: configuration, Definition: *definition,
		DisplayLocale: displayLocale, Localization: localization,
	}, nil
}

func PublicConfigurationFrom(resolved ResolvedConfiguration) PublicConfiguration {
	definition := resolved.Definition
	collectContact := definition.CollectContact &&
		resolved.Configuration.ControllerProfile != nil &&
		project.HasCompleteContactNotices(map[string]project.WidgetLocalization{
			resolved.DisplayLocale: resolved.Localization,
		})
	response := PublicConfiguration{
		SchemaVersion: configurationSchemaV1,
		ProjectID:     resolved.Configuration.ProjectID,
		DisplayLocale: resolved.DisplayLocale,
		Definition: PublicDefinition{
			ID: definition.ID, DefinitionKey: definition.DefinitionKey,
			DefinitionVersion: definition.DefinitionVersion,
			Presentation:      definition.Presentation, LauncherVariant: definition.LauncherVariant,
			Objective: definition.Objective, ResponseBlocks: definition.ResponseBlocks,
			Localization: resolved.Localization, AllowComment: definition.AllowComment,
			CollectContact: collectContact, CanAttachScreenshot: definition.CanAttachScreenshot,
			RecurrenceMode: definition.RecurrenceMode, RecurrenceDays: definition.RecurrenceDays,
			AIProcessing: PublicAIProcessing{
				Enabled:          definition.AIProcessing.Enabled,
				Provider:         definition.AIProcessing.Provider,
				ProviderLabel:    definition.AIProcessing.ProviderLabel,
				InputCategories:  definition.AIProcessing.InputCategories,
				PrivacyPolicyURL: definition.AIProcessing.PrivacyPolicyURL,
				PolicyVersion:    definition.AIProcessing.PolicyVersion,
			},
			Appearance: definition.Appearance,
		},
	}
	if collectContact {
		profile := *resolved.Configuration.ControllerProfile
		response.ControllerProfile = &profile
		response.ContactNotice = &PublicContactNotice{
			NoticeVersion:            feedback.ReporterConsentNoticeVersion,
			ControllerProfileVersion: profile.Version,
			WidgetDefinitionVersion:  definition.DefinitionVersion,
			Locale:                   resolved.DisplayLocale, ControllerName: profile.Name,
			PrivacyContactEmail: profile.PrivacyContactEmail,
			PrivacyRightsEmail:  profile.PrivacyRightsEmail,
			PrivacyPolicyURL:    profile.PrivacyPolicyURL,
			PurposeSummary:      resolved.Localization.PrivacyPurposeSummary,
			LawfulBasisSummary:  resolved.Localization.PrivacyLawfulBasisSummary,
			RetentionSummary:    resolved.Localization.PrivacyRetentionSummary,
		}
	}
	return response
}

func ResolveSessionConfiguration(
	configuration project.PublicConfiguration,
	session CaptureSession,
) (ResolvedConfiguration, error) {
	var definition *project.WidgetRule
	for index := range configuration.Rules {
		if configuration.Rules[index].ID == session.WidgetRuleID {
			definition = &configuration.Rules[index]
			break
		}
	}
	controllerVersion := configuration.ControllerProfileVersion
	if definition == nil || !definition.Enabled ||
		definition.DefinitionStatus != project.DefinitionActive ||
		definition.DefinitionVersion != session.DefinitionVersion ||
		controllerVersion != session.ControllerProfileVersion {
		return ResolvedConfiguration{}, ErrConfigurationChanged
	}
	var localization project.WidgetLocalization
	localizationFound := false
	for locale, candidate := range definition.Localizations {
		if normalizeLocale(locale) == session.DisplayLocale {
			localization = candidate
			localizationFound = true
			break
		}
	}
	if !localizationFound {
		return ResolvedConfiguration{}, ErrConfigurationChanged
	}
	if session.ContextURL != nil {
		origin, err := project.CanonicalOrigin(*session.ContextURL)
		if err != nil || !widgetapi.OriginAllowed(origin, configuration.AllowedDomains) {
			return ResolvedConfiguration{}, ErrConfigurationChanged
		}
	}
	return ResolvedConfiguration{
		Configuration: configuration, Definition: *definition,
		DisplayLocale: session.DisplayLocale, Localization: localization,
	}, nil
}

func normalizeScreen(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "", fmt.Errorf("must be an absolute logical screen path")
	}
	if index := strings.IndexAny(value, "?#"); index >= 0 {
		value = value[:index]
	}
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	if value != "/" {
		value = strings.TrimRight(value, "/")
	}
	if value == "" {
		value = "/"
	}
	if len(value) > 512 || !screenPattern.MatchString(value) {
		return "", fmt.Errorf("must be a normalized logical screen path")
	}
	return value, nil
}

func canonicalHTTPSURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("must be an absolute HTTPS URL")
	}
	page, err := project.CanonicalizePage(raw)
	if err != nil || !strings.HasPrefix(page.Target, "https://") {
		return "", fmt.Errorf("must be an absolute HTTPS URL")
	}
	if len(page.Target) > 4096 {
		return "", fmt.Errorf("must not exceed 4096 characters")
	}
	return page.Target, nil
}

func resolveLocalization(
	localizations map[string]project.WidgetLocalization,
	requested string,
	projectDefault string,
) (string, project.WidgetLocalization, bool) {
	normalized := make(map[string]string, len(localizations))
	for locale := range localizations {
		normalized[normalizeLocale(locale)] = locale
	}
	for _, candidate := range []string{requested, primaryLocale(requested), projectDefault, primaryLocale(projectDefault)} {
		if actual, exists := normalized[normalizeLocale(candidate)]; exists {
			return normalizeLocale(actual), localizations[actual], true
		}
	}
	locales := make([]string, 0, len(localizations))
	for locale := range localizations {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	if len(locales) == 0 {
		return "", project.WidgetLocalization{}, false
	}
	return normalizeLocale(locales[0]), localizations[locales[0]], true
}

func normalizeLocale(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
}

func primaryLocale(value string) string {
	return strings.SplitN(normalizeLocale(value), "-", 2)[0]
}
