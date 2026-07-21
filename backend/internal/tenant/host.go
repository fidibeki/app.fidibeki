package tenant

import (
	"errors"
	"net"
	"regexp"
	"strings"
)

var (
	ErrNotTenantHost = errors.New("not a tenant host")
	ErrInvalidSlug   = errors.New("invalid tenant slug")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,22}[a-z0-9]$`)

var reservedSlugs = map[string]struct{}{
	"admin": {}, "api": {}, "app": {}, "auth": {}, "billing": {}, "demo": {},
	"dev": {}, "help": {}, "mail": {}, "staging": {}, "support": {}, "test": {},
	"widget": {}, "www": {},
}

func SlugFromHost(host, baseDomain string) (string, error) {
	hostname := strings.ToLower(strings.TrimSuffix(host, "."))
	if parsedHost, _, err := net.SplitHostPort(hostname); err == nil {
		hostname = parsedHost
	} else if strings.Contains(hostname, ":") {
		return "", ErrNotTenantHost
	}
	baseDomain = strings.ToLower(strings.TrimSuffix(baseDomain, "."))
	suffix := "." + baseDomain
	if !strings.HasSuffix(hostname, suffix) {
		return "", ErrNotTenantHost
	}

	slug := strings.TrimSuffix(hostname, suffix)
	if strings.Contains(slug, ".") || slug == "" {
		return "", ErrNotTenantHost
	}
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	return slug, nil
}

func ValidateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return ErrInvalidSlug
	}
	if _, reserved := reservedSlugs[slug]; reserved {
		return ErrInvalidSlug
	}
	return nil
}
