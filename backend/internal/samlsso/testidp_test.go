package samlsso

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"maps"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// testIdP is a minimal identity provider that mints genuinely XML-DSig-signed
// assertions. Tests exercise the real signature path rather than a stub, so a
// regression in signature verification cannot pass by accident.
type testIdP struct {
	entityID    string
	certificate *x509.Certificate
	key         *rsa.PrivateKey
	der         []byte
}

func newTestIdP(t *testing.T, entityID string) *testIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate identity provider key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: entityID},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create identity provider certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse identity provider certificate: %v", err)
	}
	return &testIdP{entityID: entityID, certificate: certificate, key: key, der: der}
}

func (idp *testIdP) storedCertificate(t *testing.T) Certificate {
	t.Helper()
	certificate, err := certificateFromDER(idp.der, time.Now())
	if err != nil {
		t.Fatalf("convert identity provider certificate: %v", err)
	}
	certificate.ID = "certificate-" + idp.entityID
	return certificate
}

// assertionOptions describes the assertion a test wants minted. Zero values
// produce a valid assertion; each field exists so one property at a time can be
// made hostile.
type assertionOptions struct {
	assertionID  string
	issuer       string
	audience     string
	recipient    string
	destination  string
	inResponseTo string
	// responseInResponseTo overrides only the protocol Response envelope while
	// leaving the signed assertion SubjectConfirmation unchanged.
	responseInResponseTo *string
	email                string
	subjectID            string
	notBefore            time.Time
	notOnOrAfter         time.Time
	issueInstant         time.Time
	// tamper rewrites the asserted email after signing, which must invalidate
	// the signature.
	tamper bool
	// omitEmail drops the attribute statement and uses a non-email NameID
	// format, so no email claim can be recovered.
	omitEmail bool
	// extraAttributes are emitted before the email attribute, so a test can
	// prove a non-email claim never shadows the real one.
	extraAttributes map[string]string
}

func (idp *testIdP) response(t *testing.T, options assertionOptions) string {
	t.Helper()
	now := time.Now().UTC()
	if options.assertionID == "" {
		options.assertionID = "_assertion-" + now.Format("150405.000000000")
	}
	if options.issuer == "" {
		options.issuer = idp.entityID
	}
	if options.issueInstant.IsZero() {
		options.issueInstant = now
	}
	if options.notBefore.IsZero() {
		options.notBefore = now.Add(-time.Minute)
	}
	if options.notOnOrAfter.IsZero() {
		options.notOnOrAfter = now.Add(10 * time.Minute)
	}
	if options.email == "" {
		options.email = "member@acme.test"
	}
	if options.subjectID == "" {
		options.subjectID = options.email
	}

	assertion := etree.NewElement("saml:Assertion")
	assertion.CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
	assertion.CreateAttr("ID", options.assertionID)
	assertion.CreateAttr("Version", "2.0")
	assertion.CreateAttr("IssueInstant", options.issueInstant.Format(time.RFC3339Nano))
	assertion.CreateElement("saml:Issuer").SetText(options.issuer)

	subject := assertion.CreateElement("saml:Subject")
	nameID := subject.CreateElement("saml:NameID")
	if options.omitEmail {
		nameID.CreateAttr("Format", "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent")
	} else {
		nameID.CreateAttr("Format", "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress")
	}
	nameID.SetText(options.subjectID)
	confirmation := subject.CreateElement("saml:SubjectConfirmation")
	confirmation.CreateAttr("Method", "urn:oasis:names:tc:SAML:2.0:cm:bearer")
	confirmationData := confirmation.CreateElement("saml:SubjectConfirmationData")
	confirmationData.CreateAttr("Recipient", options.recipient)
	confirmationData.CreateAttr("NotOnOrAfter", options.notOnOrAfter.Format(time.RFC3339Nano))
	if options.inResponseTo != "" {
		confirmationData.CreateAttr("InResponseTo", options.inResponseTo)
	}

	conditions := assertion.CreateElement("saml:Conditions")
	conditions.CreateAttr("NotBefore", options.notBefore.Format(time.RFC3339Nano))
	conditions.CreateAttr("NotOnOrAfter", options.notOnOrAfter.Format(time.RFC3339Nano))
	audienceRestriction := conditions.CreateElement("saml:AudienceRestriction")
	audienceRestriction.CreateElement("saml:Audience").SetText(options.audience)

	authnStatement := assertion.CreateElement("saml:AuthnStatement")
	authnStatement.CreateAttr("AuthnInstant", options.issueInstant.Format(time.RFC3339Nano))
	authnStatement.CreateAttr("SessionIndex", options.assertionID)
	authnContext := authnStatement.CreateElement("saml:AuthnContext")
	authnContext.CreateElement("saml:AuthnContextClassRef").
		SetText("urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport")

	if !options.omitEmail || len(options.extraAttributes) > 0 {
		statement := assertion.CreateElement("saml:AttributeStatement")
		for _, name := range slices.Sorted(maps.Keys(options.extraAttributes)) {
			attribute := statement.CreateElement("saml:Attribute")
			attribute.CreateAttr("Name", name)
			attribute.CreateElement("saml:AttributeValue").SetText(options.extraAttributes[name])
		}
		if !options.omitEmail {
			attribute := statement.CreateElement("saml:Attribute")
			attribute.CreateAttr("Name", "email")
			attribute.CreateElement("saml:AttributeValue").SetText(options.email)
		}
	}

	signed := idp.sign(t, assertion)
	if options.tamper {
		// Rewriting a signed value must break digest verification.
		for _, value := range signed.FindElements(".//AttributeValue") {
			value.SetText("attacker@acme.test")
		}
		for _, value := range signed.FindElements(".//NameID") {
			value.SetText("attacker@acme.test")
		}
	}

	response := etree.NewElement("samlp:Response")
	response.CreateAttr("xmlns:samlp", "urn:oasis:names:tc:SAML:2.0:protocol")
	response.CreateAttr("ID", "_response-"+options.assertionID)
	response.CreateAttr("Version", "2.0")
	response.CreateAttr("IssueInstant", options.issueInstant.Format(time.RFC3339Nano))
	if options.destination != "" {
		response.CreateAttr("Destination", options.destination)
	}
	responseInResponseTo := options.inResponseTo
	if options.responseInResponseTo != nil {
		responseInResponseTo = *options.responseInResponseTo
	}
	if responseInResponseTo != "" {
		response.CreateAttr("InResponseTo", responseInResponseTo)
	}
	issuer := response.CreateElement("saml:Issuer")
	issuer.CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
	issuer.SetText(options.issuer)
	status := response.CreateElement("samlp:Status")
	status.CreateElement("samlp:StatusCode").
		CreateAttr("Value", "urn:oasis:names:tc:SAML:2.0:status:Success")
	response.AddChild(signed)

	document := etree.NewDocument()
	document.SetRoot(response)
	encoded, err := document.WriteToBytes()
	if err != nil {
		t.Fatalf("serialize SAML response: %v", err)
	}
	return base64.StdEncoding.EncodeToString(encoded)
}

func (idp *testIdP) sign(t *testing.T, element *etree.Element) *etree.Element {
	t.Helper()
	keyStore := dsig.TLSCertKeyStore(tls.Certificate{
		Certificate: [][]byte{idp.der},
		PrivateKey:  idp.key,
	})
	context := dsig.NewDefaultSigningContext(keyStore)
	// SAML uses exclusive canonicalization, which real identity providers emit
	// and which makes the assertion's digest independent of the namespaces its
	// enclosing Response happens to declare. goxmldsig's default is inclusive
	// c14n 1.1, so this must be set explicitly.
	context.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := context.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		t.Fatalf("set signature method: %v", err)
	}
	signed, err := context.SignEnveloped(element)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	return signed
}
