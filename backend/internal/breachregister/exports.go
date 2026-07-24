// This file renders restricted, downloadable documentation from facts the
// register already records: a frozen controller-notification snapshot (see
// notifications.go) or a deterministic point-in-time post-incident report.
// Rendering never mutates state, never resends anything, and never adds a
// field the underlying stores don't already carry -- it only repackages the
// same facts GetIncident already returns as a downloadable document.
package breachregister

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const exportSchemaVersion = 1

type ExportFormat string

const (
	ExportFormatJSON     ExportFormat = "json"
	ExportFormatMarkdown ExportFormat = "markdown"
)

func ParseExportFormat(value string) (ExportFormat, error) {
	switch ExportFormat(value) {
	case ExportFormatJSON, ExportFormatMarkdown:
		return ExportFormat(value), nil
	default:
		return "", ErrInvalidRequest
	}
}

// nonConclusoryFooter mirrors the disclaimer already sent in the controller
// notification email (see controllerNotificationMessage in
// notification_delivery.go) so an exported document can't be read on its own
// as a legal determination.
const nonConclusoryFooter = "This document does not determine whether an authority or " +
	"affected-person notification is required, and does not assert incident closure or compliance."

// NotificationExport wraps one frozen ControllerNotification snapshot for
// download. Notification already carries its own DeliveryAttempts.
type NotificationExport struct {
	SchemaVersion    int                    `json:"schema_version"`
	GeneratedAt      time.Time              `json:"generated_at"`
	IncidentPublicID string                 `json:"incident_public_id"`
	Notification     ControllerNotification `json:"notification"`
}

func newNotificationExport(
	generatedAt time.Time, incidentPublicID string, notification ControllerNotification,
) NotificationExport {
	if notification.DeliveryAttempts == nil {
		notification.DeliveryAttempts = []ControllerNotificationAttempt{}
	}
	return NotificationExport{
		SchemaVersion: exportSchemaVersion, GeneratedAt: generatedAt.UTC(),
		IncidentPublicID: incidentPublicID, Notification: notification,
	}
}

// ActionHistorySummary is a pure count/timestamp rollup of one action's full
// revision history. It never states or implies whether the action is
// "complete" -- CurrentStatus is copied verbatim from the latest revision.
type ActionHistorySummary struct {
	ActionID        string           `json:"action_id"`
	ActionKind      ActionKind       `json:"action_kind"`
	ContainmentArea *ContainmentArea `json:"containment_area,omitempty"`
	Title           string           `json:"title"`
	CurrentStatus   ActionStatus     `json:"current_status"`
	CurrentRevision int              `json:"current_revision"`
	RevisionCount   int              `json:"revision_count"`
	FirstOccurredAt time.Time        `json:"first_occurred_at"`
	LastOccurredAt  time.Time        `json:"last_occurred_at"`
}

// actionHistorySummaries derives one summary per action from the full,
// already-loaded action-event ledger. events is expected in the same
// sequence order Store.listActions already returns.
func actionHistorySummaries(events []ActionEvent) []ActionHistorySummary {
	order := make([]string, 0)
	byAction := make(map[string]*ActionHistorySummary)
	for _, event := range events {
		summary, exists := byAction[event.ActionID]
		if !exists {
			summary = &ActionHistorySummary{
				ActionID: event.ActionID, ActionKind: event.ActionKind,
				ContainmentArea: event.ContainmentArea, Title: event.Title,
				FirstOccurredAt: event.OccurredAt,
			}
			byAction[event.ActionID] = summary
			order = append(order, event.ActionID)
		}
		summary.RevisionCount++
		summary.CurrentStatus = event.Status
		summary.CurrentRevision = event.Revision
		summary.LastOccurredAt = event.OccurredAt
	}
	summaries := make([]ActionHistorySummary, 0, len(order))
	for _, actionID := range order {
		summaries = append(summaries, *byAction[actionID])
	}
	return summaries
}

// PostIncidentReport is a deterministic, point-in-time rendering of every
// fact currently recorded against an incident. It never infers notifiability,
// high-risk status, closure, or compliance: LatestAssessment is an explicit
// nil when no assessment has been recorded yet, rather than an omitted key.
type PostIncidentReport struct {
	SchemaVersion           int                      `json:"schema_version"`
	GeneratedAt             time.Time                `json:"generated_at"`
	IncidentPublicID        string                   `json:"incident_public_id"`
	Incident                Incident                 `json:"incident"`
	Chronology              []ChronologyEntry        `json:"chronology"`
	LatestAssessment        *Assessment              `json:"latest_assessment"`
	Actions                 []ActionEvent            `json:"actions"`
	ActionHistorySummary    []ActionHistorySummary   `json:"action_history_summary"`
	ControllerNotifications []ControllerNotification `json:"controller_notifications"`
}

func newPostIncidentReport(
	generatedAt time.Time,
	incident Incident,
	chronology []ChronologyEntry,
	assessments []Assessment,
	actions []ActionEvent,
	actionEvents []ActionEvent,
	notifications []ControllerNotification,
) PostIncidentReport {
	var latestAssessment *Assessment
	if len(assessments) > 0 {
		latest := assessments[len(assessments)-1]
		latestAssessment = &latest
	}
	if chronology == nil {
		chronology = []ChronologyEntry{}
	}
	if actions == nil {
		actions = []ActionEvent{}
	}
	if notifications == nil {
		notifications = []ControllerNotification{}
	}
	for index := range notifications {
		if notifications[index].DeliveryAttempts == nil {
			notifications[index].DeliveryAttempts = []ControllerNotificationAttempt{}
		}
	}
	return PostIncidentReport{
		SchemaVersion: exportSchemaVersion, GeneratedAt: generatedAt.UTC(),
		IncidentPublicID: incident.PublicID, Incident: incident, Chronology: chronology,
		LatestAssessment: latestAssessment, Actions: actions,
		ActionHistorySummary: actionHistorySummaries(actionEvents), ControllerNotifications: notifications,
	}
}

func renderJSON(value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode breach export: %w", err)
	}
	return encoded, nil
}

func renderNotificationMarkdown(export NotificationExport) string {
	var builder strings.Builder
	notification := export.Notification
	fmt.Fprintf(&builder, "# Breach Controller Notification -- %s\n\n", export.IncidentPublicID)
	fmt.Fprintf(&builder, "- Schema version: %d\n", export.SchemaVersion)
	fmt.Fprintf(&builder, "- Generated at: %s\n", export.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Notification ID: %s\n", notification.ID)
	fmt.Fprintf(&builder, "- Kind: %s (sequence %d, template version %d)\n",
		notification.NotificationKind, notification.Sequence, notification.TemplateVersion)
	fmt.Fprintf(&builder, "- Status: %s\n", notification.Status)
	fmt.Fprintf(&builder, "- Recipient: %s (%s)\n", notification.RecipientEmail, notification.WorkspaceName)
	fmt.Fprintf(&builder, "- Privacy profile version: %d\n", notification.PrivacyProfileVersion)
	fmt.Fprintf(&builder, "- Awareness time: %s\n\n", notification.AwarenessAt.Format(time.RFC3339))

	builder.WriteString("## Content\n\n")
	fmt.Fprintf(&builder, "- Affected environments: %s\n", joinOrNone(notification.AffectedEnvironments))
	fmt.Fprintf(&builder, "- Affected systems: %s\n", joinOrNone(notification.AffectedSystemCategories))
	fmt.Fprintf(&builder, "- Data categories: %s\n", joinOrNone(notification.AffectedDataCategories))
	fmt.Fprintf(&builder, "- Subject categories: %s\n", joinOrNone(notification.AffectedSubjectCategories))
	fmt.Fprintf(&builder, "- Impact: %s\n\n", joinOrNone(impactLabels(notification)))
	fmt.Fprintf(&builder, "**Summary:** %s\n\n", notification.Summary)
	fmt.Fprintf(&builder, "**Measures taken:** %s\n\n", notification.MeasuresTaken)
	if notification.NextUpdate != nil {
		fmt.Fprintf(&builder, "**Next update:** %s\n\n", *notification.NextUpdate)
	} else {
		builder.WriteString("**Next update:** (none provided)\n\n")
	}

	builder.WriteString("## Delivery Evidence\n\n")
	if len(notification.DeliveryAttempts) == 0 {
		builder.WriteString("No delivery attempts recorded yet.\n\n")
	} else {
		builder.WriteString("| Attempt | Status | Provider message ID | Error | Attempted at |\n")
		builder.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, attempt := range notification.DeliveryAttempts {
			fmt.Fprintf(&builder, "| %d | %s | %s | %s | %s |\n",
				attempt.AttemptNumber, attempt.Status, optionalOrNone(attempt.ProviderMessageID),
				optionalOrNone(attempt.ErrorMessage), attempt.AttemptedAt.Format(time.RFC3339))
		}
		builder.WriteString("\n")
	}

	builder.WriteString(nonConclusoryFooter + "\n")
	return builder.String()
}

func renderPostIncidentReportMarkdown(report PostIncidentReport) string {
	var builder strings.Builder
	incident := report.Incident
	fmt.Fprintf(&builder, "# Post-Incident Report -- %s\n\n", report.IncidentPublicID)
	fmt.Fprintf(&builder, "- Schema version: %d\n", report.SchemaVersion)
	fmt.Fprintf(&builder, "- Generated at: %s\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Incident ID: %s\n", incident.ID)
	fmt.Fprintf(&builder, "- Awareness time: %s\n", incident.AwarenessAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Authority notification deadline: %s\n",
		incident.AuthorityNotificationDeadlineAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Detection source: %s\n", incident.DetectionSource)
	fmt.Fprintf(&builder, "- Controller role: %s\n", incident.ControllerRole)
	fmt.Fprintf(&builder, "- Affected tenants: %d\n\n", len(incident.AffectedTenantIDs))

	builder.WriteString("## Impact\n\n")
	fmt.Fprintf(&builder, "- Affected environments: %s\n", joinOrNone(incident.AffectedEnvironments))
	fmt.Fprintf(&builder, "- Affected systems: %s\n", joinOrNone(incident.AffectedSystemCategories))
	fmt.Fprintf(&builder, "- Data categories: %s\n", joinOrNone(incident.AffectedDataCategories))
	fmt.Fprintf(&builder, "- Subject categories: %s\n", joinOrNone(incident.AffectedSubjectCategories))
	fmt.Fprintf(&builder, "- Impact dimensions: %s\n\n", joinOrNone(incidentImpactLabels(incident)))
	fmt.Fprintf(&builder, "**Containment summary:** %s\n\n", incident.ContainmentSummary)

	builder.WriteString("## Chronology\n\n")
	if len(report.Chronology) == 0 {
		builder.WriteString("No chronology entries recorded yet.\n\n")
	} else {
		for _, entry := range report.Chronology {
			fmt.Fprintf(&builder, "- %s -- %s: %s (evidence: %s)\n",
				entry.OccurredAt.Format(time.RFC3339), entry.EntryKind, entry.Summary,
				joinOrNone(entry.EvidenceReferences))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Latest Assessment\n\n")
	if report.LatestAssessment == nil {
		builder.WriteString("No assessment recorded yet (assessment_pending).\n\n")
	} else {
		assessment := *report.LatestAssessment
		fmt.Fprintf(&builder, "- Authority notification decision: %s -- %s\n",
			assessment.AuthorityNotificationDecision, assessment.AuthorityNotificationReasoning)
		fmt.Fprintf(&builder, "- High risk to individuals: %s -- %s\n",
			assessment.HighRiskToIndividuals, assessment.HighRiskReasoning)
		fmt.Fprintf(&builder, "- Data subject communication decision: %s -- %s\n",
			assessment.DataSubjectCommunicationDecision, assessment.DataSubjectCommunicationReasoning)
		fmt.Fprintf(&builder, "- Assessed at: %s\n\n", assessment.AssessedAt.Format(time.RFC3339))
	}

	builder.WriteString("## Current Actions\n\n")
	if len(report.Actions) == 0 {
		builder.WriteString("No actions recorded.\n\n")
	} else {
		builder.WriteString("| Kind | Title | Status | Revision |\n")
		builder.WriteString("| --- | --- | --- | --- |\n")
		for _, action := range report.Actions {
			fmt.Fprintf(&builder, "| %s | %s | %s | %d |\n",
				action.ActionKind, action.Title, action.Status, action.Revision)
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Action History Summary\n\n")
	if len(report.ActionHistorySummary) == 0 {
		builder.WriteString("No action history recorded.\n\n")
	} else {
		builder.WriteString("| Title | Current status | Revisions | First occurred | Last occurred |\n")
		builder.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, summary := range report.ActionHistorySummary {
			fmt.Fprintf(&builder, "| %s | %s | %d | %s | %s |\n",
				summary.Title, summary.CurrentStatus, summary.RevisionCount,
				summary.FirstOccurredAt.Format(time.RFC3339), summary.LastOccurredAt.Format(time.RFC3339))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Staged Controller Notifications\n\n")
	if len(report.ControllerNotifications) == 0 {
		builder.WriteString("No controller notifications staged.\n\n")
	} else {
		builder.WriteString("| Kind | Sequence | Status | Recipient | Attempts |\n")
		builder.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, notification := range report.ControllerNotifications {
			fmt.Fprintf(&builder, "| %s | %d | %s | %s | %d |\n",
				notification.NotificationKind, notification.Sequence, notification.Status,
				notification.RecipientEmail, notification.Attempts)
		}
		builder.WriteString("\n")
	}

	builder.WriteString(nonConclusoryFooter + "\n")
	return builder.String()
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}

func optionalOrNone(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "(none)"
	}
	return *value
}

func impactLabels(notification ControllerNotification) []string {
	labels := make([]string, 0, 3)
	if notification.ConfidentialityImpact {
		labels = append(labels, "confidentiality")
	}
	if notification.IntegrityImpact {
		labels = append(labels, "integrity")
	}
	if notification.AvailabilityImpact {
		labels = append(labels, "availability")
	}
	return labels
}

func incidentImpactLabels(incident Incident) []string {
	labels := make([]string, 0, 3)
	if incident.ConfidentialityImpact {
		labels = append(labels, "confidentiality")
	}
	if incident.IntegrityImpact {
		labels = append(labels, "integrity")
	}
	if incident.AvailabilityImpact {
		labels = append(labels, "availability")
	}
	return labels
}
