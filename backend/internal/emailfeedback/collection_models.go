package emailfeedback

import (
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

type InvitationCollection struct {
	ID                       string
	TenantID                 string
	TenantSlug               string
	ProjectID                string
	WidgetRuleID             string
	Locale                   string
	ContextURL               *string
	Status                   InvitationStatus
	FeedbackID               *string
	DefinitionVersion        int
	ControllerProfileVersion int
	ExpiresAt                time.Time
	Plan                     tenant.Plan
	TenantStatus             tenant.Status
	TrialEndsAt              *time.Time
	DeletionRequestedAt      *time.Time
	Campaign                 invitationCampaignSnapshot
	Definition               invitationDefinitionSnapshot
	Localization             project.WidgetLocalization
	Controller               invitationControllerSnapshot
	Suppressed               bool
}

func (collection InvitationCollection) AvailableAt(now time.Time) bool {
	organization := tenant.Tenant{
		Status: collection.TenantStatus, TrialEndsAt: collection.TrialEndsAt,
		DeletionRequestedAt: collection.DeletionRequestedAt,
	}
	return collection.Status == InvitationSent &&
		!collection.Suppressed &&
		collection.DeletionRequestedAt == nil &&
		collection.ExpiresAt.After(now.UTC()) &&
		organization.IsActiveAt(now.UTC())
}

func (collection InvitationCollection) Configuration() project.PublicConfiguration {
	canRate := false
	canWriteText := false
	for _, block := range collection.Definition.ResponseBlocks {
		canRate = canRate || block.Type == project.ResponseRating
		canWriteText = canWriteText || block.Type == project.ResponseFreeText
	}
	controller := &project.ControllerProfile{
		Version:             collection.ControllerProfileVersion,
		Name:                collection.Controller.Name,
		PrivacyContactEmail: collection.Controller.PrivacyContactEmail,
		PrivacyRightsEmail:  collection.Controller.PrivacyRightsEmail,
		PrivacyPolicyURL:    collection.Controller.PrivacyPolicyURL,
	}
	return project.PublicConfiguration{
		TenantID: collection.TenantID, TenantSlug: collection.TenantSlug,
		ProjectID: collection.ProjectID, DefaultLocale: collection.Locale,
		ControllerProfile:        controller,
		ControllerProfileVersion: collection.ControllerProfileVersion,
		Plan:                     collection.Plan, Status: collection.TenantStatus,
		TrialEndsAt:         collection.TrialEndsAt,
		DeletionRequestedAt: collection.DeletionRequestedAt,
		Rules: []project.WidgetRule{{
			ID: collection.WidgetRuleID, ProjectID: collection.ProjectID,
			Enabled: true, DefinitionVersion: collection.DefinitionVersion,
			DefinitionStatus: project.DefinitionActive,
			CanRate:          canRate, CanWriteText: canWriteText,
			ResponseBlocks: collection.Definition.ResponseBlocks,
			Localizations: map[string]project.WidgetLocalization{
				collection.Locale: collection.Localization,
			},
			AllowComment:   collection.Definition.AllowComment,
			CollectContact: collection.Definition.CollectContact,
			AIProcessing:   collection.Definition.AIProcessing,
		}},
	}
}

type InvitationCollectionView struct {
	Locale                   string                        `json:"locale"`
	CampaignName             string                        `json:"campaign_name"`
	Introduction             string                        `json:"introduction"`
	ResponseBlocks           []project.WidgetResponseBlock `json:"response_blocks"`
	AllowComment             bool                          `json:"allow_comment"`
	CollectContact           bool                          `json:"collect_contact"`
	AIProcessing             InvitationAIProcessingView    `json:"ai_processing"`
	Localization             project.WidgetLocalization    `json:"localization"`
	ControllerName           string                        `json:"controller_name"`
	PrivacyPolicyURL         string                        `json:"privacy_policy_url"`
	DefinitionVersion        int                           `json:"definition_version"`
	ControllerProfileVersion int                           `json:"controller_profile_version"`
}

type InvitationAIProcessingView struct {
	Enabled          bool   `json:"enabled"`
	Provider         string `json:"provider,omitempty"`
	PrivacyPolicyURL string `json:"privacy_policy_url,omitempty"`
}

func collectionView(collection InvitationCollection) InvitationCollectionView {
	return InvitationCollectionView{
		Locale:         collection.Locale,
		CampaignName:   collection.Campaign.Name,
		Introduction:   collection.Campaign.Introduction,
		ResponseBlocks: collection.Definition.ResponseBlocks,
		AllowComment:   collection.Definition.AllowComment,
		CollectContact: collection.Definition.CollectContact,
		AIProcessing: InvitationAIProcessingView{
			Enabled:          collection.Definition.AIProcessing.Enabled,
			Provider:         collection.Definition.AIProcessing.Provider,
			PrivacyPolicyURL: collection.Definition.AIProcessing.PrivacyPolicyURL,
		},
		Localization:             collection.Localization,
		ControllerName:           collection.Controller.Name,
		PrivacyPolicyURL:         collection.Controller.PrivacyPolicyURL,
		DefinitionVersion:        collection.DefinitionVersion,
		ControllerProfileVersion: collection.ControllerProfileVersion,
	}
}
