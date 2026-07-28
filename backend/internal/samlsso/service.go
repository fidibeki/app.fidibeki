package samlsso

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

const (
	// defaultRequestTTL bounds how long an issued AuthnRequest stays
	// correlatable. Long enough for a real IdP login including an MFA prompt,
	// short enough that a captured response is useless soon after.
	defaultRequestTTL = 15 * time.Minute
	// defaultHandoffTTL bounds the single-use link between a validated
	// assertion and the session exchange. The browser follows it immediately.
	defaultHandoffTTL = 2 * time.Minute
	// CertificateExpiryWarning is how far ahead the settings view warns about a
	// signing certificate that is about to expire.
	CertificateExpiryWarning = 30 * 24 * time.Hour
	maxEmailDomains          = 20
)

// MagicLinkStore issues the single-use handoff a validated assertion redirects
// through. tenantmember.Store satisfies it.
type MagicLinkStore interface {
	CreateSAMLMagicLink(
		ctx context.Context, query db.Querier, tenantID, memberID, email, tokenHash string,
		expiresAt time.Time, idpEntityID, subjectID string,
	) error
}

type Service struct {
	Store      Store
	MagicLinks MagicLinkStore
	Endpoints  Endpoints
	Fetcher    MetadataFetcher
	RequestTTL time.Duration
	HandoffTTL time.Duration
	Now        func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) requestTTL() time.Duration {
	if s.RequestTTL > 0 {
		return s.RequestTTL
	}
	return defaultRequestTTL
}

func (s Service) handoffTTL() time.Duration {
	if s.HandoffTTL > 0 {
		return s.HandoffTTL
	}
	return defaultHandoffTTL
}

// ConfigurationInput is an administrator's requested configuration. Exactly one
// of MetadataURL, MetadataXML, or the manual entity ID / SSO URL / certificate
// triple supplies the identity provider's details.
type ConfigurationInput struct {
	MetadataURL       string
	MetadataXML       string
	IdPEntityID       string
	IdPSSOURL         string
	Certificates      []string
	EmailDomains      []string
	AllowIDPInitiated bool
	// ReplaceCertificates discards the previously trusted certificates instead
	// of adding to them. Rotation leaves it false so both the outgoing and
	// incoming certificates verify and no sign-in fails mid-rotation.
	ReplaceCertificates bool
}

// Configure creates or updates an organization's configuration.
func (s Service) Configure(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	input ConfigurationInput,
	actorMemberID string,
) (Configuration, error) {
	now := s.now()
	details, err := s.resolveIdPDetails(ctx, input, now)
	if err != nil {
		return Configuration{}, err
	}
	domains, err := normalizeEmailDomains(input.EmailDomains)
	if err != nil {
		return Configuration{}, err
	}
	upsert := UpsertInput{
		IdPEntityID: details.EntityID, IdPSSOURL: details.SSOURL,
		EmailDomains: domains, AllowIDPInitiated: input.AllowIDPInitiated,
		Certificates: details.Certificates, ReplaceCertificates: input.ReplaceCertificates,
		ActorMemberID: actorMemberID, Now: now,
	}
	if metadataURL := strings.TrimSpace(input.MetadataURL); metadataURL != "" {
		upsert.IdPMetadataURL = &metadataURL
	}
	// An organization configuring SAML for the first time gets its own service
	// provider key pair, so one workspace's integration is cryptographically
	// independent of every other's.
	if _, err := s.Store.Get(ctx, query, resolved.ID); errors.Is(err, ErrNotConfigured) {
		entityID, err := s.Endpoints.EntityID(resolved.Slug)
		if err != nil {
			return Configuration{}, err
		}
		keyPair, err := GenerateKeyPair(entityID, now)
		if err != nil {
			return Configuration{}, err
		}
		upsert.KeyPair = &keyPair
	} else if err != nil {
		return Configuration{}, err
	}
	return s.Store.Upsert(ctx, query, resolved.ID, upsert)
}

func (s Service) resolveIdPDetails(
	ctx context.Context, input ConfigurationInput, now time.Time,
) (IdPDetails, error) {
	switch {
	case strings.TrimSpace(input.MetadataURL) != "":
		document, err := s.Fetcher.Fetch(ctx, input.MetadataURL)
		if err != nil {
			return IdPDetails{}, err
		}
		return ParseIdPMetadata(document, now)
	case strings.TrimSpace(input.MetadataXML) != "":
		return ParseIdPMetadata([]byte(input.MetadataXML), now)
	default:
		return manualIdPDetails(input, now)
	}
}

func manualIdPDetails(input ConfigurationInput, now time.Time) (IdPDetails, error) {
	fields := map[string]string{}
	entityID := strings.TrimSpace(input.IdPEntityID)
	if entityID == "" {
		fields["idp_entity_id"] = "identity provider entity ID is required"
	} else if len(entityID) > 1024 {
		fields["idp_entity_id"] = "identity provider entity ID is too long"
	}
	ssoURL := strings.TrimSpace(input.IdPSSOURL)
	if err := validateIdPURL(ssoURL); err != nil {
		fields["idp_sso_url"] = err.Error()
	}
	certificates := make([]Certificate, 0, len(input.Certificates))
	for index, raw := range input.Certificates {
		certificate, err := ParseCertificate(raw, now)
		if err != nil {
			var validation ValidationError
			message := "certificate is invalid"
			if errors.As(err, &validation) {
				message = validation.Fields["certificate"]
			}
			fields[fmt.Sprintf("certificates.%d", index)] = message
			continue
		}
		certificates = append(certificates, certificate)
	}
	if len(certificates) == 0 {
		fields["certificates"] = "at least one signing certificate is required"
	}
	if len(fields) > 0 {
		return IdPDetails{}, ValidationError{Fields: fields}
	}
	return IdPDetails{
		EntityID: entityID, SSOURL: ssoURL, Certificates: dedupeCertificates(certificates),
	}, nil
}

func validateIdPURL(value string) error {
	if value == "" {
		return errors.New("single sign-on URL is required")
	}
	target, err := url.Parse(value)
	if err != nil || !target.IsAbs() || target.Hostname() == "" || target.User != nil {
		return errors.New("single sign-on URL is invalid")
	}
	if !strings.EqualFold(target.Scheme, "https") {
		return errors.New("single sign-on URL must use HTTPS")
	}
	if len(value) > 2048 {
		return errors.New("single sign-on URL is too long")
	}
	return nil
}

func normalizeEmailDomains(values []string) ([]string, error) {
	domains := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		domain := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "@")))
		if domain == "" {
			continue
		}
		if _, err := mail.ParseAddress("probe@" + domain); err != nil || !strings.Contains(domain, ".") {
			return nil, ValidationError{Fields: map[string]string{
				"email_domains": "domain " + domain + " is invalid",
			}}
		}
		if _, exists := seen[domain]; exists {
			continue
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	if len(domains) > maxEmailDomains {
		return nil, ValidationError{Fields: map[string]string{
			"email_domains": fmt.Sprintf("at most %d domains are supported", maxEmailDomains),
		}}
	}
	return domains, nil
}

// SetMode changes enforcement. Required is refused until a test assertion has
// succeeded against the current IdP identity and a valid signing certificate
// exists, which is what makes a lockout impossible to reach through this path.
func (s Service) SetMode(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	mode auth.SSOMode,
	actorMemberID string,
) (Configuration, error) {
	if !mode.Valid() {
		return Configuration{}, ValidationError{Fields: map[string]string{"mode": "mode is invalid"}}
	}
	configuration, err := s.Store.Get(ctx, query, resolved.ID)
	if err != nil {
		return Configuration{}, err
	}
	if mode == auth.SSOModeRequired {
		if err := configuration.CanRequire(s.now()); err != nil {
			return Configuration{}, err
		}
	}
	if err := s.Store.SetMode(ctx, query, resolved.ID, mode, actorMemberID); err != nil {
		return Configuration{}, err
	}
	return s.Store.Get(ctx, query, resolved.ID)
}

// RemoveCertificate retires one signing certificate. While SSO is enabled the
// last remaining certificate is protected, and removing the last *currently
// valid* one drops the tenant out of Required mode rather than leaving it
// enforcing an integration that can no longer verify a signature.
func (s Service) RemoveCertificate(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	certificateID string,
	actorMemberID string,
) (Configuration, error) {
	configuration, err := s.Store.Get(ctx, query, resolved.ID)
	if err != nil {
		return Configuration{}, err
	}
	// Resolved before the last-certificate guard so an unknown ID is reported as
	// missing rather than as the protected last certificate, which would tell
	// the caller something untrue about what they asked to remove.
	var known bool
	for _, certificate := range configuration.Certificates {
		if certificate.ID == certificateID {
			known = true
			break
		}
	}
	if !known {
		return Configuration{}, ErrCertificateNotFound
	}
	if configuration.Mode != auth.SSOModeDisabled && len(configuration.Certificates) <= 1 {
		return Configuration{}, ErrLastCertificate
	}
	if err := s.Store.RemoveCertificate(ctx, query, resolved.ID, certificateID); err != nil {
		return Configuration{}, err
	}
	updated, err := s.Store.Get(ctx, query, resolved.ID)
	if err != nil {
		return Configuration{}, err
	}
	if updated.Mode == auth.SSOModeRequired && !updated.HasValidCertificate(s.now()) {
		if err := s.Store.SetMode(ctx, query, resolved.ID, auth.SSOModeOptional, actorMemberID); err != nil {
			return Configuration{}, err
		}
		return s.Store.Get(ctx, query, resolved.ID)
	}
	return updated, nil
}

// Policy is the unauthenticated login-page view. A workspace without SSO and a
// workspace whose SSO is disabled are indistinguishable, and nothing here
// depends on who is asking.
func (s Service) Policy(
	ctx context.Context, query db.Querier, resolved tenant.Tenant,
) (Policy, error) {
	configuration, err := s.Store.Get(ctx, query, resolved.ID)
	if errors.Is(err, ErrNotConfigured) {
		return Policy{Mode: auth.SSOModeDisabled, LocalLoginEnabled: true}, nil
	}
	if err != nil {
		return Policy{}, err
	}
	policy := Policy{
		Mode:                configuration.Mode,
		LocalLoginEnabled:   configuration.Mode != auth.SSOModeRequired,
		BreakGlassAvailable: configuration.Mode == auth.SSOModeRequired,
	}
	if configuration.Mode != auth.SSOModeDisabled {
		startURL, err := s.Endpoints.Start(resolved.Slug)
		if err != nil {
			return Policy{}, err
		}
		policy.StartURL = startURL
	}
	return policy, nil
}

// Metadata renders the service-provider metadata document for this tenant.
func (s Service) Metadata(
	ctx context.Context, query db.Querier, resolved tenant.Tenant,
) ([]byte, error) {
	provider, _, err := s.provider(ctx, query, resolved)
	if err != nil {
		return nil, err
	}
	return provider.Metadata()
}

// Describe returns the service-provider half an administrator copies into their
// identity provider.
func (s Service) Describe(configuration Configuration, resolved tenant.Tenant) (Descriptor, error) {
	return s.Endpoints.Descriptor(resolved.Slug, configuration.SPCertificate)
}

// StartLogin issues an AuthnRequest and returns the IdP redirect. A test run
// uses the same path so an administrator validates exactly the flow their
// members will use.
func (s Service) StartLogin(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	purpose Purpose,
	returnTo string,
	actorMemberID string,
) (string, error) {
	provider, configuration, err := s.provider(ctx, query, resolved)
	if err != nil {
		return "", err
	}
	// A test must remain possible while SSO is still disabled — that is the
	// whole point of validating before enabling.
	if configuration.Mode == auth.SSOModeDisabled && purpose != PurposeTest {
		return "", ErrSSODisabled
	}
	redirectURL, requestID, err := provider.RedirectURL("")
	if err != nil {
		return "", err
	}
	request := AuthRequest{
		ID: requestID, TenantID: resolved.ID, Purpose: purpose,
		ReturnTo:  safeReturnTo(returnTo),
		ExpiresAt: s.now().Add(s.requestTTL()),
	}
	if actorMemberID != "" {
		request.ActorMemberID = &actorMemberID
	}
	if err := s.Store.CreateAuthRequest(ctx, query, request); err != nil {
		return "", err
	}
	return redirectURL, nil
}

// Outcome is the result of consuming a SAML response.
type Outcome struct {
	Purpose Purpose
	// RedirectURL is where the browser continues: the magic-link landing page
	// for a login, or the settings page for a test.
	RedirectURL string
	MemberID    string
	IdPEntityID string
	SubjectID   string
}

// ConsumeAssertion validates a SAML response and, for a real login, issues the
// single-use handoff that the normal session exchange consumes.
//
// The order matters: the assertion is fully validated, then recorded in the
// replay ledger, then correlated and consumed, and only then is any member
// state touched. Everything runs in the caller's tenant transaction, so a
// failure at any step leaves neither a session nor a half-consumed request.
func (s Service) ConsumeAssertion(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	encodedResponse string,
) (Outcome, error) {
	now := s.now()
	provider, configuration, err := s.provider(ctx, query, resolved)
	if err != nil {
		return Outcome{}, err
	}
	liveIDs, err := s.Store.LiveAuthRequestIDs(ctx, query, resolved.ID, now)
	if err != nil {
		return Outcome{}, err
	}
	identity, err := provider.ParseResponse(
		encodedResponse, PossibleRequestIDs(liveIDs, configuration.AllowIDPInitiated),
	)
	if err != nil {
		return Outcome{}, err
	}
	if err := s.Store.RecordAssertion(
		ctx, query, resolved.ID, AssertionHash(identity.AssertionID), identity.NotOnOrAfter,
	); err != nil {
		return Outcome{}, err
	}
	purpose := PurposeLogin
	returnTo := ""
	if identity.InResponseTo != "" {
		request, err := s.Store.ConsumeAuthRequest(ctx, query, resolved.ID, identity.InResponseTo, now)
		if err != nil {
			return Outcome{}, err
		}
		purpose = request.Purpose
		returnTo = request.ReturnTo
	} else if !configuration.AllowIDPInitiated {
		// Unreachable through ParseResponse, which already rejects an
		// uncorrelated response; kept so the invariant is enforced locally
		// rather than only by the library.
		return Outcome{}, ErrRequestNotFound
	}

	email, err := normalizeAssertedEmail(identity.Email, configuration.EmailDomains)
	if err != nil {
		return Outcome{}, err
	}

	if purpose == PurposeTest {
		if err := s.Store.MarkTested(ctx, query, resolved.ID, now, ""); err != nil {
			return Outcome{}, err
		}
		redirectURL, err := s.Endpoints.TestResult(resolved.Slug, "passed", "")
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{
			Purpose: PurposeTest, RedirectURL: redirectURL,
			IdPEntityID: identity.IdPEntityID, SubjectID: identity.SubjectID,
		}, nil
	}

	if configuration.Mode == auth.SSOModeDisabled {
		return Outcome{}, ErrSSODisabled
	}
	member, err := s.Store.ResolveMember(ctx, query, resolved.ID, email, now)
	if err != nil {
		return Outcome{}, err
	}
	rawToken, tokenHash, err := auth.NewOpaqueToken()
	if err != nil {
		return Outcome{}, err
	}
	if err := s.MagicLinks.CreateSAMLMagicLink(
		ctx, query, resolved.ID, member.ID, member.Email, tokenHash,
		now.Add(s.handoffTTL()), identity.IdPEntityID, identity.SubjectID,
	); err != nil {
		return Outcome{}, err
	}
	if err := s.Store.MarkLoginSuccess(ctx, query, resolved.ID, now); err != nil {
		return Outcome{}, err
	}
	redirectURL, err := s.Endpoints.Verify(resolved.Slug, rawToken, returnTo)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		Purpose: PurposeLogin, RedirectURL: redirectURL, MemberID: member.ID,
		IdPEntityID: identity.IdPEntityID, SubjectID: identity.SubjectID,
	}, nil
}

// RecordTestFailure stores why an administrator's validation run failed so the
// settings page can explain it without exposing the assertion.
func (s Service) RecordTestFailure(
	ctx context.Context, query db.Querier, tenantID, reason string,
) error {
	return s.Store.MarkTested(ctx, query, tenantID, s.now(), reason)
}

func (s Service) provider(
	ctx context.Context, query db.Querier, resolved tenant.Tenant,
) (*Provider, Configuration, error) {
	configuration, privateKey, err := s.Store.GetWithKey(ctx, query, resolved.ID)
	if err != nil {
		return nil, Configuration{}, err
	}
	provider, err := NewProvider(configuration, privateKey, s.Endpoints, resolved.Slug, s.Now)
	if err != nil {
		return nil, Configuration{}, err
	}
	return provider, configuration, nil
}

// normalizeAssertedEmail lowercases and validates the asserted email claim and
// enforces the organization's verified domain list when one is configured.
func normalizeAssertedEmail(value string, allowedDomains []string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", fmt.Errorf("%w: asserted email claim is not a valid address", ErrAssertionInvalid)
	}
	separator := strings.LastIndexByte(email, '@')
	if separator <= 0 || separator == len(email)-1 {
		return "", fmt.Errorf("%w: asserted email claim is not a valid address", ErrAssertionInvalid)
	}
	if len(allowedDomains) == 0 {
		return email, nil
	}
	domain := email[separator+1:]
	for _, allowed := range allowedDomains {
		if domain == allowed {
			return email, nil
		}
	}
	return "", fmt.Errorf("%w: asserted email domain is not associated with this workspace", ErrMemberNotEligible)
}

// safeReturnTo keeps a post-login destination on this application. Anything
// that could leave the origin — an absolute URL or a protocol-relative path —
// is dropped rather than rejected, so a tampered link still signs the member
// in, just to the default landing page.
func safeReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || len(value) > 512 {
		return ""
	}
	return value
}
