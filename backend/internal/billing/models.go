package billing

import (
	"context"
	"errors"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
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
	ErrUnsupportedInterval = errors.New("billing interval is not available for self-service billing")
	ErrAlreadySubscribed   = errors.New("tenant already has a Stripe subscription")
	ErrConfirmation        = errors.New("tenant confirmation does not match")
	ErrDeletionPending     = errors.New("tenant deletion is already pending")
	ErrDeletionNotPending  = errors.New("tenant deletion is not pending")
	ErrProviderUnavailable = errors.New("billing provider is unavailable")
	ErrInvalidWebhook      = errors.New("invalid billing webhook")
)

type BillingInterval string

const (
	IntervalMonthly BillingInterval = "monthly"
	IntervalYearly  BillingInterval = "yearly"
)

type Tenant struct {
	ID                          string
	Name                        string
	Slug                        string
	Plan                        tenant.Plan
	PendingPlan                 *tenant.Plan
	BillingInterval             BillingInterval
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

type PlanPrices struct {
	Monthly string
	Yearly  string
}

func (p PlanPrices) price(interval BillingInterval) string {
	switch interval {
	case IntervalMonthly:
		return p.Monthly
	case IntervalYearly:
		return p.Yearly
	default:
		return ""
	}
}

type PriceCatalog map[tenant.Plan]PlanPrices

func (c PriceCatalog) Price(plan tenant.Plan, interval BillingInterval) (string, bool) {
	if plan == tenant.PlanEnterprise {
		return "", false
	}
	price := c[plan].price(interval)
	return price, price != ""
}

func (c PriceCatalog) Plan(priceID string) (tenant.Plan, BillingInterval, bool) {
	if priceID == "" {
		return "", "", false
	}
	for plan, configured := range c {
		switch priceID {
		case configured.Monthly:
			return plan, IntervalMonthly, true
		case configured.Yearly:
			return plan, IntervalYearly, true
		}
	}
	return "", "", false
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
