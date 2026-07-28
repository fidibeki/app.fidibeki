package samlsso

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestResponseInResponseToExtractsOnlyTheProtocolEnvelopeCandidate(t *testing.T) {
	t.Parallel()

	idp := newTestIdP(t, "https://idp.test/entity")
	solicited := validOptions(t)
	if got, err := ResponseInResponseTo(idp.response(t, solicited)); err != nil || got != "_request-1" {
		t.Fatalf("ResponseInResponseTo(solicited) = %q, %v", got, err)
	}

	unsolicited := validOptions(t)
	unsolicited.inResponseTo = ""
	if got, err := ResponseInResponseTo(idp.response(t, unsolicited)); err != nil || got != "" {
		t.Fatalf("ResponseInResponseTo(unsolicited) = %q, %v", got, err)
	}
}

func TestResponseInResponseToRejectsMalformedOrUnsafeEnvelopes(t *testing.T) {
	t.Parallel()

	encode := func(document string) string {
		return base64.StdEncoding.EncodeToString([]byte(document))
	}
	tests := map[string]string{
		"invalid base64": "not-base64",
		"malformed XML":  encode(`<samlp:Response`),
		"wrong root": encode(
			`<Assertion xmlns="urn:oasis:names:tc:SAML:2.0:assertion" InResponseTo="_request-1"/>`,
		),
		"wrong namespace": encode(`<Response xmlns="https://attacker.test" InResponseTo="_request-1"/>`),
		"oversized candidate": encode(
			`<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" InResponseTo="` +
				strings.Repeat("a", maxSAMLRequestIDBytes+1) + `"/>`,
		),
	}

	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ResponseInResponseTo(response); !errors.Is(err, ErrAssertionInvalid) {
				t.Fatalf("ResponseInResponseTo() error = %v, want ErrAssertionInvalid", err)
			}
		})
	}
}
