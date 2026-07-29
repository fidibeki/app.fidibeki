package emailfeedback

import (
	"strings"
	"testing"
)

func TestValidateInvitationNormalizesTheIdempotencyContract(t *testing.T) {
	contextURL := " HTTPS://SHOP.Example.com:443/orders/caf%C3%A9 "
	locale := " PT_AO "
	values, fields := ValidateInvitation(InvitationInput{
		CampaignID:      " 11111111-1111-4111-8111-111111111111 ",
		ExternalEventID: " order-123 ",
		RecipientEmail:  " Customer@Example.COM ",
		Locale:          &locale,
		ContextURL:      &contextURL,
	})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.CampaignID != "11111111-1111-4111-8111-111111111111" ||
		values.ExternalEventID != "order-123" ||
		values.RecipientEmail != "customer@example.com" ||
		values.RequestedLocale != "pt-ao" ||
		values.ContextURL == nil ||
		*values.ContextURL != "https://shop.example.com/orders/caf%C3%A9" ||
		values.RecipientDigest != digestString("customer@example.com") ||
		len(values.RequestDigest) != 64 {
		t.Fatalf("values = %#v", values)
	}

	repeated, repeatedFields := ValidateInvitation(InvitationInput{
		CampaignID:      "11111111-1111-4111-8111-111111111111",
		ExternalEventID: "order-123",
		RecipientEmail:  "customer@example.com",
		Locale:          pointer("pt-ao"),
		ContextURL:      pointer("https://shop.example.com/orders/caf%C3%A9"),
	})
	if len(repeatedFields) != 0 || repeated.RequestDigest != values.RequestDigest {
		t.Fatalf("normalized retry = %#v fields=%#v", repeated, repeatedFields)
	}
	repeated.RecipientEmail = "other@example.com"
	repeated.RecipientDigest = digestString(repeated.RecipientEmail)
	if invitationRequestDigest(repeated) == values.RequestDigest {
		t.Fatal("changed recipient reused the request digest")
	}
}

func TestValidateInvitationRejectsUnsafeInput(t *testing.T) {
	tests := []struct {
		name  string
		input InvitationInput
		field string
	}{
		{
			name: "campaign", input: validInvitationInput(),
			field: "campaign_id",
		},
		{
			name: "event control", input: validInvitationInput(),
			field: "external_event_id",
		},
		{
			name: "recipient display name", input: validInvitationInput(),
			field: "recipient_email",
		},
		{
			name: "locale", input: validInvitationInput(),
			field: "locale",
		},
		{
			name: "http context", input: validInvitationInput(),
			field: "context_url",
		},
		{
			name: "context credentials", input: validInvitationInput(),
			field: "context_url",
		},
		{
			name: "context query", input: validInvitationInput(),
			field: "context_url",
		},
		{
			name: "context fragment", input: validInvitationInput(),
			field: "context_url",
		},
	}
	tests[0].input.CampaignID = "not-a-campaign"
	tests[1].input.ExternalEventID = "order\n123"
	tests[2].input.RecipientEmail = "Customer <customer@example.com>"
	tests[3].input.Locale = pointer("not a locale")
	tests[4].input.ContextURL = pointer("http://shop.example.com/orders/123")
	tests[5].input.ContextURL = pointer("https://user:secret@shop.example.com/orders/123")
	tests[6].input.ContextURL = pointer("https://shop.example.com/orders/123?email=customer@example.com")
	tests[7].input.ContextURL = pointer("https://shop.example.com/orders/123#customer")

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, fields := ValidateInvitation(test.input)
			if fields[test.field] == "" {
				t.Fatalf("missing %s error in %#v", test.field, fields)
			}
		})
	}

	tooLong := validInvitationInput()
	tooLong.ExternalEventID = strings.Repeat("é", 201)
	if _, fields := ValidateInvitation(tooLong); fields["external_event_id"] == "" {
		t.Fatalf("long Unicode event fields = %#v", fields)
	}
}

func TestValidateInvitationPreservesIPv6ContextHosts(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{
			input: "https://[2001:DB8::1]:443/orders/123",
			want:  "https://[2001:db8::1]/orders/123",
		},
		{
			input: "https://[2001:DB8::1]:8443/orders/123",
			want:  "https://[2001:db8::1]:8443/orders/123",
		},
	} {
		input := validInvitationInput()
		input.ContextURL = pointer(test.input)
		values, fields := ValidateInvitation(input)
		if len(fields) != 0 || values.ContextURL == nil || *values.ContextURL != test.want {
			t.Fatalf("ValidateInvitation(%q) = %#v fields=%#v", test.input, values.ContextURL, fields)
		}
	}
}

func TestInvitationTokenManagerDerivesOpaqueStableTokens(t *testing.T) {
	manager, err := NewInvitationTokenManager(strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("NewInvitationTokenManager() error = %v", err)
	}
	raw, hash, err := manager.Token("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	repeatedRaw, repeatedHash, _ := manager.Token("11111111-1111-4111-8111-111111111111")
	if raw != repeatedRaw || hash != repeatedHash ||
		!strings.HasPrefix(raw, "efi_") || len(raw) != 47 ||
		len(hash) != 64 || strings.Contains(raw, "11111111") {
		t.Fatalf("token raw=%q hash=%q", raw, hash)
	}
	otherManager, _ := NewInvitationTokenManager(strings.Repeat("b", 32))
	otherRaw, _, _ := otherManager.Token("11111111-1111-4111-8111-111111111111")
	if raw == otherRaw {
		t.Fatal("different secrets derived the same invitation token")
	}
	if _, _, err := manager.Token("invalid"); err == nil {
		t.Fatal("Token(invalid ID) error = nil")
	}
	if _, err := NewInvitationTokenManager("short"); err == nil {
		t.Fatal("NewInvitationTokenManager(short secret) error = nil")
	}
}

func validInvitationInput() InvitationInput {
	return InvitationInput{
		CampaignID:      "11111111-1111-4111-8111-111111111111",
		ExternalEventID: "order-123",
		RecipientEmail:  "customer@example.com",
		Locale:          pointer("pt"),
		ContextURL:      pointer("https://shop.example.com/orders/123"),
	}
}

func pointer(value string) *string {
	return &value
}
