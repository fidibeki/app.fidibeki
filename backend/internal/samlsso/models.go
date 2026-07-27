// Package samlsso implements organization-scoped SAML 2.0 single sign-on.
//
// SAML authenticates identity only: Fidibeki permissions remain the
// authorization source, and no assertion attribute grants a role. Configuration
// is per organization and disabled by default, so a workspace that never sets
// it up keeps the existing magic-link path unchanged.
//
// A validated assertion does not mint a session directly. It issues a
// short-lived single-use magic link bound to an existing member, which the
// normal /auth/verify exchange consumes. That keeps SAML on the same audited
// session-issuing path as local login — including refresh rotation and reuse
// detection — instead of duplicating it, and carries the authentication
// provenance onto the session so IdP-assured sessions stay distinguishable.
package samlsso

import (
	"errors"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
)

var (
	// ErrNotConfigured means the organization has no SAML configuration.
	ErrNotConfigured = errors.New("SAML SSO is not configured for this workspace")
	// ErrSSODisabled means a configuration exists but its mode is disabled.
	ErrSSODisabled = errors.New("SAML SSO is disabled for this workspace")
	// ErrTestRequired guards Required mode behind a successful test assertion.
	ErrTestRequired = errors.New("a successful test login is required before enabling required mode")
	// ErrCertificateRequired guards enablement behind a currently valid signing
	// certificate, so an expired IdP certificate can never lock a workspace out.
	ErrCertificateRequired = errors.New("a currently valid signing certificate is required")
	// ErrLastCertificate refuses to remove the only remaining signing
	// certificate while SSO is enabled.
	ErrLastCertificate = errors.New("the last signing certificate cannot be removed while SSO is enabled")
	// ErrCertificateNotFound is returned for an unknown certificate ID.
	ErrCertificateNotFound = errors.New("signing certificate not found")
	// ErrAssertionInvalid covers every assertion rejection: bad signature,
	// issuer or audience mismatch, expired or future-dated conditions,
	// destination or recipient mismatch, and unsolicited responses. The
	// specific cause is logged and audited but never returned to the browser.
	ErrAssertionInvalid = errors.New("SAML assertion is invalid")
	// ErrAssertionReplayed means this assertion ID was already consumed.
	ErrAssertionReplayed = errors.New("SAML assertion was already used")
	// ErrMemberNotEligible means the asserted subject is not an existing or
	// invited member of this organization. SAML never enrols a member, and
	// never links an identity across organizations on a matching email alone.
	ErrMemberNotEligible = errors.New("no eligible workspace member for this identity")
	// ErrPlanMemberLimit means a valid invitation could not be accepted because
	// the workspace has no member capacity under its current plan.
	ErrPlanMemberLimit = errors.New("workspace member limit reached")
	// ErrRequestNotFound means the response referenced no live AuthnRequest.
	ErrRequestNotFound = errors.New("SAML request correlation failed")
	// ErrValidation carries field-level configuration errors.
	ErrValidation = errors.New("SAML configuration is invalid")
)

// ValidationError reports which configuration fields were rejected.
type ValidationError struct {
	Fields map[string]string
}

func (e ValidationError) Error() string { return ErrValidation.Error() }
func (e ValidationError) Unwrap() error { return ErrValidation }

// Certificate is one IdP signing certificate. Several can be active at once so
// an identity provider can publish its successor before retiring the current
// certificate: rotation without an authentication outage.
type Certificate struct {
	ID          string    `json:"id"`
	Certificate string    `json:"certificate"`
	Fingerprint string    `json:"fingerprint"`
	Subject     string    `json:"subject"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	CreatedAt   time.Time `json:"created_at"`
}

// ValidAt reports whether this certificate can verify a signature at t.
func (c Certificate) ValidAt(t time.Time) bool {
	return !t.Before(c.NotBefore) && t.Before(c.NotAfter)
}

// Configuration is an organization's SAML SSO settings. The SP private key is
// never part of this struct; it is unsealed only inside the store.
type Configuration struct {
	TenantID              string        `json:"-"`
	Mode                  auth.SSOMode  `json:"mode"`
	IdPEntityID           string        `json:"idp_entity_id"`
	IdPSSOURL             string        `json:"idp_sso_url"`
	IdPMetadataURL        *string       `json:"idp_metadata_url,omitempty"`
	EmailDomains          []string      `json:"email_domains"`
	AllowIDPInitiated     bool          `json:"allow_idp_initiated"`
	SPCertificate         string        `json:"sp_certificate"`
	LastTestedAt          *time.Time    `json:"last_tested_at,omitempty"`
	LastTestError         *string       `json:"last_test_error,omitempty"`
	LastSuccessfulLoginAt *time.Time    `json:"last_successful_login_at,omitempty"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
	UpdatedByMemberID     *string       `json:"updated_by_member_id,omitempty"`
	Certificates          []Certificate `json:"certificates"`
}

// Tested reports whether a test assertion has succeeded for the current IdP
// identity. Changing the entity ID or SSO URL clears it.
func (c Configuration) Tested() bool { return c.LastTestedAt != nil }

// HasValidCertificate reports whether at least one signing certificate can
// verify a signature at t.
func (c Configuration) HasValidCertificate(t time.Time) bool {
	for _, certificate := range c.Certificates {
		if certificate.ValidAt(t) {
			return true
		}
	}
	return false
}

// ExpiringCertificates lists certificates that expire within window. The
// settings UI warns on these before an outage happens rather than after.
func (c Configuration) ExpiringCertificates(now time.Time, window time.Duration) []Certificate {
	expiring := make([]Certificate, 0, len(c.Certificates))
	for _, certificate := range c.Certificates {
		if certificate.NotAfter.After(now) && certificate.NotAfter.Before(now.Add(window)) {
			expiring = append(expiring, certificate)
		}
	}
	return expiring
}

// CanRequire reports whether Required mode may be selected, and why not.
func (c Configuration) CanRequire(now time.Time) error {
	if !c.Tested() {
		return ErrTestRequired
	}
	if !c.HasValidCertificate(now) {
		return ErrCertificateRequired
	}
	return nil
}

// Descriptor is the service-provider half an administrator gives to their IdP.
// Every value derives from the running environment.
type Descriptor struct {
	EntityID    string `json:"entity_id"`
	ACSURL      string `json:"acs_url"`
	MetadataURL string `json:"metadata_url"`
	StartURL    string `json:"start_url"`
	Certificate string `json:"certificate"`
}

// Policy is the unauthenticated login-page view of an organization's SSO
// settings. It carries organization-level policy only: no membership, no member
// list, and no hint about whether any particular email exists.
type Policy struct {
	Mode              auth.SSOMode `json:"mode"`
	StartURL          string       `json:"start_url,omitempty"`
	LocalLoginEnabled bool         `json:"local_login_enabled"`
	// BreakGlassAvailable tells the login page that a restricted owner recovery
	// path exists under Required mode. It reveals no identity: every workspace
	// under Required mode reports the same value.
	BreakGlassAvailable bool `json:"break_glass_available"`
}

// Purpose distinguishes a real sign-in from an administrator's validation run.
// A test never creates a session.
type Purpose string

const (
	PurposeLogin Purpose = "login"
	PurposeTest  Purpose = "test"
)

// AuthRequest is a pending AuthnRequest this service provider issued.
type AuthRequest struct {
	ID            string
	TenantID      string
	Purpose       Purpose
	RelayState    string
	ReturnTo      string
	ActorMemberID *string
	ExpiresAt     time.Time
}

// AssertedIdentity is the small, non-sensitive projection of a validated
// assertion that the rest of the flow needs. Attribute statements beyond the
// email claim are deliberately dropped: they are neither stored nor logged.
type AssertedIdentity struct {
	AssertionID  string
	IdPEntityID  string
	SubjectID    string
	Email        string
	NotOnOrAfter time.Time
	InResponseTo string
}
