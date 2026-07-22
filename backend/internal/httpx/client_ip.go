package httpx

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIPFromRequest trusts forwarding data only when the immediate peer is configured as a proxy.
func ClientIPFromRequest(r *http.Request, trustedProxies []netip.Prefix) string {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return "unknown"
	}
	address := peer.Addr().Unmap()
	if !prefixesContain(trustedProxies, address) {
		return address.String()
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for index := len(forwarded) - 1; index >= 0; index-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(forwarded[index]))
		if err != nil {
			continue
		}
		candidate = candidate.Unmap()
		address = candidate
		if !prefixesContain(trustedProxies, candidate) {
			break
		}
	}
	return address.String()
}

func prefixesContain(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
