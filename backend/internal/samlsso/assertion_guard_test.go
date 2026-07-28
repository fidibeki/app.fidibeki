package samlsso

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
)

const assertionGuardTestSecret = "saml-assertion-guard-test-secret-at-least-32-bytes"

func TestAbuseAssertionGuardAppliesNetworkAndTenantBudgets(t *testing.T) {
	t.Parallel()

	keyring, err := abuse.NewKeyring(assertionGuardTestSecret, "")
	if err != nil {
		t.Fatalf("create keyring: %v", err)
	}
	limiter := &startGuardLimiterStub{}
	guard := AbuseAssertionGuard{Evaluator: abuse.Evaluator{Limiter: limiter, Keyring: keyring}}

	decision, err := guard.Allow(context.Background(), AssertionAttempt{
		TenantID: "tenant-secret", Network: "203.0.113.18",
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("decision = %#v, error = %v", decision, err)
	}

	wantWindows := []ratelimit.Window{
		{Limit: 20, Period: 15 * time.Minute},
		{Limit: 200, Period: 15 * time.Minute},
	}
	wantPrefixes := []string{
		"abuse:v1:saml-assertion-network:network:",
		"abuse:v1:saml-assertion-tenant:tenant:",
	}
	if len(limiter.calls) != len(wantWindows) {
		t.Fatalf("limiter calls = %d, want %d", len(limiter.calls), len(wantWindows))
	}
	for index, call := range limiter.calls {
		if call.window != wantWindows[index] {
			t.Errorf("call %d window = %#v, want %#v", index, call.window, wantWindows[index])
		}
		if !strings.HasPrefix(call.key, wantPrefixes[index]) {
			t.Errorf("call %d key = %q, want prefix %q", index, call.key, wantPrefixes[index])
		}
		for _, raw := range []string{"tenant-secret", "203.0.113.18"} {
			if strings.Contains(call.key, raw) {
				t.Errorf("call %d key %q contains raw identity %q", index, call.key, raw)
			}
		}
	}
}

func TestAbuseAssertionGuardDeniesAtEitherBudget(t *testing.T) {
	t.Parallel()

	keyring, err := abuse.NewKeyring(assertionGuardTestSecret, "")
	if err != nil {
		t.Fatalf("create keyring: %v", err)
	}
	for _, test := range []struct {
		name      string
		decisions []ratelimit.Decision
		wantCalls int
	}{
		{name: "network", decisions: []ratelimit.Decision{{Allowed: false}}, wantCalls: 1},
		{name: "tenant", decisions: []ratelimit.Decision{{Allowed: true}, {Allowed: false}}, wantCalls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limiter := &startGuardLimiterStub{decisions: test.decisions}
			guard := AbuseAssertionGuard{Evaluator: abuse.Evaluator{
				Limiter: limiter,
				Keyring: keyring,
			}}
			decision, err := guard.Allow(context.Background(), AssertionAttempt{
				TenantID: "tenant", Network: "203.0.113.18",
			})
			if err != nil || decision.Allowed ||
				decision.Reason != ReasonSAMLAssertionRateLimited {
				t.Fatalf("decision = %#v, error = %v", decision, err)
			}
			if len(limiter.calls) != test.wantCalls {
				t.Fatalf("limiter calls = %d, want %d", len(limiter.calls), test.wantCalls)
			}
		})
	}
}

func TestAbuseAssertionGuardFailsClosedWhenLimiterIsUnavailable(t *testing.T) {
	t.Parallel()

	keyring, err := abuse.NewKeyring(assertionGuardTestSecret, "")
	if err != nil {
		t.Fatalf("create keyring: %v", err)
	}
	failure := errors.New("redis unavailable")
	guard := AbuseAssertionGuard{Evaluator: abuse.Evaluator{
		Limiter: &startGuardLimiterStub{err: failure}, Keyring: keyring,
	}}
	decision, err := guard.Allow(context.Background(), AssertionAttempt{
		TenantID: "tenant", Network: "203.0.113.18",
	})
	if !errors.Is(err, failure) || decision.Allowed || !decision.Unavailable {
		t.Fatalf("decision = %#v, error = %v", decision, err)
	}

	withoutLimiter, err := (AbuseAssertionGuard{
		Evaluator: abuse.Evaluator{Keyring: keyring},
	}).Allow(context.Background(), AssertionAttempt{
		TenantID: "tenant", Network: "203.0.113.18",
	})
	if err != nil || withoutLimiter.Allowed || !withoutLimiter.Unavailable {
		t.Fatalf("missing-limiter decision = %#v, error = %v", withoutLimiter, err)
	}
}
