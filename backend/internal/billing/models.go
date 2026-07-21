package billing

import (
	"context"
	"errors"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

const (
	TrialDuration = 14 * 24 * time.Hour
	DeletionGrace = 7 * 24 * time.Hour
)

var (
	ErrNotFound            = errors.New("billing tenant not found")
	ErrSlugConflict        = errors.New("tenant slug already exists")
	ErrEmailConflict       = errors.New("member email already exists")
	ErrUnsupportedPlan     = errors.New("plan is not available for self-service billing")
	ErrAlreadySubscribed   = errors.New("tenant already has a Stripe subscription")
	ErrConfirmation        = errors.New("tenant confirmation does not match")
	ErrDeletionPending     = errors.New("tenant deletion is already pending")
	ErrDeletionNotPending  = errors.New("tenant deletion is not pending")
	ErrProviderUnavailable = errors.New("billing provider is unavailable")
	ErrInvalidWebhook      = errors.New("invalid billing webhook")
)

type Tenant struct {
	ID                          string
	Name                        string
	Slug                        string
	Plan                        tenant.Plan
	PendingPlan                 *tenant.Plan
	Status                      tenant.Status
	BillingEmail                string
	StripeCustomerID            string
	StripeSubscriptionID        string
	TrialEndsAt                 *time.Time
	DeletionRequestedAt         *time.Time
	DeletionRequestedByMemberID *string
}

type SignupInput struct {
	Name  string
	Slug  string
	Email string
	Plan  tenant.Plan
}

type SignupResult struct {
	Tenant   Tenant
	MemberID string
	Token    string
}

type Subscription struct {
	ID                string
	CustomerID        string
	PriceID           string
	Status            string
	TrialEndsAt       *time.Time
	CancelAtPeriodEnd bool
}

type EventSignal struct {
	ID             string
	Type           string
	CustomerID     string
	SubscriptionID string
}

type CheckoutInput struct {
	CustomerID string
	PriceID    string
	TenantID   string
	TrialEnd   *time.Time
	SuccessURL string
	CancelURL  string
}

type CustomerInput struct {
	Email    string
	Name     string
	TenantID string
}

type Provider interface {
	CreateCustomer(context.Context, CustomerInput) (string, error)
	CreateCheckout(context.Context, CheckoutInput) (string, error)
	CreatePortal(context.Context, string, string) (string, error)
	RetrieveSubscription(context.Context, string) (Subscription, error)
	CancelSubscription(context.Context, string) error
	VerifyEvent([]byte, string) (EventSignal, error)
}

type PriceCatalog map[tenant.Plan]string

func (c PriceCatalog) Price(plan tenant.Plan) (string, bool) {
	if plan == tenant.PlanEnterprise {
		return "", false
	}
	price := c[plan]
	return price, price != ""
}

func (c PriceCatalog) Plan(priceID string) (tenant.Plan, bool) {
	for plan, configured := range c {
		if configured == priceID && configured != "" {
			return plan, true
		}
	}
	return "", false
}

func planRank(plan tenant.Plan) int {
	switch plan {
	case tenant.PlanStarter:
		return 1
	case tenant.PlanGrowth:
		return 2
	case tenant.PlanScale:
		return 3
	case tenant.PlanEnterprise:
		return 4
	default:
		return 0
	}
}
