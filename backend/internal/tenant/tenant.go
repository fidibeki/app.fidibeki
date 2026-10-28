package tenant

import (
	"context"
	"errors"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/widgetconfig"
)

var (
	ErrUnknownTenant = errors.New("unknown tenant")
	ErrInactive      = errors.New("tenant is not active")
)

type Plan string

const (
	PlanStarter    Plan = "starter"
	PlanGrowth     Plan = "growth"
	PlanScale      Plan = "scale"
	PlanEnterprise Plan = "enterprise"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusTrial     Status = "trial"
	StatusCancelled Status = "cancelled"
)

type Tenant struct {
	ID                       string
	Name                     string
	ShortName                *string
	Slug                     string
	Plan                     Plan
	Status                   Status
	TrialEndsAt              *time.Time
	DeletionRequestedAt      *time.Time
	ReporterAccessEnabled    bool
	WidgetAppearanceDefaults widgetconfig.AppearanceOverrides
	WebsiteURL               *string
	SupportEmail             *string
	DefaultLocale            *string
	DefaultTimezone          *string
	PrivacyPolicyURL         *string
	PrivacyContactEmail      *string
	PrivacyRightsEmail       *string
	PrivacyProfileVersion    int
	TermsURL                 *string
	LogoVersion              *string
}

// IsActiveAt is the single lifecycle policy for tenant access and
// authentication. Deletion grace deliberately remains reachable but read-only;
// RequireWritable owns that separate restriction.
func (t Tenant) IsActiveAt(now time.Time) bool {
	if t.DeletionRequestedAt != nil {
		return true
	}
	if t.Status == StatusActive {
		return true
	}
	return t.Status == StatusTrial && t.TrialEndsAt != nil && t.TrialEndsAt.After(now)
}

type OrganizationProfile struct {
	Name                  string  `json:"name"`
	ShortName             *string `json:"short_name"`
	WebsiteURL            *string `json:"website_url"`
	SupportEmail          *string `json:"support_email"`
	AccentColor           *string `json:"accent_color"`
	DefaultLocale         *string `json:"default_locale"`
	DefaultTimezone       *string `json:"default_timezone"`
	PrivacyPolicyURL      *string `json:"privacy_policy_url"`
	PrivacyContactEmail   *string `json:"privacy_contact_email"`
	PrivacyRightsEmail    *string `json:"privacy_rights_email"`
	PrivacyProfileVersion int     `json:"privacy_profile_version"`
	TermsURL              *string `json:"terms_url"`
	LogoVersion           *string `json:"logo_version"`
}

func (t Tenant) OrganizationProfile() OrganizationProfile {
	return OrganizationProfile{
		Name: t.Name, ShortName: t.ShortName, WebsiteURL: t.WebsiteURL,
		SupportEmail: t.SupportEmail, AccentColor: t.WidgetAppearanceDefaults.AccentColor,
		DefaultLocale: t.DefaultLocale, DefaultTimezone: t.DefaultTimezone,
		PrivacyPolicyURL:    t.PrivacyPolicyURL,
		PrivacyContactEmail: t.PrivacyContactEmail, PrivacyRightsEmail: t.PrivacyRightsEmail,
		PrivacyProfileVersion: t.PrivacyProfileVersion,
		TermsURL:              t.TermsURL, LogoVersion: t.LogoVersion,
	}
}

type Resolver interface {
	Resolve(context.Context, string) (Tenant, error)
}

type ResolverFunc func(context.Context, string) (Tenant, error)

func (f ResolverFunc) Resolve(ctx context.Context, slug string) (Tenant, error) {
	return f(ctx, slug)
}
