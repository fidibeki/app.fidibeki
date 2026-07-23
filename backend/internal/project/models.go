package project

import (
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetconfig"
)

type MatcherType string

const (
	MatcherAllPages  MatcherType = "all_pages"
	MatcherURLExact  MatcherType = "url_exact"
	MatcherURLPrefix MatcherType = "url_prefix"
	MatcherURLRegex  MatcherType = "url_regex"
)

type TriggerType string

const (
	TriggerDelay      TriggerType = "delay"
	TriggerScroll     TriggerType = "scroll"
	TriggerExitIntent TriggerType = "exit_intent"
)

type WidgetPosition string

const (
	PositionBottomLeft  WidgetPosition = "bottom-left"
	PositionBottomRight WidgetPosition = "bottom-right"
)

type WidgetTheme string

const (
	ThemeLight WidgetTheme = "light"
	ThemeDark  WidgetTheme = "dark"
	ThemeAuto  WidgetTheme = "auto"
)

type ReplayMode string

const (
	ReplayNone     ReplayMode = "none"
	ReplayFull     ReplayMode = "full"
	ReplaySampling ReplayMode = "sampling"

	MaximumReplayDurationSeconds = 30 * 60
	MaximumReplayEvents          = 50000
)

type ReplayConsentMode string

const (
	ReplayConsentNone            ReplayConsentMode = "none"
	ReplayConsentFidibekiManaged ReplayConsentMode = "fidibeki_managed"
	ReplayConsentCustomerManaged ReplayConsentMode = "customer_cmp"
)

type ReplayConsentNotice struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	AcceptLabel   string `json:"accept_label"`
	RejectLabel   string `json:"reject_label"`
	WithdrawLabel string `json:"withdraw_label"`
}

type ReplayConsentConfig struct {
	Mode             ReplayConsentMode              `json:"mode"`
	NoticeVersion    string                         `json:"notice_version"`
	ControllerName   string                         `json:"controller_name"`
	PrivacyPolicyURL string                         `json:"privacy_policy_url"`
	SupportedLocales []string                       `json:"supported_locales"`
	RetentionDays    int                            `json:"retention_days"`
	ExpiryDays       int                            `json:"expiry_days"`
	Notices          map[string]ReplayConsentNotice `json:"notices"`
}

type RecurrenceMode string

const (
	RecurrenceCooldown RecurrenceMode = "cooldown"
	RecurrenceSession  RecurrenceMode = "session"
	RecurrenceNever    RecurrenceMode = "never"
)

type WidgetDefinitionStatus string

const (
	DefinitionDraft    WidgetDefinitionStatus = "draft"
	DefinitionActive   WidgetDefinitionStatus = "active"
	DefinitionPaused   WidgetDefinitionStatus = "paused"
	DefinitionArchived WidgetDefinitionStatus = "archived"
)

type WidgetPresentation string

const (
	PresentationLauncher WidgetPresentation = "launcher"
	PresentationInline   WidgetPresentation = "inline"
	PresentationReaction WidgetPresentation = "reaction"
	PresentationPrompt   WidgetPresentation = "prompt"
)

type WidgetObjective string

const (
	ObjectiveContinuousFeedback WidgetObjective = "continuous_feedback"
	ObjectiveAfterEngagement    WidgetObjective = "after_engagement"
	ObjectiveValidateContent    WidgetObjective = "validate_content"
	ObjectiveDetectFriction     WidgetObjective = "detect_friction"
)

type WidgetResponseBlockType string

const (
	ResponseFreeText     WidgetResponseBlockType = "free_text"
	ResponseRating       WidgetResponseBlockType = "rating"
	ResponseReaction     WidgetResponseBlockType = "reaction"
	ResponseSingleChoice WidgetResponseBlockType = "single_choice"
)

type WidgetResponseOption struct {
	Value string `json:"value"`
}

type WidgetResponseBlock struct {
	Key      string                  `json:"key"`
	Type     WidgetResponseBlockType `json:"type"`
	Required bool                    `json:"required"`
	Options  []WidgetResponseOption  `json:"options,omitempty"`
}

type WidgetLocalization struct {
	LauncherLabel      string                       `json:"launcher_label"`
	Prompt             string                       `json:"prompt"`
	CommentPlaceholder string                       `json:"comment_placeholder"`
	ContactLabel       string                       `json:"contact_label"`
	ConsentLabel       string                       `json:"consent_label"`
	SubmitLabel        string                       `json:"submit_label"`
	SuccessMessage     string                       `json:"success_message"`
	BlockLabels        map[string]string            `json:"block_labels,omitempty"`
	OptionLabels       map[string]map[string]string `json:"option_labels,omitempty"`
}

type TargetConditionKind string

const (
	TargetConditionInclude TargetConditionKind = "include"
	TargetConditionExclude TargetConditionKind = "exclude"
)

type WidgetRuleTargetCondition struct {
	ID           string      `json:"id"`
	MatcherType  MatcherType `json:"matcher_type"`
	MatcherValue *string     `json:"matcher_value"`
	Position     int         `json:"position"`
}

type WidgetRuleTargetConditionInput struct {
	MatcherType  MatcherType `json:"matcher_type"`
	MatcherValue *string     `json:"matcher_value"`
}

type WidgetRuleTargetConditionValues struct {
	MatcherType  MatcherType
	MatcherValue *string
}

type Project struct {
	ID                        string                           `json:"id"`
	TenantID                  string                           `json:"tenant_id"`
	Name                      string                           `json:"name"`
	PublicKey                 string                           `json:"public_key"`
	AllowedDomains            []string                         `json:"allowed_domains"`
	DefaultLocale             string                           `json:"default_locale"`
	ReporterAccessEnabled     *bool                            `json:"reporter_access_enabled"`
	WidgetAppearanceOverrides widgetconfig.AppearanceOverrides `json:"widget_appearance_overrides"`
	WidgetLastSeenAt          *time.Time                       `json:"widget_last_seen_at"`
	CreatedAt                 time.Time                        `json:"created_at"`
	UpdatedAt                 time.Time                        `json:"updated_at"`
}

type WidgetRule struct {
	ID                       string                           `json:"id"`
	ProjectID                string                           `json:"project_id"`
	Name                     string                           `json:"name"`
	Priority                 int                              `json:"priority"`
	Enabled                  bool                             `json:"enabled"`
	MatcherType              MatcherType                      `json:"matcher_type"`
	MatcherValue             *string                          `json:"matcher_value"`
	IncludeConditions        []WidgetRuleTargetCondition      `json:"include_conditions"`
	ExcludeConditions        []WidgetRuleTargetCondition      `json:"exclude_conditions"`
	TriggerType              TriggerType                      `json:"trigger_type"`
	TriggerDelaySeconds      *int                             `json:"trigger_delay_seconds"`
	TriggerScrollPercent     *int                             `json:"trigger_scroll_percent"`
	CanRate                  bool                             `json:"can_rate"`
	CanWriteText             bool                             `json:"can_write_text"`
	CanRecordAudio           bool                             `json:"can_record_audio"`
	CanRecordVideo           bool                             `json:"can_record_video"`
	CanAttachElement         bool                             `json:"can_attach_element"`
	CanAttachScreenshot      bool                             `json:"can_attach_screenshot"`
	Greeting                 *string                          `json:"greeting"`
	Position                 WidgetPosition                   `json:"position"`
	Theme                    WidgetTheme                      `json:"theme"`
	AccentColor              *string                          `json:"accent_color"`
	ReplayMode               ReplayMode                       `json:"replay_mode"`
	ReplaySamplingRate       int                              `json:"replay_sampling_rate"`
	ReplayMaxDurationSeconds int                              `json:"replay_max_duration_seconds"`
	ReplayMaxEvents          int                              `json:"replay_max_events"`
	ReplayMaskAllInputs      bool                             `json:"replay_mask_all_inputs"`
	ReplayMaskTextSelector   *string                          `json:"replay_mask_text_selector"`
	ReplayRecordCanvas       bool                             `json:"replay_record_canvas"`
	ReplayCanvasFPS          int                              `json:"replay_canvas_fps"`
	ReplayConsent            ReplayConsentConfig              `json:"replay_consent"`
	RecurrenceMode           RecurrenceMode                   `json:"recurrence_mode"`
	RecurrenceDays           *int                             `json:"recurrence_days"`
	DefinitionKey            string                           `json:"definition_key"`
	DefinitionVersion        int                              `json:"definition_version"`
	DefinitionStatus         WidgetDefinitionStatus           `json:"definition_status"`
	Presentation             WidgetPresentation               `json:"presentation"`
	Objective                WidgetObjective                  `json:"objective"`
	ResponseBlocks           []WidgetResponseBlock            `json:"response_blocks"`
	Localizations            map[string]WidgetLocalization    `json:"localizations"`
	AllowComment             bool                             `json:"allow_comment"`
	CollectContact           bool                             `json:"collect_contact"`
	AppearanceOverrides      widgetconfig.AppearanceOverrides `json:"appearance_overrides"`
	Appearance               widgetconfig.Appearance          `json:"appearance"`
	CreatedAt                time.Time                        `json:"created_at"`
	UpdatedAt                time.Time                        `json:"updated_at"`
}

type WidgetRuleInput struct {
	Name                     string                            `json:"name"`
	Priority                 int                               `json:"priority"`
	Enabled                  *bool                             `json:"enabled"`
	MatcherType              MatcherType                       `json:"matcher_type"`
	MatcherValue             *string                           `json:"matcher_value"`
	IncludeConditions        []WidgetRuleTargetConditionInput  `json:"include_conditions"`
	ExcludeConditions        []WidgetRuleTargetConditionInput  `json:"exclude_conditions"`
	TriggerType              TriggerType                       `json:"trigger_type"`
	TriggerDelaySeconds      *int                              `json:"trigger_delay_seconds"`
	TriggerScrollPercent     *int                              `json:"trigger_scroll_percent"`
	CanRate                  *bool                             `json:"can_rate"`
	CanWriteText             *bool                             `json:"can_write_text"`
	CanRecordAudio           bool                              `json:"can_record_audio"`
	CanRecordVideo           bool                              `json:"can_record_video"`
	CanAttachElement         bool                              `json:"can_attach_element"`
	CanAttachScreenshot      bool                              `json:"can_attach_screenshot"`
	Greeting                 *string                           `json:"greeting"`
	Position                 WidgetPosition                    `json:"position"`
	Theme                    WidgetTheme                       `json:"theme"`
	AccentColor              *string                           `json:"accent_color"`
	ReplayMode               ReplayMode                        `json:"replay_mode"`
	ReplaySamplingRate       int                               `json:"replay_sampling_rate"`
	ReplayMaxDurationSeconds int                               `json:"replay_max_duration_seconds"`
	ReplayMaxEvents          int                               `json:"replay_max_events"`
	ReplayMaskAllInputs      *bool                             `json:"replay_mask_all_inputs"`
	ReplayMaskTextSelector   *string                           `json:"replay_mask_text_selector"`
	ReplayRecordCanvas       bool                              `json:"replay_record_canvas"`
	ReplayCanvasFPS          int                               `json:"replay_canvas_fps"`
	ReplayConsent            ReplayConsentConfig               `json:"replay_consent"`
	RecurrenceMode           RecurrenceMode                    `json:"recurrence_mode"`
	RecurrenceDays           *int                              `json:"recurrence_days"`
	DefinitionKey            string                            `json:"definition_key"`
	DefinitionStatus         WidgetDefinitionStatus            `json:"definition_status"`
	Presentation             WidgetPresentation                `json:"presentation"`
	Objective                WidgetObjective                   `json:"objective"`
	ResponseBlocks           []WidgetResponseBlock             `json:"response_blocks"`
	Localizations            map[string]WidgetLocalization     `json:"localizations"`
	AllowComment             *bool                             `json:"allow_comment"`
	CollectContact           bool                              `json:"collect_contact"`
	AppearanceOverrides      *widgetconfig.AppearanceOverrides `json:"appearance_overrides"`
}

type WidgetRuleValues struct {
	Name                     string
	Priority                 int
	Enabled                  bool
	MatcherType              MatcherType
	MatcherValue             *string
	IncludeConditions        []WidgetRuleTargetConditionValues
	ExcludeConditions        []WidgetRuleTargetConditionValues
	LegacyTargeting          bool
	TriggerType              TriggerType
	TriggerDelaySeconds      *int
	TriggerScrollPercent     *int
	CanRate                  bool
	CanWriteText             bool
	CanRecordAudio           bool
	CanRecordVideo           bool
	CanAttachElement         bool
	CanAttachScreenshot      bool
	Greeting                 *string
	Position                 WidgetPosition
	Theme                    WidgetTheme
	AccentColor              *string
	ReplayMode               ReplayMode
	ReplaySamplingRate       int
	ReplayMaxDurationSeconds int
	ReplayMaxEvents          int
	ReplayMaskAllInputs      bool
	ReplayMaskTextSelector   *string
	ReplayRecordCanvas       bool
	ReplayCanvasFPS          int
	ReplayConsent            ReplayConsentConfig
	RecurrenceMode           RecurrenceMode
	RecurrenceDays           *int
	DefinitionKey            string
	DefinitionStatus         WidgetDefinitionStatus
	Presentation             WidgetPresentation
	Objective                WidgetObjective
	ResponseBlocks           []WidgetResponseBlock
	Localizations            map[string]WidgetLocalization
	AllowComment             bool
	CollectContact           bool
	AppearanceOverrides      widgetconfig.AppearanceOverrides
	ExplicitAppearance       bool
}

type PublicConfiguration struct {
	TenantID                   string
	TenantSlug                 string
	ProjectID                  string
	DefaultLocale              string
	AllowedDomains             []string
	Plan                       tenant.Plan
	Status                     tenant.Status
	TrialEndsAt                *time.Time
	DeletionRequestedAt        *time.Time
	TenantAppearanceDefaults   widgetconfig.AppearanceOverrides
	ProjectAppearanceOverrides widgetconfig.AppearanceOverrides
	Rules                      []WidgetRule
}
