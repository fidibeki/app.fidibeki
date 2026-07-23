package abuse

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
)

type Scope string

const (
	ScopeTenant  Scope = "tenant"
	ScopeProject Scope = "project"
	ScopeOrigin  Scope = "origin"
	ScopeNetwork Scope = "network"
	ScopeVisitor Scope = "visitor"
	ScopeSession Scope = "session"
)

type FailureMode string

const (
	FailOpen   FailureMode = "open"
	FailClosed FailureMode = "closed"
)

type Identity struct {
	TenantID  string
	ProjectID string
	Origin    string
	Network   string
	VisitorID string
	SessionID string
}

func (identity Identity) value(scope Scope) (string, bool) {
	tenant := strings.TrimSpace(identity.TenantID)
	project := strings.TrimSpace(identity.ProjectID)
	switch scope {
	case ScopeTenant:
		return tenant, tenant != ""
	case ScopeProject:
		return tenant + "\x00" + project, tenant != "" && project != ""
	case ScopeOrigin:
		value := strings.ToLower(strings.TrimSpace(identity.Origin))
		return tenant + "\x00" + project + "\x00" + value,
			tenant != "" && project != "" && value != ""
	case ScopeNetwork:
		value := strings.TrimSpace(identity.Network)
		return tenant + "\x00" + project + "\x00" + value,
			tenant != "" && project != "" && value != ""
	case ScopeVisitor:
		value := strings.TrimSpace(identity.VisitorID)
		return tenant + "\x00" + project + "\x00" + value,
			tenant != "" && project != "" && value != ""
	case ScopeSession:
		value := strings.TrimSpace(identity.SessionID)
		return tenant + "\x00" + project + "\x00" + value,
			tenant != "" && project != "" && value != ""
	default:
		return "", false
	}
}

type Rule struct {
	Scope    Scope
	Window   ratelimit.Window
	Optional bool
}

type Policy struct {
	Name        string
	Version     int
	Reason      string
	FailureMode FailureMode
	Rules       []Rule
}

func (policy Policy) validate() error {
	if strings.TrimSpace(policy.Name) == "" {
		return errors.New("abuse policy name is required")
	}
	if policy.Version <= 0 {
		return errors.New("abuse policy version must be positive")
	}
	if strings.TrimSpace(policy.Reason) == "" {
		return errors.New("abuse policy reason is required")
	}
	if policy.FailureMode != FailOpen && policy.FailureMode != FailClosed {
		return errors.New("abuse policy failure mode is invalid")
	}
	if len(policy.Rules) == 0 {
		return errors.New("abuse policy requires at least one rule")
	}
	for _, rule := range policy.Rules {
		if rule.Scope == "" || rule.Window.Limit <= 0 || rule.Window.Period <= 0 {
			return errors.New("abuse policy rule is invalid")
		}
	}
	return nil
}

type Keyring struct {
	current  []byte
	previous []byte
}

func NewKeyring(current, previous string) (Keyring, error) {
	if len(current) < 32 {
		return Keyring{}, errors.New("abuse bucket secret must be at least 32 characters")
	}
	keyring := Keyring{current: []byte(current)}
	if previous != "" {
		if len(previous) < 32 {
			return Keyring{}, errors.New("previous abuse bucket secret must be at least 32 characters")
		}
		if hmac.Equal([]byte(current), []byte(previous)) {
			return Keyring{}, errors.New("abuse bucket secrets must be different")
		}
		keyring.previous = []byte(previous)
	}
	return keyring, nil
}

func (keyring Keyring) keys(policy Policy, scope Scope, value string) []string {
	namespace := fmt.Sprintf("abuse:v%d:%s:%s:", policy.Version, policy.Name, scope)
	keys := []string{namespace + digest(keyring.current, namespace, value)}
	if len(keyring.previous) > 0 {
		keys = append(keys, namespace+digest(keyring.previous, namespace, value))
	}
	return keys
}

func digest(secret []byte, namespace, value string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(namespace))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

type Evaluator struct {
	Limiter ratelimit.Limiter
	Keyring Keyring
}

type Request struct {
	Identity Identity
	Cost     int
}

type Decision struct {
	Allowed     bool
	Unavailable bool
	Reason      string
	RetryAfter  time.Duration
	Limit       int
	Remaining   int
}

func (evaluator Evaluator) Evaluate(
	ctx context.Context,
	policy Policy,
	request Request,
) (Decision, error) {
	if err := policy.validate(); err != nil {
		return Decision{}, err
	}
	cost := request.Cost
	if cost == 0 {
		cost = 1
	}
	if cost < 0 {
		return Decision{}, errors.New("abuse request cost must not be negative")
	}
	if evaluator.Limiter == nil {
		return unavailableDecision(policy), nil
	}

	result := Decision{Allowed: true, Reason: policy.Reason, Remaining: math.MaxInt}
	applied := 0
	for _, rule := range policy.Rules {
		value, present := request.Identity.value(rule.Scope)
		if !present {
			if rule.Optional {
				continue
			}
			return Decision{}, fmt.Errorf("abuse identity is missing required %s scope", rule.Scope)
		}
		for _, key := range evaluator.Keyring.keys(policy, rule.Scope, value) {
			decision, err := allow(ctx, evaluator.Limiter, key, rule.Window, cost)
			if err != nil {
				unavailable := unavailableDecision(policy)
				return unavailable, fmt.Errorf("evaluate %s abuse policy: %w", policy.Name, err)
			}
			applied++
			if decision.Limit > 0 && (result.Limit == 0 || decision.Limit < result.Limit) {
				result.Limit = decision.Limit
			}
			if decision.Remaining < result.Remaining {
				result.Remaining = decision.Remaining
			}
			if !decision.Allowed {
				result.Allowed = false
				if decision.RetryAfter > result.RetryAfter {
					result.RetryAfter = decision.RetryAfter
				}
			}
		}
	}
	if applied == 0 {
		return Decision{}, errors.New("abuse policy did not apply any rules")
	}
	if result.Remaining == math.MaxInt {
		result.Remaining = 0
	}
	return result, nil
}

func unavailableDecision(policy Policy) Decision {
	return Decision{
		Allowed:     policy.FailureMode == FailOpen,
		Unavailable: true,
		Reason:      policy.Reason,
	}
}

func allow(
	ctx context.Context,
	limiter ratelimit.Limiter,
	key string,
	window ratelimit.Window,
	cost int,
) (ratelimit.Decision, error) {
	if weighted, ok := limiter.(ratelimit.CostLimiter); ok {
		return weighted.AllowN(ctx, key, window, cost)
	}
	if cost != 1 {
		return ratelimit.Decision{}, errors.New("rate limiter does not support weighted decisions")
	}
	return limiter.Allow(ctx, key, window)
}

func RetryAfterSeconds(duration time.Duration) int {
	if duration <= 0 {
		return 1
	}
	return int((duration + time.Second - 1) / time.Second)
}
