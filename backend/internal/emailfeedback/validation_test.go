package emailfeedback

import (
	"strings"
	"testing"
)

func TestValidateCampaignNormalizesAndRejectsUnsafeValues(t *testing.T) {
	values, fields := ValidateCampaign(CampaignInput{
		ProjectID:         "11111111-1111-4111-8111-111111111111",
		WidgetRuleID:      "22222222-2222-4222-8222-222222222222",
		CampaignKey:       "  Purchase-Followup ",
		Name:              "  Purchase follow-up ",
		Status:            CampaignDraft,
		DefaultLocale:     " PT_pt ",
		Subject:           "  How was it? ",
		Introduction:      "  Tell us about it. ",
		InvitationTTLDays: 30,
	}, true)
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.CampaignKey != "purchase-followup" ||
		values.DefaultLocale != "pt-pt" ||
		values.Name != "Purchase follow-up" ||
		values.Subject != "How was it?" ||
		values.Introduction != "Tell us about it." {
		t.Fatalf("values = %#v", values)
	}

	_, fields = ValidateCampaign(CampaignInput{
		ProjectID:         "11111111-1111-4111-8111-111111111111",
		WidgetRuleID:      "22222222-2222-4222-8222-222222222222",
		CampaignKey:       "unicode-limits",
		Name:              strings.Repeat("é", 120),
		Status:            CampaignDraft,
		DefaultLocale:     "pt",
		Subject:           strings.Repeat("ç", 160),
		Introduction:      strings.Repeat("ã", 1000),
		InvitationTTLDays: 30,
	}, true)
	if len(fields) != 0 {
		t.Fatalf("valid Unicode boundaries returned fields = %#v", fields)
	}

	_, fields = ValidateCampaign(CampaignInput{
		ProjectID: "bad", WidgetRuleID: "bad", CampaignKey: "UP",
		Name: "", Status: CampaignArchived, DefaultLocale: "not a locale",
		Subject: "", Introduction: string(make([]byte, 1001)),
		InvitationTTLDays: 91,
	}, true)
	for _, name := range []string{
		"project_id", "widget_rule_id", "campaign_key", "name", "status",
		"default_locale", "subject", "introduction", "invitation_ttl_days",
	} {
		if fields[name] == "" {
			t.Errorf("missing field error %q in %#v", name, fields)
		}
	}
}

func TestCampaignStatusTransitionsAreMonotonic(t *testing.T) {
	tests := []struct {
		from CampaignStatus
		to   CampaignStatus
		want bool
	}{
		{CampaignDraft, CampaignActive, true},
		{CampaignDraft, CampaignPaused, true},
		{CampaignActive, CampaignPaused, true},
		{CampaignPaused, CampaignActive, true},
		{CampaignActive, CampaignDraft, false},
		{CampaignPaused, CampaignDraft, false},
		{CampaignArchived, CampaignActive, false},
	}
	for _, test := range tests {
		if got := ValidTransition(test.from, test.to); got != test.want {
			t.Errorf("ValidTransition(%q, %q) = %t, want %t",
				test.from, test.to, got, test.want)
		}
	}
}
