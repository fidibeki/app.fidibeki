package mobilecapture

import (
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestCaptureSessionAuthorizationUsesCurrentLifecycleState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	base := CaptureSession{TenantStatus: "active"}
	tests := []struct {
		name    string
		mutate  func(*CaptureSession)
		allowed bool
	}{
		{name: "active", mutate: func(*CaptureSession) {}, allowed: true},
		{name: "active issuer expired", mutate: func(session *CaptureSession) {
			session.IssuerExpiresAt = &past
		}},
		{name: "deletion requested", mutate: func(session *CaptureSession) {
			session.TenantDeletionRequestedAt = &past
		}},
		{name: "suspended", mutate: func(session *CaptureSession) {
			session.TenantStatus = "suspended"
		}},
		{name: "unexpired trial", mutate: func(session *CaptureSession) {
			session.TenantStatus = "trial"
			session.TenantTrialEndsAt = &future
		}, allowed: true},
		{name: "expired trial", mutate: func(session *CaptureSession) {
			session.TenantStatus = "trial"
			session.TenantTrialEndsAt = &past
		}},
		{name: "trial without deadline", mutate: func(session *CaptureSession) {
			session.TenantStatus = "trial"
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			session := base
			test.mutate(&session)
			if got := session.AuthorizedAt(now); got != test.allowed {
				t.Fatalf("AuthorizedAt() = %t, want %t", got, test.allowed)
			}
		})
	}
}

func TestValidateCreateSessionNormalizesBoundNativeContext(t *testing.T) {
	t.Parallel()
	contextURL := "https://App.Example.com/chart/?asset=BAI#price"
	values, fields := ValidateCreateSession(CreateSessionRequest{
		ProjectID:           "1be03fd8-7560-4d4d-ae39-8e67a35d56d7",
		WidgetDefinitionKey: "mobile-feedback", Platform: PlatformIOS,
		ApplicationID: "com.example.fidibeki", Screen: "/chart/?asset=BAI#price",
		ContextURL: &contextURL, DisplayLocale: "PT_AO",
	})
	if len(fields) > 0 {
		t.Fatalf("ValidateCreateSession fields = %#v", fields)
	}
	if values.Screen != "/chart" || values.ContextURL == nil ||
		*values.ContextURL != "https://app.example.com/chart" ||
		values.DisplayLocale != "pt-ao" {
		t.Fatalf("normalized values = %#v", values)
	}
}

func TestValidateCreateSessionRejectsUnboundedFields(t *testing.T) {
	t.Parallel()
	insecure := "http://app.example.com/chart"
	_, fields := ValidateCreateSession(CreateSessionRequest{
		ProjectID: "not-a-uuid", WidgetDefinitionKey: "bad key",
		Platform: "web", ApplicationID: "single", Screen: "chart",
		ContextURL: &insecure, DisplayLocale: "invalid locale",
	})
	for _, field := range []string{
		"project_id", "widget_definition_key", "platform", "application_id",
		"screen", "context_url", "display_locale",
	} {
		if fields[field] == "" {
			t.Errorf("field %q was not rejected: %#v", field, fields)
		}
	}
}

func TestResolveConfigurationSelectsExactPublishedLocaleAndPublicSafeLauncher(t *testing.T) {
	t.Parallel()
	configuration := mobileTestConfiguration()
	values := CreateSessionValues{
		ProjectID: configuration.ProjectID, WidgetDefinitionKey: "mobile-feedback",
		DisplayLocale: "pt-ao",
	}
	resolved, fields := ResolveConfiguration(configuration, values)
	if len(fields) > 0 {
		t.Fatalf("ResolveConfiguration fields = %#v", fields)
	}
	public := PublicConfigurationFrom(resolved)
	if public.DisplayLocale != "pt-ao" ||
		public.Definition.Localization.Prompt != "Como correu?" ||
		public.ControllerProfile == nil || public.ContactNotice == nil ||
		public.ContactNotice.Locale != "pt-ao" ||
		public.Definition.Presentation != project.PresentationLauncher {
		t.Fatalf("public configuration = %#v", public)
	}
}

func TestResolveConfigurationRejectsInactiveAndForeignContext(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*project.WidgetRule){
		func(rule *project.WidgetRule) { rule.Enabled = false },
		func(rule *project.WidgetRule) { rule.DefinitionStatus = project.DefinitionPaused },
	} {
		configuration := mobileTestConfiguration()
		mutate(&configuration.Rules[0])
		_, fields := ResolveConfiguration(configuration, CreateSessionValues{
			ProjectID: configuration.ProjectID, WidgetDefinitionKey: "mobile-feedback",
			DisplayLocale: "en",
		})
		if fields["widget_definition_key"] == "" {
			t.Fatalf("definition mutation passed: %#v", configuration.Rules[0])
		}
	}
	configuration := mobileTestConfiguration()
	foreign := "https://evil.example/chart"
	_, fields := ResolveConfiguration(configuration, CreateSessionValues{
		ProjectID: configuration.ProjectID, WidgetDefinitionKey: "mobile-feedback",
		DisplayLocale: "en", ContextURL: &foreign,
	})
	if fields["context_url"] == "" {
		t.Fatalf("foreign context passed: %#v", fields)
	}
}

func TestResolveConfigurationAcceptsEveryActivePresentationForHostTriggeredMobileCapture(t *testing.T) {
	t.Parallel()
	for _, presentation := range []project.WidgetPresentation{
		project.PresentationLauncher,
		project.PresentationPrompt,
		project.PresentationInline,
		project.PresentationReaction,
	} {
		configuration := mobileTestConfiguration()
		configuration.Rules[0].Presentation = presentation
		resolved, fields := ResolveConfiguration(configuration, CreateSessionValues{
			ProjectID: configuration.ProjectID, WidgetDefinitionKey: "mobile-feedback",
			DisplayLocale: "en",
		})
		if len(fields) > 0 || resolved.Definition.Presentation != presentation {
			t.Fatalf("presentation %q rejected: resolved=%#v fields=%#v", presentation, resolved, fields)
		}
	}
}

func TestResolveSessionConfigurationIgnoresWebPresentationMetadata(t *testing.T) {
	t.Parallel()
	for _, presentation := range []project.WidgetPresentation{
		project.PresentationLauncher,
		project.PresentationPrompt,
		project.PresentationInline,
		project.PresentationReaction,
	} {
		configuration := mobileTestConfiguration()
		configuration.Rules[0].Presentation = presentation
		session := CaptureSession{
			WidgetRuleID:             configuration.Rules[0].ID,
			DisplayLocale:            "en",
			DefinitionVersion:        configuration.Rules[0].DefinitionVersion,
			ControllerProfileVersion: configuration.ControllerProfileVersion,
		}
		resolved, err := ResolveSessionConfiguration(configuration, session)
		if err != nil || resolved.Definition.Presentation != presentation {
			t.Fatalf("presentation %q invalidated session: resolved=%#v err=%v", presentation, resolved, err)
		}
	}
}

func TestResolveLocalizationUsesDeterministicFallbackOrder(t *testing.T) {
	t.Parallel()
	localizations := map[string]project.WidgetLocalization{
		"fr": {Prompt: "Français"},
		"pt": {Prompt: "Português"},
	}
	for _, testCase := range []struct {
		name           string
		requested      string
		projectDefault string
		wantLocale     string
		wantPrompt     string
	}{
		{
			name: "requested primary", requested: "pt-AO", projectDefault: "fr",
			wantLocale: "pt", wantPrompt: "Português",
		},
		{
			name: "project default", requested: "es", projectDefault: "fr-FR",
			wantLocale: "fr", wantPrompt: "Français",
		},
		{
			name: "sorted fallback", requested: "es", projectDefault: "de",
			wantLocale: "fr", wantPrompt: "Français",
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			locale, localization, ok := resolveLocalization(
				localizations, testCase.requested, testCase.projectDefault,
			)
			if !ok || locale != testCase.wantLocale || localization.Prompt != testCase.wantPrompt {
				t.Fatalf(
					"resolveLocalization() = (%q, %#v, %t), want (%q, %q, true)",
					locale, localization, ok, testCase.wantLocale, testCase.wantPrompt,
				)
			}
		})
	}
	if locale, localization, ok := resolveLocalization(nil, "en", "en"); ok ||
		locale != "" || localization.Prompt != "" {
		t.Fatalf("empty resolveLocalization() = (%q, %#v, %t)", locale, localization, ok)
	}
}

func mobileTestConfiguration() project.PublicConfiguration {
	return project.PublicConfiguration{
		TenantID: "tenant-id", ProjectID: "1be03fd8-7560-4d4d-ae39-8e67a35d56d7",
		DefaultLocale: "en", AllowedDomains: []string{"app.example.com"},
		Plan: tenant.PlanScale, Status: tenant.StatusActive, ControllerProfileVersion: 3,
		ControllerProfile: &project.ControllerProfile{
			Version: 3, Name: "Acme", PrivacyContactEmail: "privacy@example.com",
			PrivacyRightsEmail: "rights@example.com",
			PrivacyPolicyURL:   "https://example.com/privacy",
		},
		Rules: []project.WidgetRule{{
			ID: "rule-id", Enabled: true, DefinitionKey: "mobile-feedback",
			DefinitionVersion: 4, DefinitionStatus: project.DefinitionActive,
			Presentation: project.PresentationLauncher, CanAttachScreenshot: true,
			CollectContact: true,
			Localizations: map[string]project.WidgetLocalization{
				"en": {
					Prompt: "How was it?", PrivacyPurposeSummary: "Updates",
					PrivacyLawfulBasisSummary: "Consent", PrivacyRetentionSummary: "30 days",
				},
				"pt-ao": {
					Prompt: "Como correu?", PrivacyPurposeSummary: "Atualizações",
					PrivacyLawfulBasisSummary: "Consentimento",
					PrivacyRetentionSummary:   "30 dias",
				},
			},
		}},
	}
}
