package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	stripe "github.com/stripe/stripe-go/v85"
)

func TestPriceCatalogAndStripeStatus(t *testing.T) {
	catalog := PriceCatalog{tenant.PlanStarter: "price_starter", tenant.PlanScale: "price_scale"}
	if price, ok := catalog.Price(tenant.PlanStarter); !ok || price != "price_starter" {
		t.Fatalf("Price(starter) = %q, %t", price, ok)
	}
	if _, ok := catalog.Price(tenant.PlanEnterprise); ok {
		t.Fatal("Price(enterprise) available")
	}
	if plan, ok := catalog.Plan("price_scale"); !ok || plan != tenant.PlanScale {
		t.Fatalf("Plan(price_scale) = %q, %t", plan, ok)
	}
	if _, ok := catalog.Plan("missing"); ok {
		t.Fatal("Plan(missing) found")
	}
	if planRank(tenant.PlanGrowth) <= planRank(tenant.PlanStarter) || planRank("unknown") != 0 {
		t.Fatal("unexpected plan ranking")
	}
	_ = planRank(tenant.PlanScale)
	_ = planRank(tenant.PlanEnterprise)
	for status, expected := range map[string]tenant.Status{
		"active": tenant.StatusActive, "trialing": tenant.StatusTrial,
		"canceled": tenant.StatusCancelled, "past_due": tenant.StatusSuspended,
	} {
		if got := stripeTenantStatus(status); got != expected {
			t.Errorf("stripeTenantStatus(%q) = %q, want %q", status, got, expected)
		}
	}
}

func TestStripeIDJSONForms(t *testing.T) {
	var id stripeID
	if err := json.Unmarshal([]byte(`{"id":"cus_object"}`), &id); err != nil || id != "cus_object" {
		t.Fatalf("object Stripe ID = %q, %v", id, err)
	}
	if err := json.Unmarshal([]byte(`null`), &id); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("invalid Stripe ID error = %v", err)
	}
}

func TestTurnstileVerifier(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("secret") != "secret" || r.Form.Get("response") != "token" {
			t.Errorf("unexpected form: %v", r.Form)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"success":true,"hostname":"fidibeki.com","action":"signup"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	verifier := TurnstileVerifier{
		SecretKey: "secret", ExpectedHost: "fidibeki.com", VerificationURL: "https://turnstile.test/siteverify",
		HTTPClient: client,
	}
	if err := verifier.Verify(context.Background(), "token", "127.0.0.1", "request-id"); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	verifier.ExpectedHost = "evil.example"
	if err := verifier.Verify(context.Background(), "token", "", ""); !errors.Is(err, ErrTurnstile) {
		t.Fatalf("Verify(wrong host) error = %v", err)
	}
	if err := verifier.Verify(context.Background(), "", "", ""); !errors.Is(err, ErrTurnstile) {
		t.Fatalf("Verify(empty) error = %v", err)
	}
	if err := (LocalSignupVerifier{}).Verify(context.Background(), "", "", ""); err != nil {
		t.Fatalf("local Verify() error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestStripeWebhookVerification(t *testing.T) {
	secret := "whsec_test"
	now := time.Now().Unix()
	payload := []byte(fmt.Sprintf(`{
		"id":"evt_1","object":"event","api_version":%q,"created":%d,
		"type":"customer.subscription.updated",
		"data":{"object":{"id":"sub_1","customer":"cus_1"}}
	}`, stripe.APIVersion, now))
	message := fmt.Sprintf("%d.%s", now, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	header := fmt.Sprintf("t=%d,v1=%s", now, hex.EncodeToString(mac.Sum(nil)))
	provider := &StripeProvider{webhookSecret: secret}
	signal, err := provider.VerifyEvent(payload, header)
	if err != nil {
		t.Fatalf("VerifyEvent() error = %v", err)
	}
	if signal.ID != "evt_1" || signal.SubscriptionID != "sub_1" || signal.CustomerID != "cus_1" {
		t.Fatalf("signal = %#v", signal)
	}
	if _, err := provider.VerifyEvent(payload, "t=1,v1=bad"); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("VerifyEvent(bad signature) error = %v", err)
	}
	if _, ok := NewStripeProvider("", "").(UnavailableProvider); !ok {
		t.Fatal("NewStripeProvider(empty) did not return unavailable provider")
	}
}

func TestStripeProviderAdapter(t *testing.T) {
	trialEnd := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	backend := &stripeBackendStub{trialEnd: trialEnd.Unix()}
	client := stripe.NewClient("sk_test", stripe.WithBackends(&stripe.Backends{
		API: backend, Connect: backend, Uploads: backend, MeterEvents: backend,
	}))
	provider := &StripeProvider{client: client, webhookSecret: "whsec_test"}
	ctx := context.Background()
	customerID, err := provider.CreateCustomer(ctx, CustomerInput{Email: "owner@test.dev", Name: "Acme", TenantID: "tenant"})
	if err != nil || customerID != "cus_test" {
		t.Fatalf("CreateCustomer() = %q, %v", customerID, err)
	}
	checkoutURL, err := provider.CreateCheckout(ctx, CheckoutInput{
		CustomerID: customerID, PriceID: "price_growth", TenantID: "tenant", TrialEnd: &trialEnd,
		SuccessURL: "https://app.test/success", CancelURL: "https://app.test/cancel",
	})
	if err != nil || checkoutURL != "https://checkout.stripe.test/session" {
		t.Fatalf("CreateCheckout() = %q, %v", checkoutURL, err)
	}
	portalURL, err := provider.CreatePortal(ctx, customerID, "https://app.test/billing")
	if err != nil || portalURL != "https://billing.stripe.test/session" {
		t.Fatalf("CreatePortal() = %q, %v", portalURL, err)
	}
	subscription, err := provider.RetrieveSubscription(ctx, "sub_test")
	if err != nil || subscription.ID != "sub_test" || subscription.CustomerID != "cus_test" ||
		subscription.PriceID != "price_growth" || subscription.TrialEndsAt == nil {
		t.Fatalf("RetrieveSubscription() = %#v, %v", subscription, err)
	}
	if err := provider.CancelSubscription(ctx, "sub_test"); err != nil {
		t.Fatalf("CancelSubscription() error = %v", err)
	}
	if len(backend.paths) != 5 {
		t.Fatalf("Stripe calls = %v", backend.paths)
	}
	if _, ok := NewStripeProvider("sk_test", "whsec_test").(*StripeProvider); !ok {
		t.Fatal("NewStripeProvider(configured) did not return Stripe provider")
	}

	backend.err = errors.New("Stripe unavailable")
	if _, err := provider.CreateCustomer(ctx, CustomerInput{}); err == nil {
		t.Fatal("CreateCustomer(error) error = nil")
	}
	if _, err := provider.CreateCheckout(ctx, CheckoutInput{}); err == nil {
		t.Fatal("CreateCheckout(error) error = nil")
	}
	if _, err := provider.CreatePortal(ctx, "cus", "return"); err == nil {
		t.Fatal("CreatePortal(error) error = nil")
	}
	if _, err := provider.RetrieveSubscription(ctx, "sub"); err == nil {
		t.Fatal("RetrieveSubscription(error) error = nil")
	}
	if err := provider.CancelSubscription(ctx, "sub"); err == nil {
		t.Fatal("CancelSubscription(error) error = nil")
	}
}

func TestPlatformHTTPRoutesAndErrors(t *testing.T) {
	trialEnd := time.Now().Add(TrialDuration)
	store := &serviceStoreStub{signup: SignupResult{Tenant: Tenant{
		ID: "tenant", Name: "Acme", Slug: "acme", Plan: tenant.PlanStarter,
		Status: tenant.StatusTrial, TrialEndsAt: &trialEnd,
	}, Token: "magic"}, tenant: Tenant{ID: "tenant", StripeCustomerID: "cus_1", StripeSubscriptionID: "sub_1"}}
	provider := &providerStub{subscription: Subscription{
		ID: "sub_1", CustomerID: "cus_1", PriceID: "price_starter", Status: "active",
	}}
	service := Service{
		Store: store, Provider: provider, Verifier: LocalSignupVerifier{}, Email: &emailCapture{},
		Prices:        PriceCatalog{tenant.PlanStarter: "price_starter"},
		PublicBaseURL: "https://api.fidibeki.com", BaseDomain: "fidibeki.com", AuthFrom: "auth@fidibeki.com",
	}
	router := chi.NewRouter()
	RegisterPlatformRoutes(router, HTTPOptions{Service: service})

	request := func(path, body string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	response := request("/v1/signup", `{"name":"Acme","slug":"acme","email":"owner@acme.test","plan":"starter","turnstile_token":"token"}`, nil)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"id":"tenant"`) {
		t.Fatalf("signup response = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "https://acme.fidibeki.com/login" {
		t.Fatalf("signup Location = %q", response.Header().Get("Location"))
	}
	if got := request("/v1/signup", `{`, nil); got.Code != http.StatusBadRequest {
		t.Fatalf("invalid signup status = %d", got.Code)
	}
	if got := request("/v1/billing/webhooks/stripe", `{"event":true}`, map[string]string{"Stripe-Signature": "signature"}); got.Code != http.StatusNoContent {
		t.Fatalf("webhook status = %d body = %s", got.Code, got.Body.String())
	}
	provider.verifyErr = ErrInvalidWebhook
	if got := request("/v1/billing/webhooks/stripe", `{}`, nil); got.Code != http.StatusBadRequest {
		t.Fatalf("invalid webhook status = %d body = %s", got.Code, got.Body.String())
	}
	if got := remoteIP("127.0.0.1:8080"); got != "127.0.0.1" {
		t.Fatalf("remoteIP() = %q", got)
	}
	if got := remoteIP("invalid"); got != "" {
		t.Fatalf("remoteIP(invalid) = %q", got)
	}

	for _, test := range []struct {
		err  error
		code int
	}{
		{ErrTurnstile, http.StatusUnprocessableEntity}, {ErrSlugConflict, http.StatusConflict},
		{ErrUnsupportedPlan, http.StatusUnprocessableEntity}, {ErrAlreadySubscribed, http.StatusConflict},
		{ErrConfirmation, http.StatusUnprocessableEntity}, {ErrDeletionNotPending, http.StatusNotFound},
		{ErrProviderUnavailable, http.StatusServiceUnavailable}, {errors.New("value is invalid"), http.StatusUnprocessableEntity},
		{errors.New("database down"), http.StatusServiceUnavailable},
	} {
		recorder := httptest.NewRecorder()
		writeBillingError(recorder, test.err)
		if recorder.Code != test.code {
			t.Errorf("writeBillingError(%v) status = %d, want %d", test.err, recorder.Code, test.code)
		}
	}
}

func TestServiceSignupCheckoutPortalAndWebhook(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	trialEnd := now.Add(10 * 24 * time.Hour)
	store := &serviceStoreStub{tenant: Tenant{
		ID: "tenant-id", Name: "Acme", Slug: "acme", Plan: tenant.PlanGrowth,
		Status: tenant.StatusTrial, BillingEmail: "owner@acme.test", TrialEndsAt: &trialEnd,
	}}
	provider := &providerStub{subscription: Subscription{
		ID: "sub_1", CustomerID: "cus_1", PriceID: "price_scale", Status: "active",
	}}
	emails := &emailCapture{}
	service := Service{
		Store: store, Provider: provider, Verifier: LocalSignupVerifier{}, Email: emails,
		Prices: PriceCatalog{
			tenant.PlanStarter: "price_starter", tenant.PlanGrowth: "price_growth", tenant.PlanScale: "price_scale",
		},
		PublicBaseURL: "https://api.fidibeki.com", BaseDomain: "fidibeki.com",
		AuthFrom: "Fidibeki <auth@fidibeki.com>", Now: func() time.Time { return now },
	}
	store.signup = SignupResult{Tenant: store.tenant, MemberID: "member-id", Token: "magic-token"}
	created, err := service.Signup(context.Background(), SignupInput{
		Name: " Acme ", Slug: " ACME ", Email: "OWNER@ACME.TEST", Plan: tenant.PlanGrowth,
	}, "token", "127.0.0.1", "request-id")
	if err != nil || created.ID != "tenant-id" {
		t.Fatalf("Signup() = %#v, %v", created, err)
	}
	if store.signupInput.Slug != "acme" || store.signupInput.Email != "owner@acme.test" {
		t.Fatalf("normalized signup = %#v", store.signupInput)
	}
	if len(emails.messages) != 1 || !strings.Contains(emails.messages[0].Text, "/auth/verify#token=magic-token") {
		t.Fatalf("signup email = %#v", emails.messages)
	}

	checkoutURL, err := service.Checkout(context.Background(), "tenant-id", tenant.PlanScale)
	if err != nil || checkoutURL != "https://checkout.stripe.test/session" {
		t.Fatalf("Checkout() = %q, %v", checkoutURL, err)
	}
	if provider.customerInput.TenantID != "tenant-id" || provider.checkoutInput.TrialEnd == nil ||
		provider.checkoutInput.PriceID != "price_scale" {
		t.Fatalf("provider inputs = customer %#v checkout %#v", provider.customerInput, provider.checkoutInput)
	}
	store.tenant.StripeCustomerID = "cus_1"
	portalURL, err := service.Portal(context.Background(), "tenant-id")
	if err != nil || portalURL != "https://billing.stripe.test/session" {
		t.Fatalf("Portal() = %q, %v", portalURL, err)
	}
	processed, err := service.HandleWebhook(context.Background(), []byte("payload"), "signature")
	if err != nil || !processed || store.appliedPlan != tenant.PlanScale {
		t.Fatalf("HandleWebhook() = %t, %v, plan %q", processed, err, store.appliedPlan)
	}
}

func TestServiceRejectsInvalidSignupAndConflicts(t *testing.T) {
	service := Service{Prices: PriceCatalog{}, Verifier: LocalSignupVerifier{}}
	for _, input := range []SignupInput{
		{Name: "", Slug: "valid", Email: "a@b.test", Plan: tenant.PlanStarter},
		{Name: "Acme", Slug: "valid", Email: "bad", Plan: tenant.PlanStarter},
		{Name: "Acme", Slug: "valid", Email: "a@b.test", Plan: tenant.PlanEnterprise},
	} {
		if _, err := service.Signup(context.Background(), input, "", "", ""); err == nil {
			t.Fatalf("Signup(%#v) error = nil", input)
		}
	}
	trialEnd := time.Now().Add(10 * 24 * time.Hour)
	store := &serviceStoreStub{tenant: Tenant{
		ID: "tenant", Plan: tenant.PlanGrowth, StripeSubscriptionID: "sub", TrialEndsAt: &trialEnd,
	}}
	service.Store = store
	service.Provider = &providerStub{}
	service.Prices = PriceCatalog{tenant.PlanGrowth: "price"}
	if _, err := service.Checkout(context.Background(), "tenant", tenant.PlanGrowth); !errors.Is(err, ErrAlreadySubscribed) {
		t.Fatalf("Checkout(existing) error = %v", err)
	}
	store.tenant.StripeSubscriptionID = ""
	requested := time.Now()
	store.tenant.DeletionRequestedAt = &requested
	if _, err := service.Checkout(context.Background(), "tenant", tenant.PlanGrowth); !errors.Is(err, ErrDeletionPending) {
		t.Fatalf("Checkout(deleting) error = %v", err)
	}
}

func TestServiceErrorPaths(t *testing.T) {
	ctx := context.Background()
	valid := SignupInput{Name: "Acme", Slug: "acme", Email: "owner@acme.test", Plan: tenant.PlanStarter}
	service := Service{Prices: PriceCatalog{tenant.PlanStarter: "price_starter"}}
	if _, err := service.Signup(ctx, valid, "", "", ""); !errors.Is(err, ErrTurnstile) {
		t.Fatalf("Signup(no verifier) error = %v", err)
	}
	store := &serviceStoreStub{tenant: Tenant{ID: "tenant", Slug: "acme", BillingEmail: "owner@acme.test"}}
	provider := &providerStub{}
	emails := &emailCapture{}
	service.Store, service.Provider, service.Email = store, provider, emails
	service.Verifier = LocalSignupVerifier{}
	service.PublicBaseURL, service.BaseDomain, service.AuthFrom = "https://api.fidibeki.com", "fidibeki.com", "auth@fidibeki.com"
	store.signupErr = errors.New("database down")
	if _, err := service.Signup(ctx, valid, "", "", ""); err == nil {
		t.Fatal("Signup(store error) error = nil")
	}
	store.signupErr = nil
	store.signup = SignupResult{Tenant: store.tenant, Token: "token"}
	emails.err = errors.New("Redis down")
	if _, err := service.Signup(ctx, valid, "", "", ""); err == nil {
		t.Fatal("Signup(email error) error = nil")
	}
	emails.err = nil

	if _, err := service.Checkout(ctx, "tenant", tenant.PlanEnterprise); !errors.Is(err, ErrUnsupportedPlan) {
		t.Fatalf("Checkout(enterprise) error = %v", err)
	}
	store.getErr = errors.New("database down")
	if _, err := service.Checkout(ctx, "tenant", tenant.PlanStarter); err == nil {
		t.Fatal("Checkout(store error) error = nil")
	}
	store.getErr = nil
	provider.err = errors.New("Stripe down")
	if _, err := service.Checkout(ctx, "tenant", tenant.PlanStarter); err == nil {
		t.Fatal("Checkout(provider error) error = nil")
	}
	if _, err := service.Portal(ctx, "tenant"); err == nil {
		t.Fatal("Portal(provider error) error = nil")
	}
	provider.err = nil

	provider.signal = EventSignal{ID: "evt_ignored", Type: "product.created"}
	if processed, err := service.HandleWebhook(ctx, nil, ""); err != nil || processed {
		t.Fatalf("HandleWebhook(ignored) = %t, %v", processed, err)
	}
	provider.signal = EventSignal{ID: "evt", Type: "invoice.paid", CustomerID: "cus"}
	store.tenant.StripeSubscriptionID = ""
	if _, err := service.HandleWebhook(ctx, nil, ""); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("HandleWebhook(no subscription) error = %v", err)
	}
	store.tenant.StripeSubscriptionID = "sub"
	provider.subscription = Subscription{ID: "sub", CustomerID: "cus", PriceID: "unknown"}
	if _, err := service.HandleWebhook(ctx, nil, ""); !errors.Is(err, ErrUnsupportedPlan) {
		t.Fatalf("HandleWebhook(unknown price) error = %v", err)
	}
	provider.subscription.PriceID = "price_starter"
	store.applyErr = errors.New("database down")
	if _, err := service.HandleWebhook(ctx, nil, ""); err == nil {
		t.Fatal("HandleWebhook(apply error) error = nil")
	}

	store.requestErr = errors.New("database down")
	if _, err := service.RequestDeletion(ctx, "tenant", "member", "acme"); err == nil {
		t.Fatal("RequestDeletion(store error) error = nil")
	}
	store.requestErr = nil
	requested := time.Now()
	store.deletion = Tenant{ID: "tenant", Name: "Acme", DeletionRequestedAt: &requested}
	store.emailsErr = errors.New("database down")
	if _, err := service.RequestDeletion(ctx, "tenant", "member", "acme"); err == nil {
		t.Fatal("RequestDeletion(member error) error = nil")
	}
	store.emailsErr = nil
	store.emails = []string{"owner@acme.test"}
	emails.err = errors.New("Redis down")
	if _, err := service.RequestDeletion(ctx, "tenant", "member", "acme"); err == nil {
		t.Fatal("RequestDeletion(email error) error = nil")
	}
}

func TestDeletionAndDunning(t *testing.T) {
	ctx := context.Background()
	current := Tenant{ID: "tenant", StripeSubscriptionID: "sub_1"}
	store := &deletionStoreStub{tenant: current, due: []Tenant{current}, unverified: []Tenant{{ID: "unverified"}}}
	provider := &providerStub{}
	storage := &prefixCapture{}
	executor := DeletionExecutor{Store: store, Provider: provider, Storage: storage}
	if err := executor.Execute(ctx); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if provider.cancelled != "sub_1" || store.deleted != "tenant" || store.purged != "unverified" || len(storage.prefixes) != 2 {
		t.Fatalf("execution state provider=%q store=%#v prefixes=%v", provider.cancelled, store, storage.prefixes)
	}
	if err := executor.ForceDelete(ctx, "tenant", "admin"); err != nil || store.forcedBy != "admin" {
		t.Fatalf("ForceDelete() error = %v, forced by %q", err, store.forcedBy)
	}

	now := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	inThreeDays := now.Add(72 * time.Hour)
	expired := now.Add(-time.Minute)
	dunningStore := &dunningStoreStub{trials: []Tenant{
		{ID: "soon", Name: "Soon", BillingEmail: "billing@soon.test", TrialEndsAt: &inThreeDays},
		{ID: "expired", Name: "Expired", TrialEndsAt: &expired},
	}, emails: []string{"owner@soon.test"}}
	capture := &emailCapture{}
	dunning := Dunning{Store: dunningStore, Email: capture, AuthFrom: "auth@fidibeki.com", Now: func() time.Time { return now }}
	if err := dunning.Execute(ctx); err != nil {
		t.Fatalf("Dunning.Execute() error = %v", err)
	}
	if len(capture.messages) != 3 || dunningStore.suspended != "expired" {
		t.Fatalf("messages=%d suspended=%q", len(capture.messages), dunningStore.suspended)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := executor.Run(cancelled, time.Hour); err != nil {
		t.Fatalf("DeletionExecutor.Run() error = %v", err)
	}
	if err := dunning.Run(cancelled, time.Hour); err != nil {
		t.Fatalf("Dunning.Run() error = %v", err)
	}
	unavailable := UnavailableProvider{}
	if _, err := unavailable.CreateCustomer(ctx, CustomerInput{}); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("Unavailable CreateCustomer error = %v", err)
	}
	_, _ = unavailable.CreateCheckout(ctx, CheckoutInput{})
	_, _ = unavailable.CreatePortal(ctx, "", "")
	_, _ = unavailable.RetrieveSubscription(ctx, "")
	_ = unavailable.CancelSubscription(ctx, "")
	_, _ = unavailable.VerifyEvent(nil, "")
}

func TestMaintenanceFailurePaths(t *testing.T) {
	ctx := context.Background()
	store := &deletionStoreStub{dueErr: errors.New("database down")}
	executor := DeletionExecutor{Store: store, Provider: &providerStub{}, Storage: &prefixCapture{}}
	if err := executor.Execute(ctx); err == nil {
		t.Fatal("Execute(due error) error = nil")
	}
	store.dueErr = nil
	store.unverifiedErr = errors.New("database down")
	if err := executor.Execute(ctx); err == nil {
		t.Fatal("Execute(unverified error) error = nil")
	}
	store.unverifiedErr = nil
	store.due = []Tenant{{ID: "tenant", StripeSubscriptionID: "sub"}}
	executor.Provider = &providerStub{err: errors.New("Stripe down")}
	if err := executor.Execute(ctx); err == nil {
		t.Fatal("Execute(cancel error) error = nil")
	}
	store.due = nil
	store.unverified = []Tenant{{ID: "unverified"}}
	executor.Provider = &providerStub{}
	executor.Storage = &prefixCapture{err: errors.New("S3 down")}
	if err := executor.Execute(ctx); err == nil {
		t.Fatal("Execute(storage error) error = nil")
	}

	dunningStore := &dunningStoreStub{trialsErr: errors.New("database down")}
	dunning := Dunning{Store: dunningStore, Email: &emailCapture{}, AuthFrom: "auth@fidibeki.com"}
	if err := dunning.Execute(ctx); err == nil {
		t.Fatal("Dunning(trials error) error = nil")
	}
	trialEnd := time.Now().Add(24 * time.Hour)
	dunningStore.trialsErr = nil
	dunningStore.trials = []Tenant{{ID: "tenant", TrialEndsAt: &trialEnd}}
	dunningStore.emailsErr = errors.New("database down")
	if err := dunning.Execute(ctx); err == nil {
		t.Fatal("Dunning(member error) error = nil")
	}
	dunningStore.emailsErr = nil
	dunningStore.emails = []string{"owner@tenant.test"}
	dunning.Email = &emailCapture{err: errors.New("Redis down")}
	if err := dunning.Execute(ctx); err == nil {
		t.Fatal("Dunning(email error) error = nil")
	}
}

type serviceStoreStub struct {
	tenant      Tenant
	signup      SignupResult
	signupInput SignupInput
	appliedPlan tenant.Plan
	deletion    Tenant
	emails      []string
	signupErr   error
	getErr      error
	applyErr    error
	requestErr  error
	emailsErr   error
}

func (s *serviceStoreStub) Signup(_ context.Context, input SignupInput) (SignupResult, error) {
	s.signupInput = input
	return s.signup, s.signupErr
}
func (s *serviceStoreStub) Get(context.Context, string) (Tenant, error) { return s.tenant, s.getErr }
func (s *serviceStoreStub) SaveCustomer(_ context.Context, _, customer string) (string, error) {
	s.tenant.StripeCustomerID = customer
	return customer, nil
}
func (s *serviceStoreStub) FindByStripeReference(context.Context, string, string) (Tenant, error) {
	return s.tenant, nil
}
func (s *serviceStoreStub) ApplyEvent(_ context.Context, _ EventSignal, _ Subscription, plan tenant.Plan) (bool, error) {
	s.appliedPlan = plan
	return true, s.applyErr
}
func (s *serviceStoreStub) RequestDeletion(context.Context, string, string, string) (Tenant, error) {
	return s.deletion, s.requestErr
}
func (s *serviceStoreStub) CancelDeletion(context.Context, string) error { return nil }
func (s *serviceStoreStub) MemberEmails(context.Context, string) ([]string, error) {
	return s.emails, s.emailsErr
}

type providerStub struct {
	customerInput CustomerInput
	checkoutInput CheckoutInput
	subscription  Subscription
	cancelled     string
	verifyErr     error
	err           error
	signal        EventSignal
}

func (p *providerStub) CreateCustomer(_ context.Context, input CustomerInput) (string, error) {
	p.customerInput = input
	return "cus_1", p.err
}
func (p *providerStub) CreateCheckout(_ context.Context, input CheckoutInput) (string, error) {
	p.checkoutInput = input
	return "https://checkout.stripe.test/session", p.err
}
func (p *providerStub) CreatePortal(context.Context, string, string) (string, error) {
	return "https://billing.stripe.test/session", p.err
}
func (p *providerStub) RetrieveSubscription(context.Context, string) (Subscription, error) {
	return p.subscription, p.err
}
func (p *providerStub) CancelSubscription(_ context.Context, id string) error {
	p.cancelled = id
	return p.err
}
func (p *providerStub) VerifyEvent([]byte, string) (EventSignal, error) {
	if p.verifyErr != nil {
		return EventSignal{}, p.verifyErr
	}
	if p.signal.ID != "" {
		return p.signal, nil
	}
	return EventSignal{ID: "evt_1", Type: "customer.subscription.updated", CustomerID: "cus_1", SubscriptionID: "sub_1"}, nil
}

type stripeBackendStub struct {
	paths    []string
	trialEnd int64
	err      error
}

func (b *stripeBackendStub) Call(_ string, path, _ string, _ stripe.ParamsContainer, value stripe.LastResponseSetter) error {
	b.paths = append(b.paths, path)
	if b.err != nil {
		return b.err
	}
	switch target := value.(type) {
	case *stripe.Customer:
		target.ID = "cus_test"
	case *stripe.CheckoutSession:
		target.ID = "cs_test"
		target.URL = "https://checkout.stripe.test/session"
	case *stripe.BillingPortalSession:
		target.ID = "bps_test"
		target.URL = "https://billing.stripe.test/session"
	case *stripe.Subscription:
		target.ID = "sub_test"
		target.Customer = &stripe.Customer{ID: "cus_test"}
		target.Items = &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Price: &stripe.Price{ID: "price_growth"}}}}
		target.Status = stripe.SubscriptionStatusTrialing
		target.TrialEnd = b.trialEnd
	}
	return nil
}

func (*stripeBackendStub) CallStreaming(string, string, string, stripe.ParamsContainer, stripe.StreamingLastResponseSetter) error {
	return errors.New("not implemented")
}
func (*stripeBackendStub) CallRaw(string, string, string, []byte, *stripe.Params, stripe.LastResponseSetter) error {
	return errors.New("not implemented")
}
func (*stripeBackendStub) CallMultipart(string, string, string, string, *bytes.Buffer, *stripe.Params, stripe.LastResponseSetter) error {
	return errors.New("not implemented")
}
func (*stripeBackendStub) SetMaxNetworkRetries(int64) {}

type emailCapture struct {
	messages []email.Message
	err      error
}

func (c *emailCapture) Enqueue(_ context.Context, message email.Message) error {
	c.messages = append(c.messages, message)
	return c.err
}

type prefixCapture struct {
	prefixes []string
	err      error
}

func (c *prefixCapture) RemovePrefix(_ context.Context, prefix string) error {
	c.prefixes = append(c.prefixes, prefix)
	return c.err
}

type deletionStoreStub struct {
	tenant                    Tenant
	due, unverified           []Tenant
	deleted, purged, forcedBy string
	dueErr, unverifiedErr     error
}

func (s *deletionStoreStub) Get(context.Context, string) (Tenant, error) { return s.tenant, nil }
func (s *deletionStoreStub) DueDeletions(context.Context) ([]Tenant, error) {
	return s.due, s.dueErr
}
func (s *deletionStoreStub) UnverifiedTenants(context.Context) ([]Tenant, error) {
	return s.unverified, s.unverifiedErr
}
func (s *deletionStoreStub) PurgeUnverified(_ context.Context, id string) error {
	s.purged = id
	return nil
}
func (s *deletionStoreStub) DeleteTenant(_ context.Context, current Tenant, forcedBy string) error {
	s.deleted, s.forcedBy = current.ID, forcedBy
	return nil
}

type dunningStoreStub struct {
	trials    []Tenant
	emails    []string
	suspended string
	trialsErr error
	emailsErr error
}

func (s *dunningStoreStub) TrialsForDunning(context.Context) ([]Tenant, error) {
	return s.trials, s.trialsErr
}
func (s *dunningStoreStub) SuspendExpiredUnpaid(_ context.Context, id string) error {
	s.suspended = id
	return nil
}
func (s *dunningStoreStub) MemberEmails(context.Context, string) ([]string, error) {
	return s.emails, s.emailsErr
}
