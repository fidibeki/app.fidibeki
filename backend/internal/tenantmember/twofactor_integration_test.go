package tenantmember

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/fidibeki/app.fidibeki/backend/internal/twofactor"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const twoFactorTestKey = "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA="

// countingLimiter allows a fixed number of attempts per key so the rate-limit
// path can be exercised without a Redis dependency.
type countingLimiter struct {
	limit    int
	attempts map[string]int
}

func (l *countingLimiter) Allow(_ context.Context, key string, _ ratelimit.Window) (ratelimit.Decision, error) {
	if l.attempts == nil {
		l.attempts = map[string]int{}
	}
	l.attempts[key]++
	if l.attempts[key] > l.limit {
		return ratelimit.Decision{Allowed: false, RetryAfter: time.Second}, nil
	}
	return ratelimit.Decision{Allowed: true, Remaining: l.limit - l.attempts[key]}, nil
}

type loginGuardStub struct {
	decision LoginDecision
	err      error
	calls    []LoginAttempt
}

func (guard *loginGuardStub) Allow(
	_ context.Context,
	attempt LoginAttempt,
) (LoginDecision, error) {
	guard.calls = append(guard.calls, attempt)
	return guard.decision, guard.err
}

type twoFactorHarness struct {
	t          *testing.T
	ctx        context.Context
	root       *pgx.Conn
	handler    http.Handler
	tenantID   string
	limiter    *countingLimiter
	loginGuard *loginGuardStub
	queue      *messageCapture
	jwt        *auth.JWTManager
	memberIDs  map[string]string
	// memberSecrets remembers the shared secret each member enrolled with so a
	// later step can act as that member's authenticator app.
	memberSecrets map[string]string
	logs          *bytes.Buffer
	// totpClock drives both the derived codes and the server's verification, so
	// every code comes from a fresh time step without the test sleeping. A
	// reused step is rejected by design, which back-to-back requests would
	// otherwise trip.
	totpClock time.Time
}

func TestTenantTwoFactorAuthenticationFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	harness := newTwoFactorHarness(ctx, t)

	t.Run("passwordless login guard suppresses work without enumeration", func(t *testing.T) {
		harness.loginGuard.calls = nil
		harness.loginGuard.decision = LoginDecision{
			Allowed: false, Reason: ReasonPasswordlessLoginCooldown,
		}
		defer func() {
			harness.loginGuard.calls = nil
			harness.loginGuard.decision = LoginDecision{Allowed: true}
		}()
		messageCount := len(harness.queue.messages)
		linkCount := harness.magicLinkCount(t)
		logOffset := harness.logs.Len()

		response := harness.request(
			http.MethodPost, "/auth/login", `{"email":" Owner@ACME.TEST "}`, "", nil,
		)

		if response.Code != http.StatusAccepted ||
			response.Body.String() != "{\"status\":\"accepted\"}\n" {
			t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
		}
		if len(harness.loginGuard.calls) != 1 {
			t.Fatalf("login guard calls = %d, want 1", len(harness.loginGuard.calls))
		}
		attempt := harness.loginGuard.calls[0]
		if attempt.TenantID != harness.tenantID || attempt.Email != "owner@acme.test" ||
			attempt.Network != "192.0.2.1" {
			t.Errorf("login attempt = %#v", attempt)
		}
		if len(harness.queue.messages) != messageCount {
			t.Fatalf("email messages = %d, want unchanged %d", len(harness.queue.messages), messageCount)
		}
		if got := harness.magicLinkCount(t); got != linkCount {
			t.Fatalf("magic-link rows = %d, want unchanged %d", got, linkCount)
		}
		logged := harness.logs.String()[logOffset:]
		if !strings.Contains(logged, `"reason":"passwordless_login_cooldown"`) ||
			strings.Contains(logged, harness.tenantID) ||
			strings.Contains(logged, "owner@acme.test") ||
			strings.Contains(logged, "192.0.2.1") {
			t.Errorf("suppression log is missing its reason or contains raw identity: %s", logged)
		}
	})

	t.Run("passwordless login guard failure creates no work", func(t *testing.T) {
		harness.loginGuard.calls = nil
		harness.loginGuard.err = errors.New("redis unavailable")
		defer func() {
			harness.loginGuard.calls = nil
			harness.loginGuard.err = nil
		}()
		messageCount := len(harness.queue.messages)
		linkCount := harness.magicLinkCount(t)
		logOffset := harness.logs.Len()

		response := harness.request(
			http.MethodPost, "/auth/login", `{"email":"owner@acme.test"}`, "", nil,
		)

		if response.Code != http.StatusServiceUnavailable ||
			decodeErrorCode(t, response) != "service_unavailable" {
			t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
		}
		if len(harness.loginGuard.calls) != 1 {
			t.Fatalf("login guard calls = %d, want 1", len(harness.loginGuard.calls))
		}
		if len(harness.queue.messages) != messageCount {
			t.Fatalf("email messages = %d, want unchanged %d", len(harness.queue.messages), messageCount)
		}
		if got := harness.magicLinkCount(t); got != linkCount {
			t.Fatalf("magic-link rows = %d, want unchanged %d", got, linkCount)
		}
		logged := harness.logs.String()[logOffset:]
		if !strings.Contains(logged, `"msg":"passwordless login guard unavailable"`) ||
			strings.Contains(logged, harness.tenantID) ||
			strings.Contains(logged, "owner@acme.test") ||
			strings.Contains(logged, "192.0.2.1") {
			t.Errorf("failure log is missing or contains raw identity: %s", logged)
		}
	})

	t.Run("inactive lifecycle suppresses local login without enumeration", func(t *testing.T) {
		for _, test := range authenticationLifecycleCases() {
			t.Run(test.name, func(t *testing.T) {
				harness.setLifecycle(t, test.status, test.trialEnd, nil)
				messageCount := len(harness.queue.messages)
				linkCount := harness.magicLinkCount(t)

				response := harness.request(
					http.MethodPost, "/auth/login", `{"email":"owner@acme.test"}`, "", nil,
				)
				if response.Code != http.StatusAccepted {
					t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
				}
				if len(harness.queue.messages) != messageCount {
					t.Fatalf(
						"email messages = %d, want unchanged %d",
						len(harness.queue.messages), messageCount,
					)
				}
				if got := harness.magicLinkCount(t); got != linkCount {
					t.Fatalf("magic-link rows = %d, want unchanged %d", got, linkCount)
				}
			})
		}
		harness.setLifecycle(t, tenant.StatusActive, nil, nil)
	})

	t.Run("inactive lifecycle rejects an already issued local magic link", func(t *testing.T) {
		for _, test := range authenticationLifecycleCases() {
			t.Run(test.name, func(t *testing.T) {
				harness.setLifecycle(t, tenant.StatusActive, nil, nil)
				beforeMessages := len(harness.queue.messages)
				login := harness.request(
					http.MethodPost, "/auth/login", `{"email":"owner@acme.test"}`, "", nil,
				)
				if login.Code != http.StatusAccepted || len(harness.queue.messages) != beforeMessages+1 {
					t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
				}
				token := messageToken(t, harness.queue.messages[len(harness.queue.messages)-1])
				sessionCount := harness.sessionCount(t)

				harness.setLifecycle(t, test.status, test.trialEnd, nil)
				defer harness.setLifecycle(t, tenant.StatusActive, nil, nil)
				response := harness.request(
					http.MethodPost, "/auth/verify", `{"token":"`+token+`"}`, "", nil,
				)
				if response.Code != http.StatusForbidden {
					t.Fatalf("verify status = %d, body = %s", response.Code, response.Body.String())
				}
				if code := decodeErrorCode(t, response); code != "tenant_suspended" {
					t.Fatalf("verify error code = %q, want tenant_suspended", code)
				}
				if got := harness.sessionCount(t); got != sessionCount {
					t.Fatalf("session rows = %d, want unchanged %d", got, sessionCount)
				}
				if harness.magicLinkConsumed(t, token) {
					t.Fatal("rejected magic link was consumed")
				}
			})
		}
	})

	t.Run("eligible lifecycle keeps local authentication available", func(t *testing.T) {
		deletionRequestedAt := time.Now().Add(-time.Hour)
		for _, test := range []struct {
			name                string
			status              tenant.Status
			trialEnd            *time.Time
			deletionRequestedAt *time.Time
		}{
			{
				name:     "unexpired trial",
				status:   tenant.StatusTrial,
				trialEnd: ptrTime(time.Now().Add(time.Hour)),
			},
			{
				name:                "suspended deletion grace",
				status:              tenant.StatusSuspended,
				deletionRequestedAt: &deletionRequestedAt,
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				harness.setLifecycle(t, test.status, test.trialEnd, test.deletionRequestedAt)
				defer harness.setLifecycle(t, tenant.StatusActive, nil, nil)

				cookie, verified := harness.magicLink(t, "owner@acme.test")
				if verified.Code != http.StatusOK {
					t.Fatalf("verify status = %d, body = %s", verified.Code, verified.Body.String())
				}
				refreshed := harness.request(http.MethodPost, "/auth/refresh", "", "", cookie)
				if refreshed.Code != http.StatusOK {
					t.Fatalf("refresh status = %d, body = %s", refreshed.Code, refreshed.Body.String())
				}
			})
		}
	})

	ownerSecret := harness.enrollMember(t, "owner@acme.test")

	t.Run("inactive lifecycle rejects refresh without rotating the session", func(t *testing.T) {
		for _, test := range authenticationLifecycleCases() {
			t.Run(test.name, func(t *testing.T) {
				harness.setLifecycle(t, tenant.StatusActive, nil, nil)
				_, cookie := harness.signInExpectingChallenge(t, "owner@acme.test")
				sessionCount := harness.sessionCount(t)

				harness.setLifecycle(t, test.status, test.trialEnd, nil)
				defer harness.setLifecycle(t, tenant.StatusActive, nil, nil)
				response := harness.request(http.MethodPost, "/auth/refresh", "", "", cookie)
				if response.Code != http.StatusForbidden {
					t.Fatalf("refresh status = %d, body = %s", response.Code, response.Body.String())
				}
				if code := decodeErrorCode(t, response); code != "tenant_suspended" {
					t.Fatalf("refresh error code = %q, want tenant_suspended", code)
				}
				if got := harness.sessionCount(t); got != sessionCount {
					t.Fatalf("session rows = %d, want unchanged %d", got, sessionCount)
				}
				if !responseClearsCookie(response, tenantRefreshCookie) {
					t.Fatal("rejected refresh did not clear the refresh cookie")
				}
			})
		}
	})

	t.Run("inactive lifecycle rejects a pending two-factor challenge", func(t *testing.T) {
		for _, test := range authenticationLifecycleCases() {
			t.Run(test.name, func(t *testing.T) {
				harness.setLifecycle(t, tenant.StatusActive, nil, nil)
				_, cookie := harness.signInExpectingChallenge(t, "owner@acme.test")
				lastUsedStep := harness.lastUsedTOTPStep(t, "owner@acme.test")

				harness.setLifecycle(t, test.status, test.trialEnd, nil)
				defer harness.setLifecycle(t, tenant.StatusActive, nil, nil)
				response := harness.request(
					http.MethodPost, "/auth/2fa/challenge", harness.codeBody(t, ownerSecret), "", cookie,
				)
				if response.Code != http.StatusForbidden {
					t.Fatalf("challenge status = %d, body = %s", response.Code, response.Body.String())
				}
				if code := decodeErrorCode(t, response); code != "tenant_suspended" {
					t.Fatalf("challenge error code = %q, want tenant_suspended", code)
				}
				if state := harness.sessionTwoFactorState(t, cookie); state != TwoFactorStatePending {
					t.Fatalf("two-factor state = %q, want %q", state, TwoFactorStatePending)
				}
				if got := harness.lastUsedTOTPStep(t, "owner@acme.test"); got != lastUsedStep {
					t.Fatalf("last used TOTP step = %d, want unchanged %d", got, lastUsedStep)
				}
			})
		}
	})

	t.Run("enrolled member is challenged after primary authentication", func(t *testing.T) {
		requirement, cookie := harness.signInExpectingChallenge(t, "owner@acme.test")
		if requirement != TwoFactorRequirementChallenge {
			t.Fatalf("requirement = %q, want %q", requirement, TwoFactorRequirementChallenge)
		}
		// The pending session carries no access token, so nothing can be read
		// with it before the challenge succeeds.
		refresh := harness.request(http.MethodPost, "/auth/refresh", "", "", cookie)
		if got := decodeTwoFactorRequirement(t, refresh); got != TwoFactorRequirementChallenge {
			t.Fatalf("refresh requirement = %q, want %q", got, TwoFactorRequirementChallenge)
		}
		rotated := responseCookie(t, refresh, tenantRefreshCookie)

		wrong := harness.request(http.MethodPost, "/auth/2fa/challenge", `{"code":"000000"}`, "", rotated)
		if wrong.Code != http.StatusUnauthorized {
			t.Fatalf("wrong code status = %d, body = %s", wrong.Code, wrong.Body.String())
		}
		if !harness.hasAccessEvent("two_factor.challenge_failed") {
			t.Fatal("expected a failed challenge to be audited")
		}

		answered := harness.request(
			http.MethodPost, "/auth/2fa/challenge", harness.codeBody(t, ownerSecret), "", rotated,
		)
		session := decodeTenantSession(t, answered)
		if session.AccessToken == "" {
			t.Fatal("expected an access token after a successful challenge")
		}
		if members := harness.request(http.MethodGet, "/v1/members", "", session.AccessToken, nil); members.Code != http.StatusOK {
			t.Fatalf("member list status = %d, body = %s", members.Code, members.Body.String())
		}
		// Refreshing a satisfied session must not re-challenge it.
		refreshed := harness.request(http.MethodPost, "/auth/refresh", "", "", rotated)
		if got := decodeTwoFactorRequirement(t, refreshed); got != TwoFactorRequirementNone {
			t.Fatalf("satisfied refresh requirement = %q, want none", got)
		}
	})

	t.Run("recovery codes are single use", func(t *testing.T) {
		codes := harness.recoveryCodes(t, "owner@acme.test")
		_, cookie := harness.signInExpectingChallenge(t, "owner@acme.test")
		first := harness.request(
			http.MethodPost, "/auth/2fa/challenge", `{"code":"`+codes[0]+`"}`, "", cookie,
		)
		if session := decodeTenantSession(t, first); session.AccessToken == "" {
			t.Fatal("expected a recovery code to satisfy the challenge")
		}
		_, secondCookie := harness.signInExpectingChallenge(t, "owner@acme.test")
		replay := harness.request(
			http.MethodPost, "/auth/2fa/challenge", `{"code":"`+codes[0]+`"}`, "", secondCookie,
		)
		if replay.Code != http.StatusUnauthorized {
			t.Fatalf("reused recovery code status = %d, body = %s", replay.Code, replay.Body.String())
		}
		unused := harness.request(
			http.MethodPost, "/auth/2fa/challenge", `{"code":"`+codes[1]+`"}`, "", secondCookie,
		)
		if session := decodeTenantSession(t, unused); session.AccessToken == "" {
			t.Fatal("expected a second recovery code to work")
		}
		if !harness.hasAccessEvent("two_factor.recovery_code_used") {
			t.Fatal("expected recovery-code use to be audited")
		}
	})

	t.Run("SAML sessions bypass the Fidibeki challenge", func(t *testing.T) {
		// The session is stamped directly rather than driven through a real
		// assertion, which internal/samlsso covers end to end. The point here is
		// that the state machine recognises the method and never asks a
		// delegated session for a Fidibeki code. The identity provider and
		// subject are set too because a SAML session is required to be
		// attributable to the provider that asserted it.
		_, cookie := harness.signInExpectingChallenge(t, "owner@acme.test")
		if _, err := harness.root.Exec(harness.ctx, `
			UPDATE tenant_member_sessions
			SET authentication_method = 'saml', two_factor_state = 'not_required',
			    saml_idp_entity_id = 'https://idp.test/entity', saml_subject_id = 'owner@acme.test'
			WHERE tenant_id = $1 AND refresh_hash = $2
		`, harness.tenantID, auth.HashToken(cookie.Value)); err != nil {
			t.Fatalf("mark session as SAML: %v", err)
		}
		refreshed := harness.request(http.MethodPost, "/auth/refresh", "", "", cookie)
		if got := decodeTwoFactorRequirement(t, refreshed); got != TwoFactorRequirementNone {
			t.Fatalf("SAML requirement = %q, want none", got)
		}
		if session := decodeTenantSession(t, refreshed); session.AccessToken == "" {
			t.Fatal("expected a SAML session to refresh without a challenge")
		}
	})

	t.Run("required policy needs a recoverable administrator", func(t *testing.T) {
		memberToken := harness.signIn(t, "member@acme.test")
		denied := harness.request(
			http.MethodPatch, "/v1/security/two-factor/policy", `{"policy":"required"}`, memberToken, nil,
		)
		if denied.Code != http.StatusConflict {
			t.Fatalf("unenrolled administrator status = %d, body = %s", denied.Code, denied.Body.String())
		}
		if !harness.hasAccessEvent("two_factor.policy_denied") {
			t.Fatal("expected a denied policy change to be audited")
		}
		if policy := harness.policy(t, memberToken); policy != TwoFactorPolicyOptional {
			t.Fatalf("policy = %q, want %q", policy, TwoFactorPolicyOptional)
		}
	})

	t.Run("required policy blocks workspace access until enrollment", func(t *testing.T) {
		ownerToken := harness.signInAndChallenge(t, "owner@acme.test", ownerSecret)
		enabled := harness.request(
			http.MethodPatch, "/v1/security/two-factor/policy", `{"policy":"required"}`, ownerToken, nil,
		)
		if enabled.Code != http.StatusOK {
			t.Fatalf(
				"policy status = %d, body = %s, logs = %s",
				enabled.Code, enabled.Body.String(), harness.logs.String(),
			)
		}
		if !harness.hasAccessEvent("two_factor.policy_changed") {
			t.Fatal("expected the policy change to be audited")
		}

		// The unenrolled member's existing token stops reaching workspace data
		// immediately: there is no grace period.
		memberToken := harness.signIn(t, "member@acme.test")
		blocked := harness.request(http.MethodGet, "/v1/members", "", memberToken, nil)
		if blocked.Code != http.StatusForbidden {
			t.Fatalf("blocked status = %d, body = %s", blocked.Code, blocked.Body.String())
		}
		if code := decodeErrorCode(t, blocked); code != "two_factor_enrollment_required" {
			t.Fatalf("blocked code = %q", code)
		}
		// The enrolment endpoints stay reachable so the member can comply.
		status := harness.request(http.MethodGet, "/v1/security/two-factor", "", memberToken, nil)
		if status.Code != http.StatusOK {
			t.Fatalf("status endpoint = %d, body = %s", status.Code, status.Body.String())
		}
		secret := harness.completeEnrollment(t, memberToken)
		harness.memberSecrets["member@acme.test"] = secret
		if restored := harness.request(http.MethodGet, "/v1/members", "", memberToken, nil); restored.Code != http.StatusOK {
			t.Fatalf("restored status = %d, body = %s", restored.Code, restored.Body.String())
		}
	})

	t.Run("removal is refused while the policy requires two-factor", func(t *testing.T) {
		token := harness.signInAndChallenge(t, "owner@acme.test", ownerSecret)
		code := harness.currentCode(t, ownerSecret)
		refused := harness.request(
			http.MethodDelete, "/v1/security/two-factor", `{"code":"`+code+`"}`, token, nil,
		)
		if refused.Code != http.StatusConflict {
			t.Fatalf("removal status = %d, body = %s", refused.Code, refused.Body.String())
		}
		if !harness.hasAccessEvent("two_factor.removal_denied") {
			t.Fatal("expected a refused removal to be audited")
		}
	})

	t.Run("administrator sees compliance but never a secret", func(t *testing.T) {
		token := harness.signInAndChallenge(t, "owner@acme.test", ownerSecret)
		response := harness.request(http.MethodGet, "/v1/security/two-factor/compliance", "", token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("compliance status = %d, body = %s", response.Code, response.Body.String())
		}
		body := response.Body.String()
		for _, forbidden := range []string{"secret", "recovery_code", ownerSecret} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("compliance response leaks %q: %s", forbidden, body)
			}
		}
		var payload struct {
			Entries []TwoFactorCompliance `json:"two_factor_compliance"`
		}
		if err := json.NewDecoder(strings.NewReader(body)).Decode(&payload); err != nil {
			t.Fatalf("decode compliance: %v", err)
		}
		if len(payload.Entries) != 2 {
			t.Fatalf("compliance entries = %d, want 2", len(payload.Entries))
		}
		for _, entry := range payload.Entries {
			if !entry.Enrolled {
				t.Fatalf("member %s is not enrolled under a required policy", entry.Email)
			}
		}
	})

	t.Run("administrative reset revokes sessions and forces re-enrollment", func(t *testing.T) {
		token := harness.signInAndChallenge(t, "owner@acme.test", ownerSecret)
		memberID := harness.memberIDs["member@acme.test"]
		reset := harness.request(
			http.MethodPost, "/v1/members/"+memberID+"/two-factor/reset", "", token, nil,
		)
		if reset.Code != http.StatusNoContent {
			t.Fatalf("reset status = %d, body = %s", reset.Code, reset.Body.String())
		}
		if !harness.hasAccessEvent("two_factor.administrative_reset") {
			t.Fatal("expected the administrative reset to be audited")
		}
		var sessions int
		if err := harness.root.QueryRow(harness.ctx, `
			SELECT count(*) FROM tenant_member_sessions
			WHERE tenant_id = $1 AND member_id = $2 AND revoked_at IS NULL
		`, harness.tenantID, memberID).Scan(&sessions); err != nil {
			t.Fatalf("count member sessions: %v", err)
		}
		if sessions != 0 {
			t.Fatalf("active sessions after reset = %d, want 0", sessions)
		}
		var enrolments int
		if err := harness.root.QueryRow(harness.ctx, `
			SELECT count(*) FROM tenant_member_totp WHERE tenant_id = $1 AND member_id = $2
		`, harness.tenantID, memberID).Scan(&enrolments); err != nil {
			t.Fatalf("count enrolments: %v", err)
		}
		if enrolments != 0 {
			t.Fatalf("enrolments after reset = %d, want 0", enrolments)
		}
		// The member signs in again and is routed straight back into enrolment.
		memberToken := harness.signIn(t, "member@acme.test")
		blocked := harness.request(http.MethodGet, "/v1/members", "", memberToken, nil)
		if blocked.Code != http.StatusForbidden {
			t.Fatalf("post-reset access status = %d, body = %s", blocked.Code, blocked.Body.String())
		}
	})

	t.Run("secrets are encrypted at rest and recovery codes are hashed", func(t *testing.T) {
		var ciphertext []byte
		if err := harness.root.QueryRow(harness.ctx, `
			SELECT secret_ciphertext FROM tenant_member_totp
			WHERE tenant_id = $1 AND member_id = $2
		`, harness.tenantID, harness.memberIDs["owner@acme.test"]).Scan(&ciphertext); err != nil {
			t.Fatalf("read stored secret: %v", err)
		}
		if strings.Contains(string(ciphertext), ownerSecret) {
			t.Fatal("stored secret is not encrypted")
		}
		rows, err := harness.root.Query(harness.ctx, `
			SELECT code_hash FROM tenant_member_recovery_codes WHERE tenant_id = $1
		`, harness.tenantID)
		if err != nil {
			t.Fatalf("read recovery codes: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				t.Fatalf("scan recovery hash: %v", err)
			}
			if len(hash) != 64 || strings.Contains(hash, "-") {
				t.Fatalf("recovery code %q is not stored as a hash", hash)
			}
		}
	})

	t.Run("challenge attempts are rate limited", func(t *testing.T) {
		// The budget is reset so this subtest starts from a clean allowance
		// rather than inheriting the attempts earlier subtests spent.
		harness.limiter.limit, harness.limiter.attempts = 1, nil
		defer func() { harness.limiter.limit, harness.limiter.attempts = 1000, nil }()
		_, cookie := harness.signInExpectingChallenge(t, "owner@acme.test")
		first := harness.request(http.MethodPost, "/auth/2fa/challenge", `{"code":"000000"}`, "", cookie)
		if first.Code != http.StatusUnauthorized {
			t.Fatalf("first attempt status = %d, body = %s", first.Code, first.Body.String())
		}
		second := harness.request(http.MethodPost, "/auth/2fa/challenge", `{"code":"000000"}`, "", cookie)
		if second.Code != http.StatusTooManyRequests {
			t.Fatalf("second attempt status = %d, body = %s", second.Code, second.Body.String())
		}
		if second.Header().Get("Retry-After") == "" {
			t.Fatal("expected a Retry-After header on a throttled challenge")
		}
	})

	t.Run("policy returns to optional and removal then succeeds", func(t *testing.T) {
		token := harness.signInAndChallenge(t, "owner@acme.test", ownerSecret)
		relaxed := harness.request(
			http.MethodPatch, "/v1/security/two-factor/policy", `{"policy":"optional"}`, token, nil,
		)
		if relaxed.Code != http.StatusOK {
			t.Fatalf("relax policy status = %d, body = %s", relaxed.Code, relaxed.Body.String())
		}
		removed := harness.request(
			http.MethodDelete, "/v1/security/two-factor",
			`{"code":"`+harness.currentCode(t, ownerSecret)+`"}`, token, nil,
		)
		if removed.Code != http.StatusNoContent {
			t.Fatalf("removal status = %d, body = %s", removed.Code, removed.Body.String())
		}
		if !harness.hasAccessEvent("two_factor.removed") {
			t.Fatal("expected the removal to be audited")
		}
		// A member without an enrolment signs in without any challenge.
		next := harness.signIn(t, "owner@acme.test")
		if next == "" {
			t.Fatal("expected a plain sign-in after removal")
		}
	})

	t.Run("stale authentication cannot remove an enrollment", func(t *testing.T) {
		secret := harness.enrollMember(t, "owner@acme.test")
		token := harness.signInAndChallenge(t, "owner@acme.test", secret)
		if _, err := harness.root.Exec(harness.ctx, `
			UPDATE tenant_members SET last_login_at = now() - interval '2 hours'
			WHERE tenant_id = $1 AND id = $2
		`, harness.tenantID, harness.memberIDs["owner@acme.test"]); err != nil {
			t.Fatalf("age the sign-in: %v", err)
		}
		stale := harness.request(
			http.MethodDelete, "/v1/security/two-factor",
			`{"code":"`+harness.currentCode(t, secret)+`"}`, token, nil,
		)
		if stale.Code != http.StatusForbidden {
			t.Fatalf("stale removal status = %d, body = %s", stale.Code, stale.Body.String())
		}
		if code := decodeErrorCode(t, stale); code != "recent_authentication_required" {
			t.Fatalf("stale removal code = %q", code)
		}
	})
}

func newTwoFactorHarness(ctx context.Context, t *testing.T) *twoFactorHarness {
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
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, tenantMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}
	harness := &twoFactorHarness{
		t: t, ctx: ctx, root: root, memberIDs: map[string]string{},
		memberSecrets: map[string]string{},
		limiter:       &countingLimiter{limit: 1000},
		loginGuard:    &loginGuardStub{decision: LoginDecision{Allowed: true}},
		totpClock:     time.Now().UTC(),
		logs:          &bytes.Buffer{},
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&harness.tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	var policy string
	if err := root.QueryRow(ctx, `SELECT two_factor_policy FROM tenants WHERE id = $1`, harness.tenantID).
		Scan(&policy); err != nil || policy != TwoFactorPolicyOptional {
		t.Fatalf("default two-factor policy = %q, error = %v; want optional", policy, err)
	}
	for _, email := range []string{"owner@acme.test", "member@acme.test"} {
		var memberID string
		if err := root.QueryRow(ctx, `
			INSERT INTO tenant_members (tenant_id, email, permissions, last_login_at)
			VALUES ($1, $2, $3, now()) RETURNING id
		`, harness.tenantID, email, auth.TenantPermissionStrings()).Scan(&memberID); err != nil {
			t.Fatalf("create member %s: %v", email, err)
		}
		harness.memberIDs[email] = memberID
	}
	if _, err := root.Exec(ctx,
		`UPDATE tenants SET owner_member_id = $2 WHERE id = $1`,
		harness.tenantID, harness.memberIDs["owner@acme.test"],
	); err != nil {
		t.Fatalf("assign owner: %v", err)
	}

	pools, err := db.OpenPools(ctx, tenantRoleURL(t, directURL, "fidibeki_app", "app-password"), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	harness.jwt, _ = auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	harness.queue = &messageCapture{}
	store := Store{RefreshTTL: 30 * 24 * time.Hour, RefreshRetryGrace: 10 * time.Second}
	service := Service{
		Store: store, JWT: harness.jwt, PublicBaseURL: "http://api.localhost:8088",
		BaseDomain: "localhost", AuthFrom: "Fidibeki <auth@fidibeki.com>",
	}
	cipher, err := twofactor.NewCipher(twoFactorTestKey)
	if err != nil {
		t.Fatalf("create two-factor cipher: %v", err)
	}
	logger := slog.New(slog.NewJSONHandler(harness.logs, nil))
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Service: service, Store: store, JWT: harness.jwt, Email: harness.queue,
		TwoFactor: TwoFactorService{
			Store: store, Cipher: cipher,
			Now: func() time.Time { return harness.totpClock },
		},
		Limiter: harness.limiter, LoginGuard: harness.loginGuard,
		RefreshTTL: 30 * 24 * time.Hour, Logger: logger,
	})
	harness.handler = appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: logger,
	})(routes)
	return harness
}

func (h *twoFactorHarness) request(
	method, path, body, accessToken string,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	return performTenantRequest(h.handler, method, path, body, accessToken, cookie)
}

// signIn completes a magic-link sign-in for a member with no enrolment and
// returns the access token.
func (h *twoFactorHarness) signIn(t *testing.T, email string) string {
	t.Helper()
	_, recorder := h.magicLink(t, email)
	return decodeTenantSession(t, recorder).AccessToken
}

// signInExpectingChallenge completes primary authentication and asserts that no
// access token was issued.
func (h *twoFactorHarness) signInExpectingChallenge(t *testing.T, email string) (string, *http.Cookie) {
	t.Helper()
	cookie, recorder := h.magicLink(t, email)
	requirement := decodeTwoFactorRequirement(t, recorder)
	if strings.Contains(recorder.Body.String(), "access_token") {
		t.Fatalf("pending session received an access token: %s", recorder.Body.String())
	}
	return requirement, cookie
}

func (h *twoFactorHarness) signInAndChallenge(t *testing.T, email, secret string) string {
	t.Helper()
	_, cookie := h.signInExpectingChallenge(t, email)
	answered := h.request(http.MethodPost, "/auth/2fa/challenge", h.codeBody(t, secret), "", cookie)
	return decodeTenantSession(t, answered).AccessToken
}

func (h *twoFactorHarness) magicLink(t *testing.T, email string) (*http.Cookie, *httptest.ResponseRecorder) {
	t.Helper()
	before := len(h.queue.messages)
	login := h.request(http.MethodPost, "/auth/login", `{"email":"`+email+`"}`, "", nil)
	if login.Code != http.StatusAccepted || len(h.queue.messages) != before+1 {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	token := messageToken(t, h.queue.messages[len(h.queue.messages)-1])
	verified := h.request(http.MethodPost, "/auth/verify", `{"token":"`+token+`"}`, "", nil)
	if verified.Code != http.StatusOK {
		t.Fatalf("verify status = %d, body = %s", verified.Code, verified.Body.String())
	}
	return responseCookie(t, verified, tenantRefreshCookie), verified
}

// enrollMember runs the full enrolment for a member that can still sign in
// without a challenge, and returns the shared secret.
func (h *twoFactorHarness) enrollMember(t *testing.T, email string) string {
	t.Helper()
	token := h.signIn(t, email)
	secret := h.completeEnrollment(t, token)
	h.memberSecrets[email] = secret
	return secret
}

func (h *twoFactorHarness) completeEnrollment(t *testing.T, accessToken string) string {
	t.Helper()
	started := h.request(http.MethodPost, "/v1/security/two-factor/enroll", "", accessToken, nil)
	if started.Code != http.StatusCreated {
		t.Fatalf("enroll status = %d, body = %s", started.Code, started.Body.String())
	}
	var payload struct {
		Enrollment TwoFactorEnrollment `json:"enrollment"`
	}
	if err := json.NewDecoder(started.Body).Decode(&payload); err != nil {
		t.Fatalf("decode enrollment: %v", err)
	}
	if payload.Enrollment.Secret == "" || !strings.HasPrefix(payload.Enrollment.QRCodeSVG, "<svg ") {
		t.Fatalf("incomplete enrollment payload: %#v", payload.Enrollment)
	}
	if !strings.Contains(payload.Enrollment.ProvisioningURI, "secret="+payload.Enrollment.Secret) {
		t.Fatalf("provisioning URI does not carry the secret: %s", payload.Enrollment.ProvisioningURI)
	}
	confirmed := h.request(
		http.MethodPost, "/v1/security/two-factor/confirm",
		h.codeBody(t, payload.Enrollment.Secret), accessToken, nil,
	)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, body = %s", confirmed.Code, confirmed.Body.String())
	}
	var codes struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.NewDecoder(confirmed.Body).Decode(&codes); err != nil {
		t.Fatalf("decode recovery codes: %v", err)
	}
	if len(codes.RecoveryCodes) != twofactor.RecoveryCodeCount {
		t.Fatalf("recovery codes = %d, want %d", len(codes.RecoveryCodes), twofactor.RecoveryCodeCount)
	}
	// The status endpoint must never return the secret again.
	status := h.request(http.MethodGet, "/v1/security/two-factor", "", accessToken, nil)
	if strings.Contains(status.Body.String(), payload.Enrollment.Secret) {
		t.Fatalf("status endpoint returned the secret: %s", status.Body.String())
	}
	return payload.Enrollment.Secret
}

// recoveryCodes regenerates and returns a member's codes.
func (h *twoFactorHarness) recoveryCodes(t *testing.T, email string) []string {
	t.Helper()
	secret := h.memberSecrets[email]
	token := h.signInAndChallenge(t, email, secret)
	response := h.request(
		http.MethodPost, "/v1/security/two-factor/recovery-codes",
		h.codeBody(t, secret), token, nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("regenerate status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode regenerated codes: %v", err)
	}
	return payload.RecoveryCodes
}

func (h *twoFactorHarness) policy(t *testing.T, accessToken string) string {
	t.Helper()
	response := h.request(http.MethodGet, "/v1/security/two-factor/policy", "", accessToken, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("policy status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Policy TwoFactorPolicy `json:"two_factor_policy"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	return payload.Policy.Policy
}

// currentCode advances the shared clock to the next time step and derives the
// code an authenticator would show there.
func (h *twoFactorHarness) currentCode(t *testing.T, secret string) string {
	t.Helper()
	h.totpClock = h.totpClock.Add(twofactor.PeriodSeconds * time.Second)
	code, err := twofactor.Code(secret, twofactor.Step(h.totpClock))
	if err != nil {
		t.Fatalf("derive code: %v", err)
	}
	return code
}

func (h *twoFactorHarness) codeBody(t *testing.T, secret string) string {
	t.Helper()
	return `{"code":"` + h.currentCode(t, secret) + `"}`
}

func (h *twoFactorHarness) hasAccessEvent(kind string) bool {
	var count int
	if err := h.root.QueryRow(h.ctx, `
		SELECT count(*) FROM tenant_access_events WHERE tenant_id = $1 AND kind = $2
	`, h.tenantID, kind).Scan(&count); err != nil {
		h.t.Fatalf("count access events: %v", err)
	}
	return count > 0
}

func (h *twoFactorHarness) setLifecycle(
	t *testing.T,
	status tenant.Status,
	trialEndsAt *time.Time,
	deletionRequestedAt *time.Time,
) {
	t.Helper()
	if _, err := h.root.Exec(h.ctx, `
		UPDATE tenants
		SET status = $2, trial_ends_at = $3, deletion_requested_at = $4
		WHERE id = $1
	`, h.tenantID, status, trialEndsAt, deletionRequestedAt); err != nil {
		t.Fatalf("update tenant lifecycle: %v", err)
	}
}

func (h *twoFactorHarness) magicLinkCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := h.root.QueryRow(
		h.ctx, `SELECT count(*) FROM tenant_magic_links WHERE tenant_id = $1`, h.tenantID,
	).Scan(&count); err != nil {
		t.Fatalf("count tenant magic links: %v", err)
	}
	return count
}

func (h *twoFactorHarness) magicLinkConsumed(t *testing.T, rawToken string) bool {
	t.Helper()
	var consumed bool
	if err := h.root.QueryRow(h.ctx, `
		SELECT consumed_at IS NOT NULL
		FROM tenant_magic_links
		WHERE tenant_id = $1 AND token_hash = $2
	`, h.tenantID, auth.HashToken(rawToken)).Scan(&consumed); err != nil {
		t.Fatalf("read tenant magic link: %v", err)
	}
	return consumed
}

func (h *twoFactorHarness) sessionCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := h.root.QueryRow(
		h.ctx, `SELECT count(*) FROM tenant_member_sessions WHERE tenant_id = $1`, h.tenantID,
	).Scan(&count); err != nil {
		t.Fatalf("count tenant member sessions: %v", err)
	}
	return count
}

func (h *twoFactorHarness) sessionTwoFactorState(t *testing.T, cookie *http.Cookie) string {
	t.Helper()
	var state string
	if err := h.root.QueryRow(h.ctx, `
		SELECT two_factor_state
		FROM tenant_member_sessions
		WHERE tenant_id = $1 AND refresh_hash = $2
	`, h.tenantID, auth.HashToken(cookie.Value)).Scan(&state); err != nil {
		t.Fatalf("read tenant member session: %v", err)
	}
	return state
}

func (h *twoFactorHarness) lastUsedTOTPStep(t *testing.T, email string) int64 {
	t.Helper()
	var step int64
	if err := h.root.QueryRow(h.ctx, `
		SELECT COALESCE(last_used_step, -1)
		FROM tenant_member_totp
		WHERE tenant_id = $1 AND member_id = $2
	`, h.tenantID, h.memberIDs[email]).Scan(&step); err != nil {
		t.Fatalf("read last used TOTP step: %v", err)
	}
	return step
}

type authenticationLifecycleCase struct {
	name     string
	status   tenant.Status
	trialEnd *time.Time
}

func authenticationLifecycleCases() []authenticationLifecycleCase {
	return []authenticationLifecycleCase{
		{name: "suspended", status: tenant.StatusSuspended},
		{name: "cancelled", status: tenant.StatusCancelled},
		{
			name:     "expired trial",
			status:   tenant.StatusTrial,
			trialEnd: ptrTime(time.Now().Add(-time.Hour)),
		},
	}
}

func responseClearsCookie(recorder *httptest.ResponseRecorder, name string) bool {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}

func decodeTwoFactorRequirement(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("session status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		TwoFactor struct {
			Status string `json:"status"`
		} `json:"two_factor"`
	}
	if err := json.NewDecoder(strings.NewReader(recorder.Body.String())).Decode(&payload); err != nil {
		t.Fatalf("decode two-factor requirement: %v", err)
	}
	return payload.TwoFactor.Status
}

func decodeErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(strings.NewReader(recorder.Body.String())).Decode(&payload); err != nil {
		t.Fatalf("decode error code: %v", err)
	}
	return payload.Error.Code
}
