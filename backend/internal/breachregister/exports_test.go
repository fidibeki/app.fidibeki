package breachregister

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseExportFormat(t *testing.T) {
	t.Parallel()
	if format, err := ParseExportFormat("json"); err != nil || format != ExportFormatJSON {
		t.Fatalf("ParseExportFormat(json) = %v, %v", format, err)
	}
	if format, err := ParseExportFormat("markdown"); err != nil || format != ExportFormatMarkdown {
		t.Fatalf("ParseExportFormat(markdown) = %v, %v", format, err)
	}
	for _, invalid := range []string{"", "pdf", "JSON", "html"} {
		if _, err := ParseExportFormat(invalid); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("ParseExportFormat(%q) error = %v, want ErrInvalidRequest", invalid, err)
		}
	}
}

func TestActionHistorySummariesRollsUpFullLedger(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	events := []ActionEvent{
		{ActionID: "a1", ActionKind: ActionContainment, Title: "Rotate keys", Status: ActionPending,
			Revision: 1, OccurredAt: base},
		{ActionID: "a2", ActionKind: ActionPostIncident, Title: "Owner review", Status: ActionPending,
			Revision: 1, OccurredAt: base.Add(time.Hour)},
		{ActionID: "a1", ActionKind: ActionContainment, Title: "Rotate keys", Status: ActionInProgress,
			Revision: 2, OccurredAt: base.Add(2 * time.Hour)},
		{ActionID: "a1", ActionKind: ActionContainment, Title: "Rotate keys", Status: ActionCompleted,
			Revision: 3, OccurredAt: base.Add(3 * time.Hour)},
	}
	summaries := actionHistorySummaries(events)
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(summaries))
	}
	if summaries[0].ActionID != "a1" || summaries[0].RevisionCount != 3 ||
		summaries[0].CurrentRevision != 3 || summaries[0].CurrentStatus != ActionCompleted ||
		!summaries[0].FirstOccurredAt.Equal(base) || !summaries[0].LastOccurredAt.Equal(base.Add(3*time.Hour)) {
		t.Fatalf("summaries[0] = %#v", summaries[0])
	}
	if summaries[1].ActionID != "a2" || summaries[1].RevisionCount != 1 ||
		summaries[1].CurrentStatus != ActionPending {
		t.Fatalf("summaries[1] = %#v", summaries[1])
	}
}

func TestActionHistorySummariesEmpty(t *testing.T) {
	t.Parallel()
	summaries := actionHistorySummaries(nil)
	if len(summaries) != 0 {
		t.Fatalf("actionHistorySummaries(nil) = %#v, want empty", summaries)
	}
}

func TestNewPostIncidentReportPendingAssessmentIsExplicitNull(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	report := newPostIncidentReport(now, Incident{PublicID: "BR-AAAAAAAA"}, nil, nil, nil, nil, nil)
	if report.LatestAssessment != nil {
		t.Fatalf("LatestAssessment = %#v, want nil", report.LatestAssessment)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	rawValue, present := decoded["latest_assessment"]
	if !present {
		t.Fatalf("latest_assessment key omitted from report JSON: %s", encoded)
	}
	if rawValue != nil {
		t.Fatalf("latest_assessment = %#v, want explicit null", rawValue)
	}
	for _, key := range []string{"chronology", "actions", "action_history_summary", "controller_notifications"} {
		value, ok := decoded[key].([]any)
		if !ok || value == nil {
			t.Errorf("%s = %#v, want a present (possibly empty) array, not omitted/null", key, decoded[key])
		}
	}
}

func TestNewPostIncidentReportUsesMostRecentAssessment(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	assessments := []Assessment{
		{ID: "assess-1", AuthorityNotificationDecision: AuthorityNotificationAssessmentPending},
		{ID: "assess-2", AuthorityNotificationDecision: AuthorityNotificationNotRequired},
	}
	report := newPostIncidentReport(
		now, Incident{PublicID: "BR-AAAAAAAA"}, nil, assessments, nil, nil, nil,
	)
	if report.LatestAssessment == nil || report.LatestAssessment.ID != "assess-2" {
		t.Fatalf("LatestAssessment = %#v, want assess-2", report.LatestAssessment)
	}
}

func TestNewNotificationExportNormalizesNilDeliveryAttempts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	export := newNotificationExport(now, "BR-AAAAAAAA", ControllerNotification{ID: "n1"})
	if export.Notification.DeliveryAttempts == nil {
		t.Fatalf("DeliveryAttempts = nil, want empty slice")
	}
	if export.SchemaVersion != exportSchemaVersion || !export.GeneratedAt.Equal(now) {
		t.Fatalf("export wrapper = %#v", export)
	}
}

func sampleNotificationExport() NotificationExport {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	nextUpdate := "Another update follows in 48 hours."
	providerID := "provider-msg-1"
	return newNotificationExport(now, "BR-AAAAAAAA", ControllerNotification{
		ID: "notification-1", BreachIncidentID: "incident-1", TenantID: "tenant-1",
		NotificationKind: ControllerNotificationInitial, Sequence: 1, TemplateVersion: 1,
		IncidentPublicID: "BR-AAAAAAAA", WorkspaceName: "Acme Workspace",
		RecipientEmail: "privacy@acme.example", PrivacyProfileVersion: 3,
		AwarenessAt:               now.Add(-2 * time.Hour),
		AffectedEnvironments:      []string{"production"},
		AffectedSystemCategories:  []string{"database"},
		AffectedDataCategories:    []string{"contact_info"},
		AffectedSubjectCategories: []string{"tenant_members"},
		ConfidentialityImpact:     true,
		Summary:                   "A database access control failed.",
		MeasuresTaken:             "Access was revoked and credentials rotated.",
		NextUpdate:                &nextUpdate,
		Status:                    ControllerNotificationSent,
		Attempts:                  1,
		CreatedAt:                 now.Add(-3 * time.Hour),
		UpdatedAt:                 now.Add(-time.Hour),
		DeliveryAttempts: []ControllerNotificationAttempt{
			{ID: "attempt-1", NotificationID: "notification-1", AttemptNumber: 1,
				Status: ControllerNotificationAttemptSent, ProviderMessageID: &providerID,
				AttemptedAt: now.Add(-time.Hour)},
		},
	})
}

func TestRenderNotificationMarkdownIsDeterministicAndContainsFrozenContent(t *testing.T) {
	t.Parallel()
	export := sampleNotificationExport()
	first := renderNotificationMarkdown(export)
	second := renderNotificationMarkdown(export)
	if first != second {
		t.Fatalf("renderNotificationMarkdown is not deterministic:\n%s\n---\n%s", first, second)
	}
	for _, want := range []string{
		"BR-AAAAAAAA", "initial", "A database access control failed.",
		"Access was revoked and credentials rotated.", "Another update follows in 48 hours.",
		"privacy@acme.example", "provider-msg-1", "sent",
		"does not determine whether an authority or affected-person notification is required",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("markdown missing %q:\n%s", want, first)
		}
	}
}

func TestRenderNotificationMarkdownHandlesPendingNextUpdateAndAttempts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	export := newNotificationExport(now, "BR-AAAAAAAA", ControllerNotification{
		ID: "n1", NotificationKind: ControllerNotificationInitial, Sequence: 1,
		RecipientEmail: "privacy@acme.example", WorkspaceName: "Acme", Summary: "s", MeasuresTaken: "m",
		Status: ControllerNotificationDraft,
	})
	markdown := renderNotificationMarkdown(export)
	if !strings.Contains(markdown, "(none provided)") {
		t.Errorf("markdown missing pending next-update marker:\n%s", markdown)
	}
	if !strings.Contains(markdown, "No delivery attempts recorded yet.") {
		t.Errorf("markdown missing empty-attempts marker:\n%s", markdown)
	}
}

func samplePostIncidentReport() PostIncidentReport {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	incident := Incident{
		ID: "incident-1", PublicID: "BR-BBBBBBBB", AwarenessAt: now.Add(-48 * time.Hour),
		DetectionSource: DetectionInternalMonitoring, ControllerRole: RoleProcessor,
		AffectedEnvironments: []string{"production"}, ContainmentSummary: "Contained the affected path.",
		AffectedTenantIDs:               []string{"tenant-1"},
		ConfidentialityImpact:           true,
		AuthorityNotificationDeadlineAt: now.Add(24 * time.Hour),
	}
	chronology := []ChronologyEntry{
		{ID: "entry-1", EntryKind: EntryDetected, Summary: "Detected unusual access.",
			EvidenceReferences: []string{"log-ref-1"}, OccurredAt: now.Add(-47 * time.Hour)},
	}
	assessments := []Assessment{
		{ID: "assessment-1", AuthorityNotificationDecision: AuthorityNotificationAssessmentPending,
			AuthorityNotificationReasoning: "Still investigating scope.",
			HighRiskToIndividuals:          RiskAssessmentPending, HighRiskReasoning: "Pending.",
			DataSubjectCommunicationDecision:  CommunicationAssessmentPending,
			DataSubjectCommunicationReasoning: "Pending.", AssessedAt: now.Add(-24 * time.Hour)},
	}
	actions := []ActionEvent{
		{ID: "event-1", ActionID: "action-1", ActionKind: ActionContainment, Title: "Rotate keys",
			Status: ActionInProgress, Revision: 2, OccurredAt: now.Add(-10 * time.Hour)},
	}
	actionEvents := []ActionEvent{
		{ID: "event-0", ActionID: "action-1", ActionKind: ActionContainment, Title: "Rotate keys",
			Status: ActionPending, Revision: 1, OccurredAt: now.Add(-20 * time.Hour)},
		{ID: "event-1", ActionID: "action-1", ActionKind: ActionContainment, Title: "Rotate keys",
			Status: ActionInProgress, Revision: 2, OccurredAt: now.Add(-10 * time.Hour)},
	}
	notifications := []ControllerNotification{
		{ID: "notification-1", NotificationKind: ControllerNotificationInitial, Sequence: 1,
			RecipientEmail: "privacy@acme.example", Status: ControllerNotificationSent, Attempts: 1},
	}
	return newPostIncidentReport(now, incident, chronology, assessments, actions, actionEvents, notifications)
}

func TestRenderPostIncidentReportMarkdownIsDeterministicAndContainsFacts(t *testing.T) {
	t.Parallel()
	report := samplePostIncidentReport()
	first := renderPostIncidentReportMarkdown(report)
	second := renderPostIncidentReportMarkdown(report)
	if first != second {
		t.Fatalf("renderPostIncidentReportMarkdown is not deterministic:\n%s\n---\n%s", first, second)
	}
	for _, want := range []string{
		"BR-BBBBBBBB", "Detected unusual access.", "assessment_pending", "Rotate keys",
		"privacy@acme.example",
		"does not determine whether an authority or affected-person notification is required",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("markdown missing %q:\n%s", want, first)
		}
	}
}

func TestRenderPostIncidentReportMarkdownHandlesEmptyState(t *testing.T) {
	t.Parallel()
	report := newPostIncidentReport(
		time.Now(), Incident{PublicID: "BR-CCCCCCCC"}, nil, nil, nil, nil, nil,
	)
	markdown := renderPostIncidentReportMarkdown(report)
	for _, want := range []string{
		"No chronology entries recorded yet.",
		"No assessment recorded yet (assessment_pending).",
		"No actions recorded.",
		"No action history recorded.",
		"No controller notifications staged.",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

// TestExportJSONNeverExposesForbiddenFields mirrors
// TestIncidentJSONNeverExposesTenantContent's shape-guard style: it inspects
// actual JSON keys (not the Go struct) so a future edit that adds a raw
// payload, credential, reporter-content, IP/user-agent, query-string, or
// signed-URL field to either export is caught here.
func TestExportJSONNeverExposesForbiddenFields(t *testing.T) {
	t.Parallel()
	forbidden := []string{
		"ip_address", "ip", "user_agent", "query_string", "signed_url", "url",
		"credentials", "password", "token", "reporter_content", "raw_payload", "evidence_body",
	}
	notificationEncoded, err := json.Marshal(sampleNotificationExport())
	if err != nil {
		t.Fatalf("json.Marshal(notification export) error = %v", err)
	}
	reportEncoded, err := json.Marshal(samplePostIncidentReport())
	if err != nil {
		t.Fatalf("json.Marshal(post-incident report) error = %v", err)
	}
	assertNoForbiddenKeys(t, notificationEncoded, forbidden)
	assertNoForbiddenKeys(t, reportEncoded, forbidden)
}

func assertNoForbiddenKeys(t *testing.T, encoded []byte, forbidden []string) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	walkKeys(t, decoded, forbidden)
}

func walkKeys(t *testing.T, value any, forbidden []string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			for _, banned := range forbidden {
				if strings.EqualFold(key, banned) {
					t.Errorf("export JSON unexpectedly carries forbidden key %q", key)
				}
			}
			walkKeys(t, nested, forbidden)
		}
	case []any:
		for _, nested := range typed {
			walkKeys(t, nested, forbidden)
		}
	}
}
