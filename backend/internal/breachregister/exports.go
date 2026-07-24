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

// mdInlineSpecialChars are the Markdown-significant punctuation characters
// mdEscapeInline neutralizes with a backslash escape (CommonMark treats a
// backslash before any ASCII punctuation character as a literal escape).
// Backslash itself is handled separately, first, in mdEscapeInline.
const mdInlineSpecialChars = "|`*_#[]<>~!"

// mdEscapeInline renders operator/customer-controlled text safely inside a
// single Markdown line (a bullet value, heading fragment, or table cell).
// Backslashes are doubled first so they render literally; embedded newlines
// and other control characters become visible escape sequences instead of
// starting a new physical line (which would otherwise let the value inject
// its own heading, list item, table row, or fenced block); and remaining
// Markdown-significant punctuation is individually backslash-escaped so the
// value can never open emphasis, a link, a code span, or a heading.
func mdEscapeInline(text string) string {
	var builder strings.Builder
	for _, r := range text {
		switch {
		case r == '\\':
			builder.WriteString(`\\`)
		case r == '\r':
			builder.WriteString(`\r`)
		case r == '\n':
			builder.WriteString(`\n`)
		case r == '\t':
			builder.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&builder, `\x%02X`, r)
		case strings.ContainsRune(mdInlineSpecialChars, r):
			builder.WriteRune('\\')
			builder.WriteRune(r)
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// mdSafeBlock renders long operator/customer-controlled free text (a
// summary, reasoning, or outcome field) as an opaque fenced code block.
// CommonMark parses fenced-block content as literal text -- no heading,
// list, table, emphasis, or link syntax is recognized inside it -- so
// heading-like or delimiter-heavy input can never restructure the document.
// The fence is chosen one backtick longer than the longest run of backticks
// already present in the text, so no line inside the text can ever be
// mistaken for (and prematurely close) the block.
func mdSafeBlock(text string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	fenceLength := max(longestBacktickRun(normalized)+1, 3)
	fence := strings.Repeat("`", fenceLength)
	return fence + "\n" + normalized + "\n" + fence
}

// writeMDBlock writes a label followed by text rendered as a safe fenced
// block (see mdSafeBlock).
func writeMDBlock(builder *strings.Builder, label, text string) {
	fmt.Fprintf(builder, "%s\n\n%s\n\n", label, mdSafeBlock(text))
}

// writeMDBlockOrPending writes a label followed by an optional free-text
// field rendered as a safe fenced block, or an explicit "(pending)" marker
// when the field has not been recorded yet.
func writeMDBlockOrPending(builder *strings.Builder, label string, text *string) {
	if text == nil {
		fmt.Fprintf(builder, "%s\n\n(pending)\n\n", label)
		return
	}
	writeMDBlock(builder, label, *text)
}

func longestBacktickRun(text string) int {
	longest, current := 0, 0
	for _, r := range text {
		if r == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	return longest
}

// optionalTimeOrPending renders a not-yet-occurred lifecycle timestamp (a
// delivery attempt, queue, send, or due time that may not have happened yet)
// as an explicit "(pending)" marker rather than an empty/omitted value.
func optionalTimeOrPending(value *time.Time) string {
	if value == nil {
		return "(pending)"
	}
	return value.UTC().Format(time.RFC3339)
}

// optionalRangeOrPending renders a paired minimum/maximum estimate as an
// explicit "(pending)" marker when neither bound has been recorded yet
// (normalizeEstimateRange guarantees the pair is never partially set).
func optionalRangeOrPending(min, max *int64) string {
	if min == nil || max == nil {
		return "(pending)"
	}
	return fmt.Sprintf("%d - %d", *min, *max)
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}

// joinOrNoneEscaped is joinOrNone for free-text values (evidence references,
// corrective references) that -- unlike a fixed enum set -- may contain
// Markdown-significant punctuation and must be escaped per element.
func joinOrNoneEscaped(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	escaped := make([]string, len(values))
	for index, value := range values {
		escaped[index] = mdEscapeInline(value)
	}
	return strings.Join(escaped, ", ")
}

func optionalOrNone(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "(none)"
	}
	return *value
}

// optionalOrNoneEscaped composes optionalOrNone with mdEscapeInline for
// optional free-text fields (provider message ID, last error, corrective
// reference) that may contain Markdown-significant punctuation.
func optionalOrNoneEscaped(value *string) string {
	return mdEscapeInline(optionalOrNone(value))
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

// renderNotificationDetail writes the full field set of one frozen
// ControllerNotification snapshot -- every stable ID, lifecycle timestamp,
// version/contact/content field, and its complete delivery-attempt ledger --
// at the given heading level. It backs both the single-notification export
// and each entry in the post-incident report's notification ledger, so both
// documents stay in parity by construction.
func renderNotificationDetail(builder *strings.Builder, notification ControllerNotification, headingLevel string) {
	fmt.Fprintf(builder, "%s Notification %s -- %s (sequence %d)\n\n",
		headingLevel, notification.ID, notification.NotificationKind, notification.Sequence)
	fmt.Fprintf(builder, "- Notification ID: %s\n", notification.ID)
	fmt.Fprintf(builder, "- Breach incident ID: %s\n", notification.BreachIncidentID)
	fmt.Fprintf(builder, "- Tenant ID: %s\n", notification.TenantID)
	fmt.Fprintf(builder, "- Incident public ID: %s\n", notification.IncidentPublicID)
	fmt.Fprintf(builder, "- Kind: %s (sequence %d, template version %d)\n",
		notification.NotificationKind, notification.Sequence, notification.TemplateVersion)
	fmt.Fprintf(builder, "- Status: %s\n", notification.Status)
	fmt.Fprintf(builder, "- Workspace: %s\n", mdEscapeInline(notification.WorkspaceName))
	fmt.Fprintf(builder, "- Recipient email: %s\n", mdEscapeInline(notification.RecipientEmail))
	fmt.Fprintf(builder, "- Privacy profile version: %d\n", notification.PrivacyProfileVersion)
	fmt.Fprintf(builder, "- Awareness time: %s\n", notification.AwarenessAt.Format(time.RFC3339))
	fmt.Fprintf(builder, "- Affected environments: %s\n", joinOrNone(notification.AffectedEnvironments))
	fmt.Fprintf(builder, "- Affected systems: %s\n", joinOrNone(notification.AffectedSystemCategories))
	fmt.Fprintf(builder, "- Data categories: %s\n", joinOrNone(notification.AffectedDataCategories))
	fmt.Fprintf(builder, "- Subject categories: %s\n", joinOrNone(notification.AffectedSubjectCategories))
	fmt.Fprintf(builder, "- Impact: %s\n", joinOrNone(impactLabels(notification)))
	fmt.Fprintf(builder, "- Attempts: %d\n", notification.Attempts)
	fmt.Fprintf(builder, "- Last error: %s\n", optionalOrNoneEscaped(notification.LastError))
	fmt.Fprintf(builder, "- Provider message ID: %s\n", optionalOrNoneEscaped(notification.ProviderMessageID))
	fmt.Fprintf(builder, "- Last attempt at: %s\n", optionalTimeOrPending(notification.LastAttemptAt))
	fmt.Fprintf(builder, "- Queued at: %s\n", optionalTimeOrPending(notification.QueuedAt))
	fmt.Fprintf(builder, "- Sent at: %s\n", optionalTimeOrPending(notification.SentAt))
	fmt.Fprintf(builder, "- Created by (platform admin ID): %s\n", notification.CreatedByPlatformAdminID)
	fmt.Fprintf(builder, "- Queued by (platform admin ID): %s\n", optionalOrNone(notification.QueuedByPlatformAdminID))
	fmt.Fprintf(builder, "- Created at: %s\n", notification.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(builder, "- Updated at: %s\n\n", notification.UpdatedAt.Format(time.RFC3339))

	writeMDBlock(builder, "Summary:", notification.Summary)
	writeMDBlock(builder, "Measures taken:", notification.MeasuresTaken)
	if notification.NextUpdate != nil {
		writeMDBlock(builder, "Next update:", *notification.NextUpdate)
	} else {
		builder.WriteString("Next update:\n\n(none provided)\n\n")
	}

	builder.WriteString("Delivery attempts:\n\n")
	if len(notification.DeliveryAttempts) == 0 {
		builder.WriteString("No delivery attempts recorded yet.\n\n")
	} else {
		builder.WriteString("| Attempt ID | Attempt | Status | Provider message ID | Error | Attempted at |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
		for _, attempt := range notification.DeliveryAttempts {
			fmt.Fprintf(builder, "| %s | %d | %s | %s | %s | %s |\n",
				attempt.ID, attempt.AttemptNumber, attempt.Status,
				optionalOrNoneEscaped(attempt.ProviderMessageID), optionalOrNoneEscaped(attempt.ErrorMessage),
				attempt.AttemptedAt.Format(time.RFC3339))
		}
		builder.WriteString("\n")
	}
}

func renderNotificationMarkdown(export NotificationExport) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Breach Controller Notification -- %s\n\n", export.IncidentPublicID)
	fmt.Fprintf(&builder, "- Schema version: %d\n", export.SchemaVersion)
	fmt.Fprintf(&builder, "- Generated at: %s\n", export.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Incident public ID: %s\n\n", export.IncidentPublicID)
	renderNotificationDetail(&builder, export.Notification, "##")
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
	fmt.Fprintf(&builder, "- Incident public ID: %s\n", incident.PublicID)
	fmt.Fprintf(&builder, "- Created by (platform admin ID): %s\n", incident.CreatedByPlatformAdminID)
	fmt.Fprintf(&builder, "- Created at: %s\n", incident.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Awareness time: %s\n", incident.AwarenessAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Authority notification deadline: %s\n",
		incident.AuthorityNotificationDeadlineAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Detection source: %s\n", incident.DetectionSource)
	fmt.Fprintf(&builder, "- Controller role: %s\n", incident.ControllerRole)
	fmt.Fprintf(&builder, "- Affected tenant IDs: %s\n\n", joinOrNone(incident.AffectedTenantIDs))

	builder.WriteString("## Impact\n\n")
	fmt.Fprintf(&builder, "- Affected environments: %s\n", joinOrNone(incident.AffectedEnvironments))
	fmt.Fprintf(&builder, "- Affected systems: %s\n", joinOrNone(incident.AffectedSystemCategories))
	fmt.Fprintf(&builder, "- Data categories: %s\n", joinOrNone(incident.AffectedDataCategories))
	fmt.Fprintf(&builder, "- Subject categories: %s\n", joinOrNone(incident.AffectedSubjectCategories))
	fmt.Fprintf(&builder, "- Impact dimensions: %s\n\n", joinOrNone(incidentImpactLabels(incident)))
	writeMDBlock(&builder, "Containment summary:", incident.ContainmentSummary)

	builder.WriteString("## Chronology\n\n")
	if len(report.Chronology) == 0 {
		builder.WriteString("No chronology entries recorded yet.\n\n")
	} else {
		for _, entry := range report.Chronology {
			fmt.Fprintf(&builder, "### Entry %s -- %s\n\n", entry.ID, entry.EntryKind)
			fmt.Fprintf(&builder, "- Entry ID: %s\n", entry.ID)
			fmt.Fprintf(&builder, "- Breach incident ID: %s\n", entry.BreachIncidentID)
			fmt.Fprintf(&builder, "- Entry kind: %s\n", entry.EntryKind)
			fmt.Fprintf(&builder, "- Actor (platform admin ID): %s\n", entry.ActorPlatformAdminID)
			fmt.Fprintf(&builder, "- Occurred at: %s\n", entry.OccurredAt.Format(time.RFC3339))
			fmt.Fprintf(&builder, "- Created at: %s\n", entry.CreatedAt.Format(time.RFC3339))
			fmt.Fprintf(&builder, "- Evidence references: %s\n\n", joinOrNoneEscaped(entry.EvidenceReferences))
			writeMDBlock(&builder, "Summary:", entry.Summary)
		}
	}

	builder.WriteString("## Latest Assessment\n\n")
	if report.LatestAssessment == nil {
		builder.WriteString("No assessment recorded yet (assessment_pending).\n\n")
	} else {
		assessment := *report.LatestAssessment
		fmt.Fprintf(&builder, "- Assessment ID: %s\n", assessment.ID)
		fmt.Fprintf(&builder, "- Breach incident ID: %s\n", assessment.BreachIncidentID)
		fmt.Fprintf(&builder, "- Assessed by (platform admin ID): %s\n", assessment.AssessedByPlatformAdminID)
		fmt.Fprintf(&builder, "- Assessed at: %s\n", assessment.AssessedAt.Format(time.RFC3339))
		fmt.Fprintf(&builder, "- Created at: %s\n", assessment.CreatedAt.Format(time.RFC3339))
		fmt.Fprintf(&builder, "- Estimated affected subjects: %s\n",
			optionalRangeOrPending(assessment.EstimatedAffectedSubjectsMin, assessment.EstimatedAffectedSubjectsMax))
		fmt.Fprintf(&builder, "- Estimated affected records: %s\n\n",
			optionalRangeOrPending(assessment.EstimatedAffectedRecordsMin, assessment.EstimatedAffectedRecordsMax))

		fmt.Fprintf(&builder, "- Authority notification decision: %s\n\n", assessment.AuthorityNotificationDecision)
		writeMDBlock(&builder, "Authority notification reasoning:", assessment.AuthorityNotificationReasoning)
		fmt.Fprintf(&builder, "- High risk to individuals: %s\n\n", assessment.HighRiskToIndividuals)
		writeMDBlock(&builder, "High risk reasoning:", assessment.HighRiskReasoning)
		fmt.Fprintf(&builder, "- Data subject communication decision: %s\n\n",
			assessment.DataSubjectCommunicationDecision)
		writeMDBlock(&builder, "Data subject communication reasoning:", assessment.DataSubjectCommunicationReasoning)

		writeMDBlockOrPending(&builder, "Likely consequences:", assessment.LikelyConsequences)
		writeMDBlockOrPending(&builder, "Recovery measures:", assessment.RecoveryMeasures)
		fmt.Fprintf(&builder, "- Evidence references: %s\n\n", joinOrNoneEscaped(assessment.EvidenceReferences))
	}

	builder.WriteString("## Current Actions\n\n")
	if len(report.Actions) == 0 {
		builder.WriteString("No actions recorded.\n\n")
	} else {
		for _, action := range report.Actions {
			fmt.Fprintf(&builder, "### Action %s -- %s\n\n", action.ActionID, mdEscapeInline(action.Title))
			fmt.Fprintf(&builder, "- Ledger event ID: %s\n", action.ID)
			fmt.Fprintf(&builder, "- Ledger sequence: %d\n", action.Sequence)
			fmt.Fprintf(&builder, "- Action ID: %s\n", action.ActionID)
			fmt.Fprintf(&builder, "- Revision: %d\n", action.Revision)
			fmt.Fprintf(&builder, "- Action kind: %s\n", action.ActionKind)
			if action.ContainmentArea != nil {
				fmt.Fprintf(&builder, "- Containment area: %s\n", *action.ContainmentArea)
			} else {
				builder.WriteString("- Containment area: (not applicable)\n")
			}
			fmt.Fprintf(&builder, "- Title: %s\n", mdEscapeInline(action.Title))
			fmt.Fprintf(&builder, "- Status: %s\n", action.Status)
			fmt.Fprintf(&builder, "- Owner (platform admin ID): %s\n", optionalOrNone(action.OwnerPlatformAdminID))
			fmt.Fprintf(&builder, "- Due at: %s\n", optionalTimeOrPending(action.DueAt))
			fmt.Fprintf(&builder, "- Corrective reference: %s\n", optionalOrNoneEscaped(action.CorrectiveReference))
			fmt.Fprintf(&builder, "- Evidence references: %s\n", joinOrNoneEscaped(action.EvidenceReferences))
			fmt.Fprintf(&builder, "- Actor (platform admin ID): %s\n", action.ActorPlatformAdminID)
			fmt.Fprintf(&builder, "- Occurred at: %s\n", action.OccurredAt.Format(time.RFC3339))
			fmt.Fprintf(&builder, "- Created at: %s\n\n", action.CreatedAt.Format(time.RFC3339))
			writeMDBlockOrPending(&builder, "Outcome summary:", action.OutcomeSummary)
		}
	}

	builder.WriteString("## Action History Summary\n\n")
	if len(report.ActionHistorySummary) == 0 {
		builder.WriteString("No action history recorded.\n\n")
	} else {
		builder.WriteString("| Action ID | Title | Current status | Current revision | Revisions | First occurred | Last occurred |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
		for _, summary := range report.ActionHistorySummary {
			fmt.Fprintf(&builder, "| %s | %s | %s | %d | %d | %s | %s |\n",
				summary.ActionID, mdEscapeInline(summary.Title), summary.CurrentStatus,
				summary.CurrentRevision, summary.RevisionCount,
				summary.FirstOccurredAt.Format(time.RFC3339), summary.LastOccurredAt.Format(time.RFC3339))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Staged Controller Notifications\n\n")
	if len(report.ControllerNotifications) == 0 {
		builder.WriteString("No controller notifications staged.\n\n")
	} else {
		for _, notification := range report.ControllerNotifications {
			renderNotificationDetail(&builder, notification, "###")
		}
	}

	builder.WriteString(nonConclusoryFooter + "\n")
	return builder.String()
}
