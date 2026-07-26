package membernotify_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/membernotify"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Every optional category is collected from the same window, so one seeded
// workspace proves each query selects the events it claims to and ignores the
// member's own actions.
func TestCollectGathersEveryCategoryInTheWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	fixture := seedWorkspace(t, ctx, database, "collect")
	window := membernotify.Window{
		Start: time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC),
	}
	inside := window.Start.Add(10 * time.Minute)
	before := window.Start.Add(-time.Hour)
	after := window.End.Add(time.Minute)

	// Ambient categories only follow issues the member is accountable for, so the
	// issue is assigned to them before the comment and status change land.
	if _, err := database.Exec(ctx, `
		UPDATE issues SET assignee_id = $3 WHERE tenant_id = $1 AND id = $2
	`, fixture.TenantID, fixture.IssueID, fixture.MemberID); err != nil {
		t.Fatalf("assign the issue: %v", err)
	}

	recordMention(t, ctx, database, fixture, inside)
	seedActivity(t, ctx, database, fixture, fixture.OtherID, "issue.updated", inside,
		`{"changes": {"assignee_id": {"from": null, "to": "`+fixture.MemberID+`"}}}`)
	seedActivity(t, ctx, database, fixture, fixture.OtherID, "issue.comment_created", inside,
		`{"mention_member_ids": []}`)
	seedActivity(t, ctx, database, fixture, fixture.OtherID, "issue.status_changed", inside,
		`{"changes": {"status": {"from": "open", "to": "needs_info"}}}`)
	seedFeedback(t, ctx, database, fixture, "fb_Collect1", inside)

	// Noise that must not appear: the member's own action, a metadata-only
	// change, and events on either side of the window.
	seedActivity(t, ctx, database, fixture, fixture.MemberID, "issue.status_changed", inside,
		`{"changes": {"status": {"from": "needs_info", "to": "open"}}}`)
	seedActivity(t, ctx, database, fixture, fixture.OtherID, "issue.updated", inside,
		`{"changes": {"title": {"from": "Old", "to": "New"}}}`)
	seedActivity(t, ctx, database, fixture, fixture.OtherID, "issue.status_changed", before,
		`{"changes": {"status": {"from": "open", "to": "resolved"}}}`)
	seedActivity(t, ctx, database, fixture, fixture.OtherID, "issue.status_changed", after,
		`{"changes": {"status": {"from": "open", "to": "closed"}}}`)
	seedFeedback(t, ctx, database, fixture, "fb_Collect2", after)

	preferences := membernotify.Defaults()
	preferences.Timezone = "UTC"
	for _, category := range membernotify.Categories {
		switch category {
		case membernotify.CategoryAssignments:
			preferences.Assignments = membernotify.ModeDaily
		case membernotify.CategoryMentions:
			preferences.Mentions = membernotify.ModeDaily
		case membernotify.CategoryComments:
			preferences.Comments = membernotify.ModeDaily
		case membernotify.CategoryIssueChanges:
			preferences.IssueChanges = membernotify.ModeDaily
		case membernotify.CategoryFeedbackSummary:
			preferences.FeedbackSummary = membernotify.ModeDaily
		}
	}
	candidate := membernotify.Candidate{
		TenantID: fixture.TenantID, MemberID: fixture.MemberID,
		Permissions: []string{
			string(auth.PermissionIssueRead), string(auth.PermissionFeedbackRead),
		},
		Preferences: preferences,
	}
	store := membernotify.DigestStore{Database: database}
	sections, err := store.Collect(ctx, candidate, membernotify.CadenceDaily, window)
	if err != nil {
		t.Fatalf("collect digest sections: %v", err)
	}

	found := make(map[string]membernotify.Section, len(sections))
	for _, section := range sections {
		found[section.Category] = section
	}
	if len(found) != len(membernotify.Categories) {
		t.Fatalf("collected categories = %v, want all five", categoryNames(sections))
	}
	for category, wantDetail := range map[string]string{
		membernotify.CategoryAssignments:  "Assigned to you",
		membernotify.CategoryMentions:     "Mentioned you in a comment",
		membernotify.CategoryComments:     "New comment",
		membernotify.CategoryIssueChanges: "Status open to needs info",
	} {
		section := found[category]
		if section.Total != 1 {
			t.Fatalf("%s total = %d, want exactly the one in-window event", category, section.Total)
		}
		if section.Events[0].Detail != wantDetail {
			t.Fatalf("%s detail = %q, want %q", category, section.Events[0].Detail, wantDetail)
		}
		if section.Events[0].Reference != "ACME-2026-0001" {
			t.Fatalf("%s reference = %q", category, section.Events[0].Reference)
		}
		if section.Events[0].Actor != "Ana" {
			t.Fatalf("%s actor = %q, want the colleague", category, section.Events[0].Actor)
		}
	}
	summary := found[membernotify.CategoryFeedbackSummary]
	if summary.Total != 1 || summary.Events[0].Reference != "fb_Collect1" {
		t.Fatalf("feedback summary = %+v, want only the in-window submission", summary)
	}
	if summary.Events[0].Detail != "Web" {
		t.Fatalf("feedback summary detail = %q, want the project name", summary.Events[0].Detail)
	}

	// Read access decides what a digest may mention, not the preference.
	restricted := candidate
	restricted.Permissions = []string{string(auth.PermissionFeedbackRead)}
	restrictedSections, err := store.Collect(ctx, restricted, membernotify.CadenceDaily, window)
	if err != nil {
		t.Fatalf("collect restricted sections: %v", err)
	}
	if len(restrictedSections) != 1 ||
		restrictedSections[0].Category != membernotify.CategoryFeedbackSummary {
		t.Fatalf(
			"a member without issue access collected %v", categoryNames(restrictedSections),
		)
	}
}

// A quiet window costs one probe and no category queries, and a failed delivery
// keeps its reason for the diagnostics the member can see.
func TestQuietWindowCollectsNothingAndFailuresKeepTheirReason(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	fixture := seedWorkspace(t, ctx, database, "quiet")
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	window := membernotify.Window{Start: now.Add(-time.Hour), End: now}

	preferences := membernotify.Defaults()
	preferences.Timezone = "UTC"
	candidate := membernotify.Candidate{
		TenantID: fixture.TenantID, MemberID: fixture.MemberID,
		Permissions: []string{
			string(auth.PermissionIssueRead), string(auth.PermissionFeedbackRead),
		},
		Preferences: preferences,
	}
	store := membernotify.DigestStore{Database: database}
	sections, err := store.Collect(ctx, candidate, membernotify.CadenceImmediate, window)
	if err != nil {
		t.Fatalf("collect quiet window: %v", err)
	}
	if len(sections) != 0 {
		t.Fatalf("a quiet window collected %v", categoryNames(sections))
	}

	deliveryID, claimed, err := store.Claim(ctx, candidate, membernotify.CadenceImmediate, window, now)
	if err != nil || !claimed {
		t.Fatalf("claim window = (%q, %t, %v)", deliveryID, claimed, err)
	}
	if err := store.MarkFailed(ctx, deliveryID, strings.Repeat("provider unavailable ", 200)); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	deliveries, err := membernotify.Store{}.RecentDeliveries(
		ctx, database, fixture.TenantID, fixture.MemberID, 10,
	)
	if err != nil {
		t.Fatalf("read deliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != membernotify.StatusFailed {
		t.Fatalf("deliveries = %+v, want one failed window", deliveries)
	}
	// The column caps at 2000 characters, so the reason is truncated rather than
	// failing the write and losing the record.
	if deliveries[0].Reason == nil || len(*deliveries[0].Reason) != 2000 {
		t.Fatalf("failure reason was not truncated to the column limit: %v", deliveries[0].Reason)
	}
}

func categoryNames(sections []membernotify.Section) []string {
	names := make([]string, 0, len(sections))
	for _, section := range sections {
		names = append(names, section.Category)
	}
	return names
}

func seedActivity(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	fixture workspace,
	actorID string,
	kind string,
	at time.Time,
	data string,
) {
	t.Helper()
	if _, err := database.Exec(ctx, `
		INSERT INTO issue_activities (
			tenant_id, issue_id, actor_member_id, kind, visibility, data, created_at
		) VALUES ($1, $2, $3, $4, 'internal', $5::jsonb, $6)
	`, fixture.TenantID, fixture.IssueID, actorID, kind, data, at); err != nil {
		t.Fatalf("record %s activity: %v", kind, err)
	}
}

func seedFeedback(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	fixture workspace,
	publicID string,
	at time.Time,
) {
	t.Helper()
	if _, err := database.Exec(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, client_feedback_id, title, page_url,
			text_process_status, status, created_at
		) VALUES ($1, $2, $3, gen_random_uuid(), 'Cart total is wrong',
		          'https://example.test/cart', 'skipped', 'open', $4)
	`, fixture.TenantID, fixture.ProjectID, publicID, at); err != nil {
		t.Fatalf("create feedback: %v", err)
	}
}

// Candidates decides who a pass even considers. It must read stored
// preferences rather than defaults, follow workspace access, and skip members
// who route nothing to the cadence being processed.
func TestCandidatesRespectPreferencesAndWorkspaceAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	active := seedWorkspace(t, ctx, database, "candidates")
	suspended := seedWorkspace(t, ctx, database, "candidates-suspended")
	if _, err := database.Exec(ctx, `
		UPDATE tenants SET status = 'suspended' WHERE id = $1
	`, suspended.TenantID); err != nil {
		t.Fatalf("suspend workspace: %v", err)
	}
	store := membernotify.DigestStore{Database: database}

	// Defaults route mentions to immediate, so an untouched member is a
	// candidate for that cadence and not for the weekly one.
	immediate, err := store.Candidates(ctx, membernotify.CadenceImmediate, 50)
	if err != nil {
		t.Fatalf("list immediate candidates: %v", err)
	}
	if !containsMember(immediate, active.MemberID) {
		t.Fatal("a member on the default preferences is not an immediate candidate")
	}
	for _, candidate := range immediate {
		if candidate.TenantID == suspended.TenantID {
			t.Fatal("a suspended workspace produced a digest candidate")
		}
	}

	// Storing preferences moves the member between cadences.
	if _, err := database.Exec(ctx, `
		INSERT INTO member_notification_preferences (
			tenant_id, member_id, assignments, mentions, comments, issue_changes,
			feedback_summary, digest_hour, digest_weekday
		) VALUES ($1, $2, 'off', 'off', 'off', 'off', 'weekly', 17, 5)
	`, active.TenantID, active.MemberID); err != nil {
		t.Fatalf("store preferences: %v", err)
	}
	immediate, err = store.Candidates(ctx, membernotify.CadenceImmediate, 50)
	if err != nil {
		t.Fatalf("list immediate candidates: %v", err)
	}
	if containsMember(immediate, active.MemberID) {
		t.Fatal("a member with nothing on the immediate cadence is still a candidate")
	}
	weekly, err := store.Candidates(ctx, membernotify.CadenceWeekly, 50)
	if err != nil {
		t.Fatalf("list weekly candidates: %v", err)
	}
	candidate, found := memberCandidate(weekly, active.MemberID)
	if !found {
		t.Fatal("the weekly summary member is not a weekly candidate")
	}
	if candidate.Preferences.FeedbackSummary != membernotify.ModeWeekly ||
		candidate.Preferences.DigestHour != 17 || candidate.Preferences.DigestWeekday != 5 {
		t.Fatalf("candidate carries defaults instead of the stored row: %+v", candidate.Preferences)
	}
	if candidate.Preferences.Timezone != "UTC" || !candidate.Cursor.IsZero() {
		t.Fatalf("candidate timezone/cursor = %q/%v", candidate.Preferences.Timezone, candidate.Cursor)
	}

	// A deactivated member stops being considered at all.
	if _, err := database.Exec(ctx, `
		UPDATE tenant_members SET status = 'deactivated' WHERE tenant_id = $1 AND id = $2
	`, active.TenantID, active.MemberID); err != nil {
		t.Fatalf("deactivate member: %v", err)
	}
	weekly, err = store.Candidates(ctx, membernotify.CadenceWeekly, 50)
	if err != nil {
		t.Fatalf("list weekly candidates: %v", err)
	}
	if containsMember(weekly, active.MemberID) {
		t.Fatal("a deactivated member is still a digest candidate")
	}
}

func memberCandidate(
	candidates []membernotify.Candidate,
	memberID string,
) (membernotify.Candidate, bool) {
	for _, candidate := range candidates {
		if candidate.MemberID == memberID {
			return candidate, true
		}
	}
	return membernotify.Candidate{}, false
}

func containsMember(candidates []membernotify.Candidate, memberID string) bool {
	_, found := memberCandidate(candidates, memberID)
	return found
}
