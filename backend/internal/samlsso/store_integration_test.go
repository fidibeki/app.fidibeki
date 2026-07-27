package samlsso

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/integration"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/tenantmember"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestSAMLSSOStoreAcrossTenants exercises the full server-side flow against a
// real PostgreSQL instance with row-level security forced, for two
// organizations with different identity providers. It covers the properties
// that only hold end to end: tenant isolation, single-use correlation,
// assertion replay, member eligibility, and the authentication method landing
// on the resulting session.
func TestSAMLSSOStoreAcrossTenants(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newSAMLHarness(ctx, t)
	acme := harness.workspace(ctx, t, "acme", "Acme")
	globex := harness.workspace(ctx, t, "globex", "Globex")

	t.Run("configuration is organization scoped and disabled by default", func(t *testing.T) {
		harness.withTenant(ctx, t, globex, func(query db.Querier) {
			if _, err := harness.store.Get(ctx, query, globex.id); !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("Get(globex) error = %v, want ErrNotConfigured before configuration", err)
			}
		})
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			configuration, err := harness.store.Get(ctx, query, acme.id)
			if err != nil {
				t.Fatalf("Get(acme) error = %v", err)
			}
			if configuration.Mode != auth.SSOModeDisabled {
				t.Errorf("Mode = %q, want disabled by default", configuration.Mode)
			}
			if !configuration.HasValidCertificate(time.Now()) {
				t.Error("configuration has no valid signing certificate")
			}
		})
	})

	t.Run("required mode is refused until a test assertion succeeds", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			if _, err := harness.service.SetMode(
				ctx, query, acme.resolved, auth.SSOModeRequired, acme.ownerID,
			); !errors.Is(err, ErrTestRequired) {
				t.Fatalf("SetMode(required) error = %v, want ErrTestRequired", err)
			}
			if _, err := harness.service.SetMode(
				ctx, query, acme.resolved, auth.SSOModeOptional, acme.ownerID,
			); err != nil {
				t.Fatalf("SetMode(optional) error = %v", err)
			}
		})
	})

	t.Run("a validated assertion signs an existing member in as a SAML session", func(t *testing.T) {
		var handoffToken string
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			requestID := harness.startLogin(ctx, t, query, acme, PurposeLogin)
			outcome, err := harness.service.ConsumeAssertion(
				ctx, query, acme.resolved,
				acme.idp.response(t, harness.assertion(t, acme, requestID, "owner@acme.test")),
			)
			if err != nil {
				t.Fatalf("ConsumeAssertion() error = %v", err)
			}
			if outcome.MemberID != acme.ownerID {
				t.Errorf("MemberID = %q, want the existing owner %q", outcome.MemberID, acme.ownerID)
			}
			handoffToken = tokenFromFragment(t, outcome.RedirectURL)
		})

		// The handoff is exchanged the same way an emailed magic link is, so
		// the session it produces goes through the proven issuing path.
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			member, security, err := harness.members.ConsumeMagicLink(
				ctx, query, acme.id, auth.HashToken(handoffToken),
				strings.Repeat("a", 64), "test-agent", "203.0.113.10",
			)
			if err != nil {
				t.Fatalf("ConsumeMagicLink() error = %v", err)
			}
			if member.ID != acme.ownerID {
				t.Errorf("member = %q, want %q", member.ID, acme.ownerID)
			}
			if !security.IsSAML() || security.SAMLIdPEntityID != acme.idp.entityID {
				t.Fatalf("session security = %#v, want a SAML session from the configured IdP", security)
			}
			// The identity provider owns multi-factor assurance, so Fidibeki
			// must never ask this session for a TOTP code.
			if security.Requirement() != tenantmember.TwoFactorRequirementNone {
				t.Fatalf("Requirement() = %q, want no Fidibeki challenge for a SAML session",
					security.Requirement())
			}
		})

		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			var method, idpEntityID, subjectID string
			if err := query.QueryRow(ctx, `
				SELECT authentication_method, saml_idp_entity_id, saml_subject_id
				FROM tenant_member_sessions
				WHERE tenant_id = $1 AND member_id = $2
				ORDER BY issued_at DESC LIMIT 1
			`, acme.id, acme.ownerID).Scan(&method, &idpEntityID, &subjectID); err != nil {
				t.Fatalf("read session provenance: %v", err)
			}
			if method != tenantmember.AuthenticationMethodSAML || idpEntityID != acme.idp.entityID {
				t.Fatalf(
					"session provenance = %q/%q, want a SAML session from %q",
					method, idpEntityID, acme.idp.entityID,
				)
			}
			if subjectID != "owner@acme.test" {
				t.Errorf("saml_subject_id = %q, want the asserted subject", subjectID)
			}
		})
	})

	// A solicited response is stopped by correlation before the replay ledger
	// is reached: consuming the AuthnRequest removes it from the live set, so
	// the second attempt fails signature-stage validation. Both a byte-identical
	// replay and a freshly minted assertion quoting the same request are
	// rejected.
	t.Run("a replayed solicited response is rejected at correlation", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			requestID := harness.startLogin(ctx, t, query, acme, PurposeLogin)
			response := acme.idp.response(t, harness.assertion(t, acme, requestID, "owner@acme.test"))
			if _, err := harness.service.ConsumeAssertion(ctx, query, acme.resolved, response); err != nil {
				t.Fatalf("first ConsumeAssertion() error = %v", err)
			}
			if _, err := harness.service.ConsumeAssertion(
				ctx, query, acme.resolved, response,
			); !errors.Is(err, ErrAssertionInvalid) {
				t.Fatalf("replayed ConsumeAssertion() error = %v, want a rejection", err)
			}
			second := harness.assertion(t, acme, requestID, "owner@acme.test")
			second.assertionID = "_assertion-second"
			if _, err := harness.service.ConsumeAssertion(
				ctx, query, acme.resolved, acme.idp.response(t, second),
			); !errors.Is(err, ErrAssertionInvalid) {
				t.Fatalf("reused request ConsumeAssertion() error = %v, want a rejection", err)
			}
		})
	})

	// The assertion-ID ledger is the operative replay control for IdP-initiated
	// SSO, where there is no AuthnRequest to consume and correlation therefore
	// cannot reject anything.
	t.Run("a replayed unsolicited assertion is rejected by the ledger", func(t *testing.T) {
		harness.withTenant(ctx, t, globex, func(query db.Querier) {
			if _, err := harness.service.Configure(
				ctx, query, globex.resolved, ConfigurationInput{
					IdPEntityID: globex.idp.entityID, IdPSSOURL: "https://idp.test/sso",
					Certificates:      []string{globex.idp.storedCertificate(t).Certificate},
					AllowIDPInitiated: true,
				}, globex.ownerID,
			); err != nil {
				t.Fatalf("Configure(globex) error = %v", err)
			}
			if _, err := harness.service.SetMode(
				ctx, query, globex.resolved, auth.SSOModeOptional, globex.ownerID,
			); err != nil {
				t.Fatalf("SetMode(globex optional) error = %v", err)
			}
			unsolicited := harness.assertion(t, globex, "unsolicited", "owner@acme.test")
			unsolicited.inResponseTo = ""
			response := globex.idp.response(t, unsolicited)
			if _, err := harness.service.ConsumeAssertion(ctx, query, globex.resolved, response); err != nil {
				t.Fatalf("first unsolicited ConsumeAssertion() error = %v", err)
			}
			if _, err := harness.service.ConsumeAssertion(
				ctx, query, globex.resolved, response,
			); !errors.Is(err, ErrAssertionReplayed) {
				t.Fatalf("replayed unsolicited ConsumeAssertion() error = %v, want ErrAssertionReplayed", err)
			}
		})
	})

	t.Run("an assertion for a non-member is rejected", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			requestID := harness.startLogin(ctx, t, query, acme, PurposeLogin)
			if _, err := harness.service.ConsumeAssertion(
				ctx, query, acme.resolved,
				acme.idp.response(t, harness.assertion(t, acme, requestID, "stranger@acme.test")),
			); !errors.Is(err, ErrMemberNotEligible) {
				t.Fatalf("ConsumeAssertion(stranger) error = %v, want ErrMemberNotEligible", err)
			}
		})
	})

	// Matching emails across organizations must never link an identity. Globex
	// deliberately has a member with the same address as Acme's owner.
	t.Run("an identity is never linked across organizations by email", func(t *testing.T) {
		harness.withTenant(ctx, t, globex, func(query db.Querier) {
			requestID := harness.startLogin(ctx, t, query, globex, PurposeLogin)
			// Signed by Acme's identity provider, replayed at Globex.
			options := harness.assertion(t, globex, requestID, "owner@acme.test")
			if _, err := harness.service.ConsumeAssertion(
				ctx, query, globex.resolved, acme.idp.response(t, options),
			); !errors.Is(err, ErrAssertionInvalid) {
				t.Fatalf("cross-tenant ConsumeAssertion() error = %v, want ErrAssertionInvalid", err)
			}
		})
	})

	t.Run("row-level security isolates each organization's configuration", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			var visible int
			if err := query.QueryRow(ctx,
				`SELECT count(*) FROM tenant_saml_configurations`,
			).Scan(&visible); err != nil {
				t.Fatalf("count visible configurations: %v", err)
			}
			if visible != 1 {
				t.Fatalf("visible configurations = %d, want only this organization's", visible)
			}
			if _, err := harness.store.Get(ctx, query, globex.id); !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("Get(globex from acme) error = %v, want it invisible", err)
			}
		})
	})

	t.Run("an outstanding invitation is the only path that creates a member", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			if err := harness.members.CreateMagicLink(
				ctx, query, acme.id, nil, "invited@acme.test", []string{"feedback:read"},
				strings.Repeat("b", 64), time.Now().Add(time.Hour),
			); err != nil {
				t.Fatalf("CreateMagicLink(invitation) error = %v", err)
			}
			requestID := harness.startLogin(ctx, t, query, acme, PurposeLogin)
			outcome, err := harness.service.ConsumeAssertion(
				ctx, query, acme.resolved,
				acme.idp.response(t, harness.assertion(t, acme, requestID, "invited@acme.test")),
			)
			if err != nil {
				t.Fatalf("ConsumeAssertion(invited) error = %v", err)
			}
			var permissions []string
			if err := query.QueryRow(ctx,
				`SELECT permissions FROM tenant_members WHERE tenant_id = $1 AND id = $2`,
				acme.id, outcome.MemberID,
			).Scan(&permissions); err != nil {
				t.Fatalf("read accepted member: %v", err)
			}
			// SAML authenticates identity; the permissions come from the
			// invitation Fidibeki issued, never from the assertion.
			if len(permissions) != 1 || permissions[0] != "feedback:read" {
				t.Fatalf("permissions = %v, want exactly the invited permissions", permissions)
			}
		})
	})

	t.Run("an outstanding invitation cannot exceed current member capacity", func(t *testing.T) {
		var fillerID string
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			if err := harness.members.CreateMagicLink(
				ctx, query, acme.id, nil, "capacity@acme.test", []string{"feedback:read"},
				strings.Repeat("c", 64), time.Now().Add(time.Hour),
			); err != nil {
				t.Fatalf("CreateMagicLink(capacity invitation) error = %v", err)
			}
			if err := harness.root.QueryRow(ctx, `
				INSERT INTO tenant_members (tenant_id, email, permissions)
				VALUES ($1, 'capacity-filler@acme.test', '{}') RETURNING id
			`, acme.id).Scan(&fillerID); err != nil {
				t.Fatalf("fill growth member capacity: %v", err)
			}
			requestID := harness.startLogin(ctx, t, query, acme, PurposeLogin)
			if _, err := harness.service.ConsumeAssertion(
				ctx, query, acme.resolved,
				acme.idp.response(t, harness.assertion(t, acme, requestID, "capacity@acme.test")),
			); !errors.Is(err, ErrPlanMemberLimit) {
				t.Fatalf("ConsumeAssertion(capacity) error = %v, want ErrPlanMemberLimit", err)
			}
			var created int
			if err := query.QueryRow(ctx, `
				SELECT count(*) FROM tenant_members
				WHERE tenant_id = $1 AND email = 'capacity@acme.test'
			`, acme.id).Scan(&created); err != nil || created != 0 {
				t.Fatalf("capacity invitation created members = %d, error = %v", created, err)
			}
		})
		if _, err := harness.root.Exec(ctx, `
			UPDATE tenant_members SET status = 'removed' WHERE tenant_id = $1 AND id = $2
		`, acme.id, fillerID); err != nil {
			t.Fatalf("release SAML capacity filler: %v", err)
		}
	})

	t.Run("required mode closes local login except for the owner", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			requestID := harness.startLogin(ctx, t, query, acme, PurposeTest)
			if _, err := harness.service.ConsumeAssertion(
				ctx, query, acme.resolved,
				acme.idp.response(t, harness.assertion(t, acme, requestID, "owner@acme.test")),
			); err != nil {
				t.Fatalf("test ConsumeAssertion() error = %v", err)
			}
			if _, err := harness.service.SetMode(
				ctx, query, acme.resolved, auth.SSOModeRequired, acme.ownerID,
			); err != nil {
				t.Fatalf("SetMode(required) error = %v", err)
			}
		})
		memberService := tenantmember.Service{
			Store: harness.members, PublicBaseURL: "http://api.localhost:3000",
			BaseDomain: "localhost", AuthFrom: "Fidibeki <auth@fidibeki.test>",
		}
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			if _, err := memberService.RequestMagicLink(
				ctx, query, acme.resolved, "invited@acme.test",
			); !errors.Is(err, tenantmember.ErrLocalLoginDisabled) {
				t.Fatalf("member RequestMagicLink() error = %v, want ErrLocalLoginDisabled", err)
			}
			member, breakGlass, err := memberService.BreakGlassLogin(
				ctx, query, acme.resolved, "owner@acme.test",
			)
			if err != nil || !breakGlass || member.ID != acme.ownerID {
				t.Fatalf("BreakGlassLogin(owner) = %q, %v, %v, want the owner recovery path",
					member.ID, breakGlass, err)
			}
			if _, err := memberService.RequestMagicLink(
				ctx, query, acme.resolved, "owner@acme.test",
			); err != nil {
				t.Fatalf("owner RequestMagicLink() error = %v, want the break-glass path to work", err)
			}
		})
	})

	// Required SSO hands multi-factor assurance to the identity provider, so
	// the Fidibeki-managed control goes inert. The stored policy and any
	// enrolment survive untouched, ready to apply again if local
	// authentication is restored.
	t.Run("required mode disables the Fidibeki two-factor control", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			policy, err := harness.members.TwoFactorPolicy(ctx, query, acme.id)
			if err != nil {
				t.Fatalf("TwoFactorPolicy() error = %v", err)
			}
			if !policy.ManagedByIdP {
				t.Fatalf("ManagedByIdP = false under required SSO, want the control reported inert")
			}
			if _, err := harness.members.SetTwoFactorPolicy(
				ctx, query, acme.id, acme.ownerID, tenantmember.TwoFactorPolicyRequired,
			); !errors.Is(err, tenantmember.ErrTwoFactorManagedByIdP) {
				t.Fatalf("SetTwoFactorPolicy() error = %v, want ErrTwoFactorManagedByIdP", err)
			}
			// The underlying policy must be preserved, not rewritten.
			if policy.Policy != tenantmember.TwoFactorPolicyOptional {
				t.Fatalf("stored policy = %q, want the pre-SSO value preserved", policy.Policy)
			}
		})
	})

	t.Run("removing the last valid certificate drops the workspace out of required", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			configuration, err := harness.store.Get(ctx, query, acme.id)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if len(configuration.Certificates) != 1 {
				t.Fatalf("certificates = %d, want a single certificate for this case",
					len(configuration.Certificates))
			}
			// The only certificate is protected while SSO is enabled.
			if _, err := harness.service.RemoveCertificate(
				ctx, query, acme.resolved, configuration.Certificates[0].ID, acme.ownerID,
			); !errors.Is(err, ErrLastCertificate) {
				t.Fatalf("RemoveCertificate(last) error = %v, want ErrLastCertificate", err)
			}
			successor := newTestIdP(t, acme.idp.entityID).storedCertificate(t)
			if err := harness.store.AddCertificate(ctx, query, acme.id, successor); err != nil {
				t.Fatalf("AddCertificate() error = %v", err)
			}
			updated, err := harness.service.RemoveCertificate(
				ctx, query, acme.resolved, configuration.Certificates[0].ID, acme.ownerID,
			)
			if err != nil {
				t.Fatalf("RemoveCertificate() error = %v", err)
			}
			// A valid successor remains, so enforcement is untouched: this is
			// what makes rotation outage-free.
			if updated.Mode != auth.SSOModeRequired {
				t.Fatalf("Mode = %q, want required to survive a rotation", updated.Mode)
			}
		})
	})

	// The login page reads this before any credential is entered, so it must
	// describe the organization's policy and nothing about its members.
	t.Run("policy discovery reports organization state only", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			policy, err := harness.service.Policy(ctx, query, acme.resolved)
			if err != nil {
				t.Fatalf("Policy() error = %v", err)
			}
			if policy.Mode != auth.SSOModeRequired {
				t.Fatalf("Mode = %q, want required", policy.Mode)
			}
			if policy.LocalLoginEnabled {
				t.Error("LocalLoginEnabled = true under required mode")
			}
			if !policy.BreakGlassAvailable {
				t.Error("BreakGlassAvailable = false under required mode")
			}
			if policy.StartURL == "" {
				t.Error("StartURL is empty, want the sign-in entry point")
			}
		})
		// A workspace that never configured SSO is indistinguishable from one
		// that disabled it: both report the unchanged local-login default.
		unconfigured := harness.workspace(ctx, t, "initech", "Initech")
		harness.withTenant(ctx, t, unconfigured, func(query db.Querier) {
			policy, err := harness.service.Policy(ctx, query, unconfigured.resolved)
			if err != nil {
				t.Fatalf("Policy(unconfigured) error = %v", err)
			}
			if policy.Mode != auth.SSOModeDisabled || !policy.LocalLoginEnabled {
				t.Fatalf("policy = %#v, want the disabled local-login default", policy)
			}
			if policy.StartURL != "" || policy.BreakGlassAvailable {
				t.Fatalf("policy = %#v, want no SSO entry point advertised", policy)
			}
		})
	})

	t.Run("service provider metadata is rendered for the identity provider", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			document, err := harness.service.Metadata(ctx, query, acme.resolved)
			if err != nil {
				t.Fatalf("Metadata() error = %v", err)
			}
			rendered := string(document)
			// Derived from the harness rather than written out, so the test
			// cannot drift from the environment the endpoints are built from.
			entityID, err := harness.service.Endpoints.EntityID(acme.slug)
			if err != nil {
				t.Fatalf("EntityID() error = %v", err)
			}
			acs, err := harness.service.Endpoints.ACS(acme.slug)
			if err != nil {
				t.Fatalf("ACS() error = %v", err)
			}
			for _, want := range []string{entityID, acs, "AssertionConsumerService"} {
				if !strings.Contains(rendered, want) {
					t.Errorf("metadata does not contain %q", want)
				}
			}
		})
	})

	t.Run("a failed test is recorded without disturbing the tested marker", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			if err := harness.service.RecordTestFailure(
				ctx, query, acme.id, "assertion_invalid",
			); err != nil {
				t.Fatalf("RecordTestFailure() error = %v", err)
			}
			configuration, err := harness.store.Get(ctx, query, acme.id)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if configuration.LastTestError == nil || *configuration.LastTestError != "assertion_invalid" {
				t.Fatalf("LastTestError = %v, want the recorded reason", configuration.LastTestError)
			}
			// A failure must not retract the earlier success that unlocked
			// required mode, or a transient error would silently disable SSO.
			if !configuration.Tested() {
				t.Error("Tested() = false, want the earlier successful test preserved")
			}
		})
	})

	t.Run("removing the configuration restores local authentication", func(t *testing.T) {
		harness.withTenant(ctx, t, globex, func(query db.Querier) {
			if err := harness.store.Delete(ctx, query, globex.id); err != nil {
				t.Fatalf("Delete() error = %v", err)
			}
			if _, err := harness.store.Get(ctx, query, globex.id); !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("Get() error = %v, want ErrNotConfigured after removal", err)
			}
			// Removing a configuration that is not there is an error, not a
			// silent success, so a mistaken second call is visible.
			if err := harness.store.Delete(ctx, query, globex.id); !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("second Delete() error = %v, want ErrNotConfigured", err)
			}
			mode, err := harness.members.SSOMode(ctx, query, globex.id)
			if err != nil || mode != auth.SSOModeDisabled {
				t.Fatalf("SSOMode() = %q, %v, want disabled after removal", mode, err)
			}
		})
	})

	t.Run("expired artifacts are swept once they can no longer be accepted", func(t *testing.T) {
		harness.withTenant(ctx, t, acme, func(query db.Querier) {
			// Written directly so both timestamps sit in the past; the table
			// refuses an expiry that precedes its own creation.
			if _, err := query.Exec(ctx, `
				INSERT INTO tenant_saml_auth_requests (id, tenant_id, purpose, created_at, expires_at)
				VALUES ('_expired-request', $1, 'login', $2, $3)
			`, acme.id, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)); err != nil {
				t.Fatalf("insert expired correlation row: %v", err)
			}
		})
		purged, err := (MaintenanceStore{Database: harness.pools.Admin}).
			ExpiredArtifacts(ctx, time.Now())
		if err != nil {
			t.Fatalf("ExpiredArtifacts() error = %v", err)
		}
		if purged == 0 {
			t.Fatal("ExpiredArtifacts() purged nothing, want the expired correlation row removed")
		}
	})
}

type samlHarness struct {
	pools   *db.Pools
	root    *pgx.Conn
	store   Store
	service Service
	members tenantmember.Store
}

type samlWorkspace struct {
	id       string
	slug     string
	ownerID  string
	resolved tenant.Tenant
	idp      *testIdP
}

func newSAMLHarness(ctx context.Context, t *testing.T) *samlHarness {
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
	migrator, err := db.NewMigrator(directURL, samlMigrationsPath(t))
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
	appURL, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	appURL.User = url.UserPassword("fidibeki_app", "app-password")
	pools, err := db.OpenPools(ctx, appURL.String(), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)

	cipher, err := integration.NewCipher(
		base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))),
	)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	harness := &samlHarness{
		pools: pools, store: Store{Cipher: cipher}, members: tenantmember.Store{},
	}
	harness.service = Service{
		Store: harness.store, MagicLinks: harness.members,
		Endpoints: Endpoints{WebBaseURL: "http://api.localhost:3000", BaseDomain: "localhost"},
	}
	harness.root = root
	return harness
}

func (h *samlHarness) workspace(ctx context.Context, t *testing.T, slug, name string) samlWorkspace {
	t.Helper()
	workspace := samlWorkspace{
		slug: slug, idp: newTestIdP(t, "https://idp.test/"+slug),
	}
	if err := h.root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ($1, $2, 'growth', 'active') RETURNING id
	`, name, slug).Scan(&workspace.id); err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}
	if err := h.root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2) RETURNING id
	`, workspace.id, auth.TenantPermissionStrings()).Scan(&workspace.ownerID); err != nil {
		t.Fatalf("create owner for %s: %v", slug, err)
	}
	if _, err := h.root.Exec(ctx,
		`UPDATE tenants SET owner_member_id = $2 WHERE id = $1`, workspace.id, workspace.ownerID,
	); err != nil {
		t.Fatalf("assign owner for %s: %v", slug, err)
	}
	workspace.resolved = tenant.Tenant{
		ID: workspace.id, Name: name, Slug: slug,
		Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
	}
	if slug == "acme" {
		h.configure(ctx, t, workspace)
	}
	return workspace
}

// member adds an ordinary workspace member, used to prove the configuration
// surface is closed to anyone without workspace administration.
func (h *samlHarness) member(
	ctx context.Context, t *testing.T, workspace samlWorkspace, email string, permissions []string,
) string {
	t.Helper()
	var id string
	if err := h.root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, $2, $3) RETURNING id
	`, workspace.id, email, permissions).Scan(&id); err != nil {
		t.Fatalf("create member %s: %v", email, err)
	}
	return id
}

func (h *samlHarness) configure(ctx context.Context, t *testing.T, workspace samlWorkspace) {
	t.Helper()
	certificate := workspace.idp.storedCertificate(t)
	h.withTenant(ctx, t, workspace, func(query db.Querier) {
		if _, err := h.service.Configure(ctx, query, workspace.resolved, ConfigurationInput{
			IdPEntityID: workspace.idp.entityID, IdPSSOURL: "https://idp.test/sso",
			Certificates: []string{certificate.Certificate},
		}, workspace.ownerID); err != nil {
			t.Fatalf("Configure(%s) error = %v", workspace.slug, err)
		}
	})
}

// withTenant runs fn inside an app-pool transaction with app.tenant_id set,
// exactly as the tenant HTTP middleware does, so row-level security applies.
func (h *samlHarness) withTenant(
	ctx context.Context, t *testing.T, workspace samlWorkspace, fn func(db.Querier),
) {
	t.Helper()
	transaction, err := h.pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tenant transaction: %v", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var configured string
	if err := transaction.QueryRow(ctx,
		`SELECT set_config('app.tenant_id', $1, true)`, workspace.id,
	).Scan(&configured); err != nil {
		t.Fatalf("set tenant scope: %v", err)
	}
	fn(transaction)
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit tenant transaction: %v", err)
	}
}

func (h *samlHarness) startLogin(
	ctx context.Context, t *testing.T, query db.Querier, workspace samlWorkspace, purpose Purpose,
) string {
	t.Helper()
	if _, err := h.service.StartLogin(
		ctx, query, workspace.resolved, purpose, "", workspace.ownerID,
	); err != nil {
		t.Fatalf("StartLogin(%s) error = %v", purpose, err)
	}
	ids, err := h.store.LiveAuthRequestIDs(ctx, query, workspace.id, time.Now())
	if err != nil || len(ids) == 0 {
		t.Fatalf("LiveAuthRequestIDs() = %v, %v", ids, err)
	}
	return ids[0]
}

func (h *samlHarness) assertion(
	t *testing.T, workspace samlWorkspace, requestID, email string,
) assertionOptions {
	t.Helper()
	endpoints := h.service.Endpoints
	entityID, err := endpoints.EntityID(workspace.slug)
	if err != nil {
		t.Fatalf("EntityID() error = %v", err)
	}
	acs, err := endpoints.ACS(workspace.slug)
	if err != nil {
		t.Fatalf("ACS() error = %v", err)
	}
	return assertionOptions{
		assertionID: "_assertion-" + requestID, audience: entityID, recipient: acs,
		destination: acs, inResponseTo: requestID, email: email, subjectID: email,
	}
}

func tokenFromFragment(t *testing.T, redirectURL string) string {
	t.Helper()
	_, fragment, found := strings.Cut(redirectURL, "#")
	if !found {
		t.Fatalf("redirect URL %q carries no handoff fragment", redirectURL)
	}
	value := strings.TrimPrefix(fragment, "token=")
	if value == fragment || value == "" {
		t.Fatalf("redirect URL %q carries no handoff token", redirectURL)
	}
	return value
}

func samlMigrationsPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migrations path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "migrations")
}
