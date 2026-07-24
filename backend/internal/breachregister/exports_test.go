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

// countHeadingLines is a crude ATX-heading detector (one or more '#'
// followed by a space, at the start of a physical line). It operates on
// literal text, not a Markdown parser, which is exactly the point: it lets
// adversarial tests prove that injected "# heading" text never became a real
// physical line in the rendered document.
func countHeadingLines(markdown string) int {
	count := 0
	for _, line := range strings.Split(markdown, "\n") {
		index := 0
		for index < len(line) && line[index] == '#' {
			index++
		}
		if index > 0 && index <= 6 && index < len(line) && line[index] == ' ' {
			count++
		}
	}
	return count
}

// countUnescapedPipes counts '|' characters not immediately preceded by a
// backslash escape, i.e. the pipes that actually delimit a Markdown table
// row. A raw pipe smuggled into a cell value is backslash-escaped by
// mdEscapeInline, so it must NOT be counted here.
func countUnescapedPipes(text string) int {
	count := 0
	runes := []rune(text)
	for index, r := range runes {
		if r == '|' && (index == 0 || runes[index-1] != '\\') {
			count++
		}
	}
	return count
}

func TestMdEscapeInlineNeutralizesDelimitersAndControlCharacters(t *testing.T) {
	t.Parallel()
	input := "line1\r\nline2\tindented\x01ctrl\\slash|pipe`tick*star_score#hash[br]<gt>tilde~bang!end"
	escaped := mdEscapeInline(input)

	for _, raw := range []string{"\r", "\n", "\t", "\x01"} {
		if strings.Contains(escaped, raw) {
			t.Fatalf("mdEscapeInline left a raw control character %q in output: %q", raw, escaped)
		}
	}
	for _, marker := range []string{"\\r", "\\n", "\\t", "\\x01"} {
		if !strings.Contains(escaped, marker) {
			t.Errorf("mdEscapeInline output missing visible marker %q: %q", marker, escaped)
		}
	}
	for _, char := range []string{"|", "`", "*", "_", "#", "[", "]", "<", ">", "~", "!"} {
		if !strings.Contains(escaped, "\\"+char) {
			t.Errorf("mdEscapeInline did not escape %q: %q", char, escaped)
		}
	}
	if !strings.Contains(escaped, "\\\\") {
		t.Errorf("mdEscapeInline did not double the literal backslash: %q", escaped)
	}
	if !strings.Contains(escaped, "slash") || !strings.Contains(escaped, "pipe") ||
		!strings.Contains(escaped, "end") {
		t.Errorf("mdEscapeInline dropped content instead of escaping it: %q", escaped)
	}
}

func TestMdEscapeInlineIsIdempotentOnPlainText(t *testing.T) {
	t.Parallel()
	if got := mdEscapeInline("plain workspace name"); got != "plain workspace name" {
		t.Errorf("mdEscapeInline(plain text) = %q, want unchanged", got)
	}
}

func TestMdSafeBlockChoosesFenceLongerThanEmbeddedBackticksAndPreservesContent(t *testing.T) {
	t.Parallel()
	text := "# Fake Heading\n| a | b |\n```\nnested fence attempt\n```\nend"
	block := mdSafeBlock(text)
	lines := strings.Split(block, "\n")
	if len(lines) < 3 {
		t.Fatalf("mdSafeBlock produced too few lines: %q", block)
	}
	openFence, closeFence := lines[0], lines[len(lines)-1]
	if openFence != closeFence {
		t.Fatalf("open fence %q != close fence %q", openFence, closeFence)
	}
	if openFence != strings.Repeat("`", 4) {
		t.Fatalf("fence = %q, want exactly 4 backticks (one more than the embedded 3-backtick run)", openFence)
	}
	for _, want := range []string{"# Fake Heading", "| a | b |", "nested fence attempt"} {
		if !strings.Contains(block, want) {
			t.Errorf("mdSafeBlock did not preserve verbatim content %q: %q", want, block)
		}
	}
	for _, line := range lines[1 : len(lines)-1] {
		if strings.Contains(line, strings.Repeat("`", len(openFence))) {
			t.Errorf("interior line %q contains a run as long as the fence -- it could prematurely close the block", line)
		}
	}
}

func TestMdSafeBlockMinimumFenceIsThreeBackticks(t *testing.T) {
	t.Parallel()
	block := mdSafeBlock("plain text, no backticks at all")
	lines := strings.Split(block, "\n")
	if lines[0] != "```" || lines[len(lines)-1] != "```" {
		t.Fatalf("mdSafeBlock fence = %q / %q, want ``` / ```", lines[0], lines[len(lines)-1])
	}
}

func TestRenderNotificationMarkdownInlineFieldsResistHeadingInjectionButPreserveContent(t *testing.T) {
	t.Parallel()
	benign := sampleNotificationExport()
	benignMarkdown := renderNotificationMarkdown(benign)
	benignHeadings := countHeadingLines(benignMarkdown)

	adversarial := benign
	adversarial.Notification.WorkspaceName = "Acme\n# Injected Heading\n## Another\n| a | b |"
	adversarialMarkdown := renderNotificationMarkdown(adversarial)
	adversarialHeadings := countHeadingLines(adversarialMarkdown)

	if adversarialHeadings != benignHeadings {
		t.Fatalf("WorkspaceName injection changed heading structure: benign=%d adversarial=%d\n%s",
			benignHeadings, adversarialHeadings, adversarialMarkdown)
	}
	if !strings.Contains(adversarialMarkdown, "Injected Heading") {
		t.Fatalf("WorkspaceName content was dropped rather than escaped/preserved: %s", adversarialMarkdown)
	}
	if strings.Contains(adversarialMarkdown, "\n# Injected Heading") {
		t.Fatalf("WorkspaceName injection produced a real physical heading line: %s", adversarialMarkdown)
	}
}

func TestRenderNotificationMarkdownBlockFieldsAreFencedVerbatim(t *testing.T) {
	t.Parallel()
	benign := sampleNotificationExport()
	adversarialSummary := "Legit summary\n# Fake Heading\n| a | b |\n```\nnested fence attempt\n```\nend"
	adversarial := benign
	adversarial.Notification.Summary = adversarialSummary
	markdown := renderNotificationMarkdown(adversarial)
	if !strings.Contains(markdown, mdSafeBlock(adversarialSummary)) {
		t.Fatalf("Summary was not rendered as a matching safe fenced block:\n%s", markdown)
	}
}

func TestRenderNotificationMarkdownTableCellsEscapePipesAndBackticksInDeliveryEvidence(t *testing.T) {
	t.Parallel()
	providerID := "abc|def`ghi"
	export := newNotificationExport(time.Now(), "BR-X", ControllerNotification{
		ID: "n1", NotificationKind: ControllerNotificationInitial, Sequence: 1,
		RecipientEmail: "a@example.com", WorkspaceName: "W", Summary: "s", MeasuresTaken: "m",
		Status: ControllerNotificationSent,
		DeliveryAttempts: []ControllerNotificationAttempt{
			{ID: "attempt-1", NotificationID: "n1", AttemptNumber: 1, Status: ControllerNotificationAttemptSent,
				ProviderMessageID: &providerID, AttemptedAt: time.Now()},
		},
	})
	markdown := renderNotificationMarkdown(export)
	var row string
	for _, line := range strings.Split(markdown, "\n") {
		if strings.Contains(line, "attempt-1") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("delivery attempt table row not found:\n%s", markdown)
	}
	// Seven columns (Attempt ID, Notification ID, Attempt, Status, Provider
	// message ID, Error, Attempted at) means exactly 8 structural pipe
	// delimiters, regardless of a raw pipe smuggled into a cell value.
	if got := countUnescapedPipes(row); got != 8 {
		t.Fatalf("delivery attempt row has %d unescaped pipes, want 8 (row: %q)", got, row)
	}
	if !strings.Contains(row, "abc\\|def\\`ghi") {
		t.Fatalf("delivery attempt row did not preserve the embedded pipe/backtick in escaped form: %q", row)
	}
	if !strings.Contains(row, "| n1 |") {
		t.Fatalf("delivery attempt row missing its own NotificationID column: %q", row)
	}
}

// fullNotification returns a ControllerNotification with every optional
// field populated, so field-parity tests can assert Markdown carries the
// same facts as JSON.
func fullNotification() ControllerNotification {
	nextUpdate := "Next update text."
	lastError := "email provider delivery failed"
	providerMessageID := "provider-msg-9"
	queuedBy := "admin-queuer"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	queuedAt := now.Add(-2 * time.Hour)
	sentAt := now.Add(-time.Hour)
	lastAttemptAt := now.Add(-90 * time.Minute)
	return ControllerNotification{
		ID: "notification-full", BreachIncidentID: "incident-xyz", TenantID: "tenant-xyz",
		NotificationKind: ControllerNotificationInitial, Sequence: 1, TemplateVersion: 1,
		IncidentPublicID: "BR-FULL0001", WorkspaceName: "Acme Workspace",
		RecipientEmail: "privacy@acme.example", PrivacyProfileVersion: 2,
		AwarenessAt: now.Add(-3 * time.Hour), Summary: "Summary text.", MeasuresTaken: "Measures text.",
		NextUpdate: &nextUpdate, Status: ControllerNotificationSent, Attempts: 1,
		LastError: &lastError, ProviderMessageID: &providerMessageID, LastAttemptAt: &lastAttemptAt,
		QueuedAt: &queuedAt, SentAt: &sentAt, CreatedByPlatformAdminID: "admin-creator",
		QueuedByPlatformAdminID: &queuedBy, CreatedAt: now.Add(-4 * time.Hour), UpdatedAt: now,
		DeliveryAttempts: []ControllerNotificationAttempt{
			{ID: "attempt-full", NotificationID: "notification-full", AttemptNumber: 1,
				Status: ControllerNotificationAttemptSent, ProviderMessageID: &providerMessageID,
				AttemptedAt: lastAttemptAt},
		},
	}
}

func TestRenderNotificationMarkdownIncludesFullFieldParityWithJSON(t *testing.T) {
	t.Parallel()
	export := newNotificationExport(time.Now(), "BR-FULL0001", fullNotification())
	markdown := renderNotificationMarkdown(export)
	for _, want := range []string{
		"incident-xyz", "tenant-xyz", "admin-creator", "admin-queuer",
		"email provider delivery failed", "provider-msg-9",
		export.Notification.QueuedAt.Format(time.RFC3339),
		export.Notification.SentAt.Format(time.RFC3339),
		export.Notification.LastAttemptAt.Format(time.RFC3339),
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("markdown missing %q:\n%s", want, markdown)
		}
	}
	// The delivery-attempt table row must carry its own NotificationID
	// column, even though the whole table is already nested under this
	// notification's section.
	if !strings.Contains(markdown, "| attempt-full | notification-full |") {
		t.Errorf("delivery attempt row missing its own NotificationID column:\n%s", markdown)
	}
}

func TestRenderNotificationMarkdownRendersExplicitPendingValues(t *testing.T) {
	t.Parallel()
	export := newNotificationExport(time.Now(), "BR-MIN0001", ControllerNotification{
		ID: "notification-min", NotificationKind: ControllerNotificationInitial, Sequence: 1,
		RecipientEmail: "privacy@acme.example", WorkspaceName: "Acme", Summary: "s", MeasuresTaken: "m",
		Status: ControllerNotificationDraft, CreatedByPlatformAdminID: "admin-creator",
	})
	markdown := renderNotificationMarkdown(export)
	for _, want := range []string{
		"- Last attempt at: (pending)", "- Queued at: (pending)", "- Sent at: (pending)",
		"- Last error: (none)", "- Provider message ID: (none)", "- Queued by (platform admin ID): (none)",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("markdown missing explicit pending/none line %q:\n%s", want, markdown)
		}
	}
}

func TestRenderPostIncidentReportInlineFieldsResistHeadingInjectionButPreserveContent(t *testing.T) {
	t.Parallel()
	benign := samplePostIncidentReport()
	benignMarkdown := renderPostIncidentReportMarkdown(benign)
	benignHeadings := countHeadingLines(benignMarkdown)

	adversarial := benign
	adversarial.Actions = append([]ActionEvent{}, benign.Actions...)
	adversarial.Actions[0].Title = "Rotate\n# Injected\n## Nested\n| a | b |"
	adversarialMarkdown := renderPostIncidentReportMarkdown(adversarial)
	adversarialHeadings := countHeadingLines(adversarialMarkdown)

	if adversarialHeadings != benignHeadings {
		t.Fatalf("Action title injection changed heading structure: benign=%d adversarial=%d\n%s",
			benignHeadings, adversarialHeadings, adversarialMarkdown)
	}
	if !strings.Contains(adversarialMarkdown, "Injected") {
		t.Fatalf("Action title content was dropped rather than escaped/preserved: %s", adversarialMarkdown)
	}
	if strings.Contains(adversarialMarkdown, "\n# Injected") {
		t.Fatalf("Action title injection produced a real physical heading line: %s", adversarialMarkdown)
	}
}

func TestRenderPostIncidentReportBlockFieldsAreFencedVerbatim(t *testing.T) {
	t.Parallel()
	benign := samplePostIncidentReport()
	adversarialSummary := "Legit chronology summary\n# Fake\n```\nnested\n```"
	adversarial := benign
	adversarial.Chronology = append([]ChronologyEntry{}, benign.Chronology...)
	adversarial.Chronology[0].Summary = adversarialSummary
	markdown := renderPostIncidentReportMarkdown(adversarial)
	if !strings.Contains(markdown, mdSafeBlock(adversarialSummary)) {
		t.Fatalf("chronology Summary was not rendered as a matching safe fenced block:\n%s", markdown)
	}
}

func TestRenderPostIncidentReportIncludesFullFieldParityWithJSON(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	subjectsMin, subjectsMax := int64(10), int64(20)
	recordsMin, recordsMax := int64(100), int64(200)
	likely := "Likely consequences text."
	recovery := "Recovery measures text."
	incident := Incident{
		ID: "incident-1", PublicID: "BR-BBBBBBBB", AwarenessAt: now.Add(-48 * time.Hour),
		DetectionSource: DetectionInternalMonitoring, ControllerRole: RoleProcessor,
		AffectedEnvironments: []string{"production"}, ContainmentSummary: "Contained the affected path.",
		AffectedTenantIDs: []string{"tenant-1"}, ConfidentialityImpact: true,
		CreatedByPlatformAdminID:        "admin-incident-creator",
		CreatedAt:                       now.Add(-49 * time.Hour),
		AuthorityNotificationDeadlineAt: now.Add(24 * time.Hour),
	}
	chronology := []ChronologyEntry{
		{ID: "entry-full", BreachIncidentID: "incident-1", EntryKind: EntryDetected,
			Summary: "Detected unusual access.", EvidenceReferences: []string{"log-ref-1"},
			ActorPlatformAdminID: "admin-chronology-actor", OccurredAt: now.Add(-47 * time.Hour),
			CreatedAt: now.Add(-47 * time.Hour)},
	}
	assessments := []Assessment{
		{ID: "assessment-full", BreachIncidentID: "incident-1",
			AuthorityNotificationDecision:  AuthorityNotificationNotRequired,
			AuthorityNotificationReasoning: "Reasoning A.", HighRiskToIndividuals: RiskNotHigh,
			HighRiskReasoning:                 "Reasoning B.",
			DataSubjectCommunicationDecision:  CommunicationNotRequired,
			DataSubjectCommunicationReasoning: "Reasoning C.",
			EstimatedAffectedSubjectsMin:      &subjectsMin, EstimatedAffectedSubjectsMax: &subjectsMax,
			EstimatedAffectedRecordsMin: &recordsMin, EstimatedAffectedRecordsMax: &recordsMax,
			LikelyConsequences: &likely, RecoveryMeasures: &recovery,
			EvidenceReferences: []string{"assessment-evidence-1"}, AssessedByPlatformAdminID: "admin-assessor",
			AssessedAt: now.Add(-24 * time.Hour), CreatedAt: now.Add(-24 * time.Hour)},
	}
	owner := "admin-owner"
	dueAt := now.Add(72 * time.Hour)
	outcome := "Outcome text."
	corrective := "corrective-ref-1"
	containmentArea := ContainmentAPIKeys
	actions := []ActionEvent{
		{ID: "event-full", BreachIncidentID: "incident-1", Sequence: 5, ActionID: "action-full",
			ActionKind: ActionPostIncident, Title: "Owner review", Status: ActionCompleted, Revision: 2,
			OwnerPlatformAdminID: &owner, DueAt: &dueAt, OutcomeSummary: &outcome,
			CorrectiveReference: &corrective, EvidenceReferences: []string{"action-evidence-1"},
			ActorPlatformAdminID: "admin-action-actor",
			OccurredAt:           now.Add(-10 * time.Hour), CreatedAt: now.Add(-10 * time.Hour)},
		{ID: "event-containment", BreachIncidentID: "incident-1", Sequence: 6,
			ActionID: "action-containment", ActionKind: ActionContainment, ContainmentArea: &containmentArea,
			Title: "Rotate keys", Status: ActionInProgress, Revision: 1,
			ActorPlatformAdminID: "admin-containment-actor",
			OccurredAt:           now.Add(-11 * time.Hour), CreatedAt: now.Add(-11 * time.Hour)},
	}
	actionEvents := actions
	notifications := []ControllerNotification{fullNotification()}
	report := newPostIncidentReport(now, incident, chronology, assessments, actions, actionEvents, notifications)
	markdown := renderPostIncidentReportMarkdown(report)

	for _, want := range []string{
		"admin-incident-creator", incident.CreatedAt.Format(time.RFC3339),
		"entry-full", "incident-1", "admin-chronology-actor",
		"assessment-full", "admin-assessor", "10 - 20", "100 - 200",
		"Reasoning A.", "Reasoning B.", "Reasoning C.",
		"Likely consequences text.", "Recovery measures text.", "assessment-evidence-1",
		"event-full", "action-full", "admin-owner", dueAt.Format(time.RFC3339),
		"Outcome text.", "corrective-ref-1", "action-evidence-1", "admin-action-actor",
		"event-containment", "action-containment", "admin-containment-actor",
		"tenant-xyz", "admin-creator", // from the embedded notification ledger entry
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("report markdown missing %q:\n%s", want, markdown)
		}
	}

	// Each Current Actions entry must carry its own BreachIncidentID,
	// alongside the chronology entry's and assessment's own copies of the
	// same field.
	if got := strings.Count(markdown, "- Breach incident ID: incident-1"); got != 4 {
		t.Errorf("markdown has %d \"Breach incident ID\" lines, want 4 (1 chronology entry + 1 assessment + 2 actions):\n%s",
			got, markdown)
	}

	// The Action History Summary table must carry ActionKind and
	// ContainmentArea per row, with an explicit "(not applicable)" for the
	// post-incident action (which has no containment area) and the real
	// value for the containment action.
	if !strings.Contains(markdown, "| action-full | post_incident | (not applicable) | Owner review |") {
		t.Errorf("Action History Summary missing the post-incident action's \"(not applicable)\" containment area:\n%s",
			markdown)
	}
	if !strings.Contains(markdown, "| action-containment | containment | api_keys | Rotate keys |") {
		t.Errorf("Action History Summary missing the containment action's ActionKind/ContainmentArea:\n%s",
			markdown)
	}

	// The embedded notification's delivery-attempt row must carry its own
	// NotificationID column even though it's nested under the notification
	// ledger section.
	if !strings.Contains(markdown, "| attempt-full | notification-full |") {
		t.Errorf("embedded notification's delivery attempt row missing its own NotificationID column:\n%s", markdown)
	}
}

func TestRenderPostIncidentReportRendersExplicitPendingValues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	assessments := []Assessment{
		{ID: "assessment-min", AuthorityNotificationDecision: AuthorityNotificationAssessmentPending,
			AuthorityNotificationReasoning: "Pending.", HighRiskToIndividuals: RiskAssessmentPending,
			HighRiskReasoning:                 "Pending.",
			DataSubjectCommunicationDecision:  CommunicationAssessmentPending,
			DataSubjectCommunicationReasoning: "Pending.", AssessedByPlatformAdminID: "admin-assessor",
			AssessedAt: now, CreatedAt: now},
	}
	actions := []ActionEvent{
		{ID: "event-min", ActionID: "action-min", ActionKind: ActionContainment, Title: "Rotate keys",
			Status: ActionPending, Revision: 1, ActorPlatformAdminID: "admin-actor", OccurredAt: now,
			CreatedAt: now},
	}
	report := newPostIncidentReport(
		now, Incident{PublicID: "BR-MIN0002"}, nil, assessments, actions, actions, nil,
	)
	markdown := renderPostIncidentReportMarkdown(report)
	for _, want := range []string{
		"- Estimated affected subjects: (pending)", "- Estimated affected records: (pending)",
		"Likely consequences:\n\n(pending)", "Recovery measures:\n\n(pending)",
		"- Owner (platform admin ID): (none)", "- Due at: (pending)",
		"Outcome summary:\n\n(pending)", "- Corrective reference: (none)",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("report markdown missing explicit pending/none value %q:\n%s", want, markdown)
		}
	}
}
