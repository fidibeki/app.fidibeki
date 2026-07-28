package tenantmember

import "testing"

func TestTokenURLUsesConfiguredSecureDeploymentOrigin(t *testing.T) {
	t.Parallel()
	service := Service{
		PublicBaseURL: "https://api.fidibeki.com",
		BaseDomain:    "fidibeki.com",
	}

	got, err := service.tokenURL("acme", "/auth/verify", "secret-token")
	if err != nil {
		t.Fatalf("tokenURL() error = %v", err)
	}
	const want = "https://acme.fidibeki.com/auth/verify#token=secret-token"
	if got != want {
		t.Fatalf("tokenURL() = %q, want %q", got, want)
	}
}

func TestMemberEmailDomainUsesNormalizedEmailDomain(t *testing.T) {
	for _, test := range []struct {
		name  string
		email string
		want  string
	}{
		{name: "company domain", email: "member@acme.test", want: "acme.test"},
		{name: "nested domain", email: "member@people.eu.acme.test", want: "people.eu.acme.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := normalizeEmail(test.email)
			if err != nil {
				t.Fatalf("normalizeEmail() error = %v", err)
			}
			got, err := memberEmailDomain(normalized)
			if err != nil {
				t.Fatalf("memberEmailDomain() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("memberEmailDomain() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMemberEmailDomainRejectsMissingDomain(t *testing.T) {
	for _, value := range []string{
		"", "member", "member@", "member@localhost", "member@-acme.test",
		"member@acme-.test", "member@acme..test", "member@acme_test.com",
	} {
		if _, err := memberEmailDomain(value); err == nil {
			t.Errorf("memberEmailDomain(%q) error = nil", value)
		}
	}
}

func TestNormalizeEmailRejectsDomainsThatCannotBeStored(t *testing.T) {
	for _, value := range []string{"member@localhost", "member@acme_test.com"} {
		if _, err := normalizeEmail(value); err == nil {
			t.Errorf("normalizeEmail(%q) error = nil", value)
		}
	}
}
