package testimonial

import (
	"net/http/httptest"
	"testing"
)

// listLimit guards an unauthenticated-adjacent list endpoint, so a caller must
// not be able to ask for an unbounded page by passing a large or malformed
// value.
func TestListLimitIsBoundedAndForgiving(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{name: "absent falls back to the default", query: "", want: defaultListLimit},
		{name: "a sensible value is honoured", query: "?limit=10", want: 10},
		{name: "an oversized value is capped", query: "?limit=100000", want: maxListLimit},
		{name: "zero is not a page size", query: "?limit=0", want: defaultListLimit},
		{name: "a negative value is not a page size", query: "?limit=-5", want: defaultListLimit},
		{name: "nonsense falls back rather than failing", query: "?limit=lots", want: defaultListLimit},
		{name: "the maximum itself is allowed", query: "?limit=200", want: maxListLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("GET", "http://acme.test/v1/testimonials"+test.query, nil)
			if got := listLimit(request); got != test.want {
				t.Fatalf("listLimit(%q) = %d, want %d", test.query, got, test.want)
			}
		})
	}
}

// The consent record needs to show a human agent submitted the form. It does
// not need to identify the person, so the full user-agent string is reduced to
// a family and nothing finer.
func TestUserAgentFamilyKeepsOnlyTheFamily(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		agent string
		want  string
	}{
		{
			name:  "firefox",
			agent: "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0",
			want:  "firefox",
		},
		{
			// Edge advertises Chrome too, so ordering matters.
			name:  "edge is not reported as chrome",
			agent: "Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/126 Safari/537.36 Edg/126.0",
			want:  "edge",
		},
		{
			name:  "chrome",
			agent: "Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126 Safari/537.36",
			want:  "chrome",
		},
		{
			name:  "safari",
			agent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.5 Safari/605.1.15",
			want:  "safari",
		},
		{name: "absent", agent: "", want: "unknown"},
		{name: "anything else", agent: "curl/8.7.1", want: "other"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := userAgentFamily(test.agent)
			if got != test.want {
				t.Fatalf("userAgentFamily(%q) = %q, want %q", test.agent, got, test.want)
			}
			// Whatever the input, nothing resembling a full user-agent string
			// may survive into the stored evidence.
			if len(got) > 10 {
				t.Fatalf("userAgentFamily returned %q, which is more than a family", got)
			}
		})
	}
}

func TestBoolOrDefault(t *testing.T) {
	t.Parallel()
	enabled, disabled := true, false
	if !boolOrDefault(nil, true) {
		t.Error("an absent value must take the fallback")
	}
	if boolOrDefault(nil, false) {
		t.Error("an absent value must take the fallback")
	}
	if !boolOrDefault(&enabled, false) {
		t.Error("an explicit true must win over the fallback")
	}
	// The case that matters: an explicit false must not be mistaken for absent
	// and silently flipped back on by the default.
	if boolOrDefault(&disabled, true) {
		t.Error("an explicit false must win over a true fallback")
	}
}
