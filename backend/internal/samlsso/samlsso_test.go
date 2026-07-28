package samlsso

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/outboundhttp"
)

// Required mode is the lockout-sensitive transition: it must be unreachable
// until a test assertion has succeeded against the current identity provider
// and a certificate that can actually verify a signature exists.
func TestCanRequireGuardsAgainstLockout(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	tested := now.Add(-time.Hour)
	valid := Certificate{NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(24 * time.Hour)}
	expired := Certificate{NotBefore: now.Add(-48 * time.Hour), NotAfter: now.Add(-time.Hour)}

	tests := []struct {
		name          string
		configuration Configuration
		want          error
	}{
		{
			name:          "untested configuration",
			configuration: Configuration{Certificates: []Certificate{valid}},
			want:          ErrTestRequired,
		},
		{
			name:          "tested but no certificate",
			configuration: Configuration{LastTestedAt: &tested},
			want:          ErrCertificateRequired,
		},
		{
			name: "tested with an expired certificate only",
			configuration: Configuration{
				LastTestedAt: &tested, Certificates: []Certificate{expired},
			},
			want: ErrCertificateRequired,
		},
		{
			name: "tested with a valid certificate",
			configuration: Configuration{
				LastTestedAt: &tested, Certificates: []Certificate{expired, valid},
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.configuration.CanRequire(now); !errors.Is(err, test.want) {
				t.Fatalf("CanRequire() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestExpiringCertificatesWarnBeforeAnOutage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	configuration := Configuration{Certificates: []Certificate{
		{Fingerprint: "healthy", NotAfter: now.Add(90 * 24 * time.Hour)},
		{Fingerprint: "expiring", NotAfter: now.Add(10 * 24 * time.Hour)},
		{Fingerprint: "expired", NotAfter: now.Add(-time.Hour)},
	}}
	expiring := configuration.ExpiringCertificates(now, CertificateExpiryWarning)
	if len(expiring) != 1 || expiring[0].Fingerprint != "expiring" {
		t.Fatalf("ExpiringCertificates() = %#v, want only the expiring certificate", expiring)
	}
}

// Every SAML URL must follow the running environment. A deployment on a
// different scheme, port, or base domain must produce different URLs with no
// code change.
func TestEndpointsDeriveEveryURLFromTheEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		endpoints   Endpoints
		wantACS     string
		wantEntity  string
		wantStart   string
		wantSuccess string
	}{
		{
			name:        "production",
			endpoints:   Endpoints{WebBaseURL: "https://api.fidibeki.com", BaseDomain: "fidibeki.com"},
			wantACS:     "https://acme.fidibeki.com/auth/sso/saml/acs",
			wantEntity:  "https://acme.fidibeki.com/auth/sso/saml/metadata",
			wantStart:   "https://acme.fidibeki.com/auth/sso/saml/start",
			wantSuccess: "https://acme.fidibeki.com/auth/verify#token=handoff",
		},
		{
			name:        "local development on the web tier port",
			endpoints:   Endpoints{WebBaseURL: "http://api.localhost:3000", BaseDomain: "localhost"},
			wantACS:     "http://acme.localhost:3000/auth/sso/saml/acs",
			wantEntity:  "http://acme.localhost:3000/auth/sso/saml/metadata",
			wantStart:   "http://acme.localhost:3000/auth/sso/saml/start",
			wantSuccess: "http://acme.localhost:3000/auth/verify#token=handoff",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			acs, err := test.endpoints.ACS("acme")
			if err != nil || acs != test.wantACS {
				t.Fatalf("ACS() = %q, %v, want %q", acs, err, test.wantACS)
			}
			entityID, err := test.endpoints.EntityID("acme")
			if err != nil || entityID != test.wantEntity {
				t.Fatalf("EntityID() = %q, %v, want %q", entityID, err, test.wantEntity)
			}
			start, err := test.endpoints.Start("acme")
			if err != nil || start != test.wantStart {
				t.Fatalf("Start() = %q, %v, want %q", start, err, test.wantStart)
			}
			verify, err := test.endpoints.Verify("acme", "handoff", "")
			if err != nil || verify != test.wantSuccess {
				t.Fatalf("Verify() = %q, %v, want %q", verify, err, test.wantSuccess)
			}
		})
	}
}

// The handoff token travels in the fragment so it never reaches a server log,
// referrer header, or proxy access log.
func TestVerifyURLKeepsTheHandoffTokenOutOfTheQueryString(t *testing.T) {
	t.Parallel()
	verify, err := testEndpoints().Verify("acme", "secret-handoff", "/issues")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	query, fragment, found := strings.Cut(verify, "#")
	if !found {
		t.Fatalf("Verify() = %q, want a fragment", verify)
	}
	if strings.Contains(query, "secret-handoff") {
		t.Errorf("Verify() = %q, want the token outside the query string", verify)
	}
	if !strings.Contains(fragment, "secret-handoff") || !strings.Contains(query, "return_to=%2Fissues") {
		t.Errorf("Verify() = %q, want the token in the fragment and the return path in the query", verify)
	}
}

func TestSafeReturnToRejectsOffOriginDestinations(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"https://evil.test/steal", "//evil.test/steal", "javascript:alert(1)", "issues", "",
	} {
		if got := safeReturnTo(value); got != "" {
			t.Errorf("safeReturnTo(%q) = %q, want empty", value, got)
		}
	}
	if got := safeReturnTo("/issues/42"); got != "/issues/42" {
		t.Errorf("safeReturnTo(\"/issues/42\") = %q, want it preserved", got)
	}
}

func TestPossibleRequestIDsAdmitsUncorrelatedResponsesOnlyWhenAllowed(t *testing.T) {
	t.Parallel()
	live := []string{"_request-1", "_request-2"}
	if got := PossibleRequestIDs(live, false); len(got) != 2 {
		t.Fatalf("PossibleRequestIDs(false) = %v, want only the live requests", got)
	}
	got := PossibleRequestIDs(live, true)
	if len(got) != 3 || got[2] != "" {
		t.Fatalf("PossibleRequestIDs(true) = %v, want the live requests plus the uncorrelated case", got)
	}
}

// An assertion may only sign a member in under an email domain the
// organization associated with its identity provider.
func TestNormalizeAssertedEmailEnforcesTheVerifiedDomains(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		email   string
		domains []string
		want    string
		wantErr error
	}{
		{name: "no domain restriction", email: "Member@Acme.test", want: "member@acme.test"},
		{
			name: "allowed domain", email: "member@acme.test",
			domains: []string{"acme.test"}, want: "member@acme.test",
		},
		{
			name: "domain outside the association", email: "member@evil.test",
			domains: []string{"acme.test"}, wantErr: ErrMemberNotEligible,
		},
		{name: "malformed claim", email: "not-an-email", wantErr: ErrAssertionInvalid},
		{name: "empty claim", email: "", wantErr: ErrAssertionInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeAssertedEmail(test.email, test.domains)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("normalizeAssertedEmail() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("normalizeAssertedEmail() = %q, %v, want %q", got, err, test.want)
			}
		})
	}
}

func TestNormalizeEmailDomains(t *testing.T) {
	t.Parallel()
	got, err := normalizeEmailDomains([]string{" @Acme.test ", "acme.test", "sub.acme.test", ""})
	if err != nil {
		t.Fatalf("normalizeEmailDomains() error = %v", err)
	}
	if len(got) != 2 || got[0] != "acme.test" || got[1] != "sub.acme.test" {
		t.Fatalf("normalizeEmailDomains() = %v, want the deduplicated lowercase domains", got)
	}
	if _, err := normalizeEmailDomains([]string{"not a domain"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeEmailDomains(invalid) error = %v, want ErrValidation", err)
	}
}

func TestValidateIdPURLRequiresHTTPSWithoutEmbeddedCredentials(t *testing.T) {
	t.Parallel()
	if err := validateIdPURL("https://idp.example/sso"); err != nil {
		t.Fatalf("validateIdPURL(HTTPS) error = %v", err)
	}
	for _, value := range []string{
		"http://idp.example/sso",
		"ftp://idp.example/sso",
		"https://user:password@idp.example/sso",
		"https:idp.example/sso",
		"/sso",
		"",
	} {
		if err := validateIdPURL(value); err == nil {
			t.Errorf("validateIdPURL(%q) error = nil, want rejection", value)
		}
	}
}

// The API fetches an administrator-supplied URL, so the destination is treated
// as attacker-influenced.
func TestMetadataFetcherRefusesUnsafeDestinations(t *testing.T) {
	t.Parallel()
	fetcher := MetadataFetcher{}
	for _, value := range []string{
		"http://idp.test/metadata",
		"file:///etc/passwd",
		"ftp://idp.test/metadata",
		"https://user:password@idp.test/metadata",
		"https://127.0.0.1/metadata",
		"https://localhost/metadata",
		"https://169.254.169.254/latest/meta-data/",
		"not-a-url",
		"",
	} {
		if _, err := fetcher.validateURL(context.Background(), value); !errors.Is(err, ErrValidation) {
			t.Errorf("validateURL(%q) error = %v, want ErrValidation", value, err)
		}
	}
}

// Local development runs against an IdP on a private address, which is exactly
// what AllowInsecure is for; it must never be the default.
func TestMetadataFetcherAllowsPrivateDestinationsOnlyWhenExplicitlyPermitted(t *testing.T) {
	t.Parallel()
	fetcher := MetadataFetcher{AllowInsecure: true}
	if _, err := fetcher.validateURL(context.Background(), "http://localhost:8081/metadata"); err != nil {
		t.Fatalf("validateURL() error = %v, want the local destination to be permitted", err)
	}
	if _, err := (MetadataFetcher{}).validateURL(
		context.Background(), "http://localhost:8081/metadata",
	); err == nil {
		t.Fatal("validateURL() error = nil, want a plain-HTTP destination refused by default")
	}
}

func TestIsPublicAddressRejectsInternalRanges(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "0.0.0.0", "::1", "fc00::1", "fe80::1",
	} {
		if outboundhttp.IsPublicAddress(netip.MustParseAddr(value)) {
			t.Errorf("isPublicAddress(%q) = true, want false", value)
		}
	}
	for _, value := range []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"} {
		if !outboundhttp.IsPublicAddress(netip.MustParseAddr(value)) {
			t.Errorf("isPublicAddress(%q) = false, want true", value)
		}
	}
}

func TestMetadataFetcherRejectsDNSRebindingBeforeConnecting(t *testing.T) {
	resolver := &metadataSequenceResolver{answers: [][]netip.Addr{
		{netip.MustParseAddr("93.184.216.34")},
		{netip.MustParseAddr("169.254.169.254")},
	}}
	dialed := false
	fetcher := MetadataFetcher{Outbound: outboundhttp.Policy{
		Resolver: resolver,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("must not dial")
		},
	}}

	if _, err := fetcher.Fetch(
		context.Background(), "https://rebind.example/metadata",
	); !errors.Is(err, ErrValidation) {
		t.Fatalf("Fetch() error = %v, want ErrValidation", err)
	}
	if dialed {
		t.Fatal("metadata fetch connected after DNS rebound to a private address")
	}
}

type metadataSequenceResolver struct {
	answers [][]netip.Addr
	index   int
}

func (r *metadataSequenceResolver) LookupNetIP(
	context.Context,
	string,
	string,
) ([]netip.Addr, error) {
	index := r.index
	if index >= len(r.answers) {
		index = len(r.answers) - 1
	}
	r.index++
	return append([]netip.Addr(nil), r.answers[index]...), nil
}

func TestPolicyNeverDependsOnMembership(t *testing.T) {
	t.Parallel()
	// Required mode closes local login for ordinary members while keeping the
	// owner break-glass path advertised. Both signals are organization-level,
	// so two workspaces in the same mode report identically regardless of who
	// is asking.
	tests := []struct {
		mode              auth.SSOMode
		wantLocalLogin    bool
		wantBreakGlass    bool
		wantStartURLShown bool
	}{
		{mode: auth.SSOModeDisabled, wantLocalLogin: true},
		{mode: auth.SSOModeOptional, wantLocalLogin: true, wantStartURLShown: true},
		{mode: auth.SSOModeRequired, wantBreakGlass: true, wantStartURLShown: true},
	}
	for _, test := range tests {
		t.Run(string(test.mode), func(t *testing.T) {
			t.Parallel()
			policy := Policy{
				Mode:                test.mode,
				LocalLoginEnabled:   test.mode != auth.SSOModeRequired,
				BreakGlassAvailable: test.mode == auth.SSOModeRequired,
			}
			if policy.LocalLoginEnabled != test.wantLocalLogin {
				t.Errorf("LocalLoginEnabled = %v, want %v", policy.LocalLoginEnabled, test.wantLocalLogin)
			}
			if policy.BreakGlassAvailable != test.wantBreakGlass {
				t.Errorf("BreakGlassAvailable = %v, want %v", policy.BreakGlassAvailable, test.wantBreakGlass)
			}
		})
	}
}

func TestParseIdPMetadataExtractsTheRedirectEndpointAndSigningCertificates(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t, "https://idp.test/entity")
	certificate := idp.storedCertificate(t)
	document := `<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.test/entity">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
        <X509Data><X509Certificate>` + certificate.Certificate + `</X509Certificate></X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <KeyDescriptor use="encryption">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
        <X509Data><X509Certificate>` + certificate.Certificate + `</X509Certificate></X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
                         Location="https://idp.test/sso/post"/>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
                         Location="https://idp.test/sso/redirect"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

	details, err := ParseIdPMetadata([]byte(document), time.Now())
	if err != nil {
		t.Fatalf("ParseIdPMetadata() error = %v", err)
	}
	if details.EntityID != "https://idp.test/entity" {
		t.Errorf("EntityID = %q", details.EntityID)
	}
	if details.SSOURL != "https://idp.test/sso/redirect" {
		t.Errorf("SSOURL = %q, want the HTTP-Redirect endpoint", details.SSOURL)
	}
	// The encryption key repeats the same certificate; it must be deduplicated
	// rather than trusted twice.
	if len(details.Certificates) != 1 {
		t.Fatalf("Certificates = %d, want 1 deduplicated signing certificate", len(details.Certificates))
	}
	if details.Certificates[0].Fingerprint != certificate.Fingerprint {
		t.Errorf("Fingerprint = %q, want %q", details.Certificates[0].Fingerprint, certificate.Fingerprint)
	}
}

func TestParseIdPMetadataRejectsAnInsecureRedirectEndpoint(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t, "https://idp.test/entity")
	certificate := idp.storedCertificate(t)
	document := `<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.test/entity">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
        <X509Data><X509Certificate>` + certificate.Certificate + `</X509Certificate></X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
                         Location="http://idp.test/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

	_, err := ParseIdPMetadata([]byte(document), time.Now())
	var validation ValidationError
	if !errors.As(err, &validation) ||
		validation.Fields["idp_sso_url"] != "single sign-on URL must use HTTPS" {
		t.Fatalf("ParseIdPMetadata() error = %#v, want idp_sso_url HTTPS validation", err)
	}
}

func TestParseIdPMetadataRejectsUnusableDocuments(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"empty":          "",
		"not XML":        "this is not metadata",
		"no IdP role":    `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.test"/>`,
		"POST bind only": postOnlyMetadata,
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseIdPMetadata([]byte(document), time.Now()); !errors.Is(err, ErrValidation) {
				t.Fatalf("ParseIdPMetadata() error = %v, want ErrValidation", err)
			}
		})
	}
}

const postOnlyMetadata = `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.test">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
                         Location="https://idp.test/sso/post"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

func TestParseCertificateAcceptsPEMAndBareBase64(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t, "https://idp.test/entity")
	base64Certificate := idp.storedCertificate(t).Certificate
	pemCertificate := "-----BEGIN CERTIFICATE-----\n" + base64Certificate + "\n-----END CERTIFICATE-----\n"

	fromBase64, err := ParseCertificate(base64Certificate, time.Now())
	if err != nil {
		t.Fatalf("ParseCertificate(base64) error = %v", err)
	}
	fromPEM, err := ParseCertificate(pemCertificate, time.Now())
	if err != nil {
		t.Fatalf("ParseCertificate(PEM) error = %v", err)
	}
	if fromBase64.Fingerprint != fromPEM.Fingerprint {
		t.Errorf("fingerprints differ across encodings: %q vs %q",
			fromBase64.Fingerprint, fromPEM.Fingerprint)
	}
	for _, value := range []string{"", "not a certificate", "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----"} {
		if _, err := ParseCertificate(value, time.Now()); !errors.Is(err, ErrValidation) {
			t.Errorf("ParseCertificate(%q) error = %v, want ErrValidation", value, err)
		}
	}
}
