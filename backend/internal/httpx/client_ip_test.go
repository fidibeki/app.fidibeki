package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIPFromRequestOnlyTrustsConfiguredProxyPeers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		trusted    []netip.Prefix
		want       string
	}{
		{name: "untrusted peer cannot spoof", remoteAddr: "192.0.2.1:1234", forwarded: "203.0.113.7", want: "192.0.2.1"},
		{
			name: "trusted peer supplies client", remoteAddr: "192.0.2.1:1234", forwarded: "203.0.113.7",
			trusted: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, want: "203.0.113.7",
		},
		{
			name: "rightmost untrusted address wins", remoteAddr: "10.0.0.3:1234",
			forwarded: "203.0.113.99, 198.51.100.8, 10.0.0.2",
			trusted:   []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, want: "198.51.100.8",
		},
		{
			name: "malformed forwarding is ignored", remoteAddr: "192.0.2.1:1234", forwarded: "not-an-ip",
			trusted: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, want: "192.0.2.1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodPost, "http://widget.localhost/v1/widget/events", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-For", test.forwarded)
			if got := ClientIPFromRequest(request, test.trusted); got != test.want {
				t.Fatalf("ClientIPFromRequest() = %q, want %q", got, test.want)
			}
		})
	}
}
