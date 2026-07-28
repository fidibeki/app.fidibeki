package tenantmember

import (
	"context"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
)

const (
	ReasonPasswordlessLoginCooldown    = "passwordless_login_cooldown"
	ReasonPasswordlessLoginRateLimited = "passwordless_login_rate_limited"
	passwordlessLoginProjectScope      = "passwordless-login"
)

var passwordlessLoginCooldownPolicy = abuse.Policy{
	Name:        "tenant-passwordless-login-cooldown",
	Version:     1,
	Reason:      ReasonPasswordlessLoginCooldown,
	FailureMode: abuse.FailClosed,
	Rules: []abuse.Rule{{
		Scope: abuse.ScopeSubject,
		Window: ratelimit.Window{
			Limit:  1,
			Period: time.Minute,
		},
	}},
}

var passwordlessLoginRatePolicy = abuse.Policy{
	Name:        "tenant-passwordless-login",
	Version:     1,
	Reason:      ReasonPasswordlessLoginRateLimited,
	FailureMode: abuse.FailClosed,
	Rules: []abuse.Rule{
		{
			Scope:  abuse.ScopeSubject,
			Window: ratelimit.Window{Limit: 3, Period: 15 * time.Minute},
		},
		{
			Scope:  abuse.ScopeNetwork,
			Window: ratelimit.Window{Limit: 10, Period: 15 * time.Minute},
		},
		{
			Scope:  abuse.ScopeTenant,
			Window: ratelimit.Window{Limit: 60, Period: time.Hour},
		},
	},
}

type LoginAttempt struct {
	TenantID string
	Email    string
	Network  string
}

type LoginDecision struct {
	Allowed     bool
	Unavailable bool
	Reason      string
}

type LoginGuard interface {
	Allow(context.Context, LoginAttempt) (LoginDecision, error)
}

type AbuseLoginGuard struct {
	Evaluator abuse.Evaluator
}

func (guard AbuseLoginGuard) Allow(
	ctx context.Context,
	attempt LoginAttempt,
) (LoginDecision, error) {
	decision, err := guard.Evaluator.Evaluate(
		ctx,
		passwordlessLoginCooldownPolicy,
		abuse.Request{Identity: abuse.Identity{
			TenantID:  attempt.TenantID,
			SubjectID: attempt.Email,
		}},
	)
	if err != nil || !decision.Allowed {
		return loginDecision(decision), err
	}
	decision, err = guard.Evaluator.Evaluate(
		ctx,
		passwordlessLoginRatePolicy,
		abuse.Request{Identity: abuse.Identity{
			TenantID:  attempt.TenantID,
			ProjectID: passwordlessLoginProjectScope,
			SubjectID: attempt.Email,
			Network:   attempt.Network,
		}},
	)
	return loginDecision(decision), err
}

func loginDecision(decision abuse.Decision) LoginDecision {
	return LoginDecision{
		Allowed: decision.Allowed, Unavailable: decision.Unavailable, Reason: decision.Reason,
	}
}
