package feedback

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestValidateEmailSubmissionUsesServerOwnedInvitationContext(t *testing.T) {
	invitationID := "11111111-1111-4111-8111-111111111111"
	ruleID := "22222222-2222-4222-8222-222222222222"
	contextURL := "https://shop.example.com/orders/123"
	configuration := emailSubmissionTestConfiguration(ruleID)

	values, fields := ValidateEmailSubmissionAt(
		EmailSubmissionRequest{
			Responses: map[string]json.RawMessage{
				"rating": json.RawMessage(`4`),
				"detail": json.RawMessage(`"The payment flow was clear."`),
			},
		},
		configuration,
		invitationID,
		ruleID,
		&contextURL,
		"pt",
		time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
	)

	if len(fields) != 0 {
		t.Fatalf("ValidateEmailSubmissionAt() fields = %#v", fields)
	}
	if values.ClientFeedbackID != invitationID || values.WidgetRuleID != ruleID ||
		values.Source != "email" || values.PageURL != contextURL {
		t.Fatalf("server-owned values = %#v", values)
	}
	if values.DefinitionVersion == nil || *values.DefinitionVersion != 7 ||
		values.Rating == nil || *values.Rating != 4 ||
		values.Text == nil || *values.Text != "The payment flow was clear." {
		t.Fatalf("captured response values = %#v", values)
	}
	if values.Referrer != nil || values.UserAgent != nil || values.OS != nil ||
		values.Browser != nil || values.Device != nil || values.ViewportWidth != nil ||
		values.ViewportHeight != nil || values.Language != nil ||
		len(values.Attachments) != 0 || values.Analytics != nil ||
		values.MobileApplicationID != nil || values.MobileScreen != nil {
		t.Fatalf("email submission inherited client metadata = %#v", values)
	}
}

func TestValidateEmailSubmissionRecordsExplicitCapturedConsent(t *testing.T) {
	invitationID := "11111111-1111-4111-8111-111111111111"
	ruleID := "22222222-2222-4222-8222-222222222222"
	configuration := emailSubmissionTestConfiguration(ruleID)
	configuration.Rules[0].CollectContact = true
	reporter := " PERSON@EXAMPLE.COM "

	values, fields := ValidateEmailSubmissionAt(
		EmailSubmissionRequest{
			Responses: map[string]json.RawMessage{
				"rating": json.RawMessage(`5`),
				"detail": json.RawMessage(`"Everything worked."`),
			},
			ReporterEmail: &reporter, NotifyReporter: true,
			Consent: &ReporterConsentInput{
				Purpose:                  ReporterConsentPurposeFeedbackUpdates,
				NoticeVersion:            ReporterConsentNoticeVersion,
				ControllerProfileVersion: 3,
				WidgetDefinitionVersion:  7,
				Locale:                   "pt",
				Granted:                  true,
			},
		},
		configuration,
		invitationID,
		ruleID,
		nil,
		"pt",
		time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
	)

	if len(fields) != 0 {
		t.Fatalf("ValidateEmailSubmissionAt() fields = %#v", fields)
	}
	if values.ReporterEmail == nil || *values.ReporterEmail != "person@example.com" ||
		values.ReporterConsentSource == nil ||
		*values.ReporterConsentSource != ReporterConsentSourceEmail ||
		values.ReporterConsentNoticeSnapshot == nil {
		t.Fatalf("email consent values = %#v", values)
	}
}

func emailSubmissionTestConfiguration(ruleID string) project.PublicConfiguration {
	return project.PublicConfiguration{
		TenantID:  "33333333-3333-4333-8333-333333333333",
		ProjectID: "44444444-4444-4444-8444-444444444444",
		Plan:      tenant.PlanGrowth,
		Status:    tenant.StatusActive,
		ControllerProfile: &project.ControllerProfile{
			Version:             3,
			Name:                "Acme",
			PrivacyContactEmail: "privacy@acme.test",
			PrivacyRightsEmail:  "rights@acme.test",
			PrivacyPolicyURL:    "https://shop.example.com/privacy",
		},
		ControllerProfileVersion: 3,
		Rules: []project.WidgetRule{{
			ID: ruleID, ProjectID: "44444444-4444-4444-8444-444444444444",
			Enabled: true, DefinitionVersion: 7,
			CanRate: true, CanWriteText: true,
			ResponseBlocks: []project.WidgetResponseBlock{
				{Key: "rating", Type: project.ResponseRating, Required: true},
				{Key: "detail", Type: project.ResponseFreeText, Required: true},
			},
			Localizations: map[string]project.WidgetLocalization{
				"pt": {
					PrivacyPurposeSummary:     "Enviar atualizações sobre este feedback.",
					PrivacyLawfulBasisSummary: "Consentimento.",
					PrivacyRetentionSummary:   "Até retirar o consentimento.",
				},
			},
		}},
	}
}
