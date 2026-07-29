package emailfeedback

import "time"

type CampaignStatus string

const (
	CampaignDraft    CampaignStatus = "draft"
	CampaignActive   CampaignStatus = "active"
	CampaignPaused   CampaignStatus = "paused"
	CampaignArchived CampaignStatus = "archived"
)

type CampaignReadiness struct {
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons"`
}

type Campaign struct {
	ID                string            `json:"id"`
	TenantID          string            `json:"tenant_id"`
	ProjectID         string            `json:"project_id"`
	WidgetRuleID      string            `json:"widget_rule_id"`
	WidgetRuleName    string            `json:"widget_rule_name"`
	CampaignKey       string            `json:"campaign_key"`
	Name              string            `json:"name"`
	Status            CampaignStatus    `json:"status"`
	DefaultLocale     string            `json:"default_locale"`
	Subject           string            `json:"subject"`
	Introduction      string            `json:"introduction"`
	InvitationTTLDays int               `json:"invitation_ttl_days"`
	Readiness         CampaignReadiness `json:"readiness"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type CampaignInput struct {
	ProjectID         string         `json:"project_id"`
	WidgetRuleID      string         `json:"widget_rule_id"`
	CampaignKey       string         `json:"campaign_key"`
	Name              string         `json:"name"`
	Status            CampaignStatus `json:"status"`
	DefaultLocale     string         `json:"default_locale"`
	Subject           string         `json:"subject"`
	Introduction      string         `json:"introduction"`
	InvitationTTLDays int            `json:"invitation_ttl_days"`
}

type CampaignValues struct {
	ProjectID         string
	WidgetRuleID      string
	CampaignKey       string
	Name              string
	Status            CampaignStatus
	DefaultLocale     string
	Subject           string
	Introduction      string
	InvitationTTLDays int
}

type FieldErrors map[string]string
