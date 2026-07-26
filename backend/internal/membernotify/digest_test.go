package membernotify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
)

type recordedClaim struct {
	Cadence string
	Window  Window
}

type fakeSource struct {
	candidates   map[string][]Candidate
	sections     []Section
	claimTaken   bool
	claimErr     error
	claims       []recordedClaim
	sent         []int
	suppressed   []string
	failed       []string
	collectCalls int
}

func (f *fakeSource) Candidates(_ context.Context, cadence string, _ int) ([]Candidate, error) {
	return f.candidates[cadence], nil
}

func (f *fakeSource) Collect(
	_ context.Context, _ Candidate, _ string, _ Window,
) ([]Section, error) {
	f.collectCalls++
	return f.sections, nil
}

func (f *fakeSource) Claim(
	_ context.Context, _ Candidate, cadence string, window Window, _ time.Time,
) (string, bool, error) {
	if f.claimErr != nil {
		return "", false, f.claimErr
	}
	if f.claimTaken {
		return "", false, nil
	}
	f.claims = append(f.claims, recordedClaim{Cadence: cadence, Window: window})
	return "delivery-1", true, nil
}

func (f *fakeSource) MarkSent(_ context.Context, _ string, eventCount int, _ time.Time) error {
	f.sent = append(f.sent, eventCount)
	return nil
}

func (f *fakeSource) MarkSuppressed(_ context.Context, _ string, _ int, reason string) error {
	f.suppressed = append(f.suppressed, reason)
	return nil
}

func (f *fakeSource) MarkFailed(_ context.Context, _ string, message string) error {
	f.failed = append(f.failed, message)
	return nil
}

type fakeQueue struct {
	messages []email.Message
	err      error
}

func (q *fakeQueue) Enqueue(_ context.Context, message email.Message) error {
	if q.err != nil {
		return q.err
	}
	q.messages = append(q.messages, message)
	return nil
}

func testScheduler(t *testing.T, source *fakeSource, queue *fakeQueue) Scheduler {
	t.Helper()
	tokens, err := auth.NewJWTManager(strings.Repeat("k", 32), "fidibeki", 90*24*time.Hour)
	if err != nil {
		t.Fatalf("create token manager: %v", err)
	}
	return Scheduler{
		Store: source, Email: queue, Tokens: tokens,
		PublicBaseURL: "http://localhost:8088", BaseDomain: "localhost",
		AuthFrom: "Fidibeki <notifications@fidibeki.com>", MaxMembers: 50,
		Now: func() time.Time { return time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC) },
	}
}

func digestCandidate() Candidate {
	preferences := Defaults()
	preferences.Timezone = "UTC"
	preferences.Comments = ModeOff
	return Candidate{
		TenantID:   "11111111-1111-4111-8111-111111111111",
		TenantName: "Acme", TenantSlug: "acme",
		MemberID: "22222222-2222-4222-8222-222222222222",
		Email:    "member@example.test", Name: "Member",
		Permissions: []string{string(auth.PermissionIssueRead), string(auth.PermissionFeedbackRead)},
		Preferences: preferences,
		Cursor:      time.Date(2026, 7, 20, 8, 30, 0, 0, time.UTC),
	}
}

func mentionSections() []Section {
	return []Section{{
		Category: CategoryMentions,
		Events: []Event{{
			Category: CategoryMentions, Reference: "ISS-12", Title: "Checkout fails",
			Detail: "Mentioned you in a comment", Actor: "Ana",
			CreatedAt: time.Date(2026, 7, 20, 8, 45, 0, 0, time.UTC),
		}},
		Total: 1,
	}}
}

func TestSchedulerSendsAndMarksOneDigest(t *testing.T) {
	t.Parallel()
	source := &fakeSource{
		candidates: map[string][]Candidate{CadenceImmediate: {digestCandidate()}},
		sections:   mentionSections(),
	}
	queue := &fakeQueue{}
	if err := testScheduler(t, source, queue).Execute(context.Background()); err != nil {
		t.Fatalf("execute digests: %v", err)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("queued %d messages, want 1", len(queue.messages))
	}
	message := queue.messages[0]
	if message.To != "member@example.test" {
		t.Fatalf("recipient = %q", message.To)
	}
	if message.IdempotencyKey != "member-digest:delivery-1" {
		t.Fatalf("idempotency key = %q, want the claimed delivery", message.IdempotencyKey)
	}
	for _, want := range []string{
		"ISS-12", "Checkout fails", "/v1/member-notifications/unsubscribe",
		"acme.localhost:8088/settings/notifications",
	} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("digest text is missing %q:\n%s", want, message.Text)
		}
	}
	if !strings.Contains(message.Text, "not affected by these settings") {
		t.Fatalf("digest does not separate mandatory mail:\n%s", message.Text)
	}
	if len(source.sent) != 1 || source.sent[0] != 1 {
		t.Fatalf("marked sent = %v, want one delivery of one event", source.sent)
	}
}

// An empty window must leave no ledger row at all: claiming it would advance the
// cursor past events that have not happened yet.
func TestSchedulerSkipsEmptyWindowsWithoutClaiming(t *testing.T) {
	t.Parallel()
	source := &fakeSource{
		candidates: map[string][]Candidate{CadenceImmediate: {digestCandidate()}},
		sections:   nil,
	}
	queue := &fakeQueue{}
	if err := testScheduler(t, source, queue).Execute(context.Background()); err != nil {
		t.Fatalf("execute digests: %v", err)
	}
	if len(queue.messages) != 0 {
		t.Fatalf("an empty digest was sent: %+v", queue.messages)
	}
	if len(source.claims) != 0 {
		t.Fatalf("an empty window was claimed: %+v", source.claims)
	}
}

// The claim is the deduplication point: a window another run already took is
// silently left alone instead of producing a second email.
func TestSchedulerDoesNotResendAClaimedWindow(t *testing.T) {
	t.Parallel()
	source := &fakeSource{
		candidates: map[string][]Candidate{CadenceImmediate: {digestCandidate()}},
		sections:   mentionSections(),
		claimTaken: true,
	}
	queue := &fakeQueue{}
	if err := testScheduler(t, source, queue).Execute(context.Background()); err != nil {
		t.Fatalf("execute digests: %v", err)
	}
	if len(queue.messages) != 0 {
		t.Fatalf("a claimed window was delivered again: %+v", queue.messages)
	}
	if len(source.sent) != 0 {
		t.Fatalf("a claimed window was marked sent: %v", source.sent)
	}
}

func TestSchedulerSuppressesUndeliverableAddresses(t *testing.T) {
	t.Parallel()
	for name, candidate := range map[string]func(Candidate) Candidate{
		"bounced":    func(c Candidate) Candidate { c.Bounced = true; return c },
		"complained": func(c Candidate) Candidate { c.Complained = true; return c },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := &fakeSource{
				candidates: map[string][]Candidate{CadenceImmediate: {candidate(digestCandidate())}},
				sections:   mentionSections(),
			}
			queue := &fakeQueue{}
			if err := testScheduler(t, source, queue).Execute(context.Background()); err != nil {
				t.Fatalf("execute digests: %v", err)
			}
			if len(queue.messages) != 0 {
				t.Fatalf("mail was sent to an undeliverable address: %+v", queue.messages)
			}
			// The window is still claimed and recorded, so the suppression is
			// visible in delivery diagnostics rather than looking like silence.
			if len(source.claims) != 1 || len(source.suppressed) != 1 {
				t.Fatalf(
					"claims = %+v, suppressed = %v, want one recorded suppression",
					source.claims, source.suppressed,
				)
			}
		})
	}
}

// A queue failure must leave the window retakeable instead of consuming it.
func TestSchedulerMarksFailedHandOffs(t *testing.T) {
	t.Parallel()
	source := &fakeSource{
		candidates: map[string][]Candidate{CadenceImmediate: {digestCandidate()}},
		sections:   mentionSections(),
	}
	queue := &fakeQueue{err: errors.New("redis unavailable")}
	err := testScheduler(t, source, queue).Execute(context.Background())
	if err == nil {
		t.Fatal("a failed hand-off was reported as success")
	}
	if len(source.failed) != 1 || !strings.Contains(source.failed[0], "redis unavailable") {
		t.Fatalf("failure record = %v", source.failed)
	}
	if len(source.sent) != 0 {
		t.Fatalf("a failed hand-off was marked sent: %v", source.sent)
	}
}

func TestSchedulerOnlyRunsCadencesThatAreDue(t *testing.T) {
	t.Parallel()
	candidate := digestCandidate()
	// The daily boundary is 08:00 UTC and the cursor already covers it.
	candidate.Cursor = time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	source := &fakeSource{
		candidates: map[string][]Candidate{CadenceDaily: {candidate}},
		sections:   mentionSections(),
	}
	queue := &fakeQueue{}
	if err := testScheduler(t, source, queue).Execute(context.Background()); err != nil {
		t.Fatalf("execute digests: %v", err)
	}
	if source.collectCalls != 0 || len(queue.messages) != 0 {
		t.Fatalf(
			"a covered daily boundary was processed: %d collects, %d messages",
			source.collectCalls, len(queue.messages),
		)
	}
}

func TestSuppressionReasonsAndPermissionScoping(t *testing.T) {
	t.Parallel()
	if Suppression(false, false) != "" {
		t.Fatal("a healthy address was suppressed")
	}
	if Suppression(true, false) == "" || Suppression(false, true) == "" {
		t.Fatal("an undeliverable address was not suppressed")
	}
	reader := Candidate{Permissions: []string{string(auth.PermissionIssueRead)}}
	if !categoryPermitted(reader, CategoryMentions) {
		t.Fatal("a member with issue access cannot receive mentions")
	}
	if categoryPermitted(reader, CategoryFeedbackSummary) {
		t.Fatal("a member without feedback access received the feedback summary")
	}
	none := Candidate{Permissions: []string{string(auth.PermissionMemberRead)}}
	for _, category := range Categories {
		if categoryPermitted(none, category) {
			t.Fatalf("category %q leaked to a member without read access", category)
		}
	}
}

func TestDigestCopyReflectsCadenceAndVolume(t *testing.T) {
	t.Parallel()
	candidate := digestCandidate()
	cases := []struct {
		cadence     string
		total       int
		wantSubject string
		wantHeading string
	}{
		{CadenceImmediate, 1, "Acme: 1 update for you", "1 update in your activity"},
		{CadenceImmediate, 4, "Acme: 4 updates for you", "4 updates in your activity"},
		{CadenceDaily, 9, "Acme: daily digest", "9 updates in your daily digest"},
		{CadenceWeekly, 30, "Acme: weekly digest", "30 updates in your weekly digest"},
	}
	for _, testCase := range cases {
		if subject := digestSubject(testCase.cadence, candidate, testCase.total); subject != testCase.wantSubject {
			t.Fatalf("subject = %q, want %q", subject, testCase.wantSubject)
		}
		if heading := digestHeading(testCase.cadence, testCase.total); heading != testCase.wantHeading {
			t.Fatalf("heading = %q, want %q", heading, testCase.wantHeading)
		}
	}
}

// A long section lists a bounded sample and says how much it is showing, rather
// than silently dropping the rest.
func TestSectionSummaryReportsTruncation(t *testing.T) {
	t.Parallel()
	if summary := sectionSummary(Section{Total: 1}); summary != "1 update" {
		t.Fatalf("single-event summary = %q", summary)
	}
	if summary := sectionSummary(Section{Total: 3, Events: make([]Event, 3)}); summary != "3 updates" {
		t.Fatalf("complete summary = %q", summary)
	}
	truncated := sectionSummary(Section{
		Total: 25, Events: make([]Event, maxSectionEvents), Remainder: 15,
	})
	if !strings.Contains(truncated, "25 updates") || !strings.Contains(truncated, "showing the first 10") {
		t.Fatalf("truncated summary = %q", truncated)
	}
}

// The loop keeps running past a failing pass and stops when the context ends.
func TestRunSurvivesFailuresAndStopsWithTheContext(t *testing.T) {
	t.Parallel()
	source := &fakeSource{
		candidates: map[string][]Candidate{CadenceImmediate: {digestCandidate()}},
		sections:   mentionSections(),
	}
	queue := &fakeQueue{err: errors.New("redis unavailable")}
	scheduler := testScheduler(t, source, queue)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx, time.Millisecond) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v, want a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop when its context was cancelled")
	}
	if len(source.failed) == 0 {
		t.Fatal("the first pass did not record its failure")
	}
}

// A digest that cannot be composed must leave its window retakeable rather than
// consuming it silently, and must never reach the queue half-built.
func TestSchedulerMarksUncomposableDigestsFailed(t *testing.T) {
	t.Parallel()
	source := &fakeSource{
		candidates: map[string][]Candidate{CadenceImmediate: {digestCandidate()}},
		sections:   mentionSections(),
	}
	queue := &fakeQueue{}
	scheduler := testScheduler(t, source, queue)
	// An unusable base URL cannot produce the workspace and opt-out links every
	// digest carries.
	scheduler.PublicBaseURL = "not-a-url"
	if err := scheduler.Execute(context.Background()); err == nil {
		t.Fatal("composing an unusable digest was reported as success")
	}
	if len(queue.messages) != 0 {
		t.Fatalf("a half-composed digest reached the queue: %+v", queue.messages)
	}
	if len(source.failed) != 1 {
		t.Fatalf("failures recorded = %v, want the window marked failed", source.failed)
	}
	if len(source.sent) != 0 {
		t.Fatalf("an uncomposable window was marked sent: %v", source.sent)
	}
}

// Without a token manager the footer cannot carry a signed opt-out, so the
// digest must fail rather than ship an email with no way to turn it off.
func TestSchedulerRefusesToSendWithoutAnOptOutLink(t *testing.T) {
	t.Parallel()
	source := &fakeSource{
		candidates: map[string][]Candidate{CadenceImmediate: {digestCandidate()}},
		sections:   mentionSections(),
	}
	queue := &fakeQueue{}
	scheduler := testScheduler(t, source, queue)
	scheduler.Tokens = nil
	if err := scheduler.Execute(context.Background()); err == nil {
		t.Fatal("a digest without an opt-out link was reported as success")
	}
	if len(queue.messages) != 0 {
		t.Fatalf("a digest shipped without an opt-out link: %+v", queue.messages)
	}
	if len(source.failed) != 1 {
		t.Fatalf("failures recorded = %v, want the window marked failed", source.failed)
	}
}

func TestExecuteRequiresItsCollaborators(t *testing.T) {
	t.Parallel()
	if err := (Scheduler{}).Execute(context.Background()); err == nil {
		t.Fatal("a scheduler with no queue or store reported success")
	}
	source := &fakeSource{candidates: map[string][]Candidate{}}
	if err := (Scheduler{Store: source}).Execute(context.Background()); err == nil {
		t.Fatal("a scheduler with no queue reported success")
	}
}
