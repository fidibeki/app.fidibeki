package project

import (
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
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
)

type Project struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Name           string    `json:"name"`
	PublicKey      string    `json:"public_key"`
	AllowedDomains []string  `json:"allowed_domains"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type WidgetRule struct {
	ID                       string         `json:"id"`
	ProjectID                string         `json:"project_id"`
	Name                     string         `json:"name"`
	Priority                 int            `json:"priority"`
	Enabled                  bool           `json:"enabled"`
	MatcherType              MatcherType    `json:"matcher_type"`
	MatcherValue             *string        `json:"matcher_value"`
	TriggerType              TriggerType    `json:"trigger_type"`
	TriggerDelaySeconds      *int           `json:"trigger_delay_seconds"`
	TriggerScrollPercent     *int           `json:"trigger_scroll_percent"`
	CanRate                  bool           `json:"can_rate"`
	CanWriteText             bool           `json:"can_write_text"`
	CanRecordAudio           bool           `json:"can_record_audio"`
	CanRecordVideo           bool           `json:"can_record_video"`
	CanAttachElement         bool           `json:"can_attach_element"`
	CanAttachScreenshot      bool           `json:"can_attach_screenshot"`
	Greeting                 *string        `json:"greeting"`
	Position                 WidgetPosition `json:"position"`
	Theme                    WidgetTheme    `json:"theme"`
	AccentColor              *string        `json:"accent_color"`
	ReplayMode               ReplayMode     `json:"replay_mode"`
	ReplaySamplingRate       int            `json:"replay_sampling_rate"`
	ReplayMaxDurationSeconds int            `json:"replay_max_duration_seconds"`
	ReplayMaxEvents          int            `json:"replay_max_events"`
	ReplayMaskAllInputs      bool           `json:"replay_mask_all_inputs"`
	ReplayMaskTextSelector   *string        `json:"replay_mask_text_selector"`
	CreatedAt                time.Time      `json:"created_at"`
	UpdatedAt                time.Time      `json:"updated_at"`
}

type WidgetRuleInput struct {
	Name                     string         `json:"name"`
	Priority                 int            `json:"priority"`
	Enabled                  *bool          `json:"enabled"`
	MatcherType              MatcherType    `json:"matcher_type"`
	MatcherValue             *string        `json:"matcher_value"`
	TriggerType              TriggerType    `json:"trigger_type"`
	TriggerDelaySeconds      *int           `json:"trigger_delay_seconds"`
	TriggerScrollPercent     *int           `json:"trigger_scroll_percent"`
	CanRate                  *bool          `json:"can_rate"`
	CanWriteText             *bool          `json:"can_write_text"`
	CanRecordAudio           bool           `json:"can_record_audio"`
	CanRecordVideo           bool           `json:"can_record_video"`
	CanAttachElement         bool           `json:"can_attach_element"`
	CanAttachScreenshot      bool           `json:"can_attach_screenshot"`
	Greeting                 *string        `json:"greeting"`
	Position                 WidgetPosition `json:"position"`
	Theme                    WidgetTheme    `json:"theme"`
	AccentColor              *string        `json:"accent_color"`
	ReplayMode               ReplayMode     `json:"replay_mode"`
	ReplaySamplingRate       int            `json:"replay_sampling_rate"`
	ReplayMaxDurationSeconds int            `json:"replay_max_duration_seconds"`
	ReplayMaxEvents          int            `json:"replay_max_events"`
	ReplayMaskAllInputs      *bool          `json:"replay_mask_all_inputs"`
	ReplayMaskTextSelector   *string        `json:"replay_mask_text_selector"`
}

type WidgetRuleValues struct {
	Name                     string
	Priority                 int
	Enabled                  bool
	MatcherType              MatcherType
	MatcherValue             *string
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
}

type PublicConfiguration struct {
	TenantID            string
	TenantSlug          string
	ProjectID           string
	AllowedDomains      []string
	Plan                tenant.Plan
	Status              tenant.Status
	TrialEndsAt         *time.Time
	DeletionRequestedAt *time.Time
	Rules               []WidgetRule
}
