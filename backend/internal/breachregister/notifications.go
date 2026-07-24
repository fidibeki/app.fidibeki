package breachregister

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

const controllerNotificationTemplateVersion = 1

type ControllerNotificationKind string

const (
	ControllerNotificationInitial ControllerNotificationKind = "initial"
	ControllerNotificationUpdate  ControllerNotificationKind = "update"
	ControllerNotificationClosure ControllerNotificationKind = "closure"
)

type ControllerNotificationStatus string

const (
	ControllerNotificationDraft      ControllerNotificationStatus = "draft"
	ControllerNotificationPending    ControllerNotificationStatus = "pending"
	ControllerNotificationProcessing ControllerNotificationStatus = "processing"
	ControllerNotificationSent       ControllerNotificationStatus = "sent"
	ControllerNotificationFailed     ControllerNotificationStatus = "failed"
)

type ControllerNotificationAttemptStatus string

const (
	ControllerNotificationAttemptSent   ControllerNotificationAttemptStatus = "sent"
	ControllerNotificationAttemptFailed ControllerNotificationAttemptStatus = "failed"
)

type ControllerNotification struct {
	ID                        string                          `json:"id"`
	BreachIncidentID          string                          `json:"breach_incident_id"`
	TenantID                  string                          `json:"tenant_id"`
	NotificationKind          ControllerNotificationKind      `json:"notification_kind"`
	Sequence                  int                             `json:"sequence"`
	TemplateVersion           int                             `json:"template_version"`
	IncidentPublicID          string                          `json:"incident_public_id"`
	WorkspaceName             string                          `json:"workspace_name"`
	RecipientEmail            string                          `json:"recipient_email"`
	PrivacyProfileVersion     int                             `json:"privacy_profile_version"`
	AwarenessAt               time.Time                       `json:"awareness_at"`
	AffectedEnvironments      []string                        `json:"affected_environments"`
	AffectedSystemCategories  []string                        `json:"affected_system_categories"`
	AffectedDataCategories    []string                        `json:"affected_data_categories"`
	AffectedSubjectCategories []string                        `json:"affected_subject_categories"`
	ConfidentialityImpact     bool                            `json:"confidentiality_impact"`
	IntegrityImpact           bool                            `json:"integrity_impact"`
	AvailabilityImpact        bool                            `json:"availability_impact"`
	Summary                   string                          `json:"summary"`
	MeasuresTaken             string                          `json:"measures_taken"`
	NextUpdate                *string                         `json:"next_update,omitempty"`
	Status                    ControllerNotificationStatus    `json:"status"`
	Attempts                  int                             `json:"attempts"`
	LastError                 *string                         `json:"last_error,omitempty"`
	ProviderMessageID         *string                         `json:"provider_message_id,omitempty"`
	LastAttemptAt             *time.Time                      `json:"last_attempt_at,omitempty"`
	QueuedAt                  *time.Time                      `json:"queued_at,omitempty"`
	SentAt                    *time.Time                      `json:"sent_at,omitempty"`
	CreatedByPlatformAdminID  string                          `json:"created_by_platform_admin_id"`
	QueuedByPlatformAdminID   *string                         `json:"queued_by_platform_admin_id,omitempty"`
	CreatedAt                 time.Time                       `json:"created_at"`
	UpdatedAt                 time.Time                       `json:"updated_at"`
	DeliveryAttempts          []ControllerNotificationAttempt `json:"delivery_attempts"`
}

type ControllerNotificationAttempt struct {
	ID                string                              `json:"id"`
	NotificationID    string                              `json:"notification_id"`
	AttemptNumber     int                                 `json:"attempt_number"`
	Status            ControllerNotificationAttemptStatus `json:"status"`
	ProviderMessageID *string                             `json:"provider_message_id,omitempty"`
	ErrorMessage      *string                             `json:"error_message,omitempty"`
	AttemptedAt       time.Time                           `json:"attempted_at"`
}

type StageControllerNotificationInput struct {
	TenantID         string
	NotificationKind ControllerNotificationKind
	Summary          string
	MeasuresTaken    string
	NextUpdate       *string
}

func ParseControllerNotificationKind(value string) (ControllerNotificationKind, error) {
	switch ControllerNotificationKind(value) {
	case ControllerNotificationInitial, ControllerNotificationUpdate, ControllerNotificationClosure:
		return ControllerNotificationKind(value), nil
	default:
		return "", ErrInvalidRequest
	}
}

func normalizeStageControllerNotificationInput(
	input StageControllerNotificationInput,
) (StageControllerNotificationInput, error) {
	if _, err := ParseControllerNotificationKind(string(input.NotificationKind)); err != nil {
		return StageControllerNotificationInput{}, err
	}
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.Summary = strings.TrimSpace(input.Summary)
	input.MeasuresTaken = strings.TrimSpace(input.MeasuresTaken)
	if _, err := uuid.Parse(input.TenantID); err != nil {
		return StageControllerNotificationInput{}, ErrInvalidRequest
	}
	if input.TenantID == "" ||
		input.Summary == "" || len(input.Summary) > 4000 ||
		input.MeasuresTaken == "" || len(input.MeasuresTaken) > 4000 ||
		strings.ContainsRune(input.Summary, '\x00') ||
		strings.ContainsRune(input.MeasuresTaken, '\x00') {
		return StageControllerNotificationInput{}, ErrInvalidRequest
	}
	if input.NextUpdate != nil {
		nextUpdate := strings.TrimSpace(*input.NextUpdate)
		if nextUpdate == "" || len(nextUpdate) > 1000 || strings.ContainsRune(nextUpdate, '\x00') {
			return StageControllerNotificationInput{}, ErrInvalidRequest
		}
		input.NextUpdate = &nextUpdate
	}
	return input, nil
}

func stageControllerNotificationFingerprint(
	input StageControllerNotificationInput,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		Operation string                           `json:"operation"`
		Input     StageControllerNotificationInput `json:"input"`
	}{Operation: "stage_controller_notification", Input: input})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
