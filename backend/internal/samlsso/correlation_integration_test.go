package samlsso

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

func TestSAMLCorrelationKeepsAnIssuedRequestValidBeyondTheNewestFifty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newSAMLHarness(ctx, t)
	workspace := harness.workspace(ctx, t, "correlation", "Correlation")
	harness.configure(ctx, t, workspace)
	harness.withTenant(ctx, t, workspace, func(query db.Querier) {
		if _, err := harness.service.SetMode(
			ctx, query, workspace.resolved, auth.SSOModeOptional, workspace.ownerID,
		); err != nil {
			t.Fatalf("SetMode(optional) error = %v", err)
		}
	})

	var victimRequestID string
	harness.withTenant(ctx, t, workspace, func(query db.Querier) {
		victimRequestID = harness.startLogin(ctx, t, query, workspace, PurposeLogin)
	})
	harness.withTenant(ctx, t, workspace, func(query db.Querier) {
		for range 51 {
			if _, err := harness.service.StartLogin(
				ctx, query, workspace.resolved, PurposeLogin, "", "",
			); err != nil {
				t.Fatalf("attacker StartLogin() error = %v", err)
			}
		}
	})
	harness.withTenant(ctx, t, workspace, func(query db.Querier) {
		response := workspace.idp.response(
			t, harness.assertion(t, workspace, victimRequestID, "owner@acme.test"),
		)
		outcome, err := harness.service.ConsumeAssertion(
			ctx, query, workspace.resolved, response,
		)
		if err != nil {
			t.Fatalf("victim ConsumeAssertion() error = %v", err)
		}
		if outcome.MemberID != workspace.ownerID {
			t.Fatalf("MemberID = %q, want %q", outcome.MemberID, workspace.ownerID)
		}
	})
}

func TestSAMLAuthRequestCapacityIsAtomicAndNonEvicting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newSAMLHarness(ctx, t)

	t.Run("exact lookup isolates tenant expiry and consumption", func(t *testing.T) {
		workspace := harness.workspace(ctx, t, "exact-correlation", "Exact Correlation")
		other := harness.workspace(ctx, t, "other-correlation", "Other Correlation")
		now := time.Now().UTC()
		harness.withTenant(ctx, t, workspace, func(query db.Querier) {
			for _, request := range []AuthRequest{
				{
					ID: "_consumed", TenantID: workspace.id,
					Purpose: PurposeLogin, ExpiresAt: now.Add(15 * time.Minute),
				},
				{
					ID: "_expiring", TenantID: workspace.id,
					Purpose: PurposeLogin, ExpiresAt: now.Add(time.Minute),
				},
			} {
				if err := harness.store.CreateAuthRequest(ctx, query, request, now); err != nil {
					t.Fatalf("CreateAuthRequest(%s) error = %v", request.ID, err)
				}
			}
			live, err := harness.store.IsAuthRequestLive(
				ctx, query, workspace.id, "_consumed", now,
			)
			if err != nil || !live {
				t.Fatalf("live request = %v, error = %v", live, err)
			}
			crossTenant, err := harness.store.IsAuthRequestLive(
				ctx, query, other.id, "_consumed", now,
			)
			if err != nil || crossTenant {
				t.Fatalf("cross-tenant request = %v, error = %v", crossTenant, err)
			}
			if _, err := harness.store.ConsumeAuthRequest(
				ctx, query, workspace.id, "_consumed", now,
			); err != nil {
				t.Fatalf("ConsumeAuthRequest() error = %v", err)
			}
			consumed, err := harness.store.IsAuthRequestLive(
				ctx, query, workspace.id, "_consumed", now,
			)
			if err != nil || consumed {
				t.Fatalf("consumed request = %v, error = %v", consumed, err)
			}
			expired, err := harness.store.IsAuthRequestLive(
				ctx, query, workspace.id, "_expiring", now.Add(2*time.Minute),
			)
			if err != nil || expired {
				t.Fatalf("expired request = %v, error = %v", expired, err)
			}
		})
	})

	t.Run("capacity rejects only the new request", func(t *testing.T) {
		workspace := harness.workspace(ctx, t, "capacity", "Capacity")
		now := time.Now().UTC()
		harness.withTenant(ctx, t, workspace, func(query db.Querier) {
			for index := range maxOutstandingAuthRequests {
				request := AuthRequest{
					ID: fmt.Sprintf("_request-%03d", index), TenantID: workspace.id,
					Purpose: PurposeLogin, ExpiresAt: now.Add(15 * time.Minute),
				}
				if err := harness.store.CreateAuthRequest(ctx, query, request, now); err != nil {
					t.Fatalf("CreateAuthRequest(%d) error = %v", index, err)
				}
			}
			excess := AuthRequest{
				ID: "_request-excess", TenantID: workspace.id,
				Purpose: PurposeLogin, ExpiresAt: now.Add(15 * time.Minute),
			}
			if err := harness.store.CreateAuthRequest(
				ctx, query, excess, now,
			); !errors.Is(err, ErrAuthRequestCapacity) {
				t.Fatalf("excess CreateAuthRequest() error = %v, want ErrAuthRequestCapacity", err)
			}
			live, err := harness.store.IsAuthRequestLive(
				ctx, query, workspace.id, "_request-000", now,
			)
			if err != nil || !live {
				t.Fatalf("first request live = %v, error = %v", live, err)
			}
		})
		if got := harness.authRequestCount(ctx, t, workspace); got != maxOutstandingAuthRequests {
			t.Fatalf("auth requests = %d, want %d", got, maxOutstandingAuthRequests)
		}
	})

	t.Run("expired requests free capacity", func(t *testing.T) {
		workspace := harness.workspace(ctx, t, "capacity-expiry", "Capacity Expiry")
		now := time.Now().UTC()
		harness.withTenant(ctx, t, workspace, func(query db.Querier) {
			for index := range maxOutstandingAuthRequests {
				request := AuthRequest{
					ID: fmt.Sprintf("_expiring-%03d", index), TenantID: workspace.id,
					Purpose: PurposeLogin, ExpiresAt: now.Add(time.Minute),
				}
				if err := harness.store.CreateAuthRequest(ctx, query, request, now); err != nil {
					t.Fatalf("CreateAuthRequest(%d) error = %v", index, err)
				}
			}
		})

		later := now.Add(2 * time.Minute)
		harness.withTenant(ctx, t, workspace, func(query db.Querier) {
			replacement := AuthRequest{
				ID: "_replacement", TenantID: workspace.id,
				Purpose: PurposeLogin, ExpiresAt: later.Add(time.Minute),
			}
			if err := harness.store.CreateAuthRequest(ctx, query, replacement, later); err != nil {
				t.Fatalf("replacement CreateAuthRequest() error = %v", err)
			}
		})
		if got := harness.authRequestCount(ctx, t, workspace); got != 1 {
			t.Fatalf("auth requests after expiry cleanup = %d, want 1", got)
		}
	})

	t.Run("concurrent reservations never exceed capacity", func(t *testing.T) {
		workspace := harness.workspace(ctx, t, "capacity-race", "Capacity Race")
		now := time.Now().UTC()
		attempts := maxOutstandingAuthRequests + 32
		results := make(chan error, attempts)
		var ready sync.WaitGroup
		ready.Add(attempts)
		start := make(chan struct{})

		for index := range attempts {
			go func() {
				ready.Done()
				<-start
				transaction, err := harness.pools.App.Begin(ctx)
				if err != nil {
					results <- err
					return
				}
				defer func() { _ = transaction.Rollback(ctx) }()
				var configured string
				if err := transaction.QueryRow(
					ctx, `SELECT set_config('app.tenant_id', $1, true)`, workspace.id,
				).Scan(&configured); err != nil {
					results <- err
					return
				}
				request := AuthRequest{
					ID: fmt.Sprintf("_concurrent-%03d", index), TenantID: workspace.id,
					Purpose: PurposeLogin, ExpiresAt: now.Add(15 * time.Minute),
				}
				if err := harness.store.CreateAuthRequest(ctx, transaction, request, now); err != nil {
					results <- err
					return
				}
				results <- transaction.Commit(ctx)
			}()
		}
		ready.Wait()
		close(start)

		accepted := 0
		rejected := 0
		for range attempts {
			err := <-results
			switch {
			case err == nil:
				accepted++
			case errors.Is(err, ErrAuthRequestCapacity):
				rejected++
			default:
				t.Fatalf("concurrent CreateAuthRequest() error = %v", err)
			}
		}
		if accepted != maxOutstandingAuthRequests || rejected != attempts-maxOutstandingAuthRequests {
			t.Fatalf("accepted/rejected = %d/%d, want %d/%d",
				accepted, rejected, maxOutstandingAuthRequests, attempts-maxOutstandingAuthRequests)
		}
		if got := harness.authRequestCount(ctx, t, workspace); got != maxOutstandingAuthRequests {
			t.Fatalf("auth requests = %d, want %d", got, maxOutstandingAuthRequests)
		}
	})
}
