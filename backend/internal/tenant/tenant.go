package tenant

import (
	"context"
	"errors"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/widgetconfig"
)

var ErrUnknownTenant = errors.New("unknown tenant")

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
	TermsURL                 *string
	LogoVersion              *string
}

type Resolver interface {
	Resolve(context.Context, string) (Tenant, error)
}

type ResolverFunc func(context.Context, string) (Tenant, error)

func (f ResolverFunc) Resolve(ctx context.Context, slug string) (Tenant, error) {
	return f(ctx, slug)
}
