package mobilecapture

import (
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetconfig"
)

const (
	TokenPrefix           = "fmc_"
	SubmitTTL             = 10 * time.Minute
	RetryTTL              = 10 * time.Minute
	CleanupGrace          = 24 * time.Hour
	maximumRequestBody    = 1 << 20
	configurationSchemaV1 = 1
)

type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
)

func (platform Platform) Valid() bool {
	return platform == PlatformIOS || platform == PlatformAndroid
}

type CreateSessionRequest struct {
	ProjectID           string   `json:"project_id"`
	WidgetDefinitionKey string   `json:"widget_definition_key"`
	Platform            Platform `json:"platform"`
	ApplicationID       string   `json:"application_id"`
	Screen              string   `json:"screen"`
	ContextURL          *string  `json:"context_url"`
	DisplayLocale       string   `json:"display_locale"`
}

type CreateSessionValues struct {
	ProjectID           string
	WidgetDefinitionKey string
	Platform            Platform
	ApplicationID       string
	Screen              string
	ContextURL          *string
	DisplayLocale       string
}

type CaptureSession struct {
	ID                        string
	TenantID                  string
	ProjectID                 string
	WidgetRuleID              string
	IssuerAPIKeyID            string
	TokenHash                 string
	Platform                  Platform
	ApplicationID             string
	Screen                    string
	ContextURL                *string
	DisplayLocale             string
	DefinitionVersion         int
	ControllerProfileVersion  int
	FeedbackID                *string
	ClientFeedbackID          *string
	RequestDigest             *string
	AcceptedAt                *time.Time
	SubmitExpiresAt           time.Time
	RetryExpiresAt            *time.Time
	CleanupAfter              time.Time
	CreatedAt                 time.Time
	IssuerExpiresAt           *time.Time
	TenantStatus              string
	TenantTrialEndsAt         *time.Time
	TenantDeletionRequestedAt *time.Time
}

func (session CaptureSession) AuthorizedAt(now time.Time) bool {
	now = now.UTC()
	if session.IssuerExpiresAt != nil && !now.Before(*session.IssuerExpiresAt) {
		return false
	}
	if session.TenantDeletionRequestedAt != nil {
		return false
	}
	switch session.TenantStatus {
	case "active":
		return true
	case "trial":
		return session.TenantTrialEndsAt != nil && now.Before(*session.TenantTrialEndsAt)
	default:
		return false
	}
}

type ResolvedConfiguration struct {
	Configuration project.PublicConfiguration
	Definition    project.WidgetRule
	DisplayLocale string
	Localization  project.WidgetLocalization
}

type PublicConfiguration struct {
	SchemaVersion     int                        `json:"schema_version"`
	ProjectID         string                     `json:"project_id"`
	DisplayLocale     string                     `json:"display_locale"`
	Definition        PublicDefinition           `json:"definition"`
	ControllerProfile *project.ControllerProfile `json:"controller_profile,omitempty"`
	ContactNotice     *PublicContactNotice       `json:"contact_notice,omitempty"`
}

type PublicDefinition struct {
	ID                  string                        `json:"id"`
	DefinitionKey       string                        `json:"definition_key"`
	DefinitionVersion   int                           `json:"definition_version"`
	Presentation        project.WidgetPresentation    `json:"presentation"`
	LauncherVariant     project.WidgetLauncherVariant `json:"launcher_variant"`
	Objective           project.WidgetObjective       `json:"objective"`
	ResponseBlocks      []project.WidgetResponseBlock `json:"response_blocks"`
	Localization        project.WidgetLocalization    `json:"localization"`
	AllowComment        bool                          `json:"allow_comment"`
	CollectContact      bool                          `json:"collect_contact"`
	CanAttachScreenshot bool                          `json:"can_attach_screenshot"`
	RecurrenceMode      project.RecurrenceMode        `json:"recurrence_mode"`
	RecurrenceDays      *int                          `json:"recurrence_days"`
	AIProcessing        PublicAIProcessing            `json:"ai_processing"`
	Appearance          widgetconfig.Appearance       `json:"appearance"`
}

type PublicAIProcessing struct {
	ProviderLabel    string                    `json:"provider_label,omitempty"`
	Enabled          bool                      `json:"enabled"`
	Provider         string                    `json:"provider,omitempty"`
	InputCategories  []project.AIInputCategory `json:"input_categories,omitempty"`
	PrivacyPolicyURL string                    `json:"privacy_policy_url,omitempty"`
	PolicyVersion    string                    `json:"policy_version,omitempty"`
}

type PublicContactNotice struct {
	NoticeVersion            string `json:"notice_version"`
	ControllerProfileVersion int    `json:"controller_profile_version"`
	WidgetDefinitionVersion  int    `json:"widget_definition_version"`
	Locale                   string `json:"locale"`
	ControllerName           string `json:"controller_name"`
	PrivacyContactEmail      string `json:"privacy_contact_email"`
	PrivacyRightsEmail       string `json:"privacy_rights_email"`
	PrivacyPolicyURL         string `json:"privacy_policy_url"`
	PurposeSummary           string `json:"purpose_summary"`
	LawfulBasisSummary       string `json:"lawful_basis_summary"`
	RetentionSummary         string `json:"retention_summary"`
}

type CreateSessionResponse struct {
	Token           string              `json:"token"`
	SubmitExpiresAt time.Time           `json:"submit_expires_at"`
	Configuration   PublicConfiguration `json:"configuration"`
}

type ResolvedToken struct {
	TenantID  string
	SessionID string
}

type FieldErrors map[string]string
