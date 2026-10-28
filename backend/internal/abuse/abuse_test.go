package abuse

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
)

const (
	currentSecret  = "current-secret-that-is-at-least-32-bytes"
	previousSecret = "previous-secret-that-is-at-least-32-bytes"
)

func TestEvaluatorHashesEveryScopeAndSupportsRotation(t *testing.T) {
	t.Parallel()

	keyring, err := NewKeyring(currentSecret, previousSecret)
	if err != nil {
		t.Fatalf("new keyring: %v", err)
	}
	limiter := &limiterStub{}
	evaluator := Evaluator{Limiter: limiter, Keyring: keyring}
	policy := Policy{
		Name: "test", Version: 2, Reason: ReasonSubmissionRateLimited, FailureMode: FailOpen,
		Rules: []Rule{
			{Scope: ScopeProject, Window: ratelimit.Window{Limit: 3, Period: time.Minute}},
			{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 2, Period: time.Minute}},
			{Scope: ScopeSubject, Window: ratelimit.Window{Limit: 1, Period: time.Minute}},
		},
	}
	identity := Identity{
		TenantID: "tenant-secret-value", ProjectID: "project-secret-value",
		Network: "203.0.113.17", SubjectID: "member@example.test",
	}

	first, err := evaluator.Evaluate(context.Background(), policy, Request{Identity: identity})
	if err != nil || !first.Allowed {
		t.Fatalf("first decision = %#v, error = %v", first, err)
	}
	if len(limiter.keys) != 6 {
		t.Fatalf("key count = %d, want 6 for three scopes and two secrets", len(limiter.keys))
	}
	for _, key := range limiter.keys {
		for _, raw := range []string{
			identity.TenantID, identity.ProjectID, identity.Network, identity.SubjectID,
		} {
			if strings.Contains(key, raw) {
				t.Fatalf("limiter key %q contains raw identity %q", key, raw)
			}
		}
		if !strings.HasPrefix(key, "abuse:v2:test:") {
			t.Errorf("limiter key = %q", key)
		}
	}

	firstKeys := append([]string(nil), limiter.keys...)
	limiter.keys = nil
	second, err := evaluator.Evaluate(context.Background(), policy, Request{Identity: identity})
	if err != nil || !second.Allowed {
		t.Fatalf("second decision = %#v, error = %v", second, err)
	}
	for index := range firstKeys {
		if firstKeys[index] != limiter.keys[index] {
			t.Errorf("key %d changed: %q != %q", index, firstKeys[index], limiter.keys[index])
		}
	}
}

func TestEvaluatorComposesDenialsAndRetry(t *testing.T) {
	t.Parallel()

	keyring, _ := NewKeyring(currentSecret, "")
	limiter := &limiterStub{decisions: []ratelimit.Decision{
		{Allowed: true, Limit: 100, Remaining: 40},
		{Allowed: false, Limit: 10, Remaining: 0, RetryAfter: 1100 * time.Millisecond},
		{Allowed: false, Limit: 20, Remaining: 0, RetryAfter: 3100 * time.Millisecond},
	}}
	policy := Policy{
		Name: "compose", Version: 1, Reason: ReasonSubmissionRateLimited, FailureMode: FailOpen,
		Rules: []Rule{
			{Scope: ScopeProject, Window: ratelimit.Window{Limit: 100, Period: time.Hour}},
			{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 10, Period: time.Minute}},
			{Scope: ScopeVisitor, Window: ratelimit.Window{Limit: 20, Period: time.Hour}},
		},
	}
	decision, err := (Evaluator{Limiter: limiter, Keyring: keyring}).Evaluate(
		context.Background(),
		policy,
		Request{Identity: Identity{
			TenantID: "tenant", ProjectID: "project", Network: "network", VisitorID: "visitor",
		}},
	)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Allowed || decision.RetryAfter != 3100*time.Millisecond ||
		RetryAfterSeconds(decision.RetryAfter) != 4 {
		t.Errorf("decision = %#v", decision)
	}
	if len(limiter.keys) != 3 {
		t.Errorf("evaluated keys = %d, want every scope", len(limiter.keys))
	}
}

func TestEvaluatorClassifiesLimiterFailureByPolicy(t *testing.T) {
	t.Parallel()

	keyring, _ := NewKeyring(currentSecret, "")
	failure := errors.New("redis unavailable")
	identity := Identity{TenantID: "tenant", ProjectID: "project"}
	for _, test := range []struct {
		name    string
		mode    FailureMode
		allowed bool
	}{
		{name: "text intake remains open", mode: FailOpen, allowed: true},
		{name: "cost grant closes", mode: FailClosed, allowed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := Policy{
				Name: "failure", Version: 1, Reason: ReasonUploadRateLimited,
				FailureMode: test.mode,
				Rules: []Rule{{
					Scope: ScopeProject, Window: ratelimit.Window{Limit: 1, Period: time.Minute},
				}},
			}
			decision, err := (Evaluator{
				Limiter: &limiterStub{err: failure}, Keyring: keyring,
			}).Evaluate(context.Background(), policy, Request{Identity: identity})
			if !errors.Is(err, failure) || !decision.Unavailable || decision.Allowed != test.allowed {
				t.Errorf("decision = %#v, error = %v", decision, err)
			}
		})
	}
}

func TestEvaluatorUsesWeightedLimiterAndSkipsOptionalIdentity(t *testing.T) {
	t.Parallel()

	keyring, _ := NewKeyring(currentSecret, "")
	limiter := &weightedLimiterStub{}
	policy := Policy{
		Name: "bytes", Version: 1, Reason: ReasonUploadRateLimited, FailureMode: FailClosed,
		Rules: []Rule{
			{Scope: ScopeProject, Window: ratelimit.Window{Limit: 125, Period: time.Hour}},
			{Scope: ScopeVisitor, Window: ratelimit.Window{Limit: 25, Period: time.Hour}, Optional: true},
		},
	}
	decision, err := (Evaluator{Limiter: limiter, Keyring: keyring}).Evaluate(
		context.Background(),
		policy,
		Request{Identity: Identity{TenantID: "tenant", ProjectID: "project"}, Cost: 17},
	)
	if err != nil || !decision.Allowed {
		t.Fatalf("decision = %#v, error = %v", decision, err)
	}
	if limiter.cost != 17 || limiter.calls != 1 {
		t.Errorf("weighted calls = %d, cost = %d", limiter.calls, limiter.cost)
	}
}

func TestKeyringRejectsUnsafeSecrets(t *testing.T) {
	t.Parallel()

	if _, err := NewKeyring("short", ""); err == nil {
		t.Fatal("expected short current secret to be rejected")
	}
	if _, err := NewKeyring(currentSecret, currentSecret); err == nil {
		t.Fatal("expected duplicate rotation secrets to be rejected")
	}
}

func TestDiagnosticEventRejectsInvalidBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Now()
	valid := DiagnosticEvent{
		TenantID: "tenant", ProjectID: "project", Category: CategorySpam,
		Reason: "spam_rejected", OccurredAt: now,
	}
	tests := []DiagnosticEvent{
		func() DiagnosticEvent {
			event := valid
			event.Reason = ""
			return event
		}(),
		func() DiagnosticEvent {
			event := valid
			event.Count = -1
			return event
		}(),
		func() DiagnosticEvent {
			event := valid
			event.OccurredAt = time.Time{}
			return event
		}(),
		func() DiagnosticEvent {
			event := valid
			event.FirstOccurredAt = now.Add(time.Second)
			return event
		}(),
	}
	for _, event := range tests {
		if err := event.validate(); err == nil {
			t.Fatalf("validate(%#v) error = nil", event)
		}
	}
	if RetryAfterSeconds(0) != 1 {
		t.Fatal("RetryAfterSeconds(0) did not enforce the minimum")
	}
}

type limiterStub struct {
	keys      []string
	decisions []ratelimit.Decision
	err       error
}

func (limiter *limiterStub) Allow(
	_ context.Context,
	key string,
	window ratelimit.Window,
) (ratelimit.Decision, error) {
	limiter.keys = append(limiter.keys, key)
	if limiter.err != nil {
		return ratelimit.Decision{}, limiter.err
	}
	if len(limiter.decisions) == 0 {
		return ratelimit.Decision{
			Allowed: true, Limit: window.Limit, Remaining: window.Limit - 1,
		}, nil
	}
	decision := limiter.decisions[0]
	limiter.decisions = limiter.decisions[1:]
	return decision, nil
}

type weightedLimiterStub struct {
	calls int
	cost  int
}

func (limiter *weightedLimiterStub) Allow(
	ctx context.Context,
	key string,
	window ratelimit.Window,
) (ratelimit.Decision, error) {
	return limiter.AllowN(ctx, key, window, 1)
}

func (limiter *weightedLimiterStub) AllowN(
	_ context.Context,
	_ string,
	window ratelimit.Window,
	cost int,
) (ratelimit.Decision, error) {
	limiter.calls++
	limiter.cost = cost
	return ratelimit.Decision{
		Allowed: true, Limit: window.Limit, Remaining: window.Limit - cost,
	}, nil
}
