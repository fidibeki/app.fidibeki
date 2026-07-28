package outboundhttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
)

func TestIsPublicAddressRejectsNonPublicRanges(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"0.0.0.0", "0.0.0.1", "10.0.0.1", "100.64.0.1", "127.0.0.1",
		"169.254.169.254", "172.16.0.1", "192.168.1.1", "198.18.0.1",
		"224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "ff02::1",
		"64:ff9b::7f00:1", "64:ff9b:1::7f00:1", "2002:7f00:1::1",
	} {
		if IsPublicAddress(netip.MustParseAddr(value)) {
			t.Errorf("IsPublicAddress(%q) = true, want false", value)
		}
	}

	for _, value := range []string{
		"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111",
	} {
		if !IsPublicAddress(netip.MustParseAddr(value)) {
			t.Errorf("IsPublicAddress(%q) = false, want true", value)
		}
	}
}

func TestPolicyRejectsMixedDNSAnswers(t *testing.T) {
	t.Parallel()

	policy := Policy{Resolver: &sequenceResolver{answers: [][]netip.Addr{{
		netip.MustParseAddr("93.184.216.34"),
		netip.MustParseAddr("10.0.0.1"),
	}}}}

	if err := policy.CheckHost(context.Background(), "mixed.example"); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("CheckHost() error = %v, want ErrUnsafeDestination", err)
	}
}

func TestGuardedClientRevalidatesDNSAtDialTime(t *testing.T) {
	t.Parallel()

	resolver := &sequenceResolver{answers: [][]netip.Addr{
		{netip.MustParseAddr("93.184.216.34")},
		{netip.MustParseAddr("127.0.0.1")},
	}}
	dialed := false
	policy := Policy{
		Resolver: resolver,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("must not dial")
		},
	}
	if err := policy.CheckHost(context.Background(), "rebind.example"); err != nil {
		t.Fatalf("preflight CheckHost() error = %v", err)
	}

	client := policy.NewClient(0)
	request, err := http.NewRequest(http.MethodGet, "https://rebind.example/metadata", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if _, err := client.Do(request); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("client.Do() error = %v, want ErrUnsafeDestination", err)
	}
	if dialed {
		t.Fatal("underlying dialer was called for a rebound private address")
	}
}

func TestGuardedClientPinsTheValidatedAddress(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		answer     string
		wantDialed string
	}{
		{name: "IPv4", answer: "93.184.216.34", wantDialed: "93.184.216.34:443"},
		{name: "IPv6", answer: "2606:4700:4700::1111", wantDialed: "[2606:4700:4700::1111]:443"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var dialed string
			policy := Policy{
				Resolver: &sequenceResolver{answers: [][]netip.Addr{{
					netip.MustParseAddr(testCase.answer),
				}}},
				DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
					dialed = address
					return nil, errors.New("stop after observing pinned address")
				},
			}
			request, err := http.NewRequest(http.MethodGet, "https://public.example/resource", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			if _, err := policy.NewClient(0).Do(request); err == nil ||
				!strings.Contains(err.Error(), "stop after observing pinned address") {
				t.Fatalf("client.Do() error = %v", err)
			}
			if dialed != testCase.wantDialed {
				t.Fatalf("dialed address = %q, want %q", dialed, testCase.wantDialed)
			}
		})
	}
}

func TestGuardedClientDisablesRedirects(t *testing.T) {
	t.Parallel()

	client := Policy{}.NewClient(0)
	if err := client.CheckRedirect(
		&http.Request{}, []*http.Request{{}},
	); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v, want http.ErrUseLastResponse", err)
	}
}

type sequenceResolver struct {
	mu      sync.Mutex
	answers [][]netip.Addr
	index   int
}

func (r *sequenceResolver) LookupNetIP(
	_ context.Context,
	_ string,
	_ string,
) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.answers) == 0 {
		return nil, errors.New("no DNS answers")
	}
	index := r.index
	if index >= len(r.answers) {
		index = len(r.answers) - 1
	}
	r.index++
	return append([]netip.Addr(nil), r.answers[index]...), nil
}
