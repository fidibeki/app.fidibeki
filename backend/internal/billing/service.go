package billing

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

type Service struct {
	Store         ServiceStore
	Provider      Provider
	Verifier      SignupVerifier
	Email         email.Enqueuer
	Prices        PriceCatalog
	PublicBaseURL string
	BaseDomain    string
	AuthFrom      string
	Now           func() time.Time
}

type ServiceStore interface {
	Signup(context.Context, SignupInput) (SignupResult, error)
	Get(context.Context, string) (Tenant, error)
	SaveCustomer(context.Context, string, string) (string, error)
	FindByStripeReference(context.Context, string, string) (Tenant, error)
	ApplyEvent(context.Context, EventSignal, Subscription, tenant.Plan) (bool, error)
	RequestDeletion(context.Context, string, string, string) (Tenant, error)
	CancelDeletion(context.Context, string) error
	MemberEmails(context.Context, string) ([]string, error)
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) Signup(
	ctx context.Context,
	input SignupInput,
	turnstileToken, remoteIP, idempotencyKey string,
) (Tenant, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if len(input.Name) < 1 || len(input.Name) > 120 || tenant.ValidateSlug(input.Slug) != nil {
		return Tenant{}, errors.New("name or slug is invalid")
	}
	parsed, err := mail.ParseAddress(input.Email)
	if err != nil || parsed.Address != input.Email {
		return Tenant{}, errors.New("email is invalid")
	}
	if input.Plan != tenant.PlanStarter && input.Plan != tenant.PlanGrowth && input.Plan != tenant.PlanScale {
		return Tenant{}, ErrUnsupportedPlan
	}
	if s.Verifier == nil {
		return Tenant{}, ErrTurnstile
	}
	if err := s.Verifier.Verify(ctx, turnstileToken, remoteIP, idempotencyKey); err != nil {
		return Tenant{}, err
	}
	result, err := s.Store.Signup(ctx, input)
	if err != nil {
		return Tenant{}, err
	}
	loginURL, err := s.tenantURL(result.Tenant.Slug, "/auth/verify")
	if err != nil {
		return Tenant{}, err
	}
	loginURL += "#token=" + url.QueryEscape(result.Token)
	message, err := s.composeEmail(email.Message{
		From: s.AuthFrom, To: input.Email, Subject: "Start your Fidibeki trial",
		IdempotencyKey: "signup:" + result.Tenant.ID,
	}, email.Content{
		Preheader: "Activate your " + result.Tenant.Name + " workspace",
		Heading:   "Start your Fidibeki trial", Workspace: result.Tenant.Name,
		Body: []string{
			"Your workspace is ready. Use this secure link to activate the owner account and sign in.",
			"The link expires in 10 minutes and can only be used once.",
		},
		Metadata: []email.Metadata{{Label: "Plan", Value: string(result.Tenant.Plan)}},
		Action:   &email.Link{Label: "Activate workspace", URL: loginURL}, ShowActionURL: true,
		Footer:      "You received this because this email address created a Fidibeki workspace.",
		FooterLinks: []email.Link{{Label: "Contact support", URL: "mailto:support@fidibeki.com"}},
	})
	if err != nil {
		return Tenant{}, err
	}
	if s.Email == nil {
		return Tenant{}, errors.New("email queue is unavailable")
	}
	if err := s.Email.Enqueue(ctx, message); err != nil {
		return Tenant{}, fmt.Errorf("enqueue signup email: %w", err)
	}
	return result.Tenant, nil
}

func (s Service) Checkout(ctx context.Context, tenantID string, plan tenant.Plan) (string, error) {
	priceID, ok := s.Prices.Price(plan)
	if !ok {
		return "", ErrUnsupportedPlan
	}
	current, err := s.Store.Get(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if current.StripeSubscriptionID != "" {
		return "", ErrAlreadySubscribed
	}
	if current.DeletionRequestedAt != nil {
		return "", ErrDeletionPending
	}
	customerID, err := s.ensureCustomer(ctx, current)
	if err != nil {
		return "", err
	}
	successURL, err := s.tenantURL(current.Slug, "/settings/billing?checkout=success")
	if err != nil {
		return "", err
	}
	cancelURL, err := s.tenantURL(current.Slug, "/settings/billing?checkout=cancelled")
	if err != nil {
		return "", err
	}
	var stripeTrialEnd *time.Time
	if current.TrialEndsAt != nil && current.TrialEndsAt.After(s.now().Add(48*time.Hour)) {
		value := *current.TrialEndsAt
		stripeTrialEnd = &value
	}
	return s.Provider.CreateCheckout(ctx, CheckoutInput{
		CustomerID: customerID, PriceID: priceID, TenantID: current.ID,
		TrialEnd: stripeTrialEnd, SuccessURL: successURL, CancelURL: cancelURL,
	})
}

func (s Service) Portal(ctx context.Context, tenantID string) (string, error) {
	current, err := s.Store.Get(ctx, tenantID)
	if err != nil {
		return "", err
	}
	customerID, err := s.ensureCustomer(ctx, current)
	if err != nil {
		return "", err
	}
	returnURL, err := s.tenantURL(current.Slug, "/settings/billing")
	if err != nil {
		return "", err
	}
	return s.Provider.CreatePortal(ctx, customerID, returnURL)
}

func (s Service) ensureCustomer(ctx context.Context, current Tenant) (string, error) {
	if current.StripeCustomerID != "" {
		return current.StripeCustomerID, nil
	}
	created, err := s.Provider.CreateCustomer(ctx, CustomerInput{
		Email: current.BillingEmail, Name: current.Name, TenantID: current.ID,
	})
	if err != nil {
		return "", err
	}
	return s.Store.SaveCustomer(ctx, current.ID, created)
}

func (s Service) HandleWebhook(ctx context.Context, payload []byte, signature string) (bool, error) {
	signal, err := s.Provider.VerifyEvent(payload, signature)
	if err != nil {
		return false, err
	}
	if !supportedEvent(signal.Type) {
		return false, nil
	}
	current, err := s.Store.FindByStripeReference(ctx, signal.CustomerID, signal.SubscriptionID)
	if err != nil {
		return false, err
	}
	subscriptionID := signal.SubscriptionID
	if subscriptionID == "" {
		subscriptionID = current.StripeSubscriptionID
	}
	if subscriptionID == "" {
		return false, ErrInvalidWebhook
	}
	subscription, err := s.Provider.RetrieveSubscription(ctx, subscriptionID)
	if err != nil {
		return false, err
	}
	remotePlan, ok := s.Prices.Plan(subscription.PriceID)
	if !ok {
		return false, ErrUnsupportedPlan
	}
	return s.Store.ApplyEvent(ctx, signal, subscription, remotePlan)
}

func (s Service) RequestDeletion(ctx context.Context, tenantID, memberID, confirmation string) (time.Time, error) {
	current, err := s.Store.RequestDeletion(ctx, tenantID, memberID, strings.TrimSpace(confirmation))
	if err != nil {
		return time.Time{}, err
	}
	emails, err := s.Store.MemberEmails(ctx, tenantID)
	if err != nil {
		return time.Time{}, err
	}
	executesAt := current.DeletionRequestedAt.Add(DeletionGrace)
	billingURL, err := s.tenantURL(current.Slug, "/settings/billing")
	if err != nil {
		return time.Time{}, err
	}
	for _, address := range emails {
		message, err := s.composeEmail(email.Message{
			From: s.AuthFrom, To: address, Subject: "Fidibeki account deletion scheduled",
			IdempotencyKey: "tenant-deletion:" + tenantID + ":" + address,
		}, email.Content{
			Preheader: current.Name + " is scheduled for deletion",
			Heading:   "Account deletion scheduled", Workspace: current.Name,
			Body: []string{
				"This workspace is scheduled for permanent deletion. Its data will remain available during the recovery period.",
				"An authorized administrator can cancel the deletion before the scheduled time.",
			},
			Metadata:    []email.Metadata{{Label: "Scheduled for", Value: executesAt.UTC().Format(time.RFC1123)}},
			Action:      &email.Link{Label: "Review account deletion", URL: billingURL},
			Footer:      "You received this mandatory account notice because you are a member of this workspace.",
			FooterLinks: []email.Link{{Label: "Contact support", URL: "mailto:support@fidibeki.com"}},
		})
		if err != nil {
			return time.Time{}, err
		}
		if err := s.Email.Enqueue(ctx, message); err != nil {
			return time.Time{}, fmt.Errorf("enqueue tenant deletion email: %w", err)
		}
	}
	return executesAt, nil
}

func (s Service) composeEmail(message email.Message, content email.Content) (email.Message, error) {
	logoURL, err := email.BrandLogoURL(s.PublicBaseURL, s.BaseDomain)
	if err != nil {
		return email.Message{}, err
	}
	content.LogoURL = logoURL
	return email.Compose(message, content)
}

func (s Service) tenantURL(slug, path string) (string, error) {
	return tenantURL(s.PublicBaseURL, s.BaseDomain, slug, path)
}

func tenantURL(publicBaseURL, baseDomain, slug, path string) (string, error) {
	base, err := url.Parse(publicBaseURL)
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", errors.New("public base URL is invalid")
	}
	port := base.Port()
	base.Host = slug + "." + baseDomain
	if port != "" {
		base.Host += ":" + port
	}
	base.Path = path
	base.RawQuery = ""
	if index := strings.Index(path, "?"); index >= 0 {
		base.Path = path[:index]
		base.RawQuery = path[index+1:]
	}
	return base.String(), nil
}
