package tenant

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var ErrInvalidPublicBaseURL = errors.New("public base URL is invalid")

// PublicURL builds a browser-facing URL on a tenant's own host by taking the
// scheme and port from the configured public base URL and the host from the
// tenant slug plus the base domain. Every callback, metadata, and login URL is
// derived this way so a deployment's URLs always follow its own environment
// instead of a hardcoded staging or production host.
func PublicURL(publicBaseURL, baseDomain, slug, path string) (*url.URL, error) {
	base, err := url.Parse(publicBaseURL)
	if err != nil || !base.IsAbs() || base.Host == "" {
		return nil, ErrInvalidPublicBaseURL
	}
	baseDomain = strings.ToLower(strings.TrimSuffix(baseDomain, "."))
	if baseDomain == "" {
		return nil, ErrInvalidPublicBaseURL
	}
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	host := slug + "." + baseDomain
	if _, port, splitErr := net.SplitHostPort(base.Host); splitErr == nil && port != "" {
		host = net.JoinHostPort(host, port)
	}
	return &url.URL{Scheme: base.Scheme, Host: host, Path: path}, nil
}
