package project

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

const publicKeyAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var (
	domainLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	accentColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

type FieldErrors map[string]string

func GeneratePublicKey() (string, error) {
	key := make([]byte, 22)
	ceiling := big.NewInt(int64(len(publicKeyAlphabet)))
	for index := range key {
		value, err := rand.Int(rand.Reader, ceiling)
		if err != nil {
			return "", fmt.Errorf("generate project public key: %w", err)
		}
		key[index] = publicKeyAlphabet[value.Int64()]
	}
	return "pk_live_" + string(key), nil
}

func ValidateProject(name string, domains []string) (string, []string, FieldErrors) {
	fields := FieldErrors{}
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 120 {
		fields["name"] = "must be between 1 and 120 characters"
	}
	normalized, err := NormalizeAllowedDomains(domains)
	if err != nil {
		fields["allowed_domains"] = err.Error()
	}
	return name, normalized, fields
}

func NormalizeAllowedDomains(domains []string) ([]string, error) {
	normalized := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, candidate := range domains {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		wildcard := strings.HasPrefix(candidate, "*.")
		host := candidate
		if wildcard {
			host = strings.TrimPrefix(candidate, "*.")
		}
		if !validDomain(host, wildcard) {
			return nil, fmt.Errorf("%q must be an exact hostname or a leading wildcard hostname", candidate)
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		normalized = append(normalized, candidate)
	}
	return normalized, nil
}

func validDomain(host string, wildcard bool) bool {
	if host == "" || len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	if net.ParseIP(host) != nil {
		return !wildcard
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !domainLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func ValidateWidgetRule(input WidgetRuleInput, resolved tenant.Tenant) (WidgetRuleValues, FieldErrors) {
	values := normalizeWidgetRule(input)
	fields := FieldErrors{}
	if len(values.Name) < 1 || len(values.Name) > 120 {
		fields["name"] = "must be between 1 and 120 characters"
	}
	validateMatcher(values, fields)
	validateTrigger(values, fields)
	validateAppearance(values, fields)
	validateReplay(values, fields)
	validatePlanFeatures(values, resolved, fields)
	return values, fields
}

func normalizeWidgetRule(input WidgetRuleInput) WidgetRuleValues {
	values := WidgetRuleValues{
		Name: strings.TrimSpace(input.Name), Priority: input.Priority,
		Enabled: valueOr(input.Enabled, true), MatcherType: input.MatcherType,
		MatcherValue: trimOptional(input.MatcherValue), TriggerType: input.TriggerType,
		TriggerDelaySeconds: input.TriggerDelaySeconds, TriggerScrollPercent: input.TriggerScrollPercent,
		CanRate: valueOr(input.CanRate, true), CanWriteText: valueOr(input.CanWriteText, true),
		CanRecordAudio: input.CanRecordAudio, CanRecordVideo: input.CanRecordVideo,
		CanAttachElement: input.CanAttachElement, CanAttachScreenshot: input.CanAttachScreenshot,
		Greeting: trimOptional(input.Greeting), Position: input.Position, Theme: input.Theme,
		AccentColor: trimOptional(input.AccentColor), ReplayMode: input.ReplayMode,
		ReplaySamplingRate:       input.ReplaySamplingRate,
		ReplayMaxDurationSeconds: input.ReplayMaxDurationSeconds,
		ReplayMaxEvents:          input.ReplayMaxEvents,
		ReplayMaskAllInputs:      valueOr(input.ReplayMaskAllInputs, true),
		ReplayMaskTextSelector:   trimOptional(input.ReplayMaskTextSelector),
	}
	if values.Position == "" {
		values.Position = PositionBottomLeft
	}
	if values.Theme == "" {
		values.Theme = ThemeLight
	}
	if values.ReplayMode == "" {
		values.ReplayMode = ReplayNone
	}
	if values.ReplayMaxDurationSeconds == 0 {
		values.ReplayMaxDurationSeconds = 1800
	}
	if values.ReplayMaxEvents == 0 {
		values.ReplayMaxEvents = 50000
	}
	return values
}

func validateMatcher(values WidgetRuleValues, fields FieldErrors) {
	switch values.MatcherType {
	case MatcherAllPages:
		if values.MatcherValue != nil {
			fields["matcher_value"] = "must be null for all_pages"
		}
	case MatcherURLExact, MatcherURLPrefix:
		if values.MatcherValue == nil {
			fields["matcher_value"] = "is required for this matcher type"
		} else if len(*values.MatcherValue) > 2048 {
			fields["matcher_value"] = "must not exceed 2048 characters"
		}
	case MatcherURLRegex:
		if values.MatcherValue == nil {
			fields["matcher_value"] = "is required for url_regex"
		} else if len(*values.MatcherValue) > 2048 {
			fields["matcher_value"] = "must not exceed 2048 characters"
		} else if _, err := regexp.Compile(*values.MatcherValue); err != nil {
			fields["matcher_value"] = "must be a valid regular expression"
		}
	default:
		fields["matcher_type"] = "must be all_pages, url_exact, url_prefix, or url_regex"
	}
}

func validateTrigger(values WidgetRuleValues, fields FieldErrors) {
	switch values.TriggerType {
	case TriggerDelay:
		if values.TriggerDelaySeconds == nil || *values.TriggerDelaySeconds < 0 || *values.TriggerDelaySeconds > 86400 {
			fields["trigger_delay_seconds"] = "must be between 0 and 86400 for delay triggers"
		}
		if values.TriggerScrollPercent != nil {
			fields["trigger_scroll_percent"] = "must be null for delay triggers"
		}
	case TriggerScroll:
		if values.TriggerScrollPercent == nil || *values.TriggerScrollPercent < 1 || *values.TriggerScrollPercent > 100 {
			fields["trigger_scroll_percent"] = "must be between 1 and 100 for scroll triggers"
		}
		if values.TriggerDelaySeconds != nil {
			fields["trigger_delay_seconds"] = "must be null for scroll triggers"
		}
	case TriggerExitIntent:
		if values.TriggerDelaySeconds != nil {
			fields["trigger_delay_seconds"] = "must be null for exit_intent triggers"
		}
		if values.TriggerScrollPercent != nil {
			fields["trigger_scroll_percent"] = "must be null for exit_intent triggers"
		}
	default:
		fields["trigger_type"] = "must be delay, scroll, or exit_intent"
	}
}

func validateAppearance(values WidgetRuleValues, fields FieldErrors) {
	if values.Greeting != nil && len(*values.Greeting) > 500 {
		fields["greeting"] = "must not exceed 500 characters"
	}
	if values.Position != PositionBottomLeft && values.Position != PositionBottomRight {
		fields["position"] = "must be bottom-left or bottom-right"
	}
	if values.Theme != ThemeLight && values.Theme != ThemeDark && values.Theme != ThemeAuto {
		fields["theme"] = "must be light, dark, or auto"
	}
	if values.AccentColor != nil && !accentColorPattern.MatchString(*values.AccentColor) {
		fields["accent_color"] = "must be a six-digit hex color"
	}
	if values.ReplayMaskTextSelector != nil && len(*values.ReplayMaskTextSelector) > 1000 {
		fields["replay_mask_text_selector"] = "must not exceed 1000 characters"
	}
}

func validateReplay(values WidgetRuleValues, fields FieldErrors) {
	switch values.ReplayMode {
	case ReplayNone, ReplayFull:
		if values.ReplaySamplingRate != 0 {
			fields["replay_sampling_rate"] = "must be zero unless replay_mode is sampling"
		}
	case ReplaySampling:
		if values.ReplaySamplingRate < 1 || values.ReplaySamplingRate > 100 {
			fields["replay_sampling_rate"] = "must be between 1 and 100 for sampling replay"
		}
	default:
		fields["replay_mode"] = "must be none, full, or sampling"
	}
	if values.ReplayMaxDurationSeconds < 1 || values.ReplayMaxDurationSeconds > 7200 {
		fields["replay_max_duration_seconds"] = "must be between 1 and 7200"
	}
	if values.ReplayMaxEvents < 1 || values.ReplayMaxEvents > 200000 {
		fields["replay_max_events"] = "must be between 1 and 200000"
	}
}

func validatePlanFeatures(values WidgetRuleValues, resolved tenant.Tenant, fields FieldErrors) {
	if values.CanRecordAudio && !config.CanUseFeature(resolved, config.FeatureAudio) {
		fields["can_record_audio"] = "is not available on the current plan"
	}
	if values.CanRecordVideo && !config.CanUseFeature(resolved, config.FeatureVideo) {
		fields["can_record_video"] = "is not available on the current plan"
	}
	if (values.CanAttachElement || values.CanAttachScreenshot) && !config.CanUseFeature(resolved, config.FeatureDOMCapture) {
		if values.CanAttachElement {
			fields["can_attach_element"] = "is not available on the current plan"
		}
		if values.CanAttachScreenshot {
			fields["can_attach_screenshot"] = "is not available on the current plan"
		}
	}
	if values.ReplayMode != ReplayNone && !config.CanUseFeature(resolved, config.FeatureReplay) {
		fields["replay_mode"] = "is not available on the current plan"
	}
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func valueOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
