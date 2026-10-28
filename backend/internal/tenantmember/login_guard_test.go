package tenantmember

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/abuse"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
)

const loginGuardTestSecret = "login-guard-test-secret-at-least-32-bytes"

func TestAbuseLoginGuardCooldownSuppressesBeforeBroaderBudgets(t *testing.T) {
	t.Parallel()

	keyring, err := abuse.NewKeyring(loginGuardTestSecret, "")
	if err != nil {
		t.Fatalf("create keyring: %v", err)
	}
	limiter := &loginGuardLimiterStub{decisions: []ratelimit.Decision{{
		Allowed: false, RetryAfter: time.Minute,
	}}}
	guard := AbuseLoginGuard{
		Evaluator: abuse.Evaluator{Limiter: limiter, Keyring: keyring},
	}

	decision, err := guard.Allow(context.Background(), LoginAttempt{
		TenantID: "tenant-secret", Email: "member@example.test", Network: "203.0.113.18",
	})
	if err != nil {
		t.Fatalf("allow login: %v", err)
	}
	if decision.Allowed || decision.Reason != ReasonPasswordlessLoginCooldown {
		t.Fatalf("decision = %#v, want cooldown suppression", decision)
	}
	if len(limiter.calls) != 1 {
		t.Fatalf("limiter calls = %d, want only cooldown", len(limiter.calls))
	}
	call := limiter.calls[0]
	if call.window != (ratelimit.Window{Limit: 1, Period: time.Minute}) {
		t.Errorf("cooldown window = %#v", call.window)
	}
	for _, raw := range []string{"tenant-secret", "member@example.test", "203.0.113.18"} {
		if strings.Contains(call.key, raw) {
			t.Errorf("cooldown key %q contains raw identity %q", call.key, raw)
		}
	}
}

func TestAbuseLoginGuardAppliesEveryPasswordlessLoginBudget(t *testing.T) {
	t.Parallel()

	keyring, err := abuse.NewKeyring(loginGuardTestSecret, "")
	if err != nil {
		t.Fatalf("create keyring: %v", err)
	}
	limiter := &loginGuardLimiterStub{}
	guard := AbuseLoginGuard{
		Evaluator: abuse.Evaluator{Limiter: limiter, Keyring: keyring},
	}

	decision, err := guard.Allow(context.Background(), LoginAttempt{
		TenantID: "tenant-secret", Email: "member@example.test", Network: "203.0.113.18",
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("decision = %#v, error = %v", decision, err)
	}

	wantWindows := []ratelimit.Window{
		{Limit: 1, Period: time.Minute},
		{Limit: 3, Period: 15 * time.Minute},
		{Limit: 10, Period: 15 * time.Minute},
		{Limit: 60, Period: time.Hour},
	}
	wantPrefixes := []string{
		"abuse:v1:tenant-passwordless-login-cooldown:subject:",
		"abuse:v1:tenant-passwordless-login:subject:",
		"abuse:v1:tenant-passwordless-login:network:",
		"abuse:v1:tenant-passwordless-login:tenant:",
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
		for _, raw := range []string{"tenant-secret", "member@example.test", "203.0.113.18"} {
			if strings.Contains(call.key, raw) {
				t.Errorf("call %d key %q contains raw identity %q", index, call.key, raw)
			}
		}
	}
}

func TestAbuseLoginGuardSuppressesAtEveryPasswordlessLoginBudget(t *testing.T) {
	t.Parallel()

	keyring, err := abuse.NewKeyring(loginGuardTestSecret, "")
	if err != nil {
		t.Fatalf("create keyring: %v", err)
	}
	for _, test := range []struct {
		name       string
		decisions  []ratelimit.Decision
		wantCalls  int
		wantReason string
	}{
		{
			name:      "cooldown",
			decisions: []ratelimit.Decision{{Allowed: false}},
			wantCalls: 1, wantReason: ReasonPasswordlessLoginCooldown,
		},
		{
			name: "subject",
			decisions: []ratelimit.Decision{
				{Allowed: true}, {Allowed: false}, {Allowed: true}, {Allowed: true},
			},
			wantCalls: 4, wantReason: ReasonPasswordlessLoginRateLimited,
		},
		{
			name: "network",
			decisions: []ratelimit.Decision{
				{Allowed: true}, {Allowed: true}, {Allowed: false}, {Allowed: true},
			},
			wantCalls: 4, wantReason: ReasonPasswordlessLoginRateLimited,
		},
		{
			name: "tenant",
			decisions: []ratelimit.Decision{
				{Allowed: true}, {Allowed: true}, {Allowed: true}, {Allowed: false},
			},
			wantCalls: 4, wantReason: ReasonPasswordlessLoginRateLimited,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			limiter := &loginGuardLimiterStub{decisions: test.decisions}
			guard := AbuseLoginGuard{
				Evaluator: abuse.Evaluator{Limiter: limiter, Keyring: keyring},
			}
			decision, err := guard.Allow(context.Background(), LoginAttempt{
				TenantID: "tenant", Email: "member@example.test", Network: "203.0.113.18",
			})
			if err != nil {
				t.Fatalf("allow login: %v", err)
			}
			if decision.Allowed || decision.Reason != test.wantReason {
				t.Fatalf("decision = %#v, want %q suppression", decision, test.wantReason)
			}
			if len(limiter.calls) != test.wantCalls {
				t.Errorf("limiter calls = %d, want %d", len(limiter.calls), test.wantCalls)
			}
		})
	}
}

func TestAbuseLoginGuardFailsClosedWhenLimiterIsUnavailable(t *testing.T) {
	t.Parallel()

	keyring, err := abuse.NewKeyring(loginGuardTestSecret, "")
	if err != nil {
		t.Fatalf("create keyring: %v", err)
	}
	failure := errors.New("redis unavailable")
	guard := AbuseLoginGuard{
		Evaluator: abuse.Evaluator{
			Limiter: &loginGuardLimiterStub{err: failure},
			Keyring: keyring,
		},
	}

	decision, err := guard.Allow(context.Background(), LoginAttempt{
		TenantID: "tenant", Email: "member@example.test", Network: "203.0.113.18",
	})
	if !errors.Is(err, failure) || decision.Allowed || !decision.Unavailable {
		t.Fatalf("decision = %#v, error = %v", decision, err)
	}

	withoutLimiter, err := (AbuseLoginGuard{
		Evaluator: abuse.Evaluator{Keyring: keyring},
	}).Allow(context.Background(), LoginAttempt{
		TenantID: "tenant", Email: "member@example.test", Network: "203.0.113.18",
	})
	if err != nil || withoutLimiter.Allowed || !withoutLimiter.Unavailable {
		t.Fatalf("missing-limiter decision = %#v, error = %v", withoutLimiter, err)
	}
}

type loginGuardLimiterCall struct {
	key    string
	window ratelimit.Window
}

type loginGuardLimiterStub struct {
	decisions []ratelimit.Decision
	calls     []loginGuardLimiterCall
	err       error
}

func (limiter *loginGuardLimiterStub) Allow(
	_ context.Context,
	key string,
	window ratelimit.Window,
) (ratelimit.Decision, error) {
	limiter.calls = append(limiter.calls, loginGuardLimiterCall{key: key, window: window})
	if limiter.err != nil {
		return ratelimit.Decision{}, limiter.err
	}
	if len(limiter.decisions) == 0 {
		return ratelimit.Decision{Allowed: true}, nil
	}
	decision := limiter.decisions[0]
	limiter.decisions = limiter.decisions[1:]
	return decision, nil
}
