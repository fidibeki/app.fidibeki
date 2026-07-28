package samlsso

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/outboundhttp"
	"github.com/crewjam/saml"
	xrv "github.com/mattermost/xml-roundtrip-validator"
)

// maxMetadataBytes bounds an IdP metadata document. Real documents are a few
// kilobytes; this stops a hostile or misconfigured endpoint from streaming
// unbounded data into the API.
const maxMetadataBytes = 512 << 10

const metadataFetchTimeout = 10 * time.Second

// RedirectBinding is the only AuthnRequest binding this SP issues.
const RedirectBinding = saml.HTTPRedirectBinding

// IdPDetails is the subset of an IdP's metadata this service provider uses.
type IdPDetails struct {
	EntityID     string
	SSOURL       string
	Certificates []Certificate
}

// ParseIdPMetadata extracts the entity ID, HTTP-Redirect SSO endpoint, and
// signing certificates from an IdP metadata document. Both <EntityDescriptor>
// and <EntitiesDescriptor> wrappers are accepted because real providers emit
// both.
func ParseIdPMetadata(document []byte, now time.Time) (IdPDetails, error) {
	if len(document) == 0 {
		return IdPDetails{}, ValidationError{Fields: map[string]string{
			"idp_metadata_xml": "metadata document is empty",
		}}
	}
	if len(document) > maxMetadataBytes {
		return IdPDetails{}, ValidationError{Fields: map[string]string{
			"idp_metadata_xml": "metadata document is too large",
		}}
	}
	entity, err := parseEntityDescriptor(document)
	if err != nil {
		return IdPDetails{}, ValidationError{Fields: map[string]string{
			"idp_metadata_xml": "metadata document could not be parsed",
		}}
	}
	if entity.EntityID == "" {
		return IdPDetails{}, ValidationError{Fields: map[string]string{
			"idp_metadata_xml": "metadata document has no entity ID",
		}}
	}
	details := IdPDetails{EntityID: entity.EntityID}
	for _, descriptor := range entity.IDPSSODescriptors {
		for _, service := range descriptor.SingleSignOnServices {
			if service.Binding == RedirectBinding && details.SSOURL == "" {
				details.SSOURL = service.Location
			}
		}
		for _, key := range descriptor.KeyDescriptors {
			// An empty use means the key serves every purpose, which the
			// metadata specification permits.
			if key.Use != "" && key.Use != "signing" {
				continue
			}
			for _, data := range key.KeyInfo.X509Data.X509Certificates {
				certificate, err := parseBase64Certificate(data.Data, now)
				if err != nil {
					continue
				}
				details.Certificates = append(details.Certificates, certificate)
			}
		}
	}
	fields := map[string]string{}
	if details.SSOURL == "" {
		fields["idp_sso_url"] = "metadata has no HTTP-Redirect single sign-on endpoint"
	} else {
		details.SSOURL = strings.TrimSpace(details.SSOURL)
		if err := validateIdPURL(details.SSOURL); err != nil {
			fields["idp_sso_url"] = err.Error()
		}
	}
	if len(details.Certificates) == 0 {
		fields["idp_certificate"] = "metadata has no usable signing certificate"
	}
	if len(fields) > 0 {
		return IdPDetails{}, ValidationError{Fields: fields}
	}
	details.Certificates = dedupeCertificates(details.Certificates)
	return details, nil
}

// parseEntityDescriptor unwraps the two shapes real identity providers publish:
// a bare <EntityDescriptor>, or an <EntitiesDescriptor> federation aggregate
// from which the first IdP entry is taken. The round-trip validator runs first
// so a document that parses differently on a second pass — the shape that
// enables XML signature-wrapping — is rejected before any of it is trusted.
func parseEntityDescriptor(document []byte) (*saml.EntityDescriptor, error) {
	if err := xrv.Validate(bytes.NewReader(document)); err != nil {
		return nil, fmt.Errorf("validate metadata XML: %w", err)
	}
	entity := &saml.EntityDescriptor{}
	err := xml.Unmarshal(document, entity)
	if err == nil {
		return entity, nil
	}
	// encoding/xml reports a wrapper mismatch only through this message.
	if !strings.Contains(err.Error(), "EntitiesDescriptor") {
		return nil, fmt.Errorf("parse metadata XML: %w", err)
	}
	entities := &saml.EntitiesDescriptor{}
	if err := xml.Unmarshal(document, entities); err != nil {
		return nil, fmt.Errorf("parse metadata aggregate: %w", err)
	}
	for index := range entities.EntityDescriptors {
		if len(entities.EntityDescriptors[index].IDPSSODescriptors) > 0 {
			return &entities.EntityDescriptors[index], nil
		}
	}
	return nil, errors.New("metadata aggregate contains no identity provider")
}

// ParseCertificate accepts a PEM block or bare base64 DER, which is what
// administrators paste out of an IdP console.
func ParseCertificate(value string, now time.Time) (Certificate, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Certificate{}, ValidationError{Fields: map[string]string{
			"certificate": "certificate is required",
		}}
	}
	if block, _ := pem.Decode([]byte(trimmed)); block != nil {
		if block.Type != "CERTIFICATE" {
			return Certificate{}, ValidationError{Fields: map[string]string{
				"certificate": "PEM block is not a certificate",
			}}
		}
		return certificateFromDER(block.Bytes, now)
	}
	return parseBase64Certificate(trimmed, now)
}

func parseBase64Certificate(value string, now time.Time) (Certificate, error) {
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	der, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return Certificate{}, ValidationError{Fields: map[string]string{
			"certificate": "certificate is not valid base64 or PEM",
		}}
	}
	return certificateFromDER(der, now)
}

func certificateFromDER(der []byte, now time.Time) (Certificate, error) {
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return Certificate{}, ValidationError{Fields: map[string]string{
			"certificate": "certificate could not be parsed",
		}}
	}
	if !now.IsZero() && now.After(parsed.NotAfter) {
		return Certificate{}, ValidationError{Fields: map[string]string{
			"certificate": "certificate expired on " + parsed.NotAfter.UTC().Format(time.RFC3339),
		}}
	}
	digest := sha256.Sum256(der)
	return Certificate{
		Certificate: base64.StdEncoding.EncodeToString(der),
		Fingerprint: hex.EncodeToString(digest[:]),
		Subject:     parsed.Subject.String(),
		NotBefore:   parsed.NotBefore.UTC(),
		NotAfter:    parsed.NotAfter.UTC(),
	}, nil
}

func dedupeCertificates(certificates []Certificate) []Certificate {
	seen := make(map[string]struct{}, len(certificates))
	unique := make([]Certificate, 0, len(certificates))
	for _, certificate := range certificates {
		if _, exists := seen[certificate.Fingerprint]; exists {
			continue
		}
		seen[certificate.Fingerprint] = struct{}{}
		unique = append(unique, certificate)
	}
	return unique
}

// MetadataFetcher retrieves an IdP metadata document by URL. The API makes this
// request on an administrator's behalf, so the destination is treated as
// attacker-influenced: only HTTPS is allowed outside local development, private
// and loopback address space is refused before and after DNS resolution, and
// redirects are not followed.
type MetadataFetcher struct {
	Client *http.Client
	// Outbound controls DNS resolution and the connected peer in non-local
	// environments. Its zero value applies the production-safe defaults.
	Outbound outboundhttp.Policy
	// AllowInsecure permits plain HTTP and private destinations. Only local and
	// test environments set it, matching the integration package's convention.
	AllowInsecure bool
}

func (f MetadataFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	target, err := f.validateURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var fetchClient *http.Client
	if f.AllowInsecure {
		client := f.Client
		if client == nil {
			client = &http.Client{}
		}
		localClient := *client
		fetchClient = &localClient
	} else {
		fetchClient = f.Outbound.NewClient(metadataFetchTimeout)
	}
	// A metadata URL must resolve directly. Following redirects would let a
	// public host bounce the request into private address space after the
	// destination check has already passed.
	fetchClient.Timeout = metadataFetchTimeout
	fetchClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, ValidationError{Fields: map[string]string{"idp_metadata_url": "URL is invalid"}}
	}
	request.Header.Set("Accept", "application/samlmetadata+xml, application/xml;q=0.9, */*;q=0.1")
	response, err := fetchClient.Do(request)
	if err != nil {
		return nil, ValidationError{Fields: map[string]string{
			"idp_metadata_url": "metadata URL could not be reached",
		}}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, ValidationError{Fields: map[string]string{
			"idp_metadata_url": fmt.Sprintf("metadata URL returned HTTP %d", response.StatusCode),
		}}
	}
	document, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return nil, ValidationError{Fields: map[string]string{
			"idp_metadata_url": "metadata document could not be read",
		}}
	}
	if len(document) > maxMetadataBytes {
		return nil, ValidationError{Fields: map[string]string{
			"idp_metadata_url": "metadata document is too large",
		}}
	}
	return document, nil
}

func (f MetadataFetcher) validateURL(ctx context.Context, rawURL string) (*url.URL, error) {
	invalid := func(message string) error {
		return ValidationError{Fields: map[string]string{"idp_metadata_url": message}}
	}
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !target.IsAbs() || target.Host == "" {
		return nil, invalid("metadata URL is invalid")
	}
	switch target.Scheme {
	case "https":
	case "http":
		if !f.AllowInsecure {
			return nil, invalid("metadata URL must use HTTPS")
		}
	default:
		return nil, invalid("metadata URL must use HTTPS")
	}
	if target.User != nil {
		return nil, invalid("metadata URL must not contain credentials")
	}
	if f.AllowInsecure {
		return target, nil
	}
	host := target.Hostname()
	if host == "" {
		return nil, invalid("metadata URL is invalid")
	}
	if err := f.Outbound.CheckHost(ctx, host); err != nil {
		return nil, invalid("metadata URL must resolve to a public address")
	}
	return target, nil
}
