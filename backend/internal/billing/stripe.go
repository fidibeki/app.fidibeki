package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

type StripeProvider struct {
	client        *stripe.Client
	webhookSecret string
}

func NewStripeProvider(secretKey, webhookSecret string) Provider {
	if secretKey == "" || webhookSecret == "" {
		return UnavailableProvider{}
	}
	return &StripeProvider{client: stripe.NewClient(secretKey), webhookSecret: webhookSecret}
}

func (p *StripeProvider) CreateCustomer(ctx context.Context, input CustomerInput) (string, error) {
	params := &stripe.CustomerCreateParams{
		Email: stripe.String(input.Email), Name: stripe.String(input.Name),
		Metadata: map[string]string{"tenant_id": input.TenantID},
	}
	customer, err := p.client.V1Customers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create Stripe customer: %w", err)
	}
	return customer.ID, nil
}

func (p *StripeProvider) CreateCheckout(ctx context.Context, input CheckoutInput) (string, error) {
	params := &stripe.CheckoutSessionCreateParams{
		Customer:          stripe.String(input.CustomerID),
		Mode:              stripe.String("subscription"),
		SuccessURL:        stripe.String(input.SuccessURL),
		CancelURL:         stripe.String(input.CancelURL),
		ClientReferenceID: stripe.String(input.TenantID),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Price: stripe.String(input.PriceID), Quantity: stripe.Int64(max(1, input.Quantity)),
		}},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata: map[string]string{"tenant_id": input.TenantID},
		},
	}
	if input.TrialEnd != nil {
		params.SubscriptionData.TrialEnd = stripe.Int64(input.TrialEnd.Unix())
	}
	session, err := p.client.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create Stripe checkout: %w", err)
	}
	return session.URL, nil
}

func (p *StripeProvider) CreatePortal(ctx context.Context, customerID, returnURL string) (string, error) {
	session, err := p.client.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer: stripe.String(customerID), ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		return "", fmt.Errorf("create Stripe customer portal: %w", err)
	}
	return session.URL, nil
}

func (p *StripeProvider) RetrieveSubscription(ctx context.Context, subscriptionID string) (Subscription, error) {
	remote, err := p.client.V1Subscriptions.Retrieve(ctx, subscriptionID, &stripe.SubscriptionRetrieveParams{})
	if err != nil {
		return Subscription{}, fmt.Errorf("retrieve Stripe subscription: %w", err)
	}
	result := Subscription{
		ID: remote.ID, Status: string(remote.Status), CancelAtPeriodEnd: remote.CancelAtPeriodEnd,
	}
	if remote.Customer != nil {
		result.CustomerID = remote.Customer.ID
	}
	if remote.Items == nil || remote.Items.HasMore || len(remote.Items.Data) != 1 ||
		remote.Items.Data[0] == nil || remote.Items.Data[0].ID == "" ||
		remote.Items.Data[0].Price == nil || remote.Items.Data[0].Price.ID == "" ||
		remote.Items.Data[0].Quantity < 1 {
		return Subscription{}, ErrSubscriptionItems
	}
	result.ItemID = remote.Items.Data[0].ID
	result.PriceID = remote.Items.Data[0].Price.ID
	result.Quantity = int(remote.Items.Data[0].Quantity)
	if remote.TrialEnd > 0 {
		trialEnd := time.Unix(remote.TrialEnd, 0).UTC()
		result.TrialEndsAt = &trialEnd
	}
	return result, nil
}

func (p *StripeProvider) UpdateSubscriptionQuantity(
	ctx context.Context,
	itemID string,
	quantity int,
	idempotencyKey string,
) error {
	if itemID == "" || quantity < 1 || idempotencyKey == "" {
		return ErrSubscriptionItems
	}
	params := &stripe.SubscriptionItemUpdateParams{
		Quantity:          stripe.Int64(int64(quantity)),
		ProrationBehavior: stripe.String("create_prorations"),
	}
	params.SetIdempotencyKey(idempotencyKey)
	if _, err := p.client.V1SubscriptionItems.Update(ctx, itemID, params); err != nil {
		return fmt.Errorf("update Stripe subscription item quantity: %w", err)
	}
	return nil
}

func (p *StripeProvider) CancelSubscription(ctx context.Context, subscriptionID string) error {
	if _, err := p.client.V1Subscriptions.Cancel(ctx, subscriptionID, &stripe.SubscriptionCancelParams{}); err != nil {
		return fmt.Errorf("cancel Stripe subscription: %w", err)
	}
	return nil
}

type stripeID string

func (id *stripeID) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err == nil && value != "" {
		*id = stripeID(value)
		return nil
	}
	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &object); err != nil || object.ID == "" {
		return ErrInvalidWebhook
	}
	*id = stripeID(object.ID)
	return nil
}

func (p *StripeProvider) VerifyEvent(payload []byte, signature string) (EventSignal, error) {
	event, err := webhook.ConstructEvent(payload, signature, p.webhookSecret)
	if err != nil {
		return EventSignal{}, fmt.Errorf("%w: %v", ErrInvalidWebhook, err)
	}
	if !supportedEvent(string(event.Type)) {
		return EventSignal{ID: event.ID, Type: string(event.Type)}, nil
	}
	var object struct {
		ID           string   `json:"id"`
		Customer     stripeID `json:"customer"`
		Subscription stripeID `json:"subscription"`
	}
	if err := json.Unmarshal(event.Data.Raw, &object); err != nil {
		return EventSignal{}, fmt.Errorf("%w: decode event object", ErrInvalidWebhook)
	}
	signal := EventSignal{ID: event.ID, Type: string(event.Type), CustomerID: string(object.Customer)}
	if event.Type == "customer.subscription.created" || event.Type == "customer.subscription.updated" ||
		event.Type == "customer.subscription.deleted" {
		signal.SubscriptionID = object.ID
	} else {
		signal.SubscriptionID = string(object.Subscription)
	}
	return signal, nil
}

func supportedEvent(eventType string) bool {
	switch eventType {
	case "checkout.session.completed", "customer.subscription.created", "customer.subscription.updated",
		"customer.subscription.deleted", "invoice.paid", "invoice.payment_failed":
		return true
	default:
		return false
	}
}

type UnavailableProvider struct{}

func (UnavailableProvider) CreateCustomer(context.Context, CustomerInput) (string, error) {
	return "", ErrProviderUnavailable
}
func (UnavailableProvider) CreateCheckout(context.Context, CheckoutInput) (string, error) {
	return "", ErrProviderUnavailable
}
func (UnavailableProvider) CreatePortal(context.Context, string, string) (string, error) {
	return "", ErrProviderUnavailable
}
func (UnavailableProvider) RetrieveSubscription(context.Context, string) (Subscription, error) {
	return Subscription{}, ErrProviderUnavailable
}
func (UnavailableProvider) UpdateSubscriptionQuantity(context.Context, string, int, string) error {
	return ErrProviderUnavailable
}
func (UnavailableProvider) CancelSubscription(context.Context, string) error {
	return ErrProviderUnavailable
}
func (UnavailableProvider) VerifyEvent([]byte, string) (EventSignal, error) {
	return EventSignal{}, ErrProviderUnavailable
}
