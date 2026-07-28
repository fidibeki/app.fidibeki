// Package outboundhttp protects HTTP requests whose destination is controlled
// by a tenant. It resolves and validates every address at dial time, then dials
// the selected IP directly so DNS rebinding cannot change the connected peer.
package outboundhttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

var (
	ErrUnsafeDestination     = errors.New("outbound HTTP destination is not public")
	ErrUnresolvedDestination = errors.New("outbound HTTP destination could not be resolved")
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type DialContextFunc func(context.Context, string, string) (net.Conn, error)

// Policy accepts injectable network dependencies so its DNS and dial behavior
// can be tested without relying on the host network.
type Policy struct {
	Resolver    Resolver
	DialContext DialContextFunc
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// Translation and transition ranges can encode an otherwise blocked IPv4
	// destination, so they are not safe tenant-controlled egress targets.
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

func IsPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsUnspecified() || address.IsLoopback() ||
		address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsMulticast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

// CheckHost resolves every answer and rejects the destination if any answer is
// unsafe. Rejecting mixed answer sets prevents a resolver or dialer from
// selecting a private address from an otherwise public-looking response.
func (p Policy) CheckHost(ctx context.Context, host string) error {
	_, err := p.resolve(ctx, host)
	return err
}

func (p Policy) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, ErrUnresolvedDestination
	}
	if literal, err := netip.ParseAddr(host); err == nil {
		literal = literal.Unmap()
		if !IsPublicAddress(literal) {
			return nil, ErrUnsafeDestination
		}
		return []netip.Addr{literal}, nil
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnresolvedDestination, err)
	}
	if len(addresses) == 0 {
		return nil, ErrUnresolvedDestination
	}
	validated := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !IsPublicAddress(address) {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeDestination, address)
		}
		validated = append(validated, address)
	}
	return validated, nil
}

// NewClient returns a redirect-free client whose transport does not use
// environment proxies. A proxy resolving the original hostname itself would
// otherwise bypass the pinned-IP guarantee.
func (p Policy) NewClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = p.dialContext
	transport.DialTLSContext = nil
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (p Policy) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnresolvedDestination, err)
	}
	addresses, err := p.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	dial := p.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	for _, resolved := range addresses {
		if (network == "tcp4" && !resolved.Is4()) || (network == "tcp6" && !resolved.Is6()) {
			continue
		}
		return dial(ctx, network, net.JoinHostPort(resolved.String(), port))
	}
	return nil, fmt.Errorf("%w: no address for %s", ErrUnresolvedDestination, network)
}
