package project

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestGeneratePublicKeyHasExpectedFormatAndUniqueValues(t *testing.T) {
	t.Parallel()
	pattern := regexp.MustCompile(`^pk_live_[A-Za-z0-9]{22}$`)
	seen := map[string]struct{}{}
	for range 100 {
		key, err := GeneratePublicKey()
		if err != nil {
			t.Fatalf("GeneratePublicKey() error = %v", err)
		}
		if !pattern.MatchString(key) {
			t.Fatalf("key = %q", key)
		}
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate key = %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestNormalizeAllowedDomains(t *testing.T) {
	t.Parallel()
	normalized, err := NormalizeAllowedDomains([]string{
		" Example.COM ", "*.Sub.Example.com", "example.com", "localhost", "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("NormalizeAllowedDomains() error = %v", err)
	}
	want := []string{"example.com", "*.sub.example.com", "localhost", "127.0.0.1"}
	if !reflect.DeepEqual(normalized, want) {
		t.Errorf("normalized = %#v, want %#v", normalized, want)
	}

	invalid := []string{
		"https://example.com", "example.com/path", "example.com:443", "*example.com",
		"foo.*.example.com", "*.127.0.0.1", "-example.com", "example..com", "example.com.",
	}
	for _, domain := range invalid {
		domain := domain
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			if _, err := NormalizeAllowedDomains([]string{domain}); err == nil {
				t.Errorf("NormalizeAllowedDomains(%q) error = nil", domain)
			}
		})
	}
}

func TestValidateProjectReturnsFieldErrors(t *testing.T) {
	t.Parallel()
	name, _, fields := ValidateProject("   ", []string{"https://example.com"})
	if name != "" || fields["name"] == "" || fields["allowed_domains"] == "" {
		t.Fatalf("name = %q, fields = %#v", name, fields)
	}
	if _, _, fields := ValidateProject(strings.Repeat("a", 121), nil); fields["name"] == "" {
		t.Errorf("long name fields = %#v", fields)
	}
}

func TestValidateWidgetRuleNormalizesDefaults(t *testing.T) {
	t.Parallel()
	delay := 2
	values, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: " Checkout ", MatcherType: MatcherAllPages,
		TriggerType: TriggerDelay, TriggerDelaySeconds: &delay,
	}, tenant.Tenant{Plan: tenant.PlanStarter})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.Name != "Checkout" || !values.Enabled || !values.CanRate || !values.CanWriteText {
		t.Errorf("boolean/name defaults = %#v", values)
	}
	if values.Position != PositionBottomLeft || values.Theme != ThemeLight || values.ReplayMode != ReplayNone {
		t.Errorf("appearance defaults = %#v", values)
	}
	if values.ReplayMaxDurationSeconds != 1800 || values.ReplayMaxEvents != 50000 || !values.ReplayMaskAllInputs {
		t.Errorf("replay defaults = %#v", values)
	}
	if values.ReplayRecordCanvas || values.ReplayCanvasFPS != 2 {
		t.Errorf("canvas replay defaults = %#v", values)
	}
	if values.RecurrenceMode != RecurrenceCooldown || values.RecurrenceDays == nil || *values.RecurrenceDays != 30 {
		t.Errorf("recurrence defaults = %#v", values)
	}
	if values.DefinitionStatus != DefinitionActive || values.Presentation != PresentationPrompt ||
		values.Objective != ObjectiveContinuousFeedback || len(values.ResponseBlocks) != 2 ||
		values.Localizations["en"].Prompt != "How was your experience?" {
		t.Errorf("definition compatibility defaults = %#v", values)
	}
	if values.LauncherVariant != LauncherVariantEdgeTab {
		t.Errorf("launcher variant default = %#v, want %q", values.LauncherVariant, LauncherVariantEdgeTab)
	}
}

func TestValidateWidgetRuleAcceptsBothLauncherVariants(t *testing.T) {
	t.Parallel()
	for _, variant := range []WidgetLauncherVariant{LauncherVariantEdgeTab, LauncherVariantFloatingButton} {
		delay := 2
		values, fields := ValidateWidgetRule(WidgetRuleInput{
			Name: "Checkout", MatcherType: MatcherAllPages,
			TriggerType: TriggerDelay, TriggerDelaySeconds: &delay,
			Presentation: PresentationLauncher, LauncherVariant: variant,
		}, tenant.Tenant{Plan: tenant.PlanStarter})
		if len(fields) != 0 {
			t.Fatalf("variant %q fields = %#v", variant, fields)
		}
		if values.LauncherVariant != variant {
			t.Errorf("variant %q normalized = %q", variant, values.LauncherVariant)
		}
	}
}

func TestValidateWidgetRuleRejectsUnknownLauncherVariant(t *testing.T) {
	t.Parallel()
	delay := 2
	_, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Checkout", MatcherType: MatcherAllPages,
		TriggerType: TriggerDelay, TriggerDelaySeconds: &delay,
		Presentation: PresentationLauncher, LauncherVariant: "sidebar_drawer",
	}, tenant.Tenant{Plan: tenant.PlanStarter})
	if fields["launcher_variant"] == "" {
		t.Fatalf("fields = %#v, want launcher_variant error", fields)
	}
}

func TestValidateWidgetRuleForcesAnonymousReplayCanvasOff(t *testing.T) {
	t.Parallel()
	values, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Replay", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
		ReplayRecordCanvas: true, ReplayCanvasFPS: 5,
	}, tenant.Tenant{Plan: tenant.PlanEnterprise})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.ReplayRecordCanvas || values.ReplayCanvasFPS != 2 {
		t.Fatalf("canvas privacy policy = %t/%d, want false/2", values.ReplayRecordCanvas, values.ReplayCanvasFPS)
	}
}

func TestValidateWidgetRuleKeepsAIProcessingDisabledByDefault(t *testing.T) {
	t.Parallel()
	values, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "No implicit AI", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
	}, tenant.Tenant{Plan: tenant.PlanEnterprise})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.AIProcessing.Enabled || values.AIProcessing.Acknowledged {
		t.Fatalf("AI processing = %#v, want safe disabled default", values.AIProcessing)
	}
}

func TestValidateWidgetRuleRequiresExplicitCurrentAIProcessingAcknowledgment(t *testing.T) {
	t.Parallel()
	input := WidgetRuleInput{
		Name: "AI analysis", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
		AIProcessing: AIProcessingConfig{
			Enabled: true, InputCategories: []AIInputCategory{AIInputFeedbackText},
			PrivacyPolicyURL: "https://example.com/privacy",
		},
	}
	_, fields := ValidateWidgetRule(input, tenant.Tenant{Plan: tenant.PlanEnterprise})
	if fields["ai_processing.acknowledged"] == "" {
		t.Fatalf("fields = %#v, want acknowledgment error", fields)
	}

	input.AIProcessing.Acknowledged = true
	values, fields := ValidateWidgetRule(input, tenant.Tenant{Plan: tenant.PlanEnterprise})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.AIProcessing.PolicyVersion != AIProcessingPolicyVersion ||
		values.AIProcessing.Provider != AIProviderGoogleGemini ||
		values.AIProcessing.SensitiveDataMode != AISensitiveDataProhibit {
		t.Fatalf("AI processing = %#v", values.AIProcessing)
	}
}

func TestValidateWidgetRuleRejectsUnavailableAIInputsAndUnsafePrivacyURL(t *testing.T) {
	t.Parallel()
	_, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Unsafe AI", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
		AIProcessing: AIProcessingConfig{
			Enabled: true, Acknowledged: true,
			InputCategories:  []AIInputCategory{AIInputAudio, "replay"},
			PrivacyPolicyURL: "http://example.com/privacy",
		},
	}, tenant.Tenant{Plan: tenant.PlanStarter})
	for _, field := range []string{
		"ai_processing.enabled", "ai_processing.input_categories[0]",
		"ai_processing.input_categories[1]", "ai_processing.privacy_policy_url",
	} {
		if fields[field] == "" {
			t.Errorf("fields[%q] missing from %#v", field, fields)
		}
	}
}

func TestValidateDefaultLocale(t *testing.T) {
	t.Parallel()
	if locale, message := ValidateDefaultLocale(""); locale != "en" || message != "" {
		t.Fatalf("empty locale = %q/%q", locale, message)
	}
	if locale, message := ValidateDefaultLocale(" pt-AO "); locale != "pt-ao" || message != "" {
		t.Fatalf("normalized locale = %q/%q", locale, message)
	}
	if locale, message := ValidateDefaultLocale("not_a_locale"); locale != "" || message == "" {
		t.Fatalf("invalid locale = %q/%q", locale, message)
	}
}

func TestValidateWidgetDefinitionAcceptsTypedLocalizedContract(t *testing.T) {
	t.Parallel()
	allowComment := true
	input := WidgetRuleInput{
		Name: "Checkout reaction", DefinitionKey: "checkout-reaction",
		DefinitionStatus: DefinitionActive, Presentation: PresentationReaction,
		Objective: ObjectiveAfterEngagement, MatcherType: MatcherAllPages,
		TriggerType: TriggerExitIntent,
		ResponseBlocks: []WidgetResponseBlock{{
			Key: "reaction", Type: ResponseReaction, Required: true,
			Options: []WidgetResponseOption{{Value: "helpful"}, {Value: "unclear"}},
		}},
		Localizations: map[string]WidgetLocalization{
			"pt-AO": {
				LauncherLabel: "Opinião", Prompt: "Isto foi útil?", CommentPlaceholder: "Conta-nos mais",
				ContactLabel: "Email", ConsentLabel: "Contactem-me", SubmitLabel: "Enviar",
				SuccessMessage:            "Recebido.",
				PrivacyPurposeSummary:     "Enviar atualizações sobre este feedback.",
				PrivacyLawfulBasisSummary: "Consentimento para atualizações opcionais.",
				PrivacyRetentionSummary:   "Mantido até retirar o consentimento ou terminar o caso.",
				BlockLabels:               map[string]string{"reaction": "Escolhe uma opção"},
				OptionLabels: map[string]map[string]string{
					"reaction": {"helpful": "Útil", "unclear": "Pouco claro"},
				},
			},
		},
		AllowComment: &allowComment, CollectContact: true,
	}
	values, fields := ValidateWidgetRule(input, tenant.Tenant{Plan: tenant.PlanStarter})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.DefinitionKey != "checkout-reaction" || values.Presentation != PresentationReaction ||
		values.CanRate || values.CanWriteText || !values.AllowComment || !values.CollectContact ||
		values.Localizations["pt-ao"].Prompt != "Isto foi útil?" {
		t.Fatalf("values = %#v", values)
	}
}

func TestValidateWidgetDefinitionRequiresContextualNoticesForContactCollection(t *testing.T) {
	t.Parallel()
	input := WidgetRuleInput{
		Name: "Contact follow-up", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
		ResponseBlocks: []WidgetResponseBlock{{Key: "feedback", Type: ResponseFreeText}},
		Localizations: map[string]WidgetLocalization{
			"en": defaultLocalization("Tell us what happened"),
		},
		CollectContact: true,
	}
	_, fields := ValidateWidgetRule(input, tenant.Tenant{Plan: tenant.PlanStarter})
	for _, field := range []string{
		"localizations.en.privacy_purpose_summary",
		"localizations.en.privacy_lawful_basis_summary",
		"localizations.en.privacy_retention_summary",
	} {
		if fields[field] == "" {
			t.Errorf("fields[%q] missing from %#v", field, fields)
		}
	}
}

func TestControllerProfileCompleteness(t *testing.T) {
	t.Parallel()
	valid := ControllerProfile{
		Version: 1, Name: "Acme", PrivacyContactEmail: "privacy@acme.test",
		PrivacyRightsEmail: "rights@acme.test", PrivacyPolicyURL: "https://acme.test/privacy",
	}
	if !valid.Complete() {
		t.Fatal("valid controller profile reported incomplete")
	}
	valid.PrivacyPolicyURL = "https://acme.test/privacy?source=widget"
	if valid.Complete() {
		t.Fatal("controller profile with query-bearing privacy URL reported complete")
	}
}

func TestValidateWidgetDefinitionRejectsInvalidContract(t *testing.T) {
	t.Parallel()
	input := WidgetRuleInput{
		Name: "Invalid", DefinitionKey: "Bad key", DefinitionStatus: "deleted",
		Presentation: "modal", LauncherVariant: "sidebar_drawer", Objective: "sell_more",
		MatcherType: MatcherAllPages,
		TriggerType: TriggerExitIntent,
		ResponseBlocks: []WidgetResponseBlock{
			{Key: "Choice!", Type: ResponseSingleChoice, Options: []WidgetResponseOption{{Value: "one"}}},
			{Key: "Choice!", Type: ResponseSingleChoice},
		},
		Localizations: map[string]WidgetLocalization{"invalid_locale": {}},
	}
	_, fields := ValidateWidgetRule(input, tenant.Tenant{Plan: tenant.PlanStarter})
	for _, field := range []string{
		"definition_key", "definition_status", "presentation", "launcher_variant", "objective",
		"response_blocks[0].key", "response_blocks[0].options", "response_blocks[1].type",
		"localizations.invalid_locale", "localizations.invalid_locale.prompt",
	} {
		if fields[field] == "" {
			t.Errorf("fields[%q] missing from %#v", field, fields)
		}
	}
}

func TestValidateWidgetRuleForcesReplayInputMasking(t *testing.T) {
	t.Parallel()
	requestedFalse := false
	delay := 2
	values, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Checkout", MatcherType: MatcherAllPages,
		TriggerType: TriggerDelay, TriggerDelaySeconds: &delay,
		ReplayMaskAllInputs: &requestedFalse,
	}, tenant.Tenant{Plan: tenant.PlanStarter})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if !values.ReplayMaskAllInputs {
		t.Fatalf(
			"ReplayMaskAllInputs = false, want true; input masking is an anonymity-safe default and must not be disableable",
		)
	}
}

func TestValidateWidgetRuleAcceptsRecurrenceModes(t *testing.T) {
	t.Parallel()
	days := 45
	for _, input := range []WidgetRuleInput{
		{Name: "Cooldown", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
			RecurrenceMode: RecurrenceCooldown, RecurrenceDays: &days},
		{Name: "Session", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
			RecurrenceMode: RecurrenceSession},
		{Name: "Never", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
			RecurrenceMode: RecurrenceNever},
	} {
		values, fields := ValidateWidgetRule(input, tenant.Tenant{Plan: tenant.PlanStarter})
		if len(fields) != 0 {
			t.Errorf("%s recurrence fields = %#v", input.RecurrenceMode, fields)
		}
		if values.RecurrenceMode != input.RecurrenceMode {
			t.Errorf("recurrence mode = %q, want %q", values.RecurrenceMode, input.RecurrenceMode)
		}
	}
}

func TestValidateWidgetRuleRejectsInvalidRecurrence(t *testing.T) {
	t.Parallel()
	zero, aboveMaximum, unused := 0, 3651, 30
	tests := []WidgetRuleInput{
		{Name: "Low", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
			RecurrenceMode: RecurrenceCooldown, RecurrenceDays: &zero},
		{Name: "High", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
			RecurrenceMode: RecurrenceCooldown, RecurrenceDays: &aboveMaximum},
		{Name: "Session", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
			RecurrenceMode: RecurrenceSession, RecurrenceDays: &unused},
		{Name: "Never", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
			RecurrenceMode: RecurrenceNever, RecurrenceDays: &unused},
		{Name: "Unknown", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
			RecurrenceMode: "sometimes"},
	}
	for _, input := range tests {
		_, fields := ValidateWidgetRule(input, tenant.Tenant{Plan: tenant.PlanStarter})
		if fields["recurrence_mode"] == "" && fields["recurrence_days"] == "" {
			t.Errorf("invalid recurrence %q/%v was accepted", input.RecurrenceMode, input.RecurrenceDays)
		}
	}
}

func TestValidateWidgetRuleReportsConsistencyAndPlanErrors(t *testing.T) {
	t.Parallel()
	delay := 1
	scroll := 101
	accent := "red"
	matcher := "["
	values, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Rule", MatcherType: MatcherURLRegex, MatcherValue: &matcher,
		TriggerType: TriggerScroll, TriggerDelaySeconds: &delay, TriggerScrollPercent: &scroll,
		AccentColor: &accent, ReplayMode: ReplaySampling, ReplaySamplingRate: 0,
		CanRecordAudio: true, CanRecordVideo: true, CanAttachElement: true,
	}, tenant.Tenant{Plan: tenant.PlanStarter})
	if values.ReplayMaxDurationSeconds != 1800 {
		t.Errorf("normalized values = %#v", values)
	}
	for _, field := range []string{
		"matcher_value", "trigger_delay_seconds", "trigger_scroll_percent", "accent_color",
		"replay_sampling_rate", "can_record_audio", "can_record_video", "can_attach_element", "replay_mode",
	} {
		if fields[field] == "" {
			t.Errorf("fields[%q] missing from %#v", field, fields)
		}
	}
}

func TestValidateWidgetRuleAllowsGrowthFeaturesAndValidRegex(t *testing.T) {
	t.Parallel()
	matcher := `^https://example\.com/(docs|help)`
	delay := 0
	values, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Docs", MatcherType: MatcherURLRegex, MatcherValue: &matcher,
		TriggerType: TriggerDelay, TriggerDelaySeconds: &delay,
		CanRecordAudio: true, CanAttachScreenshot: true,
		ReplayMode: ReplaySampling, ReplaySamplingRate: 25,
		ReplayConsent: ReplayConsentConfig{
			Mode: ReplayConsentCustomerManaged, NoticeVersion: "2026-07",
			SupportedLocales: []string{"pt-PT"}, ExpiryDays: 30,
		},
	}, tenant.Tenant{Plan: tenant.PlanGrowth})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if !values.CanRecordAudio || !values.CanAttachScreenshot || values.ReplaySamplingRate != 25 ||
		values.ReplayConsent.Mode != ReplayConsentCustomerManaged ||
		len(values.ReplayConsent.SupportedLocales) != 1 ||
		values.ReplayConsent.SupportedLocales[0] != "pt-pt" {
		t.Errorf("values = %#v", values)
	}
}

func TestValidateWidgetRuleRequiresExplicitReplayConsentMode(t *testing.T) {
	t.Parallel()
	values, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Replay", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
		ReplayMode: ReplayFull,
	}, tenant.Tenant{Plan: tenant.PlanGrowth})
	if values.ReplayConsent.Mode != ReplayConsentNone {
		t.Fatalf("replay consent mode = %q, want safe none default", values.ReplayConsent.Mode)
	}
	if fields["replay_consent.mode"] == "" {
		t.Fatalf("fields = %#v, want replay consent mode error", fields)
	}
}

func TestValidateWidgetRuleRejectsIncompleteCustomerManagedReplayConsent(t *testing.T) {
	t.Parallel()
	_, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Replay", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
		ReplayMode: ReplayFull,
		ReplayConsent: ReplayConsentConfig{
			Mode: ReplayConsentCustomerManaged,
		},
	}, tenant.Tenant{Plan: tenant.PlanGrowth})
	for _, field := range []string{
		"replay_consent.notice_version",
		"replay_consent.supported_locales",
		"replay_consent.expiry_days",
	} {
		if fields[field] == "" {
			t.Errorf("fields[%q] missing from %#v", field, fields)
		}
	}
}

func TestValidateWidgetRuleRejectsIncompleteFidibekiManagedReplayConsent(t *testing.T) {
	t.Parallel()
	_, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Replay", MatcherType: MatcherAllPages, TriggerType: TriggerExitIntent,
		ReplayMode: ReplayFull,
		ReplayConsent: ReplayConsentConfig{
			Mode:             ReplayConsentFidibekiManaged,
			NoticeVersion:    "2026-07",
			SupportedLocales: []string{"pt-PT"},
			ExpiryDays:       30,
		},
	}, tenant.Tenant{Plan: tenant.PlanGrowth})
	for _, field := range []string{
		"replay_consent.controller_name",
		"replay_consent.privacy_policy_url",
		"replay_consent.retention_days",
		"replay_consent.notices.pt-pt",
	} {
		if fields[field] == "" {
			t.Errorf("fields[%q] missing from %#v", field, fields)
		}
	}
}

func TestValidateWidgetRuleCoversRemainingInvalidVariants(t *testing.T) {
	t.Parallel()
	value := "https://example.com"
	delay := -1
	scroll := 20
	longGreeting := strings.Repeat("g", 501)
	longSelector := strings.Repeat("s", 1001)
	tests := []struct {
		name  string
		input WidgetRuleInput
		field string
	}{
		{
			name: "all pages value", field: "matcher_value",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherAllPages, MatcherValue: &value,
				TriggerType: TriggerExitIntent},
		},
		{
			name: "unknown matcher", field: "matcher_type",
			input: WidgetRuleInput{Name: "Rule", MatcherType: "unknown", TriggerType: TriggerExitIntent},
		},
		{
			name: "invalid delay", field: "trigger_delay_seconds",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLExact, MatcherValue: &value,
				TriggerType: TriggerDelay, TriggerDelaySeconds: &delay, TriggerScrollPercent: &scroll},
		},
		{
			name: "invalid exit intent", field: "trigger_scroll_percent",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLPrefix, MatcherValue: &value,
				TriggerType: TriggerExitIntent, TriggerScrollPercent: &scroll},
		},
		{
			name: "unknown trigger", field: "trigger_type",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLExact, MatcherValue: &value,
				TriggerType: "unknown"},
		},
		{
			name: "appearance and selector", field: "position",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLExact, MatcherValue: &value,
				TriggerType: TriggerExitIntent, Position: "top", Theme: "neon",
				Greeting: &longGreeting, ReplayMaskTextSelector: &longSelector},
		},
		{
			name: "full replay sampling rate", field: "replay_sampling_rate",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLExact, MatcherValue: &value,
				TriggerType: TriggerExitIntent, ReplayMode: ReplayFull, ReplaySamplingRate: 5},
		},
		{
			name: "unknown replay", field: "replay_mode",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLExact, MatcherValue: &value,
				TriggerType: TriggerExitIntent, ReplayMode: "unknown"},
		},
		{
			name: "replay duration above recording cap", field: "replay_max_duration_seconds",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLExact, MatcherValue: &value,
				TriggerType: TriggerExitIntent, ReplayMaxDurationSeconds: 1801},
		},
		{
			name: "replay events above recording cap", field: "replay_max_events",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLExact, MatcherValue: &value,
				TriggerType: TriggerExitIntent, ReplayMaxEvents: 50001},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, fields := ValidateWidgetRule(tt.input, tenant.Tenant{Plan: tenant.PlanEnterprise})
			if fields[tt.field] == "" {
				t.Errorf("fields[%q] missing from %#v", tt.field, fields)
			}
		})
	}
}
