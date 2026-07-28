package samlsso

import (
	"context"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
)

const (
	ReasonSAMLAssertionRateLimited = "saml_assertion_rate_limited"
	samlAssertionProjectScope      = "saml-assertion"
)

var samlAssertionNetworkPolicy = abuse.Policy{
	Name:        "saml-assertion-network",
	Version:     1,
	Reason:      ReasonSAMLAssertionRateLimited,
	FailureMode: abuse.FailClosed,
	Rules: []abuse.Rule{{
		Scope:  abuse.ScopeNetwork,
		Window: ratelimit.Window{Limit: 20, Period: 15 * time.Minute},
	}},
}

var samlAssertionTenantPolicy = abuse.Policy{
	Name:        "saml-assertion-tenant",
	Version:     1,
	Reason:      ReasonSAMLAssertionRateLimited,
	FailureMode: abuse.FailClosed,
	Rules: []abuse.Rule{{
		Scope:  abuse.ScopeTenant,
		Window: ratelimit.Window{Limit: 200, Period: 15 * time.Minute},
	}},
}

type AssertionAttempt struct {
	TenantID string
	Network  string
}

type AssertionDecision struct {
	Allowed     bool
	Unavailable bool
	Reason      string
}

type AssertionGuard interface {
	Allow(context.Context, AssertionAttempt) (AssertionDecision, error)
}

type AbuseAssertionGuard struct {
	Evaluator abuse.Evaluator
}

func (guard AbuseAssertionGuard) Allow(
	ctx context.Context,
	attempt AssertionAttempt,
) (AssertionDecision, error) {
	decision, err := guard.Evaluator.Evaluate(
		ctx,
		samlAssertionNetworkPolicy,
		abuse.Request{Identity: abuse.Identity{
			TenantID:  attempt.TenantID,
			ProjectID: samlAssertionProjectScope,
			Network:   attempt.Network,
		}},
	)
	if err != nil || !decision.Allowed {
		return assertionDecision(decision), err
	}
	decision, err = guard.Evaluator.Evaluate(
		ctx,
		samlAssertionTenantPolicy,
		abuse.Request{Identity: abuse.Identity{TenantID: attempt.TenantID}},
	)
	return assertionDecision(decision), err
}

func assertionDecision(decision abuse.Decision) AssertionDecision {
	return AssertionDecision{
		Allowed: decision.Allowed, Unavailable: decision.Unavailable, Reason: decision.Reason,
	}
}
