package samlsso

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
)

const (
	testWebBaseURL = "https://app.fidibeki.test"
	testBaseDomain = "fidibeki.test"
	testSlug       = "acme"
)

func testEndpoints() Endpoints {
	return Endpoints{WebBaseURL: testWebBaseURL, BaseDomain: testBaseDomain}
}

// testProvider builds a service provider trusting idp, with an SP key pair so
// the signing path is exercised end to end.
func testProvider(t *testing.T, idp *testIdP, mutate func(*Configuration)) (*Provider, Configuration) {
	t.Helper()
	endpoints := testEndpoints()
	entityID, err := endpoints.EntityID(testSlug)
	if err != nil {
		t.Fatalf("EntityID() error = %v", err)
	}
	keyPair, err := GenerateKeyPair(entityID, time.Now())
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	configuration := Configuration{
		TenantID: "tenant-1", Mode: auth.SSOModeOptional,
		IdPEntityID: idp.entityID, IdPSSOURL: "https://idp.test/sso",
		SPCertificate: keyPair.CertificatePEM,
		Certificates:  []Certificate{idp.storedCertificate(t)},
	}
	if mutate != nil {
		mutate(&configuration)
	}
	provider, err := NewProvider(configuration, keyPair.PrivateKeyDER, endpoints, testSlug, nil)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	return provider, configuration
}

func validOptions(t *testing.T) assertionOptions {
	t.Helper()
	endpoints := testEndpoints()
	entityID, err := endpoints.EntityID(testSlug)
	if err != nil {
		t.Fatalf("EntityID() error = %v", err)
	}
	acs, err := endpoints.ACS(testSlug)
	if err != nil {
		t.Fatalf("ACS() error = %v", err)
	}
	return assertionOptions{
		audience: entityID, recipient: acs, destination: acs, inResponseTo: "_request-1",
	}
}

func TestParseResponseAcceptsValidAssertion(t *testing.T) {
	idp := newTestIdP(t, "https://idp.test/entity")
	provider, _ := testProvider(t, idp, nil)

	identity, err := provider.ParseResponse(
		idp.response(t, validOptions(t)), PossibleRequestIDs([]string{"_request-1"}, false),
	)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if identity.Email != "member@acme.test" {
		t.Errorf("Email = %q, want member@acme.test", identity.Email)
	}
	if identity.SubjectID != "member@acme.test" {
		t.Errorf("SubjectID = %q, want member@acme.test", identity.SubjectID)
	}
	if identity.IdPEntityID != idp.entityID {
		t.Errorf("IdPEntityID = %q, want %q", identity.IdPEntityID, idp.entityID)
	}
	if identity.InResponseTo != "_request-1" {
		t.Errorf("InResponseTo = %q, want _request-1", identity.InResponseTo)
	}
	if identity.AssertionID == "" || identity.NotOnOrAfter.IsZero() {
		t.Errorf("assertion identity is incomplete: %#v", identity)
	}
}

// Each case makes exactly one property of an otherwise valid assertion
// hostile, so a rejection can only be attributed to that property.
func TestParseResponseRejectsInvalidAssertions(t *testing.T) {
	idp := newTestIdP(t, "https://idp.test/entity")
	otherIdP := newTestIdP(t, "https://other-idp.test/entity")

	tests := []struct {
		name     string
		mutate   func(*assertionOptions)
		signer   *testIdP
		wantErr  string
		verified bool
	}{
		{
			name:    "tampered assertion",
			mutate:  func(options *assertionOptions) { options.tamper = true },
			wantErr: "signature",
		},
		{
			name:    "assertion signed by another identity provider",
			signer:  otherIdP,
			wantErr: "",
		},
		{
			name:    "issuer mismatch",
			mutate:  func(options *assertionOptions) { options.issuer = "https://spoofed.test/entity" },
			wantErr: "Issuer",
		},
		{
			name:    "audience mismatch",
			mutate:  func(options *assertionOptions) { options.audience = "https://elsewhere.test/metadata" },
			wantErr: "Audience",
		},
		{
			name:    "recipient mismatch",
			mutate:  func(options *assertionOptions) { options.recipient = "https://elsewhere.test/acs" },
			wantErr: "Recipient",
		},
		{
			name:    "destination mismatch",
			mutate:  func(options *assertionOptions) { options.destination = "https://elsewhere.test/acs" },
			wantErr: "Destination",
		},
		{
			name: "expired conditions",
			mutate: func(options *assertionOptions) {
				options.notBefore = time.Now().Add(-2 * time.Hour)
				options.notOnOrAfter = time.Now().Add(-time.Hour)
			},
			wantErr: "expired",
		},
		{
			name: "not yet valid conditions",
			mutate: func(options *assertionOptions) {
				options.notBefore = time.Now().Add(time.Hour)
				options.notOnOrAfter = time.Now().Add(2 * time.Hour)
			},
			wantErr: "not yet valid",
		},
		{
			name:    "unsolicited response",
			mutate:  func(options *assertionOptions) { options.inResponseTo = "" },
			wantErr: "InResponseTo",
		},
		{
			name:    "forged request correlation",
			mutate:  func(options *assertionOptions) { options.inResponseTo = "_never-issued" },
			wantErr: "InResponseTo",
		},
		{
			name:     "no email claim",
			mutate:   func(options *assertionOptions) { options.omitEmail = true },
			wantErr:  "email claim",
			verified: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, _ := testProvider(t, idp, nil)
			options := validOptions(t)
			if test.mutate != nil {
				test.mutate(&options)
			}
			signer := idp
			if test.signer != nil {
				signer = test.signer
			}
			_, err := provider.ParseResponse(
				signer.response(t, options), PossibleRequestIDs([]string{"_request-1"}, false),
			)
			if !errors.Is(err, ErrAssertionInvalid) {
				t.Fatalf("ParseResponse() error = %v, want ErrAssertionInvalid", err)
			}
			if test.wantErr != "" && !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("ParseResponse() error = %v, want it to mention %q", err, test.wantErr)
			}
			var verified *verifiedAssertionFailure
			if got := errors.As(err, &verified); got != test.verified {
				t.Errorf("verified provenance = %v, want %v for %s", got, test.verified, test.name)
			}
		})
	}
}

// IdP-initiated support must widen exactly one thing: an absent InResponseTo.
// A response that quotes a request ID this service provider never issued stays
// rejected, which is the property crewjam's AllowIDPInitiated would have given
// up.
func TestParseResponseIdPInitiatedAcceptsOnlyUncorrelatedResponses(t *testing.T) {
	idp := newTestIdP(t, "https://idp.test/entity")
	provider, _ := testProvider(t, idp, nil)

	unsolicited := validOptions(t)
	unsolicited.inResponseTo = ""
	identity, err := provider.ParseResponse(
		idp.response(t, unsolicited), PossibleRequestIDs(nil, true),
	)
	if err != nil {
		t.Fatalf("unsolicited ParseResponse() error = %v", err)
	}
	if identity.InResponseTo != "" {
		t.Errorf("InResponseTo = %q, want empty for an unsolicited response", identity.InResponseTo)
	}

	forged := validOptions(t)
	forged.inResponseTo = "_never-issued"
	if _, err := provider.ParseResponse(
		idp.response(t, forged), PossibleRequestIDs(nil, true),
	); !errors.Is(err, ErrAssertionInvalid) {
		t.Fatalf("forged correlation ParseResponse() error = %v, want ErrAssertionInvalid", err)
	}
}

// A workspace must never accept an assertion signed by a different workspace's
// identity provider, even when every other field lines up.
func TestParseResponseRejectsAnotherTenantsIdentityProvider(t *testing.T) {
	acmeIdP := newTestIdP(t, "https://idp.test/entity")
	globexIdP := newTestIdP(t, "https://idp.test/entity")

	provider, _ := testProvider(t, acmeIdP, nil)
	if _, err := provider.ParseResponse(
		globexIdP.response(t, validOptions(t)), PossibleRequestIDs([]string{"_request-1"}, false),
	); !errors.Is(err, ErrAssertionInvalid) {
		t.Fatalf("ParseResponse() error = %v, want ErrAssertionInvalid", err)
	}
}

// Rotation means both the outgoing and incoming certificates verify, so no
// sign-in fails while the identity provider is mid-rotation.
func TestParseResponseAcceptsEitherCertificateDuringRotation(t *testing.T) {
	outgoing := newTestIdP(t, "https://idp.test/entity")
	incoming := newTestIdP(t, "https://idp.test/entity")

	provider, _ := testProvider(t, outgoing, func(configuration *Configuration) {
		configuration.Certificates = append(
			configuration.Certificates, incoming.storedCertificate(t),
		)
	})

	for name, signer := range map[string]*testIdP{"outgoing": outgoing, "incoming": incoming} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.ParseResponse(
				signer.response(t, validOptions(t)), PossibleRequestIDs([]string{"_request-1"}, false),
			); err != nil {
				t.Fatalf("ParseResponse() error = %v", err)
			}
		})
	}
}

func TestMetadataDescribesTheEnvironmentDerivedServiceProvider(t *testing.T) {
	idp := newTestIdP(t, "https://idp.test/entity")
	provider, _ := testProvider(t, idp, nil)

	document, err := provider.Metadata()
	if err != nil {
		t.Fatalf("Metadata() error = %v", err)
	}
	rendered := string(document)
	for _, want := range []string{
		"https://acme.fidibeki.test/auth/sso/saml/metadata",
		"https://acme.fidibeki.test/auth/sso/saml/acs",
		"AssertionConsumerService",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("metadata does not contain %q:\n%s", want, rendered)
		}
	}
}

func TestRedirectURLTargetsTheConfiguredIdentityProvider(t *testing.T) {
	idp := newTestIdP(t, "https://idp.test/entity")
	provider, _ := testProvider(t, idp, nil)

	redirectURL, requestID, err := provider.RedirectURL("")
	if err != nil {
		t.Fatalf("RedirectURL() error = %v", err)
	}
	if !strings.HasPrefix(redirectURL, "https://idp.test/sso?") {
		t.Errorf("redirect URL = %q, want the configured single sign-on endpoint", redirectURL)
	}
	if !strings.Contains(redirectURL, "SAMLRequest=") || !strings.Contains(redirectURL, "Signature=") {
		t.Errorf("redirect URL = %q, want a signed SAMLRequest", redirectURL)
	}
	if requestID == "" {
		t.Error("RedirectURL() returned no request ID to correlate against")
	}
}

func TestNewProviderRequiresATrustedCertificate(t *testing.T) {
	idp := newTestIdP(t, "https://idp.test/entity")
	endpoints := testEndpoints()
	configuration := Configuration{
		TenantID: "tenant-1", IdPEntityID: idp.entityID, IdPSSOURL: "https://idp.test/sso",
	}
	if _, err := NewProvider(configuration, nil, endpoints, testSlug, nil); !errors.Is(err, ErrCertificateRequired) {
		t.Fatalf("NewProvider() error = %v, want ErrCertificateRequired", err)
	}
}

func TestNewProviderRejectsAStoredInsecureIdentityProviderEndpoint(t *testing.T) {
	idp := newTestIdP(t, "https://idp.test/entity")
	endpoints := testEndpoints()
	entityID, err := endpoints.EntityID(testSlug)
	if err != nil {
		t.Fatalf("EntityID() error = %v", err)
	}
	keyPair, err := GenerateKeyPair(entityID, time.Now())
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	configuration := Configuration{
		TenantID: "tenant-1", Mode: auth.SSOModeOptional,
		IdPEntityID: idp.entityID, IdPSSOURL: "http://idp.test/sso",
		SPCertificate: keyPair.CertificatePEM,
		Certificates:  []Certificate{idp.storedCertificate(t)},
	}

	_, err = NewProvider(
		configuration, keyPair.PrivateKeyDER, endpoints, testSlug, nil,
	)
	var validation ValidationError
	if !errors.As(err, &validation) ||
		validation.Fields["idp_sso_url"] != "single sign-on URL must use HTTPS" {
		t.Fatalf("NewProvider() error = %#v, want idp_sso_url HTTPS validation", err)
	}
}

func TestAssertionHashIsStableAndOpaque(t *testing.T) {
	first := AssertionHash("_assertion-1")
	if first != AssertionHash("_assertion-1") {
		t.Error("AssertionHash() is not stable for the same assertion ID")
	}
	if first == AssertionHash("_assertion-2") {
		t.Error("AssertionHash() collides across distinct assertion IDs")
	}
	if strings.Contains(first, "_assertion-1") || len(first) != 64 {
		t.Errorf("AssertionHash() = %q, want an opaque 64-character digest", first)
	}
}

// An identity provider that also asserts an opaque subject identifier must
// still sign the member in under their real email. Listing such an attribute
// among the email candidates would shadow the genuine claim and reject a valid
// assertion.
func TestParseResponsePrefersTheRealEmailOverOpaqueIdentifiers(t *testing.T) {
	idp := newTestIdP(t, "https://idp.test/entity")
	provider, _ := testProvider(t, idp, nil)

	options := validOptions(t)
	options.email = "member@acme.test"
	options.extraAttributes = map[string]string{
		"urn:oasis:names:tc:SAML:attribute:subject-id":               "8f14e45f-ea8d-4c2b-9f1a-000000000000",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name": "Member Name",
	}

	identity, err := provider.ParseResponse(
		idp.response(t, options), PossibleRequestIDs([]string{"_request-1"}, false),
	)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if identity.Email != "member@acme.test" {
		t.Fatalf("Email = %q, want the asserted email rather than an opaque identifier", identity.Email)
	}
}
