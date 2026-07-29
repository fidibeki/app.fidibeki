package emailfeedback

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	campaignKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,63}$`)
	localePattern      = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)
)

func ValidateCampaign(input CampaignInput, creating bool) (CampaignValues, FieldErrors) {
	values := CampaignValues{
		ProjectID:         strings.TrimSpace(input.ProjectID),
		WidgetRuleID:      strings.TrimSpace(input.WidgetRuleID),
		CampaignKey:       strings.ToLower(strings.TrimSpace(input.CampaignKey)),
		Name:              strings.TrimSpace(input.Name),
		Status:            input.Status,
		DefaultLocale:     normalizeLocale(input.DefaultLocale),
		Subject:           strings.TrimSpace(input.Subject),
		Introduction:      strings.TrimSpace(input.Introduction),
		InvitationTTLDays: input.InvitationTTLDays,
	}
	fields := FieldErrors{}
	if _, err := uuid.Parse(values.ProjectID); err != nil {
		fields["project_id"] = "must identify a project"
	}
	if _, err := uuid.Parse(values.WidgetRuleID); err != nil {
		fields["widget_rule_id"] = "must identify a Widget Definition"
	}
	if !campaignKeyPattern.MatchString(values.CampaignKey) {
		fields["campaign_key"] = "must be 3-64 lowercase letters, numbers, or hyphens"
	}
	if count := utf8.RuneCountInString(values.Name); count < 1 || count > 120 {
		fields["name"] = "must contain between 1 and 120 characters"
	}
	if !validStatus(values.Status) ||
		(creating && values.Status != CampaignDraft && values.Status != CampaignActive) {
		fields["status"] = "must be draft or active"
	}
	if !localePattern.MatchString(values.DefaultLocale) || len(values.DefaultLocale) > 35 {
		fields["default_locale"] = "must identify a valid locale"
	}
	if count := utf8.RuneCountInString(values.Subject); count < 1 || count > 160 {
		fields["subject"] = "must contain between 1 and 160 characters"
	}
	if utf8.RuneCountInString(values.Introduction) > 1000 {
		fields["introduction"] = "must not exceed 1000 characters"
	}
	if values.InvitationTTLDays < 1 || values.InvitationTTLDays > 90 {
		fields["invitation_ttl_days"] = "must be between 1 and 90"
	}
	return values, fields
}

func ValidTransition(from CampaignStatus, to CampaignStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case CampaignDraft:
		return to == CampaignActive || to == CampaignPaused || to == CampaignArchived
	case CampaignActive:
		return to == CampaignPaused || to == CampaignArchived
	case CampaignPaused:
		return to == CampaignActive || to == CampaignArchived
	default:
		return false
	}
}

func validStatus(status CampaignStatus) bool {
	switch status {
	case CampaignDraft, CampaignActive, CampaignPaused, CampaignArchived:
		return true
	default:
		return false
	}
}

func normalizeLocale(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
}
