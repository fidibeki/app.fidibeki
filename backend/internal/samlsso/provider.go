package samlsso

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"
)

// spCertificateLifetime outlives any realistic IdP integration, so an SP key
// rotation is a deliberate act rather than a surprise expiry.
const spCertificateLifetime = 5 * 365 * 24 * time.Hour

// Attribute names identity providers use for the email claim, in preference
// order. Okta, Entra ID, Google Workspace, and OneLogin between them emit all
// of these.
//
// Every entry must be an attribute whose value really is an email address.
// subject-id and similar opaque identifiers are deliberately absent: matching
// one first would shadow the real email attribute later in this list and
// reject an otherwise valid assertion.
var emailAttributeNames = []string{
	"urn:oid:0.9.2342.19200300.100.1.3",
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	"urn:oasis:names:tc:SAML:attribute:mail",
	"email",
	"emailaddress",
	"emailAddress",
	"mail",
	"User.Email",
}

// KeyPair is a generated service-provider key pair. The private key is PKCS#8
// DER and is sealed before it reaches the database.
type KeyPair struct {
	PrivateKeyDER  []byte
	CertificatePEM string
}

// GenerateKeyPair creates the SP key pair used to sign AuthnRequests and
// decrypt encrypted assertions. Each organization gets its own, so one
// workspace's IdP integration is cryptographically independent of another's.
func GenerateKeyPair(entityID string, now time.Time) (KeyPair, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate SAML service provider key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate SAML certificate serial: %w", err)
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: entityID, Organization: []string{"Fidibeki"}},
		NotBefore:             now.Add(-time.Hour).UTC(),
		NotAfter:              now.Add(spCertificateLifetime).UTC(),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return KeyPair{}, fmt.Errorf("create SAML service provider certificate: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return KeyPair{}, fmt.Errorf("encode SAML service provider key: %w", err)
	}
	return KeyPair{
		PrivateKeyDER:  privateDER,
		CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
	}, nil
}

// Provider wraps the SAML service provider for one organization.
type Provider struct {
	sp  *saml.ServiceProvider
	now func() time.Time
}

// NewProvider builds the service provider for a configuration. The SP's own
// identifiers come from endpoints (that is, from the running environment); the
// trusted signing certificates come from the tenant's stored configuration, so
// an assertion is only ever accepted against that organization's own IdP.
func NewProvider(
	configuration Configuration,
	privateKeyDER []byte,
	endpoints Endpoints,
	slug string,
	now func() time.Time,
) (*Provider, error) {
	descriptor, err := endpoints.Descriptor(slug, configuration.SPCertificate)
	if err != nil {
		return nil, err
	}
	metadataURL, err := url.Parse(descriptor.MetadataURL)
	if err != nil {
		return nil, fmt.Errorf("parse SAML metadata URL: %w", err)
	}
	acsURL, err := url.Parse(descriptor.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("parse SAML ACS URL: %w", err)
	}
	idpMetadata, err := idpEntityDescriptor(configuration)
	if err != nil {
		return nil, err
	}
	provider := &saml.ServiceProvider{
		EntityID:          descriptor.EntityID,
		MetadataURL:       *metadataURL,
		AcsURL:            *acsURL,
		IDPMetadata:       idpMetadata,
		AuthnNameIDFormat: saml.EmailAddressNameIDFormat,
		// Correlation is enforced through the possible-request-ID set instead:
		// see PossibleRequestIDs. crewjam's AllowIDPInitiated would skip the
		// InResponseTo check outright, which would also accept a forged value
		// on a solicited response.
		AllowIDPInitiated: false,
	}
	if len(privateKeyDER) > 0 {
		key, err := x509.ParsePKCS8PrivateKey(privateKeyDER)
		if err != nil {
			return nil, fmt.Errorf("parse SAML service provider key: %w", err)
		}
		signer, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("SAML service provider key is not RSA")
		}
		certificate, err := parseSPCertificate(configuration.SPCertificate)
		if err != nil {
			return nil, err
		}
		provider.Key = signer
		provider.Certificate = certificate
		provider.SignatureMethod = dsig.RSASHA256SignatureMethod
	}
	if now == nil {
		now = time.Now
	}
	return &Provider{sp: provider, now: now}, nil
}

// Metadata renders this service provider's metadata document.
func (p *Provider) Metadata() ([]byte, error) {
	document, err := xml.MarshalIndent(p.sp.Metadata(), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render SAML service provider metadata: %w", err)
	}
	return append([]byte(xml.Header), document...), nil
}

// RedirectURL builds the HTTP-Redirect AuthnRequest URL and returns it with the
// request ID to correlate the eventual response against.
func (p *Provider) RedirectURL(relayState string) (string, string, error) {
	request, err := p.sp.MakeAuthenticationRequest(
		p.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		return "", "", fmt.Errorf("build SAML authentication request: %w", err)
	}
	target, err := request.Redirect(relayState, p.sp)
	if err != nil {
		return "", "", fmt.Errorf("encode SAML authentication request: %w", err)
	}
	return target.String(), request.ID, nil
}

// PossibleRequestIDs converts the live AuthnRequest IDs into the set the
// response must correlate against. When the organization has opted into
// IdP-initiated SSO, the empty string joins the set so an unsolicited response
// (which carries no InResponseTo) is accepted — but a response carrying an
// InResponseTo that matches no live request is still rejected, whether or not
// IdP-initiated is allowed.
func PossibleRequestIDs(liveRequestIDs []string, allowIDPInitiated bool) []string {
	possible := make([]string, 0, len(liveRequestIDs)+1)
	possible = append(possible, liveRequestIDs...)
	if allowIDPInitiated {
		possible = append(possible, "")
	}
	return possible
}

// ParseResponse validates an encoded SAML Response and projects the small,
// non-sensitive identity the rest of the flow needs.
//
// Signature, issuer, audience, recipient, destination, and condition
// timestamps are all validated here; replay is enforced separately by the
// caller against the assertion-ID ledger, because that requires the tenant's
// transaction.
func (p *Provider) ParseResponse(encodedResponse string, possibleRequestIDs []string) (AssertedIdentity, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedResponse))
	if err != nil {
		return AssertedIdentity{}, fmt.Errorf("%w: response is not valid base64", ErrAssertionInvalid)
	}
	assertion, err := p.sp.ParseXMLResponse(decoded, possibleRequestIDs, p.sp.AcsURL)
	if err != nil {
		// InvalidResponseError embeds the whole response document; unwrap to the
		// private cause so the assertion never reaches a log.
		var invalid *saml.InvalidResponseError
		if errors.As(err, &invalid) && invalid.PrivateErr != nil {
			return AssertedIdentity{}, fmt.Errorf("%w: %s", ErrAssertionInvalid, invalid.PrivateErr)
		}
		return AssertedIdentity{}, fmt.Errorf("%w: %s", ErrAssertionInvalid, err)
	}
	identity := AssertedIdentity{
		AssertionID: assertion.ID,
		IdPEntityID: assertion.Issuer.Value,
	}
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		identity.SubjectID = strings.TrimSpace(assertion.Subject.NameID.Value)
		for _, confirmation := range assertion.Subject.SubjectConfirmations {
			if confirmation.SubjectConfirmationData == nil {
				continue
			}
			identity.InResponseTo = confirmation.SubjectConfirmationData.InResponseTo
			identity.NotOnOrAfter = confirmation.SubjectConfirmationData.NotOnOrAfter
		}
	}
	if assertion.Conditions != nil && identity.NotOnOrAfter.IsZero() {
		identity.NotOnOrAfter = assertion.Conditions.NotOnOrAfter
	}
	if identity.NotOnOrAfter.IsZero() {
		// Bound the replay ledger entry even when the IdP omitted an explicit
		// expiry, so a replayed assertion can never outlive its record.
		identity.NotOnOrAfter = p.now().Add(saml.MaxIssueDelay + saml.MaxClockSkew)
	}
	identity.Email = assertionEmail(assertion)
	if identity.SubjectID == "" {
		return AssertedIdentity{}, fmt.Errorf("%w: assertion carries no subject identifier", ErrAssertionInvalid)
	}
	if identity.AssertionID == "" {
		return AssertedIdentity{}, fmt.Errorf("%w: assertion carries no ID", ErrAssertionInvalid)
	}
	if identity.Email == "" {
		return AssertedIdentity{}, fmt.Errorf("%w: assertion carries no email claim", ErrAssertionInvalid)
	}
	return identity, nil
}

// AssertionHash keys the replay ledger. The assertion ID is hashed rather than
// stored so the ledger holds no identifier an operator could correlate back to
// a specific sign-in event.
func AssertionHash(assertionID string) string {
	digest := sha256.Sum256([]byte(assertionID))
	return hex.EncodeToString(digest[:])
}

// assertionEmail reads the email claim from the attribute statements, falling
// back to an emailAddress-format NameID. Only this one claim is read: no other
// IdP-asserted attribute influences the session, and none is stored.
func assertionEmail(assertion *saml.Assertion) string {
	for _, wanted := range emailAttributeNames {
		for _, statement := range assertion.AttributeStatements {
			for _, attribute := range statement.Attributes {
				if !attributeMatches(attribute, wanted) {
					continue
				}
				for _, value := range attribute.Values {
					if candidate := strings.TrimSpace(value.Value); candidate != "" {
						return candidate
					}
				}
			}
		}
	}
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		nameID := assertion.Subject.NameID
		if nameID.Format == "" || nameID.Format == string(saml.EmailAddressNameIDFormat) {
			return strings.TrimSpace(nameID.Value)
		}
	}
	return ""
}

func attributeMatches(attribute saml.Attribute, wanted string) bool {
	return strings.EqualFold(attribute.Name, wanted) ||
		strings.EqualFold(attribute.FriendlyName, wanted)
}

// idpEntityDescriptor assembles the trusted IdP view from stored configuration.
// Every currently stored certificate is offered to the signature verifier, which
// is what lets an IdP publish its next certificate before retiring the current
// one without an authentication outage.
func idpEntityDescriptor(configuration Configuration) (*saml.EntityDescriptor, error) {
	if configuration.IdPEntityID == "" || configuration.IdPSSOURL == "" {
		return nil, ErrNotConfigured
	}
	idpSSOURL := strings.TrimSpace(configuration.IdPSSOURL)
	if err := validateIdPURL(idpSSOURL); err != nil {
		return nil, ValidationError{Fields: map[string]string{"idp_sso_url": err.Error()}}
	}
	keys := make([]saml.KeyDescriptor, 0, len(configuration.Certificates))
	for _, certificate := range configuration.Certificates {
		keys = append(keys, saml.KeyDescriptor{
			Use: "signing",
			KeyInfo: saml.KeyInfo{
				X509Data: saml.X509Data{
					X509Certificates: []saml.X509Certificate{{Data: certificate.Certificate}},
				},
			},
		})
	}
	if len(keys) == 0 {
		return nil, ErrCertificateRequired
	}
	return &saml.EntityDescriptor{
		EntityID: configuration.IdPEntityID,
		IDPSSODescriptors: []saml.IDPSSODescriptor{{
			SSODescriptor: saml.SSODescriptor{
				RoleDescriptor: saml.RoleDescriptor{
					ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
					KeyDescriptors:             keys,
				},
			},
			SingleSignOnServices: []saml.Endpoint{{
				Binding:  saml.HTTPRedirectBinding,
				Location: idpSSOURL,
			}},
		}},
	}, nil
}

func parseSPCertificate(certificatePEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certificatePEM))
	if block == nil {
		return nil, fmt.Errorf("SAML service provider certificate is not valid PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse SAML service provider certificate: %w", err)
	}
	return certificate, nil
}
