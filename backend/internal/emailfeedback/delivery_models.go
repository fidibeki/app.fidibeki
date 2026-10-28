package emailfeedback

import (
	"encoding/json"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/project"
)

const TaskDeliverInvitation = "email-feedback:deliver-invitation"

type DeliveryTask struct {
	OutboxEventID string `json:"outbox_event_id"`
	TenantID      string `json:"tenant_id"`
	ResourceID    string `json:"resource_id"`
}

type invitationCampaignSnapshot struct {
	Name         string `json:"name"`
	Subject      string `json:"subject"`
	Introduction string `json:"introduction"`
}

type invitationControllerSnapshot struct {
	Name                string `json:"name"`
	PrivacyContactEmail string `json:"privacy_contact_email"`
	PrivacyRightsEmail  string `json:"privacy_rights_email"`
	PrivacyPolicyURL    string `json:"privacy_policy_url"`
}

type invitationDefinitionSnapshot struct {
	ResponseBlocks []project.WidgetResponseBlock `json:"response_blocks"`
	AllowComment   bool                          `json:"allow_comment"`
	CollectContact bool                          `json:"collect_contact"`
	AIProcessing   project.AIProcessingConfig    `json:"ai_processing"`
}

type InvitationDelivery struct {
	OutboxEventID     string
	TenantID          string
	TenantSlug        string
	InvitationID      string
	Status            InvitationStatus
	RecipientEmail    *string
	RecipientDigest   string
	TokenHash         *string
	Locale            string
	ExpiresAt         time.Time
	CampaignSnapshot  invitationCampaignSnapshot
	ControllerProfile invitationControllerSnapshot
	Suppressed        bool
}

type deliverySnapshots struct {
	Campaign   json.RawMessage
	Controller json.RawMessage
}
