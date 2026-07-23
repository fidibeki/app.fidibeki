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
				SuccessMessage: "Recebido.", BlockLabels: map[string]string{"reaction": "Escolhe uma opção"},
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

func TestValidateWidgetDefinitionRejectsInvalidContract(t *testing.T) {
	t.Parallel()
	input := WidgetRuleInput{
		Name: "Invalid", DefinitionKey: "Bad key", DefinitionStatus: "deleted",
		Presentation: "modal", Objective: "sell_more", MatcherType: MatcherAllPages,
		TriggerType: TriggerExitIntent,
		ResponseBlocks: []WidgetResponseBlock{
			{Key: "Choice!", Type: ResponseSingleChoice, Options: []WidgetResponseOption{{Value: "one"}}},
			{Key: "Choice!", Type: ResponseSingleChoice},
		},
		Localizations: map[string]WidgetLocalization{"invalid_locale": {}},
	}
	_, fields := ValidateWidgetRule(input, tenant.Tenant{Plan: tenant.PlanStarter})
	for _, field := range []string{
		"definition_key", "definition_status", "presentation", "objective",
		"response_blocks[0].key", "response_blocks[0].options", "response_blocks[1].type",
		"localizations.invalid_locale", "localizations.invalid_locale.prompt",
	} {
		if fields[field] == "" {
			t.Errorf("fields[%q] missing from %#v", field, fields)
		}
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
	}, tenant.Tenant{Plan: tenant.PlanGrowth})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if !values.CanRecordAudio || !values.CanAttachScreenshot || values.ReplaySamplingRate != 25 {
		t.Errorf("values = %#v", values)
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
		{
			name: "canvas fps above capture cap", field: "replay_canvas_fps",
			input: WidgetRuleInput{Name: "Rule", MatcherType: MatcherURLExact, MatcherValue: &value,
				TriggerType: TriggerExitIntent, ReplayCanvasFPS: 6},
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
