package membernotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

// maxSectionEvents bounds how many individual events one digest section lists.
// Anything beyond it is reported as a remainder count instead of a longer email.
const maxSectionEvents = 10

// staleClaimGrace is how long a claimed window may stay unfinished before
// another pass may retake it. A pass marks its outcome immediately after handing
// the message to the queue, so anything older belongs to a process that died.
const staleClaimGrace = 15 * time.Minute

// Candidate is one member who routes at least one category to the cadence being
// processed, together with everything the run needs: their resolved preferences,
// their delivery cursor, and the address state that decides suppression.
type Candidate struct {
	TenantID    string
	TenantName  string
	TenantSlug  string
	MemberID    string
	Email       string
	Name        string
	Permissions []string
	Bounced     bool
	Complained  bool
	Preferences Preferences
	Cursor      time.Time
}

// Can reports whether the member holds a permission.
func (c Candidate) Can(permission auth.Permission) bool {
	for _, held := range c.Permissions {
		if held == string(permission) {
			return true
		}
	}
	return false
}

// Event is one thing that happened to the member inside a digest window.
type Event struct {
	Category  string
	IssueID   string
	Reference string
	Title     string
	Detail    string
	Actor     string
	CreatedAt time.Time
}

// Section groups one category's events. Remainder counts events the section
// deliberately did not list.
type Section struct {
	Category  string
	Events    []Event
	Remainder int
	Total     int
}

// DigestStore reads digest candidates and window events across workspaces. It is
// constructed with the admin pool because the scheduler is not a tenant request.
type DigestStore struct {
	Database db.Querier
}

// Candidates lists active members of live workspaces who route any category to
// the given cadence, newest cursor state included. Suspended and cancelled
// workspaces are excluded: optional product mail follows workspace access.
func (s DigestStore) Candidates(
	ctx context.Context,
	cadence string,
	limit int,
) ([]Candidate, error) {
	if limit <= 0 {
		limit = 200
	}
	defaults := Defaults()
	rows, err := s.Database.Query(ctx, `
		SELECT member.tenant_id, tenant.name, tenant.slug, member.id, member.email::text,
		       COALESCE(member.name, ''), member.permissions,
		       member.email_bounced, member.email_complained,
		       COALESCE(preference.assignments, $3),
		       COALESCE(preference.mentions, $4),
		       COALESCE(preference.comments, $5),
		       COALESCE(preference.issue_changes, $6),
		       COALESCE(preference.feedback_summary, $7),
		       COALESCE(preference.digest_hour, $8),
		       COALESCE(preference.digest_weekday, $9),
		       COALESCE(member.timezone, tenant.default_timezone, 'UTC'),
		       delivered.cursor
		FROM tenant_members AS member
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		LEFT JOIN member_notification_preferences AS preference
		  ON preference.tenant_id = member.tenant_id AND preference.member_id = member.id
		LEFT JOIN LATERAL (
			-- An unfinished window is resumed at its own start so a crashed or
			-- failed attempt is retaken rather than recomputed into an
			-- overlapping second window. Otherwise the cursor is the end of the
			-- last window that actually reached a recipient.
			SELECT COALESCE(
				(
					SELECT min(delivery.window_start)
					FROM member_digest_deliveries AS delivery
					WHERE delivery.tenant_id = member.tenant_id
					  AND delivery.member_id = member.id
					  AND delivery.cadence = $1
					  AND delivery.status IN ('claimed', 'failed')
				),
				(
					SELECT max(delivery.window_end)
					FROM member_digest_deliveries AS delivery
					WHERE delivery.tenant_id = member.tenant_id
					  AND delivery.member_id = member.id
					  AND delivery.cadence = $1
					  AND delivery.status IN ('sent', 'suppressed')
				)
			) AS cursor
		) AS delivered ON true
		WHERE member.status = 'active'
		  AND tenant.status IN ('active', 'trial')
		  AND $1 = ANY(ARRAY[
		      COALESCE(preference.assignments, $3),
		      COALESCE(preference.mentions, $4),
		      COALESCE(preference.comments, $5),
		      COALESCE(preference.issue_changes, $6),
		      COALESCE(preference.feedback_summary, $7)
		  ])
		ORDER BY member.tenant_id, member.id
		LIMIT $2
	`, cadence, limit,
		defaults.Assignments, defaults.Mentions, defaults.Comments,
		defaults.IssueChanges, defaults.FeedbackSummary,
		defaults.DigestHour, defaults.DigestWeekday,
	)
	if err != nil {
		return nil, fmt.Errorf("list digest candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]Candidate, 0)
	for rows.Next() {
		var candidate Candidate
		var cursor *time.Time
		if err := rows.Scan(
			&candidate.TenantID, &candidate.TenantName, &candidate.TenantSlug,
			&candidate.MemberID, &candidate.Email, &candidate.Name, &candidate.Permissions,
			&candidate.Bounced, &candidate.Complained,
			&candidate.Preferences.Assignments, &candidate.Preferences.Mentions,
			&candidate.Preferences.Comments, &candidate.Preferences.IssueChanges,
			&candidate.Preferences.FeedbackSummary,
			&candidate.Preferences.DigestHour, &candidate.Preferences.DigestWeekday,
			&candidate.Preferences.Timezone, &cursor,
		); err != nil {
			return nil, fmt.Errorf("scan digest candidate: %w", err)
		}
		if cursor != nil {
			candidate.Cursor = cursor.UTC()
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest candidates: %w", err)
	}
	return candidates, nil
}

// Collect gathers the events in one window for the categories the member routes
// to this cadence and is permitted to see. Every query reads append-only rows by
// created_at inside the half-open window, so the same window always yields the
// same events no matter how often it is retried.
func (s DigestStore) Collect(
	ctx context.Context,
	candidate Candidate,
	cadence string,
	window Window,
) ([]Section, error) {
	selected := make([]string, 0, len(Categories))
	for _, category := range candidate.Preferences.CategoriesFor(cadence) {
		if categoryPermitted(candidate, category) {
			selected = append(selected, category)
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	// The immediate cadence runs every minute for every opted-in member, and a
	// quiet workspace is the common case. One indexed existence probe answers it
	// instead of a per-category query each pass.
	activity, feedback, err := s.windowHasActivity(ctx, candidate.TenantID, window)
	if err != nil {
		return nil, err
	}
	sections := make([]Section, 0, len(selected))
	for _, category := range selected {
		if category == CategoryFeedbackSummary && !feedback {
			continue
		}
		if category != CategoryFeedbackSummary && !activity {
			continue
		}
		section, err := s.collectCategory(ctx, candidate, category, window)
		if err != nil {
			return nil, err
		}
		if section.Total == 0 {
			continue
		}
		sections = append(sections, section)
	}
	return sections, nil
}

// windowHasActivity reports whether the workspace recorded any issue activity or
// any new feedback inside the window, without regard to who may see it.
func (s DigestStore) windowHasActivity(
	ctx context.Context,
	tenantID string,
	window Window,
) (bool, bool, error) {
	var activity, feedback bool
	if err := s.Database.QueryRow(ctx, `
		SELECT
			EXISTS (
				SELECT 1 FROM issue_activities
				WHERE tenant_id = $1 AND created_at >= $2 AND created_at < $3
			),
			EXISTS (
				SELECT 1 FROM feedbacks
				WHERE tenant_id = $1 AND created_at >= $2 AND created_at < $3
			)
	`, tenantID, window.Start.UTC(), window.End.UTC()).Scan(&activity, &feedback); err != nil {
		return false, false, fmt.Errorf("probe digest window activity: %w", err)
	}
	return activity, feedback, nil
}

// categoryPermitted keeps a digest inside the member's own read access: a member
// without issue access never learns about issues through email, and one without
// feedback access never receives the new-feedback summary.
func categoryPermitted(candidate Candidate, category string) bool {
	if category == CategoryFeedbackSummary {
		return candidate.Can(auth.PermissionFeedbackRead)
	}
	return candidate.Can(auth.PermissionIssueRead)
}

func (s DigestStore) collectCategory(
	ctx context.Context,
	candidate Candidate,
	category string,
	window Window,
) (Section, error) {
	if category == CategoryFeedbackSummary {
		return s.collectFeedback(ctx, candidate, window)
	}
	rows, err := s.Database.Query(
		ctx, activityQuery(category),
		candidate.TenantID, window.Start.UTC(), window.End.UTC(), candidate.MemberID,
	)
	if err != nil {
		return Section{}, fmt.Errorf("collect %s digest events: %w", category, err)
	}
	defer rows.Close()
	section := Section{Category: category}
	for rows.Next() {
		event := Event{Category: category}
		var kind string
		var payload []byte
		if err := rows.Scan(
			&event.IssueID, &event.Reference, &event.Title, &kind, &payload,
			&event.Actor, &event.CreatedAt,
		); err != nil {
			return Section{}, fmt.Errorf("scan %s digest event: %w", category, err)
		}
		event.Detail = describeActivity(category, kind, payload)
		section.append(event)
	}
	if err := rows.Err(); err != nil {
		return Section{}, fmt.Errorf("iterate %s digest events: %w", category, err)
	}
	return section, nil
}

func (s DigestStore) collectFeedback(
	ctx context.Context,
	candidate Candidate,
	window Window,
) (Section, error) {
	rows, err := s.Database.Query(ctx, `
		SELECT feedback.public_id, COALESCE(feedback.title, ''), project.name,
		       feedback.created_at
		FROM feedbacks AS feedback
		JOIN projects AS project
		  ON project.tenant_id = feedback.tenant_id AND project.id = feedback.project_id
		WHERE feedback.tenant_id = $1
		  AND feedback.created_at >= $2 AND feedback.created_at < $3
		  AND feedback.status = 'open'
		ORDER BY feedback.created_at, feedback.id
	`, candidate.TenantID, window.Start.UTC(), window.End.UTC())
	if err != nil {
		return Section{}, fmt.Errorf("collect feedback summary events: %w", err)
	}
	defer rows.Close()
	section := Section{Category: CategoryFeedbackSummary}
	for rows.Next() {
		event := Event{Category: CategoryFeedbackSummary}
		if err := rows.Scan(
			&event.Reference, &event.Title, &event.Detail, &event.CreatedAt,
		); err != nil {
			return Section{}, fmt.Errorf("scan feedback summary event: %w", err)
		}
		section.append(event)
	}
	if err := rows.Err(); err != nil {
		return Section{}, fmt.Errorf("iterate feedback summary events: %w", err)
	}
	return section, nil
}

// append records one event, listing it only while the section is under its cap.
func (s *Section) append(event Event) {
	s.Total++
	if len(s.Events) < maxSectionEvents {
		s.Events = append(s.Events, event)
		return
	}
	s.Remainder++
}

// activityQuery returns the window query for one issue-derived category. All of
// them exclude the member's own actions: a digest reports what other people did.
func activityQuery(category string) string {
	switch category {
	case CategoryAssignments:
		return activitySelect + `
			  AND activity.data->'changes'->'assignee_id'->>'to' = $4::text
			  AND (activity.actor_member_id IS NULL OR activity.actor_member_id <> $4::uuid)
			ORDER BY activity.created_at, activity.id
		`
	case CategoryMentions:
		return activitySelect + `
			  AND activity.kind = 'issue.comment_created'
			  AND activity.data->'mention_member_ids' @> to_jsonb($4::text)
			  AND (activity.actor_member_id IS NULL OR activity.actor_member_id <> $4::uuid)
			ORDER BY activity.created_at, activity.id
		`
	case CategoryComments:
		return activitySelect + `
			  AND activity.kind = 'issue.comment_created'
			  AND NOT COALESCE(
			      activity.data->'mention_member_ids' @> to_jsonb($4::text), false
			  )
			  AND (activity.actor_member_id IS NULL OR activity.actor_member_id <> $4::uuid)
			  AND (` + watchedIssueCondition + `)
			ORDER BY activity.created_at, activity.id
		`
	case CategoryIssueChanges:
		return activitySelect + `
			  AND activity.kind IN (
			      'issue.status_changed', 'issue.updated', 'issue.merged', 'issue.unmerged'
			  )
			  AND (
			      activity.kind <> 'issue.updated'
			      OR jsonb_exists_any(
			          COALESCE(activity.data->'changes', '{}'::jsonb),
			          ARRAY['status', 'priority']
			      )
			  )
			  AND (activity.actor_member_id IS NULL OR activity.actor_member_id <> $4::uuid)
			  AND (` + watchedIssueCondition + `)
			ORDER BY activity.created_at, activity.id
		`
	default:
		return ""
	}
}

// describeActivity renders one activity as a short, non-quoting line. Comment
// bodies and other member-authored text are deliberately not copied into the
// email: the digest says what happened and links to where it happened.
func describeActivity(category, kind string, payload []byte) string {
	var data struct {
		Changes map[string]struct {
			From any `json:"from"`
			To   any `json:"to"`
		} `json:"changes"`
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &data)
	}
	switch category {
	case CategoryAssignments:
		return "Assigned to you"
	case CategoryMentions:
		return "Mentioned you in a comment"
	case CategoryComments:
		return "New comment"
	}
	switch kind {
	case "issue.merged":
		return "Merged into another issue"
	case "issue.unmerged":
		return "Merge undone"
	}
	if change, ok := data.Changes["status"]; ok {
		return "Status " + transition(change.From, change.To)
	}
	if change, ok := data.Changes["priority"]; ok {
		return "Priority " + transition(change.From, change.To)
	}
	return "Updated"
}

func transition(from, to any) string {
	previous, current := describeValue(from), describeValue(to)
	if previous == "" {
		return "set to " + current
	}
	return previous + " to " + current
}

func describeValue(value any) string {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return ""
	}
	return strings.ReplaceAll(text, "_", " ")
}

// watchedIssueCondition scopes ambient activity to issues the member is
// accountable for: assigned to them, or one they already commented on. Without
// it, "comments" would mean every comment in the workspace.
const watchedIssueCondition = `
	issue.assignee_id = $4::uuid
	OR EXISTS (
		SELECT 1 FROM issue_comments AS own
		WHERE own.tenant_id = activity.tenant_id
		  AND own.issue_id = activity.issue_id
		  AND own.author_member_id = $4::uuid
		  AND own.created_at < activity.created_at
	)
`

const activitySelect = `
	SELECT activity.issue_id::text, issue.public_id, issue.title,
	       activity.kind, activity.data,
	       COALESCE(actor.name, actor.email::text, ''), activity.created_at
	FROM issue_activities AS activity
	JOIN issues AS issue
	  ON issue.tenant_id = activity.tenant_id AND issue.id = activity.issue_id
	LEFT JOIN tenant_members AS actor
	  ON actor.tenant_id = activity.tenant_id AND actor.id = activity.actor_member_id
	WHERE activity.tenant_id = $1
	  AND activity.created_at >= $2 AND activity.created_at < $3
`

// Claim reserves one window for one member and cadence. The unique window key
// makes the reservation the deduplication point: a second scheduler, or a rerun
// after a crash, cannot claim the same window twice. A window whose previous
// attempt ended in failure is retaken so a transient provider error does not
// permanently skip it.
func (s DigestStore) Claim(
	ctx context.Context,
	candidate Candidate,
	cadence string,
	window Window,
	now time.Time,
) (string, bool, error) {
	var id string
	err := s.Database.QueryRow(ctx, `
		INSERT INTO member_digest_deliveries (
			tenant_id, member_id, cadence, window_start, window_end, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, member_id, cadence, window_start) DO NOTHING
		RETURNING id
	`, candidate.TenantID, candidate.MemberID, cadence,
		window.Start.UTC(), window.End.UTC(), now.UTC()).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("claim digest window: %w", err)
	}
	// A window still marked claimed long after its pass ended belongs to a
	// process that died mid-delivery. Retaking it re-enqueues under the same
	// delivery ID, and therefore the same idempotency key, so a message that did
	// reach the queue is not duplicated.
	err = s.Database.QueryRow(ctx, `
		UPDATE member_digest_deliveries
		SET status = 'claimed', error_message = NULL, window_end = $5
		WHERE tenant_id = $1 AND member_id = $2 AND cadence = $3 AND window_start = $4
		  AND (status = 'failed' OR (status = 'claimed' AND created_at < $6))
		RETURNING id
	`, candidate.TenantID, candidate.MemberID, cadence, window.Start.UTC(),
		window.End.UTC(), now.UTC().Add(-staleClaimGrace)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("retake failed digest window: %w", err)
	}
	return id, true, nil
}

// MarkSent records a handed-off digest. Delivery itself is the email queue's
// job; the stable idempotency key on the message keeps a duplicated hand-off
// from producing a second send.
func (s DigestStore) MarkSent(
	ctx context.Context,
	deliveryID string,
	eventCount int,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE member_digest_deliveries
		SET status = 'sent', event_count = $2, sent_at = $3, error_message = NULL
		WHERE id = $1
	`, deliveryID, eventCount, now.UTC()); err != nil {
		return fmt.Errorf("mark digest sent: %w", err)
	}
	return nil
}

// MarkSuppressed records that a digest was withheld because the address is not
// deliverable. The row stays in the ledger so the suppression is visible rather
// than looking like silence.
func (s DigestStore) MarkSuppressed(
	ctx context.Context,
	deliveryID string,
	eventCount int,
	reason string,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE member_digest_deliveries
		SET status = 'suppressed', event_count = $2, error_message = $3
		WHERE id = $1
	`, deliveryID, eventCount, reason); err != nil {
		return fmt.Errorf("mark digest suppressed: %w", err)
	}
	return nil
}

// MarkFailed leaves the window retakeable by the next run.
func (s DigestStore) MarkFailed(ctx context.Context, deliveryID string, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE member_digest_deliveries
		SET status = 'failed', error_message = $2
		WHERE id = $1
	`, deliveryID, message); err != nil {
		return fmt.Errorf("mark digest failed: %w", err)
	}
	return nil
}
