package project

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

const publicKeyAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var (
	domainLabelPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	accentColorPattern   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	definitionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}[a-z0-9]$`)
	responseKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	localePattern        = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$`)
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

func ValidateDefaultLocale(candidate string) (string, string) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "en", ""
	}
	if !localePattern.MatchString(candidate) {
		return "", "must use a valid language tag"
	}
	return strings.ToLower(candidate), ""
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
	validateTargeting(&values, fields)
	validateTrigger(values, fields)
	validateAppearance(values, fields)
	validateReplay(values, fields)
	validateRecurrence(values, fields)
	validateDefinition(values, fields)
	validatePlanFeatures(values, resolved, fields)
	return values, fields
}

func normalizeWidgetRule(input WidgetRuleInput) WidgetRuleValues {
	legacyTargeting := input.IncludeConditions == nil && input.ExcludeConditions == nil
	explicitBlocks := input.ResponseBlocks != nil
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
		RecurrenceMode:           input.RecurrenceMode,
		RecurrenceDays:           input.RecurrenceDays,
		IncludeConditions:        normalizeTargetConditions(input.IncludeConditions),
		ExcludeConditions:        normalizeTargetConditions(input.ExcludeConditions),
		LegacyTargeting:          legacyTargeting,
		DefinitionKey:            strings.ToLower(strings.TrimSpace(input.DefinitionKey)),
		DefinitionStatus:         input.DefinitionStatus,
		Presentation:             input.Presentation,
		Objective:                input.Objective,
		ResponseBlocks:           normalizeResponseBlocks(input.ResponseBlocks),
		Localizations:            normalizeLocalizations(input.Localizations),
		AllowComment:             valueOr(input.AllowComment, false),
		CollectContact:           input.CollectContact,
	}
	if legacyTargeting {
		values.IncludeConditions = []WidgetRuleTargetConditionValues{{
			MatcherType: values.MatcherType, MatcherValue: values.MatcherValue,
		}}
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
	if values.RecurrenceMode == "" {
		values.RecurrenceMode = RecurrenceCooldown
	}
	if values.RecurrenceMode == RecurrenceCooldown && values.RecurrenceDays == nil {
		defaultDays := 30
		values.RecurrenceDays = &defaultDays
	}
	if values.DefinitionStatus == "" {
		if values.Enabled {
			values.DefinitionStatus = DefinitionActive
		} else {
			values.DefinitionStatus = DefinitionPaused
		}
	}
	values.Enabled = values.DefinitionStatus == DefinitionActive
	if values.Presentation == "" {
		values.Presentation = PresentationPrompt
	}
	if values.Objective == "" {
		values.Objective = ObjectiveContinuousFeedback
	}
	if !explicitBlocks {
		values.ResponseBlocks = legacyResponseBlocks(values.CanRate, values.CanWriteText)
	} else {
		values.CanRate = hasResponseBlock(values.ResponseBlocks, ResponseRating)
		values.CanWriteText = hasResponseBlock(values.ResponseBlocks, ResponseFreeText)
	}
	if len(values.Localizations) == 0 {
		prompt := "How was your experience?"
		if values.Greeting != nil {
			prompt = *values.Greeting
		}
		values.Localizations = map[string]WidgetLocalization{"en": defaultLocalization(prompt)}
	} else if prompt := preferredPrompt(values.Localizations); prompt != "" {
		values.Greeting = &prompt
	}
	return values
}

func normalizeResponseBlocks(blocks []WidgetResponseBlock) []WidgetResponseBlock {
	if blocks == nil {
		return nil
	}
	normalized := make([]WidgetResponseBlock, len(blocks))
	for index, block := range blocks {
		normalized[index] = WidgetResponseBlock{
			Key: strings.ToLower(strings.TrimSpace(block.Key)), Type: block.Type,
			Required: block.Required, Options: make([]WidgetResponseOption, len(block.Options)),
		}
		for optionIndex, option := range block.Options {
			normalized[index].Options[optionIndex].Value = strings.ToLower(strings.TrimSpace(option.Value))
		}
	}
	return normalized
}

func normalizeLocalizations(localizations map[string]WidgetLocalization) map[string]WidgetLocalization {
	if localizations == nil {
		return nil
	}
	normalized := make(map[string]WidgetLocalization, len(localizations))
	for locale, content := range localizations {
		content.LauncherLabel = strings.TrimSpace(content.LauncherLabel)
		content.Prompt = strings.TrimSpace(content.Prompt)
		content.CommentPlaceholder = strings.TrimSpace(content.CommentPlaceholder)
		content.ContactLabel = strings.TrimSpace(content.ContactLabel)
		content.ConsentLabel = strings.TrimSpace(content.ConsentLabel)
		content.SubmitLabel = strings.TrimSpace(content.SubmitLabel)
		content.SuccessMessage = strings.TrimSpace(content.SuccessMessage)
		content.BlockLabels = normalizeLabels(content.BlockLabels)
		content.OptionLabels = normalizeOptionLabels(content.OptionLabels)
		normalized[strings.ToLower(strings.TrimSpace(locale))] = content
	}
	return normalized
}

func normalizeLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	normalized := make(map[string]string, len(labels))
	for key, label := range labels {
		normalized[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(label)
	}
	return normalized
}

func normalizeOptionLabels(labels map[string]map[string]string) map[string]map[string]string {
	if labels == nil {
		return nil
	}
	normalized := make(map[string]map[string]string, len(labels))
	for blockKey, options := range labels {
		normalized[strings.ToLower(strings.TrimSpace(blockKey))] = normalizeLabels(options)
	}
	return normalized
}

func legacyResponseBlocks(canRate, canWriteText bool) []WidgetResponseBlock {
	blocks := make([]WidgetResponseBlock, 0, 2)
	if canRate {
		blocks = append(blocks, WidgetResponseBlock{Key: "rating", Type: ResponseRating})
	}
	if canWriteText {
		blocks = append(blocks, WidgetResponseBlock{Key: "feedback", Type: ResponseFreeText})
	}
	return blocks
}

func hasResponseBlock(blocks []WidgetResponseBlock, blockType WidgetResponseBlockType) bool {
	for _, block := range blocks {
		if block.Type == blockType {
			return true
		}
	}
	return false
}

func defaultLocalization(prompt string) WidgetLocalization {
	return WidgetLocalization{
		LauncherLabel: "Feedback", Prompt: prompt, CommentPlaceholder: "Tell us more",
		ContactLabel: "Email address", ConsentLabel: "Email me about this feedback",
		SubmitLabel: "Send feedback", SuccessMessage: "Your feedback was sent.",
	}
}

func preferredPrompt(localizations map[string]WidgetLocalization) string {
	if content, exists := localizations["en"]; exists {
		return content.Prompt
	}
	locales := make([]string, 0, len(localizations))
	for locale := range localizations {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	if len(locales) == 0 {
		return ""
	}
	return localizations[locales[0]].Prompt
}

func normalizeTargetConditions(input []WidgetRuleTargetConditionInput) []WidgetRuleTargetConditionValues {
	if input == nil {
		return nil
	}
	conditions := make([]WidgetRuleTargetConditionValues, len(input))
	for index, condition := range input {
		conditions[index] = WidgetRuleTargetConditionValues{
			MatcherType: condition.MatcherType, MatcherValue: trimOptional(condition.MatcherValue),
		}
	}
	return conditions
}

func validateTargeting(values *WidgetRuleValues, fields FieldErrors) {
	includeField := "include_conditions"
	if values.LegacyTargeting {
		includeField = "matcher"
	}
	if len(values.IncludeConditions) < 1 || len(values.IncludeConditions) > 20 {
		fields[mapTargetField(values.LegacyTargeting, includeField, -1, "")] = "must contain between 1 and 20 conditions"
	}
	if len(values.ExcludeConditions) > 20 {
		fields["exclude_conditions"] = "must not contain more than 20 conditions"
	}
	allPages := 0
	for index := range values.IncludeConditions {
		if values.IncludeConditions[index].MatcherType == MatcherAllPages {
			allPages++
		}
		validateTargetCondition(&values.IncludeConditions[index], TargetConditionInclude, index, values.LegacyTargeting, fields)
	}
	if allPages > 0 && len(values.IncludeConditions) != 1 {
		fields[mapTargetField(values.LegacyTargeting, includeField, -1, "")] = "all_pages must be the sole include condition"
	}
	for index := range values.ExcludeConditions {
		validateTargetCondition(&values.ExcludeConditions[index], TargetConditionExclude, index, false, fields)
	}
	if len(values.IncludeConditions) > 0 {
		values.MatcherType = values.IncludeConditions[0].MatcherType
		values.MatcherValue = values.IncludeConditions[0].MatcherValue
	}
}

func validateTargetCondition(
	condition *WidgetRuleTargetConditionValues,
	kind TargetConditionKind,
	index int,
	legacy bool,
	fields FieldErrors,
) {
	typeField := mapTargetField(legacy, string(kind)+"_conditions", index, "matcher_type")
	valueField := mapTargetField(legacy, string(kind)+"_conditions", index, "matcher_value")
	switch condition.MatcherType {
	case MatcherAllPages:
		if kind == TargetConditionExclude {
			fields[typeField] = "all_pages cannot be an exclude condition"
		}
		if condition.MatcherValue != nil {
			fields[valueField] = "must be null for all_pages"
		}
	case MatcherURLExact, MatcherURLPrefix:
		if condition.MatcherValue == nil {
			fields[valueField] = "is required for this matcher type"
		} else if len(*condition.MatcherValue) > 2048 {
			fields[valueField] = "must not exceed 2048 characters"
		} else if page, err := CanonicalizePage(*condition.MatcherValue); err != nil {
			fields[valueField] = err.Error()
		} else {
			condition.MatcherValue = &page.Target
		}
	case MatcherURLRegex:
		if condition.MatcherValue == nil {
			fields[valueField] = "is required for url_regex"
		} else if len(*condition.MatcherValue) > 2048 {
			fields[valueField] = "must not exceed 2048 characters"
		} else if _, err := regexp.Compile(*condition.MatcherValue); err != nil {
			fields[valueField] = "must be a valid regular expression"
		}
	default:
		fields[typeField] = "must be all_pages, url_exact, url_prefix, or url_regex"
	}
}

func mapTargetField(legacy bool, collection string, index int, field string) string {
	if legacy {
		if field == "matcher_type" || field == "" {
			return "matcher_type"
		}
		return "matcher_value"
	}
	if index < 0 || field == "" {
		return collection
	}
	return fmt.Sprintf("%s[%d].%s", collection, index, field)
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
	if values.ReplayMaxDurationSeconds < 1 || values.ReplayMaxDurationSeconds > MaximumReplayDurationSeconds {
		fields["replay_max_duration_seconds"] = "must be between 1 and 1800"
	}
	if values.ReplayMaxEvents < 1 || values.ReplayMaxEvents > MaximumReplayEvents {
		fields["replay_max_events"] = "must be between 1 and 50000"
	}
}

func validateRecurrence(values WidgetRuleValues, fields FieldErrors) {
	switch values.RecurrenceMode {
	case RecurrenceCooldown:
		if values.RecurrenceDays == nil || *values.RecurrenceDays < 1 || *values.RecurrenceDays > 3650 {
			fields["recurrence_days"] = "must be between 1 and 3650 for cooldown recurrence"
		}
	case RecurrenceSession, RecurrenceNever:
		if values.RecurrenceDays != nil {
			fields["recurrence_days"] = "must be null unless recurrence_mode is cooldown"
		}
	default:
		fields["recurrence_mode"] = "must be cooldown, session, or never"
	}
}

func validateDefinition(values WidgetRuleValues, fields FieldErrors) {
	if values.DefinitionKey != "" && !definitionKeyPattern.MatchString(values.DefinitionKey) {
		fields["definition_key"] = "must contain 3-64 lowercase letters, numbers, or hyphens"
	}
	switch values.DefinitionStatus {
	case DefinitionDraft, DefinitionActive, DefinitionPaused, DefinitionArchived:
	default:
		fields["definition_status"] = "must be draft, active, paused, or archived"
	}
	switch values.Presentation {
	case PresentationLauncher, PresentationInline, PresentationReaction, PresentationPrompt:
	default:
		fields["presentation"] = "must be launcher, inline, reaction, or prompt"
	}
	switch values.Objective {
	case ObjectiveContinuousFeedback, ObjectiveAfterEngagement, ObjectiveValidateContent, ObjectiveDetectFriction:
	default:
		fields["objective"] = "must be continuous_feedback, after_engagement, validate_content, or detect_friction"
	}
	validateResponseBlocks(values.ResponseBlocks, fields)
	validateLocalizations(values.Localizations, fields)
}

func validateResponseBlocks(blocks []WidgetResponseBlock, fields FieldErrors) {
	if len(blocks) < 1 || len(blocks) > 4 {
		fields["response_blocks"] = "must contain between 1 and 4 blocks"
	}
	keys := make(map[string]struct{}, len(blocks))
	types := make(map[WidgetResponseBlockType]struct{}, len(blocks))
	for index, block := range blocks {
		prefix := fmt.Sprintf("response_blocks[%d]", index)
		if !responseKeyPattern.MatchString(block.Key) {
			fields[prefix+".key"] = "must contain 1-32 lowercase letters, numbers, or underscores"
		} else if _, exists := keys[block.Key]; exists {
			fields[prefix+".key"] = "must be unique"
		}
		keys[block.Key] = struct{}{}
		if _, exists := types[block.Type]; exists {
			fields[prefix+".type"] = "must be unique within a definition"
		}
		types[block.Type] = struct{}{}
		switch block.Type {
		case ResponseFreeText, ResponseRating:
			if len(block.Options) != 0 {
				fields[prefix+".options"] = "must be empty for this block type"
			}
		case ResponseReaction:
			validateResponseOptions(prefix, block.Options, 2, 5, fields)
		case ResponseSingleChoice:
			validateResponseOptions(prefix, block.Options, 2, 10, fields)
		default:
			fields[prefix+".type"] = "must be free_text, rating, reaction, or single_choice"
		}
	}
}

func validateResponseOptions(
	prefix string,
	options []WidgetResponseOption,
	minimum int,
	maximum int,
	fields FieldErrors,
) {
	if len(options) < minimum || len(options) > maximum {
		fields[prefix+".options"] = fmt.Sprintf("must contain between %d and %d options", minimum, maximum)
	}
	seen := make(map[string]struct{}, len(options))
	for index, option := range options {
		field := fmt.Sprintf("%s.options[%d].value", prefix, index)
		if !responseKeyPattern.MatchString(option.Value) {
			fields[field] = "must contain 1-32 lowercase letters, numbers, or underscores"
		} else if _, exists := seen[option.Value]; exists {
			fields[field] = "must be unique"
		}
		seen[option.Value] = struct{}{}
	}
}

func validateLocalizations(localizations map[string]WidgetLocalization, fields FieldErrors) {
	if len(localizations) < 1 || len(localizations) > 20 {
		fields["localizations"] = "must contain between 1 and 20 locales"
	}
	for locale, content := range localizations {
		prefix := "localizations." + locale
		if !localePattern.MatchString(locale) {
			fields[prefix] = "must use a valid language tag"
		}
		validateLocalizedText(prefix+".launcher_label", content.LauncherLabel, 120, fields)
		validateLocalizedText(prefix+".prompt", content.Prompt, 500, fields)
		validateLocalizedText(prefix+".comment_placeholder", content.CommentPlaceholder, 240, fields)
		validateLocalizedText(prefix+".contact_label", content.ContactLabel, 120, fields)
		validateLocalizedText(prefix+".consent_label", content.ConsentLabel, 240, fields)
		validateLocalizedText(prefix+".submit_label", content.SubmitLabel, 120, fields)
		validateLocalizedText(prefix+".success_message", content.SuccessMessage, 500, fields)
		for key, label := range content.BlockLabels {
			if !responseKeyPattern.MatchString(key) || len(label) < 1 || len(label) > 240 {
				fields[prefix+".block_labels."+key] = "must identify a block and contain 1-240 characters"
			}
		}
		for blockKey, labels := range content.OptionLabels {
			if !responseKeyPattern.MatchString(blockKey) {
				fields[prefix+".option_labels."+blockKey] = "must identify a response block"
			}
			for option, label := range labels {
				if !responseKeyPattern.MatchString(option) || len(label) < 1 || len(label) > 120 {
					fields[prefix+".option_labels."+blockKey+"."+option] =
						"must identify an option and contain 1-120 characters"
				}
			}
		}
	}
}

func validateLocalizedText(field string, value string, maximum int, fields FieldErrors) {
	if len(value) < 1 || len(value) > maximum {
		fields[field] = fmt.Sprintf("must contain between 1 and %d characters", maximum)
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
