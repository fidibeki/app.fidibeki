package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStoreBillingLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := billingTestDatabase(t, ctx)
	now := time.Now().UTC().Truncate(time.Second)
	store := Store{Database: pool, Now: func() time.Time { return now }}

	signup, err := store.Signup(ctx, SignupInput{
		Name: "Acme", Slug: "acme-billing", Email: "owner@acme.test", Plan: tenant.PlanScale,
	})
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	if signup.Tenant.Status != tenant.StatusTrial || signup.Tenant.TrialEndsAt == nil ||
		!signup.Tenant.TrialEndsAt.Equal(now.Add(TrialDuration)) {
		t.Fatalf("signup tenant = %#v", signup.Tenant)
	}
	var permissions []string
	if err := pool.QueryRow(ctx, `SELECT permissions FROM tenant_members WHERE id = $1`, signup.MemberID).Scan(&permissions); err != nil {
		t.Fatalf("load signup owner: %v", err)
	}
	if len(permissions) != len(auth.AllTenantPermissions) {
		t.Fatalf("owner permission count = %d, want %d", len(permissions), len(auth.AllTenantPermissions))
	}
	var links int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM tenant_magic_links
		WHERE tenant_id = $1 AND token_hash = $2
	`, signup.Tenant.ID, auth.HashToken(signup.Token)).Scan(&links); err != nil || links != 1 {
		t.Fatalf("magic link count = %d, error = %v", links, err)
	}
	if _, err := store.Signup(ctx, SignupInput{
		Name: "Duplicate", Slug: "acme-billing", Email: "other@acme.test", Plan: tenant.PlanStarter,
	}); !errors.Is(err, ErrSlugConflict) {
		t.Fatalf("Signup(duplicate) error = %v", err)
	}

	loaded, err := store.Get(ctx, signup.Tenant.ID)
	if err != nil || loaded.Name != "Acme" {
		t.Fatalf("Get() = %#v, %v", loaded, err)
	}
	customerID, err := store.SaveCustomer(ctx, loaded.ID, "cus_1")
	if err != nil || customerID != "cus_1" {
		t.Fatalf("SaveCustomer() = %q, %v", customerID, err)
	}
	if existing, err := store.SaveCustomer(ctx, loaded.ID, "cus_orphan"); err != nil || existing != "cus_1" {
		t.Fatalf("SaveCustomer(existing) = %q, %v", existing, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tenants SET billing_email = 'billing@acme.test' WHERE id = $1`, loaded.ID); err != nil {
		t.Fatalf("set distinct billing contact: %v", err)
	}
	testTenantBillingHTTP(t, pool, store, loaded, signup.MemberID, now)

	trialEnd := now.Add(7 * 24 * time.Hour)
	subscription := Subscription{
		ID: "sub_1", CustomerID: "cus_1", PriceID: "price_growth", Status: "trialing", TrialEndsAt: &trialEnd,
	}
	processed, err := store.ApplyEvent(ctx, EventSignal{ID: "evt_updated", Type: "customer.subscription.updated"}, subscription, tenant.PlanGrowth)
	if err != nil || !processed {
		t.Fatalf("ApplyEvent(updated) = %t, %v", processed, err)
	}
	loaded, _ = store.Get(ctx, loaded.ID)
	if loaded.Plan != tenant.PlanScale || loaded.PendingPlan == nil || *loaded.PendingPlan != tenant.PlanGrowth ||
		loaded.StripeSubscriptionID != "sub_1" {
		t.Fatalf("tenant after downgrade = %#v", loaded)
	}
	if duplicate, err := store.ApplyEvent(ctx, EventSignal{ID: "evt_updated", Type: "customer.subscription.updated"}, subscription, tenant.PlanGrowth); err != nil || duplicate {
		t.Fatalf("ApplyEvent(duplicate) = %t, %v", duplicate, err)
	}
	subscription.Status = "active"
	processed, err = store.ApplyEvent(ctx, EventSignal{ID: "evt_paid", Type: "invoice.paid"}, subscription, tenant.PlanGrowth)
	if err != nil || !processed {
		t.Fatalf("ApplyEvent(paid) = %t, %v", processed, err)
	}
	loaded, _ = store.Get(ctx, loaded.ID)
	if loaded.Plan != tenant.PlanGrowth || loaded.PendingPlan != nil || loaded.Status != tenant.StatusActive {
		t.Fatalf("tenant after paid invoice = %#v", loaded)
	}
	var billingEventCount int
	var paidEventID string
	var paidEventData []byte
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE tenant_id = $1 AND event = 'billing.state_changed'
	`, loaded.ID).Scan(&billingEventCount); err != nil || billingEventCount != 2 {
		t.Fatalf("billing lifecycle event count = %d error = %v", billingEventCount, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT id, data FROM outbox_events
		WHERE tenant_id = $1 AND event = 'billing.state_changed' AND event_key = 'evt_paid'
	`, loaded.ID).Scan(&paidEventID, &paidEventData); err != nil ||
		!bytes.Contains(paidEventData, []byte(`"status": "active"`)) {
		t.Fatalf("billing lifecycle events = %d data = %s error = %v", billingEventCount, paidEventData, err)
	}
	billingSender := &billingLifecycleSender{}
	billingHandler := email.LifecycleHandler{
		Database: pool, Sender: billingSender,
		PublicBaseURL: "https://api.fidibeki.com", BaseDomain: "fidibeki.com",
		AuthFrom:       "Fidibeki <auth@fidibeki.com>",
		WelcomeFrom:    "Acidiney from Fidibeki <acidiney.dias@fidibeki.com>",
		WelcomeReplyTo: "acidiney.dias@fidibeki.com", Now: func() time.Time { return now },
	}.Handler()
	billingPayload, _ := json.Marshal(map[string]string{
		"outbox_event_id": paidEventID, "tenant_id": loaded.ID, "resource_id": loaded.ID,
	})
	if err := billingHandler(ctx, asynq.NewTask(email.TaskLifecycleEmail, billingPayload)); err != nil {
		t.Fatalf("send billing lifecycle email: %v", err)
	}
	if len(billingSender.messages) != 2 ||
		!lifecycleRecipientsAre(billingSender.messages, "billing@acme.test", "owner@acme.test") ||
		billingSender.messages[0].Subject != "Fidibeki subscription updated" ||
		!strings.Contains(billingSender.messages[0].Text, "https://acme-billing.fidibeki.com/settings/billing") {
		t.Fatalf("billing lifecycle messages = %#v", billingSender.messages)
	}
	found, err := store.FindByStripeReference(ctx, "cus_1", "")
	if err != nil || found.ID != loaded.ID {
		t.Fatalf("FindByStripeReference() = %#v, %v", found, err)
	}

	if _, err := store.RequestDeletion(ctx, loaded.ID, signup.MemberID, "wrong"); !errors.Is(err, ErrConfirmation) {
		t.Fatalf("RequestDeletion(wrong) error = %v", err)
	}
	deleting, err := store.RequestDeletion(ctx, loaded.ID, signup.MemberID, loaded.Slug)
	if err != nil || deleting.DeletionRequestedAt == nil {
		t.Fatalf("RequestDeletion() = %#v, %v", deleting, err)
	}
	if _, err := store.RequestDeletion(ctx, loaded.ID, signup.MemberID, loaded.Slug); !errors.Is(err, ErrDeletionPending) {
		t.Fatalf("RequestDeletion(duplicate) error = %v", err)
	}
	emails, err := store.MemberEmails(ctx, loaded.ID)
	if err != nil || len(emails) != 1 || emails[0] != "owner@acme.test" {
		t.Fatalf("MemberEmails() = %v, %v", emails, err)
	}
	if err := store.CancelDeletion(ctx, loaded.ID, signup.MemberID); err != nil {
		t.Fatalf("CancelDeletion() error = %v", err)
	}
	var cancellationEvents int
	var cancellationEventID string
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE tenant_id = $1 AND event = 'tenant.deletion_cancelled'
	`, loaded.ID).Scan(&cancellationEvents); err != nil || cancellationEvents != 2 {
		t.Fatalf("deletion cancellation events = %d error = %v", cancellationEvents, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT id FROM outbox_events
		WHERE tenant_id = $1 AND event = 'tenant.deletion_cancelled'
		ORDER BY created_at DESC, id DESC LIMIT 1
	`, loaded.ID).Scan(&cancellationEventID); err != nil {
		t.Fatalf("load deletion cancellation event: %v", err)
	}
	cancellationPayload, _ := json.Marshal(map[string]string{
		"outbox_event_id": cancellationEventID, "tenant_id": loaded.ID, "resource_id": loaded.ID,
	})
	cancellationSender := &billingLifecycleSender{}
	cancellationHandler := email.LifecycleHandler{
		Database: pool, Sender: cancellationSender,
		PublicBaseURL: "https://api.fidibeki.com", BaseDomain: "fidibeki.com",
		AuthFrom:       "Fidibeki <auth@fidibeki.com>",
		WelcomeFrom:    "Acidiney from Fidibeki <acidiney.dias@fidibeki.com>",
		WelcomeReplyTo: "acidiney.dias@fidibeki.com", Now: func() time.Time { return now },
	}.Handler()
	if err := cancellationHandler(ctx, asynq.NewTask(email.TaskLifecycleEmail, cancellationPayload)); err != nil {
		t.Fatalf("send deletion cancellation email: %v", err)
	}
	if len(cancellationSender.messages) != 2 ||
		!strings.Contains(cancellationSender.messages[0].Text, "Cancelled by: owner@acme.test") ||
		!strings.Contains(cancellationSender.messages[0].Text, "Original request:") {
		t.Fatalf("deletion cancellation messages = %#v", cancellationSender.messages)
	}
	if err := store.CancelDeletion(ctx, loaded.ID, signup.MemberID); !errors.Is(err, ErrDeletionNotPending) {
		t.Fatalf("CancelDeletion(missing) error = %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE tenants SET trial_ends_at = $2, status = 'trial', stripe_subscription_id = NULL WHERE id = $1`, loaded.ID, now.Add(3*24*time.Hour)); err != nil {
		t.Fatalf("prepare trial dunning: %v", err)
	}
	trials, err := store.TrialsForDunning(ctx)
	if err != nil || len(trials) != 1 {
		t.Fatalf("TrialsForDunning() = %d, %v", len(trials), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tenants SET trial_ends_at = $2 WHERE id = $1`, loaded.ID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("expire trial: %v", err)
	}
	if err := store.SuspendExpiredUnpaid(ctx, loaded.ID); err != nil {
		t.Fatalf("SuspendExpiredUnpaid() error = %v", err)
	}
	loaded, _ = store.Get(ctx, loaded.ID)
	if loaded.Status != tenant.StatusSuspended {
		t.Fatalf("status after trial expiry = %q", loaded.Status)
	}

	if _, err := pool.Exec(ctx, `UPDATE tenants SET deletion_requested_at = $2, deletion_requested_by_member_id = $3 WHERE id = $1`, loaded.ID, now.Add(-DeletionGrace), signup.MemberID); err != nil {
		t.Fatalf("prepare due deletion: %v", err)
	}
	due, err := store.DueDeletions(ctx)
	if err != nil || len(due) != 1 {
		t.Fatalf("DueDeletions() = %d, %v", len(due), err)
	}
	if err := store.DeleteTenant(ctx, due[0], ""); err != nil {
		t.Fatalf("DeleteTenant() error = %v", err)
	}
	var auditSlug string
	if err := pool.QueryRow(ctx, `SELECT tenant_slug FROM tenant_deletions WHERE tenant_id = $1`, loaded.ID).Scan(&auditSlug); err != nil || auditSlug != loaded.Slug {
		t.Fatalf("deletion audit slug = %q, error = %v", auditSlug, err)
	}
	if _, err := store.Get(ctx, loaded.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(deleted) error = %v", err)
	}

	unverified, err := store.Signup(ctx, SignupInput{
		Name: "Unverified", Slug: "unverified-company", Email: "new@company.test", Plan: tenant.PlanStarter,
	})
	if err != nil {
		t.Fatalf("Signup(unverified) error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tenants SET created_at = $2 WHERE id = $1`, unverified.Tenant.ID, now.Add(-25*time.Hour)); err != nil {
		t.Fatalf("age unverified tenant: %v", err)
	}
	unverifiedTenants, err := store.UnverifiedTenants(ctx)
	if err != nil || len(unverifiedTenants) != 1 {
		t.Fatalf("UnverifiedTenants() = %d, %v", len(unverifiedTenants), err)
	}
	if err := store.PurgeUnverified(ctx, unverified.Tenant.ID); err != nil {
		t.Fatalf("PurgeUnverified() error = %v", err)
	}
}

func testTenantBillingHTTP(t *testing.T, pool *pgxpool.Pool, store Store, current Tenant, memberID string, now time.Time) {
	t.Helper()
	jwt, err := auth.NewJWTManager(strings.Repeat("x", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create billing JWT: %v", err)
	}
	token, _, err := jwt.IssueTenant(memberID, current.ID, []string{
		string(auth.PermissionBillingRead), string(auth.PermissionBillingManage),
	})
	if err != nil {
		t.Fatalf("issue billing JWT: %v", err)
	}
	provider := &providerStub{}
	emails := &emailCapture{}
	service := Service{
		Store: store, Provider: provider, Email: emails,
		Prices: PriceCatalog{
			tenant.PlanStarter: "price_starter", tenant.PlanGrowth: "price_growth", tenant.PlanScale: "price_scale",
		},
		PublicBaseURL: "http://api.localhost:8088", BaseDomain: "localhost", AuthFrom: "auth@fidibeki.com",
		Now: func() time.Time { return now },
	}
	router := chi.NewRouter()
	RegisterTenantRoutes(router, HTTPOptions{Service: service, JWT: jwt})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pool},
		Begin: db.BeginFromPool(pool), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(router)

	requestWithToken := func(method, path, body, accessToken string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://"+current.Slug+".localhost"+path, bytes.NewBufferString(body))
		req.Host = current.Slug + ".localhost"
		if accessToken != "" {
			req.Header.Set("Authorization", "Bearer "+accessToken)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	request := func(method, path, body string, authenticate bool) *httptest.ResponseRecorder {
		if !authenticate {
			return requestWithToken(method, path, body, "")
		}
		return requestWithToken(method, path, body, token)
	}
	if got := request(http.MethodGet, "/v1/billing", "", false); got.Code != http.StatusUnauthorized {
		t.Fatalf("GET billing without auth status = %d", got.Code)
	}
	if got := request(http.MethodGet, "/v1/billing", "", true); got.Code != http.StatusOK {
		t.Fatalf("GET billing status = %d body = %s", got.Code, got.Body.String())
	}
	if got := request(http.MethodPost, "/v1/billing/checkout", `{"plan":"growth"}`, true); got.Code != http.StatusOK {
		t.Fatalf("POST checkout status = %d body = %s", got.Code, got.Body.String())
	}
	if provider.checkoutInput.PriceID != "price_growth" || provider.checkoutInput.CustomerID != "cus_1" {
		t.Fatalf("checkout input = %#v", provider.checkoutInput)
	}
	if got := request(http.MethodPost, "/v1/billing/portal", "", true); got.Code != http.StatusOK {
		t.Fatalf("POST portal status = %d body = %s", got.Code, got.Body.String())
	}
	if got := request(http.MethodPost, "/v1/billing/delete-account", `{"confirmation":"wrong"}`, true); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST delete wrong status = %d body = %s", got.Code, got.Body.String())
	}
	if got := request(http.MethodPost, "/v1/billing/delete-account", `{"confirmation":"`+current.Slug+`"}`, true); got.Code != http.StatusAccepted {
		t.Fatalf("POST delete status = %d body = %s", got.Code, got.Body.String())
	}
	if len(emails.messages) == 0 {
		t.Fatal("delete account did not enqueue member email")
	}
	pending, err := store.Get(context.Background(), current.ID)
	if err != nil || pending.DeletionRequestedAt == nil {
		t.Fatalf("load pending deletion = %#v, %v", pending, err)
	}
	providerCalls := [3]int{provider.createCustomerCalls, provider.createCheckoutCalls, provider.createPortalCalls}
	emailCount := len(emails.messages)
	for _, blocked := range []struct {
		path string
		body string
	}{
		{path: "/v1/billing/checkout", body: `{"plan":"scale"}`},
		{path: "/v1/billing/portal"},
		{path: "/v1/billing/delete-account", body: `{"confirmation":"` + current.Slug + `"}`},
	} {
		if got := request(http.MethodPost, blocked.path, blocked.body, true); got.Code != http.StatusForbidden {
			t.Fatalf("POST %s during deletion grace status = %d body = %s", blocked.path, got.Code, got.Body.String())
		}
	}
	afterBlocked, err := store.Get(context.Background(), current.ID)
	if err != nil || afterBlocked.DeletionRequestedAt == nil ||
		!afterBlocked.DeletionRequestedAt.Equal(*pending.DeletionRequestedAt) {
		t.Fatalf("tenant changed after blocked routes = %#v, %v", afterBlocked, err)
	}
	if got := [3]int{provider.createCustomerCalls, provider.createCheckoutCalls, provider.createPortalCalls}; got != providerCalls {
		t.Fatalf("provider calls after blocked routes = %v, want %v", got, providerCalls)
	}
	if len(emails.messages) != emailCount {
		t.Fatalf("email count after blocked routes = %d, want %d", len(emails.messages), emailCount)
	}

	manageOnlyToken, _, err := jwt.IssueTenant(memberID, current.ID, []string{string(auth.PermissionBillingManage)})
	if err != nil {
		t.Fatalf("issue manage-only billing JWT: %v", err)
	}
	if got := requestWithToken(http.MethodGet, "/v1/billing", "", manageOnlyToken); got.Code != http.StatusOK {
		t.Fatalf("GET billing with manage-only token status = %d body = %s", got.Code, got.Body.String())
	}
	if got := requestWithToken(http.MethodPost, "/v1/billing/cancel-deletion", "", manageOnlyToken); got.Code != http.StatusNoContent {
		t.Fatalf("POST cancel deletion status = %d body = %s", got.Code, got.Body.String())
	}
}

func billingTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, billingMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate billing database: %v", err)
	}
	pool, err := pgxpool.New(ctx, directURL)
	if err != nil {
		t.Fatalf("open billing database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func billingMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	return path
}

type billingLifecycleSender struct {
	messages []email.Message
}

func (s *billingLifecycleSender) Send(_ context.Context, message email.Message) (string, error) {
	s.messages = append(s.messages, message)
	return "email-id", nil
}

func lifecycleRecipientsAre(messages []email.Message, expected ...string) bool {
	seen := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		seen[message.To] = struct{}{}
	}
	if len(seen) != len(expected) {
		return false
	}
	for _, address := range expected {
		if _, ok := seen[address]; !ok {
			return false
		}
	}
	return true
}
