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
				TriggerType: TriggerExitIntent, ReplayMode: "unknown",
				ReplayMaxDurationSeconds: 7201, ReplayMaxEvents: 200001},
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
