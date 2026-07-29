package emailfeedback

import (
	"encoding/json"
	"time"
)

const EventInvitationCreated = "email_feedback.invitation_created"

type InvitationStatus string

const (
	InvitationQueued     InvitationStatus = "queued"
	InvitationSent       InvitationStatus = "sent"
	InvitationSubmitted  InvitationStatus = "submitted"
	InvitationSuppressed InvitationStatus = "suppressed"
	InvitationBounced    InvitationStatus = "bounced"
	InvitationComplained InvitationStatus = "complained"
	InvitationFailed     InvitationStatus = "failed"
	InvitationExpired    InvitationStatus = "expired"
)

type InvitationInput struct {
	CampaignID      string  `json:"campaign_id"`
	ExternalEventID string  `json:"external_event_id"`
	RecipientEmail  string  `json:"recipient_email"`
	Locale          *string `json:"locale"`
	ContextURL      *string `json:"context_url"`
}

type InvitationValues struct {
	CampaignID      string
	ExternalEventID string
	RecipientEmail  string
	RecipientDigest string
	RequestedLocale string
	ContextURL      *string
	RequestDigest   string
}

type Invitation struct {
	ID              string           `json:"id"`
	TenantID        string           `json:"-"`
	ProjectID       string           `json:"-"`
	CampaignID      string           `json:"campaign_id"`
	WidgetRuleID    string           `json:"-"`
	ExternalEventID string           `json:"external_event_id"`
	Status          InvitationStatus `json:"status"`
	Locale          string           `json:"locale"`
	ContextURL      *string          `json:"context_url,omitempty"`
	ExpiresAt       time.Time        `json:"expires_at"`
	CreatedAt       time.Time        `json:"created_at"`
	RequestDigest   string           `json:"-"`
}

type invitationCampaign struct {
	ProjectID                string
	WidgetRuleID             string
	Locale                   string
	InvitationTTLDays        int
	DefinitionVersion        int
	ControllerProfileVersion int
	CampaignSnapshot         json.RawMessage
	DefinitionSnapshot       json.RawMessage
	LocalizationSnapshot     json.RawMessage
	ControllerSnapshot       json.RawMessage
}
