package samlsso

import (
	"net/url"

	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

// Paths are relative to the tenant's own host. They live under /auth/ so the
// web tier's existing API proxy forwards them without extra routing.
const (
	MetadataPath = "/auth/sso/saml/metadata"
	StartPath    = "/auth/sso/saml/start"
	ACSPath      = "/auth/sso/saml/acs"
	StatusPath   = "/auth/sso/saml/status"
	// VerifyPath is the existing magic-link landing page the assertion consumer
	// hands off to.
	VerifyPath = "/auth/verify"
	// SettingsPath is where a test login returns the administrator.
	SettingsPath = "/settings"
)

// Endpoints derives every SAML URL for one tenant from the running
// environment. WebBaseURL supplies the scheme and port of the browser-facing
// tier; BaseDomain plus the tenant slug supply the host. Nothing here is
// hardcoded to a staging or production deployment.
type Endpoints struct {
	WebBaseURL string
	BaseDomain string
}

func (e Endpoints) url(slug, path string) (*url.URL, error) {
	return tenant.PublicURL(e.WebBaseURL, e.BaseDomain, slug, path)
}

func (e Endpoints) Metadata(slug string) (string, error) { return e.stringURL(slug, MetadataPath) }
func (e Endpoints) Start(slug string) (string, error)    { return e.stringURL(slug, StartPath) }
func (e Endpoints) ACS(slug string) (string, error)      { return e.stringURL(slug, ACSPath) }

// EntityID identifies this service provider to the IdP. It is the metadata URL,
// which is unique per tenant and stable for the deployment's lifetime.
func (e Endpoints) EntityID(slug string) (string, error) { return e.stringURL(slug, MetadataPath) }

// Verify is the magic-link landing page a successful login hands off to.
func (e Endpoints) Verify(slug, token, returnTo string) (string, error) {
	target, err := e.url(slug, VerifyPath)
	if err != nil {
		return "", err
	}
	if returnTo != "" {
		target.RawQuery = url.Values{"return_to": []string{returnTo}}.Encode()
	}
	// The token travels in the fragment so it never reaches a server log,
	// referrer header, or proxy access log — the same handling as the emailed
	// magic link.
	target.Fragment = url.Values{"token": []string{token}}.Encode()
	return target.String(), nil
}

// TestResult is where an administrator's validation run returns to.
func (e Endpoints) TestResult(slug, outcome, detail string) (string, error) {
	target, err := e.url(slug, SettingsPath)
	if err != nil {
		return "", err
	}
	query := url.Values{"tab": []string{"sso"}, "saml_test": []string{outcome}}
	if detail != "" {
		query.Set("saml_test_detail", detail)
	}
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func (e Endpoints) stringURL(slug, path string) (string, error) {
	target, err := e.url(slug, path)
	if err != nil {
		return "", err
	}
	return target.String(), nil
}

// Descriptor assembles the values an administrator copies into their IdP.
func (e Endpoints) Descriptor(slug, spCertificate string) (Descriptor, error) {
	entityID, err := e.EntityID(slug)
	if err != nil {
		return Descriptor{}, err
	}
	acs, err := e.ACS(slug)
	if err != nil {
		return Descriptor{}, err
	}
	start, err := e.Start(slug)
	if err != nil {
		return Descriptor{}, err
	}
	return Descriptor{
		EntityID: entityID, ACSURL: acs, MetadataURL: entityID, StartURL: start,
		Certificate: spCertificate,
	}, nil
}
