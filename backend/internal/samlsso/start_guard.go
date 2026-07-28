package samlsso

import (
	"context"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
)

const (
	ReasonSAMLStartRateLimited = "saml_start_rate_limited"
	samlStartProjectScope      = "saml-start"
)

var samlStartNetworkPolicy = abuse.Policy{
	Name:        "saml-start-network",
	Version:     1,
	Reason:      ReasonSAMLStartRateLimited,
	FailureMode: abuse.FailClosed,
	Rules: []abuse.Rule{{
		Scope:  abuse.ScopeNetwork,
		Window: ratelimit.Window{Limit: 10, Period: 15 * time.Minute},
	}},
}

var samlStartTenantPolicy = abuse.Policy{
	Name:        "saml-start-tenant",
	Version:     1,
	Reason:      ReasonSAMLStartRateLimited,
	FailureMode: abuse.FailClosed,
	Rules: []abuse.Rule{{
		Scope:  abuse.ScopeTenant,
		Window: ratelimit.Window{Limit: 100, Period: 15 * time.Minute},
	}},
}

type StartAttempt struct {
	TenantID string
	Network  string
}

type StartDecision struct {
	Allowed     bool
	Unavailable bool
	Reason      string
}

type StartGuard interface {
	Allow(context.Context, StartAttempt) (StartDecision, error)
}

type AbuseStartGuard struct {
	Evaluator abuse.Evaluator
}

func (guard AbuseStartGuard) Allow(
	ctx context.Context,
	attempt StartAttempt,
) (StartDecision, error) {
	decision, err := guard.Evaluator.Evaluate(
		ctx,
		samlStartNetworkPolicy,
		abuse.Request{Identity: abuse.Identity{
			TenantID:  attempt.TenantID,
			ProjectID: samlStartProjectScope,
			Network:   attempt.Network,
		}},
	)
	if err != nil || !decision.Allowed {
		return startDecision(decision), err
	}
	decision, err = guard.Evaluator.Evaluate(
		ctx,
		samlStartTenantPolicy,
		abuse.Request{Identity: abuse.Identity{TenantID: attempt.TenantID}},
	)
	return startDecision(decision), err
}

func startDecision(decision abuse.Decision) StartDecision {
	return StartDecision{
		Allowed: decision.Allowed, Unavailable: decision.Unavailable, Reason: decision.Reason,
	}
}
