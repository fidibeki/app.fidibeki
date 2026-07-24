package widgetanalytics

import (
	"errors"
	"time"
)

// The widget_analytics consent ledger deliberately mirrors the shape of the session_replay ledger
// (internal/replay) but stays a fully separate table/type/HTTP surface: analytics grants must never
// be inferred from, or joined with, replay or reporter-contact consent (issue #58).
const ConsentPurpose = "widget_analytics"

var (
	ErrConsentRequired = errors.New("active widget analytics consent is required")
	ErrConsentExpired  = errors.New("widget analytics consent expired")
	ErrNotFound        = errors.New("widget rule not found")
)

type ConsentState string

const (
	ConsentActive    ConsentState = "active"
	ConsentDenied    ConsentState = "denied"
	ConsentWithdrawn ConsentState = "withdrawn"
	ConsentExpired   ConsentState = "expired"
)

type Consent struct {
	ID                       string       `json:"id"`
	TenantID                 string       `json:"tenant_id"`
	ProjectID                string       `json:"project_id"`
	WidgetRuleID             string       `json:"widget_rule_id"`
	Purpose                  string       `json:"purpose"`
	NoticeVersion            string       `json:"notice_version"`
	Locale                   string       `json:"locale"`
	Source                   string       `json:"source"`
	CustomerConsentReference *string      `json:"customer_consent_reference,omitempty"`
	State                    ConsentState `json:"state"`
	AcceptedAt               *time.Time   `json:"accepted_at,omitempty"`
	WithdrawnAt              *time.Time   `json:"withdrawn_at,omitempty"`
	ExpiresAt                *time.Time   `json:"expires_at,omitempty"`
	CreatedAt                time.Time    `json:"created_at"`
	UpdatedAt                time.Time    `json:"updated_at"`
}
