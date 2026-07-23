package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/activity"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestIssueStatusNotificationAndDeliveryLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, issueMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app role: %v", err)
	}

	var tenantID, memberID, triageMemberID, projectID, issueID, targetID, feedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	const ownerAvatarVersion = "10000000-0000-4000-8000-000000000001"
	const triageAvatarVersion = "10000000-0000-4000-8000-000000000002"
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, avatar_version)
		VALUES ($1, 'owner@acme.test', $2, $3) RETURNING id
	`, tenantID, auth.TenantPermissionStrings(), ownerAvatarVersion).Scan(&memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name, permissions, avatar_version)
		VALUES ($1, 'triage@acme.test', 'Ada Triage', $2, $3) RETURNING id
	`, tenantID, auth.TenantPermissionStrings(), triageAvatarVersion).Scan(&triageMemberID); err != nil {
		t.Fatalf("create triage member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.com}') RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (
			tenant_id, project_id, public_id, title, reporter_email, notify_reporter
		) VALUES ($1, $2, 'ACME-2026-0001', 'Checkout is confusing', 'reporter@example.com', true)
		RETURNING id
	`, tenantID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title)
		VALUES ($1, $2, 'ACME-2026-0002', 'Target issue') RETURNING id
	`, tenantID, projectID).Scan(&targetID); err != nil {
		t.Fatalf("create merge target: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, issue_id, client_feedback_id,
			text, reporter_email, notify_reporter, page_url, text_process_status
		) VALUES ($1, $2, 'fb_Issue001', $3, gen_random_uuid(), 'Checkout is confusing',
		          'reporter@example.com', true, 'https://example.com/checkout', 'pending')
		RETURNING id
	`, tenantID, projectID, issueID).Scan(&feedbackID); err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	if _, err := root.Exec(ctx,
		`UPDATE issues SET priority = 'high', assignee_id = $2 WHERE id = $1`, issueID, memberID,
	); err != nil {
		t.Fatalf("add readable issue triage metadata: %v", err)
	}
	var customerImpactTagID, checkoutTagID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color)
		VALUES ($1, 'Customer impact', '#DC6B2F') RETURNING id
	`, tenantID).Scan(&customerImpactTagID); err != nil {
		t.Fatalf("create customer impact issue tag: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color) VALUES ($1, 'Checkout', '#167D68') RETURNING id
	`, tenantID).Scan(&checkoutTagID); err != nil {
		t.Fatalf("create checkout issue tag: %v", err)
	}
	for _, tagID := range []string{customerImpactTagID, checkoutTagID} {
		if _, err := root.Exec(ctx, `
			INSERT INTO issue_tags (tenant_id, issue_id, tag_id) VALUES ($1, $2, $3)
		`, tenantID, issueID, tagID); err != nil {
			t.Fatalf("assign readable issue tag: %v", err)
		}
	}

	pools, err := db.OpenPools(ctx, issueRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	accessTokens := newTestJWT(t, 10*time.Minute)
	reporterTokens := newTestJWT(t, 90*24*time.Hour)
	accessToken, _, err := accessTokens.IssueTenant(memberID, tenantID, auth.TenantPermissionStrings())
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	triageAccessToken, _, err := accessTokens.IssueTenant(
		triageMemberID, tenantID, auth.TenantPermissionStrings(),
	)
	if err != nil {
		t.Fatalf("issue triage access token: %v", err)
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{Store: Store{}, JWT: accessTokens, Now: func() time.Time { return now }})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "fidibeki.localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)
	createdCommentResponse := issueRequest(
		handler, accessToken, http.MethodPost, "/v1/issues/"+issueID+"/comments",
		fmt.Sprintf(`{"body":"Please verify this with the checkout team.","mention_member_ids":[%q,%q]}`,
			triageMemberID, memberID),
	)
	if createdCommentResponse.Code != http.StatusCreated {
		t.Fatalf("create standalone comment = %d, body = %s",
			createdCommentResponse.Code, createdCommentResponse.Body.String())
	}
	var createdComment struct {
		Comment  Comment        `json:"comment"`
		Activity activity.Event `json:"activity"`
	}
	if err := json.Unmarshal(createdCommentResponse.Body.Bytes(), &createdComment); err != nil {
		t.Fatalf("decode standalone comment: %v", err)
	}
	if createdComment.Comment.Body != "Please verify this with the checkout team." ||
		len(createdComment.Comment.Mentions) != 2 ||
		createdComment.Comment.Author == nil ||
		createdComment.Comment.Author.AvatarVersion == nil ||
		*createdComment.Comment.Author.AvatarVersion != ownerAvatarVersion ||
		createdComment.Comment.Mentions[0].AvatarVersion == nil ||
		createdComment.Comment.Mentions[1].AvatarVersion == nil ||
		createdComment.Activity.Kind != "issue.comment_created" ||
		createdComment.Activity.Actor == nil || createdComment.Activity.Actor.ID != memberID ||
		createdComment.Activity.Actor.AvatarVersion == nil ||
		*createdComment.Activity.Actor.AvatarVersion != ownerAvatarVersion {
		t.Fatalf("standalone comment response = %#v", createdComment)
	}
	notificationResponse := issueRequest(
		handler, triageAccessToken, http.MethodGet, "/v1/notifications", "",
	)
	if notificationResponse.Code != http.StatusOK {
		t.Fatalf("list mention notifications = %d, body = %s",
			notificationResponse.Code, notificationResponse.Body.String())
	}
	var notificationPage struct {
		Notifications []MemberNotification `json:"notifications"`
		UnreadCount   int                  `json:"unread_count"`
	}
	if err := json.Unmarshal(notificationResponse.Body.Bytes(), &notificationPage); err != nil {
		t.Fatalf("decode mention notifications: %v", err)
	}
	if notificationPage.UnreadCount != 1 || len(notificationPage.Notifications) != 1 ||
		notificationPage.Notifications[0].IssueID != issueID ||
		notificationPage.Notifications[0].Actor == nil ||
		notificationPage.Notifications[0].Actor.ID != memberID ||
		notificationPage.Notifications[0].Actor.AvatarVersion == nil ||
		*notificationPage.Notifications[0].Actor.AvatarVersion != ownerAvatarVersion {
		t.Fatalf("mention notifications = %#v", notificationPage)
	}
	notificationID := notificationPage.Notifications[0].ID
	foreignRead := issueRequest(
		handler, accessToken, http.MethodPatch, "/v1/notifications/"+notificationID+"/read", "",
	)
	if foreignRead.Code != http.StatusNotFound {
		t.Fatalf("foreign notification read = %d, body = %s", foreignRead.Code, foreignRead.Body.String())
	}
	markedRead := issueRequest(
		handler, triageAccessToken, http.MethodPatch, "/v1/notifications/"+notificationID+"/read", "",
	)
	if markedRead.Code != http.StatusOK {
		t.Fatalf("mark notification read = %d, body = %s", markedRead.Code, markedRead.Body.String())
	}
	readNotificationResponse := issueRequest(
		handler, triageAccessToken, http.MethodGet, "/v1/notifications", "",
	)
	if !strings.Contains(readNotificationResponse.Body.String(), `"unread_count":0`) {
		t.Fatalf("read mention notification response = %s", readNotificationResponse.Body.String())
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO issue_counters (tenant_id, year, next_seq) VALUES ($1, 2026, 3)
	`, tenantID); err != nil {
		t.Fatalf("seed issue counter: %v", err)
	}
	var unlinkedFeedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, client_feedback_id, title, description, text,
			reporter_email, notify_reporter, page_url, text_process_status
		) VALUES (
			$1, $2, 'fb_Unlinked', gen_random_uuid(), 'Checkout freezes after promotion',
			'The checkout freezes after I apply a promotion code.',
			'The checkout freezes after I apply a promotion code.',
			'reporter2@example.com', true, 'https://example.com/checkout', 'completed'
		) RETURNING id
	`, tenantID, projectID).Scan(&unlinkedFeedbackID); err != nil {
		t.Fatalf("create unlinked feedback: %v", err)
	}
	createdFromFeedback := issueRequest(
		handler, accessToken, http.MethodPost, "/v1/feedbacks/"+unlinkedFeedbackID+"/issue",
		fmt.Sprintf(`{"title":"Promotion code freezes checkout","description":"Applying a valid promotion code freezes checkout before payment.","priority":"high","assignee_id":%q,"tag_ids":[%q]}`,
			memberID, checkoutTagID),
	)
	if createdFromFeedback.Code != http.StatusCreated ||
		!strings.Contains(createdFromFeedback.Body.String(), "ACME-2026-0003") ||
		!strings.Contains(createdFromFeedback.Body.String(), "Applying a valid promotion code") {
		t.Fatalf("create issue from feedback = %d, body = %s", createdFromFeedback.Code, createdFromFeedback.Body.String())
	}
	var createdIssueID, createdTitle, createdDescription, linkedIssueID, createdPriority string
	var createdAssigneeID *string
	var createdNotify bool
	if err := root.QueryRow(ctx, `
		SELECT id, title, description, priority, assignee_id, notify_reporter
		FROM issues WHERE tenant_id = $1 AND public_id = 'ACME-2026-0003'
	`, tenantID).Scan(
		&createdIssueID, &createdTitle, &createdDescription, &createdPriority, &createdAssigneeID, &createdNotify,
	); err != nil {
		t.Fatalf("read issue created from feedback: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT issue_id FROM feedbacks WHERE id = $1`, unlinkedFeedbackID).
		Scan(&linkedIssueID); err != nil {
		t.Fatalf("read new feedback issue link: %v", err)
	}
	if createdTitle != "Promotion code freezes checkout" ||
		createdDescription != "Applying a valid promotion code freezes checkout before payment." ||
		createdPriority != "high" || createdAssigneeID == nil || *createdAssigneeID != memberID ||
		!createdNotify || linkedIssueID != createdIssueID {
		t.Errorf("created issue/link = %q/%q priority=%q assignee=%v notify=%t linked=%q issue=%q",
			createdTitle, createdDescription, createdPriority, createdAssigneeID,
			createdNotify, linkedIssueID, createdIssueID)
	}
	var createdTagCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM issue_tags WHERE issue_id = $1 AND tag_id = $2
	`, createdIssueID, checkoutTagID).Scan(&createdTagCount); err != nil || createdTagCount != 1 {
		t.Errorf("created issue tag count = %d, error = %v, want 1", createdTagCount, err)
	}
	duplicateCreate := issueRequest(
		handler, accessToken, http.MethodPost, "/v1/feedbacks/"+unlinkedFeedbackID+"/issue",
		`{"title":"Duplicate","description":"Must not be created."}`,
	)
	if duplicateCreate.Code != http.StatusConflict {
		t.Errorf("duplicate issue creation = %d, body = %s", duplicateCreate.Code, duplicateCreate.Body.String())
	}
	var multiFeedbackA, multiFeedbackB string
	for publicID, input := range map[string]struct {
		priority    string
		destination *string
	}{
		"fb_Multi001": {priority: "low", destination: &multiFeedbackA},
		"fb_Multi002": {priority: "urgent", destination: &multiFeedbackB},
	} {
		if err := root.QueryRow(ctx, `
			INSERT INTO feedbacks (
				tenant_id, project_id, public_id, client_feedback_id, title, description,
				priority, assignee_id, reporter_email, notify_reporter, page_url, text_process_status
			) VALUES (
				$1, $2, $3, gen_random_uuid(), 'Repeated checkout failure',
				'The checkout fails after applying a promotion.', $4, $5,
				'reporter3@example.com', true, 'https://example.com/checkout', 'completed'
			) RETURNING id
		`, tenantID, projectID, publicID, input.priority, triageMemberID).Scan(input.destination); err != nil {
			t.Fatalf("create multi-issue feedback %s: %v", publicID, err)
		}
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO feedback_tags (tenant_id, feedback_id, tag_id)
		VALUES ($1, $2, $4), ($1, $3, $5)
	`, tenantID, multiFeedbackA, multiFeedbackB, checkoutTagID, customerImpactTagID); err != nil {
		t.Fatalf("tag multi-issue feedback: %v", err)
	}
	multiCreate := issueRequest(
		handler, accessToken, http.MethodPost, "/v1/issues/from-feedback",
		fmt.Sprintf(`{"feedback_ids":[%q,%q],"title":"Promotion failures affect checkout","description":"Several customers cannot complete checkout after applying promotions."}`,
			multiFeedbackA, multiFeedbackB),
	)
	if multiCreate.Code != http.StatusCreated {
		t.Fatalf("create issue from multiple feedback = %d body=%s", multiCreate.Code, multiCreate.Body.String())
	}
	var multiEnvelope struct {
		Issue Issue `json:"issue"`
	}
	if err := json.Unmarshal(multiCreate.Body.Bytes(), &multiEnvelope); err != nil {
		t.Fatalf("decode multi-feedback issue: %v", err)
	}
	multiIssue := multiEnvelope.Issue
	if multiIssue.PublicID != "ACME-2026-0004" || multiIssue.Priority == nil ||
		string(*multiIssue.Priority) != "urgent" || multiIssue.Assignee == nil ||
		multiIssue.Assignee.ID != triageMemberID || multiIssue.Assignee.AvatarVersion == nil ||
		*multiIssue.Assignee.AvatarVersion != triageAvatarVersion ||
		multiIssue.ReporterEmail == nil ||
		*multiIssue.ReporterEmail != "reporter3@example.com" || !multiIssue.NotifyReporter ||
		multiIssue.FeedbackCount != 2 || len(multiIssue.Tags) != 2 ||
		multiIssue.Tags[0].Name != "Checkout" || multiIssue.Tags[1].Name != "Customer impact" {
		t.Fatalf("multi-feedback issue copy rules = %#v", multiIssue)
	}
	var multiLinkCount, multiEventCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks
		WHERE id = ANY($1::uuid[]) AND issue_id = $2
	`, []string{multiFeedbackA, multiFeedbackB}, multiIssue.ID).Scan(&multiLinkCount); err != nil {
		t.Fatalf("count multi-feedback issue links: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE resource_id = $1 AND event = 'issue.created'
	`, multiIssue.ID).Scan(&multiEventCount); err != nil {
		t.Fatalf("count multi-feedback issue outbox: %v", err)
	}
	if multiLinkCount != 2 || multiEventCount != 1 {
		t.Fatalf("multi-feedback issue links/events = %d/%d, want 2/1", multiLinkCount, multiEventCount)
	}
	var rollbackFeedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, client_feedback_id, title, page_url, text_process_status
		) VALUES ($1, $2, 'fb_Multi003', gen_random_uuid(), 'Rollback candidate',
		          'https://example.com/checkout', 'skipped') RETURNING id
	`, tenantID, projectID).Scan(&rollbackFeedbackID); err != nil {
		t.Fatalf("create rollback feedback: %v", err)
	}
	conflictingMultiCreate := issueRequest(
		handler, accessToken, http.MethodPost, "/v1/issues/from-feedback",
		fmt.Sprintf(`{"feedback_ids":[%q,%q],"title":"Must roll back","description":"One feedback is already linked."}`,
			multiFeedbackA, rollbackFeedbackID),
	)
	if conflictingMultiCreate.Code != http.StatusConflict {
		t.Fatalf("conflicting multi-feedback issue = %d body=%s",
			conflictingMultiCreate.Code, conflictingMultiCreate.Body.String())
	}
	var rollbackIssueID *string
	var nextSequence, rolledBackIssueCount int
	if err := root.QueryRow(ctx, `SELECT issue_id FROM feedbacks WHERE id = $1`, rollbackFeedbackID).
		Scan(&rollbackIssueID); err != nil {
		t.Fatalf("read rollback feedback link: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT next_seq FROM issue_counters WHERE tenant_id = $1 AND year = 2026
	`, tenantID).Scan(&nextSequence); err != nil {
		t.Fatalf("read issue counter after rollback: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM issues WHERE tenant_id = $1 AND public_id = 'ACME-2026-0005'
	`, tenantID).Scan(&rolledBackIssueCount); err != nil {
		t.Fatalf("count rolled-back issue: %v", err)
	}
	if rollbackIssueID != nil || nextSequence != 5 || rolledBackIssueCount != 0 {
		t.Fatalf("multi-feedback rollback = link %v next_seq %d issue_count %d",
			rollbackIssueID, nextSequence, rolledBackIssueCount)
	}

	list := issueRequest(handler, accessToken, http.MethodGet, "/v1/issues", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "ACME-2026-0001") {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var listEnvelope struct {
		Issues []Issue `json:"issues"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode issue list: %v", err)
	}
	var listedIssue, listedLegacy *Issue
	for index := range listEnvelope.Issues {
		switch listEnvelope.Issues[index].ID {
		case issueID:
			listedIssue = &listEnvelope.Issues[index]
		case targetID:
			listedLegacy = &listEnvelope.Issues[index]
		}
	}
	if listedIssue == nil || listedLegacy == nil {
		t.Fatalf("issue list did not contain seeded records: %#v", listEnvelope.Issues)
	}
	assertIssueTriageRead(t, *listedIssue, memberID)
	if listedLegacy.Priority != nil || listedLegacy.Assignee != nil || len(listedLegacy.Tags) != 0 {
		t.Fatalf("legacy issue triage metadata = %#v", *listedLegacy)
	}
	var regressionTagID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color)
		VALUES ($1, 'Regression', '#7C3AED') RETURNING id
	`, tenantID).Scan(&regressionTagID); err != nil {
		t.Fatalf("create issue metadata tag: %v", err)
	}
	metadataUpdate := issueRequest(
		handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		fmt.Sprintf(`{"title":"Checkout total is unclear","description":"Customers cannot verify the final total before payment.","priority":"urgent","assignee_id":%q,"add_tag_ids":[%q],"remove_tag_ids":[%q]}`,
			triageMemberID, regressionTagID, customerImpactTagID),
	)
	if metadataUpdate.Code != http.StatusOK {
		t.Fatalf("update issue metadata = %d body=%s", metadataUpdate.Code, metadataUpdate.Body.String())
	}
	var metadataEnvelope struct {
		Issue  Issue   `json:"issue"`
		Update *Update `json:"update"`
	}
	if err := json.Unmarshal(metadataUpdate.Body.Bytes(), &metadataEnvelope); err != nil {
		t.Fatalf("decode issue metadata update: %v", err)
	}
	if metadataEnvelope.Update != nil || metadataEnvelope.Issue.Title != "Checkout total is unclear" ||
		metadataEnvelope.Issue.Description != "Customers cannot verify the final total before payment." ||
		metadataEnvelope.Issue.Priority == nil || string(*metadataEnvelope.Issue.Priority) != "urgent" ||
		metadataEnvelope.Issue.Assignee == nil || metadataEnvelope.Issue.Assignee.ID != triageMemberID ||
		len(metadataEnvelope.Issue.Tags) != 2 || metadataEnvelope.Issue.Tags[0].Name != "Checkout" ||
		metadataEnvelope.Issue.Tags[1].Name != "Regression" {
		t.Fatalf("updated issue metadata = %#v", metadataEnvelope)
	}
	var metadataHistoryCount int
	if err := root.QueryRow(ctx, `SELECT count(*) FROM issue_updates WHERE issue_id = $1`, issueID).
		Scan(&metadataHistoryCount); err != nil {
		t.Fatalf("count metadata-only issue updates: %v", err)
	}
	if metadataHistoryCount != 0 {
		t.Fatalf("metadata-only issue history count = %d, want 0", metadataHistoryCount)
	}
	var otherTenantID, otherMemberID, otherTagID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Other', 'other-issue', 'growth', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other issue tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email)
		VALUES ($1, 'owner@other.test') RETURNING id
	`, otherTenantID).Scan(&otherMemberID); err != nil {
		t.Fatalf("create other issue member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color)
		VALUES ($1, 'Private', '#111827') RETURNING id
	`, otherTenantID).Scan(&otherTagID); err != nil {
		t.Fatalf("create other issue tag: %v", err)
	}
	invalidMetadata := issueRequest(
		handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		fmt.Sprintf(`{"title":"Must not persist","priority":"low","assignee_id":%q,"add_tag_ids":[%q]}`,
			otherMemberID, otherTagID),
	)
	if invalidMetadata.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant issue metadata = %d body=%s", invalidMetadata.Code, invalidMetadata.Body.String())
	}
	var persistedTitle, persistedPriority, persistedAssignee string
	if err := root.QueryRow(ctx, `
		SELECT title, priority, assignee_id FROM issues WHERE id = $1
	`, issueID).Scan(&persistedTitle, &persistedPriority, &persistedAssignee); err != nil {
		t.Fatalf("read issue after invalid metadata: %v", err)
	}
	if persistedTitle != "Checkout total is unclear" || persistedPriority != "urgent" ||
		persistedAssignee != triageMemberID {
		t.Fatalf("issue changed after invalid metadata = %q/%q/%q",
			persistedTitle, persistedPriority, persistedAssignee)
	}
	updated := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"in_progress","comment":"We are investigating"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body.String())
	}
	var updateCount, eventCount int
	var updateMemberID, oldStatus, newStatus string
	if err := root.QueryRow(ctx, `
		SELECT member_id, old_status, new_status
		FROM issue_updates WHERE issue_id = $1 ORDER BY created_at, id LIMIT 1
	`, issueID).Scan(&updateMemberID, &oldStatus, &newStatus); err != nil {
		t.Fatalf("read issue updates: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM issue_updates WHERE issue_id = $1`, issueID).
		Scan(&updateCount); err != nil {
		t.Fatalf("count issue updates: %v", err)
	}
	if updateCount != 1 || updateMemberID != memberID || oldStatus != "open" || newStatus != "in_progress" {
		t.Errorf("update history = count %d member %q %s -> %s", updateCount, updateMemberID, oldStatus, newStatus)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE resource_id = $1`, issueID).Scan(&eventCount); err != nil {
		t.Fatalf("count outbox events: %v", err)
	}
	if eventCount != 1 {
		t.Errorf("outbox event count = %d", eventCount)
	}
	detail := issueRequest(handler, accessToken, http.MethodGet, "/v1/issues/"+issueID, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "We are investigating") {
		t.Fatalf("issue detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	var detailEnvelope struct {
		Issue      Issue            `json:"issue"`
		Comments   []Comment        `json:"comments"`
		Activities []activity.Event `json:"activities"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &detailEnvelope); err != nil {
		t.Fatalf("decode issue detail: %v", err)
	}
	if detailEnvelope.Issue.Priority == nil || string(*detailEnvelope.Issue.Priority) != "urgent" ||
		detailEnvelope.Issue.Assignee == nil || detailEnvelope.Issue.Assignee.ID != triageMemberID ||
		detailEnvelope.Issue.Assignee.AvatarVersion == nil ||
		*detailEnvelope.Issue.Assignee.AvatarVersion != triageAvatarVersion ||
		len(detailEnvelope.Issue.Tags) != 2 || detailEnvelope.Issue.Tags[0].Name != "Checkout" ||
		detailEnvelope.Issue.Tags[1].Name != "Regression" {
		t.Fatalf("updated issue detail metadata = %#v", detailEnvelope.Issue)
	}
	activityKinds := make(map[string]bool, len(detailEnvelope.Activities))
	for _, event := range detailEnvelope.Activities {
		activityKinds[event.Kind] = true
	}
	if len(detailEnvelope.Comments) != 1 || len(detailEnvelope.Activities) != 3 ||
		!activityKinds["issue.comment_created"] || !activityKinds["issue.updated"] ||
		!activityKinds["issue.status_changed"] {
		t.Fatalf("issue collaboration detail = comments %#v activities %#v",
			detailEnvelope.Comments, detailEnvelope.Activities)
	}
	invalidStatus := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"invalid"}`)
	if invalidStatus.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid status = %d, body = %s", invalidStatus.Code, invalidStatus.Body.String())
	}
	malformed := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID, `{`)
	if malformed.Code != http.StatusBadRequest {
		t.Errorf("malformed status update = %d, body = %s", malformed.Code, malformed.Body.String())
	}
	longComment := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		fmt.Sprintf(`{"status":"resolved","comment":%q}`, strings.Repeat("x", 2001)))
	if longComment.Code != http.StatusUnprocessableEntity {
		t.Errorf("long status comment = %d, body = %s", longComment.Code, longComment.Body.String())
	}
	missing := issueRequest(handler, accessToken, http.MethodGet,
		"/v1/issues/00000000-0000-0000-0000-000000000000", "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing issue = %d, body = %s", missing.Code, missing.Body.String())
	}
	invalidPath := issueRequest(handler, accessToken, http.MethodGet, "/v1/issues/not-a-uuid", "")
	if invalidPath.Code != http.StatusNotFound {
		t.Errorf("invalid issue path = %d, body = %s", invalidPath.Code, invalidPath.Body.String())
	}
	noChange := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"in_progress"}`)
	if noChange.Code != http.StatusConflict {
		t.Errorf("same status = %d, body = %s", noChange.Code, noChange.Body.String())
	}
	invalidMerge := issueRequest(handler, accessToken, http.MethodPost, "/v1/issues/"+issueID+"/merge",
		`{"target_issue_id":"invalid"}`)
	if invalidMerge.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid merge target = %d, body = %s", invalidMerge.Code, invalidMerge.Body.String())
	}
	selfMerge := issueRequest(handler, accessToken, http.MethodPost, "/v1/issues/"+issueID+"/merge",
		fmt.Sprintf(`{"target_issue_id":%q}`, issueID))
	if selfMerge.Code != http.StatusConflict {
		t.Errorf("self merge = %d, body = %s", selfMerge.Code, selfMerge.Body.String())
	}

	merged := issueRequest(handler, accessToken, http.MethodPost, "/v1/issues/"+issueID+"/merge",
		fmt.Sprintf(`{"target_issue_id":%q}`, targetID))
	if merged.Code != http.StatusOK || !strings.Contains(merged.Body.String(), targetID) {
		t.Fatalf("merge status = %d, body = %s", merged.Code, merged.Body.String())
	}
	unmerged := issueRequest(handler, accessToken, http.MethodDelete, "/v1/issues/"+issueID+"/merge", "")
	if unmerged.Code != http.StatusOK || strings.Contains(unmerged.Body.String(), "merged_into_id") {
		t.Fatalf("unmerge status = %d, body = %s", unmerged.Code, unmerged.Body.String())
	}
	missingUnmerge := issueRequest(handler, accessToken, http.MethodDelete,
		"/v1/issues/00000000-0000-0000-0000-000000000000/merge", "")
	if missingUnmerge.Code != http.StatusNotFound {
		t.Errorf("missing unmerge = %d, body = %s", missingUnmerge.Code, missingUnmerge.Body.String())
	}
	var mergeActivityCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM issue_activities
		WHERE tenant_id = $1 AND issue_id = $2
		  AND kind IN ('issue.merged', 'issue.unmerged')
	`, tenantID, issueID).Scan(&mergeActivityCount); err != nil || mergeActivityCount != 2 {
		t.Fatalf("merge activity count = %d, error = %v, want 2", mergeActivityCount, err)
	}
	var creationActivityCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM issue_activities
		WHERE tenant_id = $1 AND issue_id = $2 AND kind = 'issue.created'
	`, tenantID, createdIssueID).Scan(&creationActivityCount); err != nil || creationActivityCount != 1 {
		t.Fatalf("issue creation activity count = %d, error = %v, want 1", creationActivityCount, err)
	}
	const moderatedRaw = "an abusive raw report"
	var moderatedIssueID string
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title, description)
		VALUES ($1, $2, 'ACME-2026-0099', $3, $3) RETURNING id
	`, tenantID, projectID, moderatedRaw).Scan(&moderatedIssueID); err != nil {
		t.Fatalf("create historical moderated issue: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, issue_id, client_feedback_id,
			title, description, text, text_cleaned, text_moderation_flags,
			ai_suggested_title, page_url, text_process_status, ai_processed_at
		) VALUES ($1, $2, 'fb_Mod00001', $3, gen_random_uuid(), $4, $4, $4,
		          'cleaned but still flagged', '{harassment}', 'Checkout feedback needs review',
		          'https://example.com/checkout', 'completed', now())
	`, tenantID, projectID, moderatedIssueID, moderatedRaw); err != nil {
		t.Fatalf("create historical moderated feedback: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO issue_activities (tenant_id, issue_id, actor_member_id, kind, data)
		VALUES ($1, $2, $3, 'issue.created', jsonb_build_object('title', $4::text, 'description', $4::text))
	`, tenantID, moderatedIssueID, memberID, moderatedRaw); err != nil {
		t.Fatalf("create historical moderated activity: %v", err)
	}
	moderatedIssueResponse := issueRequest(
		handler, accessToken, http.MethodGet, "/v1/issues/"+moderatedIssueID, "",
	)
	if moderatedIssueResponse.Code != http.StatusOK ||
		!strings.Contains(moderatedIssueResponse.Body.String(), "Checkout feedback needs review") ||
		!strings.Contains(moderatedIssueResponse.Body.String(), `"description":"[MODERATED]"`) ||
		strings.Contains(moderatedIssueResponse.Body.String(), moderatedRaw) {
		t.Fatalf("historical moderated issue = %d body = %s", moderatedIssueResponse.Code, moderatedIssueResponse.Body.String())
	}

	store := Store{Database: pools.Admin}
	sender := &sequenceSender{}
	reporter := ReporterHandler{
		Store: store, Sender: sender, Tokens: reporterTokens,
		PublicBaseURL: "http://api.fidibeki.localhost:8088", BaseDomain: "fidibeki.localhost",
		Now: func() time.Time { return now },
	}
	feedbackReceiptSender := &sequenceSender{}
	feedbackReceiptReporter := reporter
	feedbackReceiptReporter.Sender = feedbackReceiptSender
	var feedbackCreatedEvent string
	if err := root.QueryRow(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id)
		VALUES ($1, 'feedback.created', 'feedback', $2) RETURNING id
	`, tenantID, feedbackID).Scan(&feedbackCreatedEvent); err != nil {
		t.Fatalf("create feedback receipt event: %v", err)
	}
	if err := feedbackReceiptReporter.Handler()(
		ctx, reporterTask(t, feedbackCreatedEvent, tenantID, feedbackID),
	); err != nil {
		t.Fatalf("send feedback receipt: %v", err)
	}
	if len(feedbackReceiptSender.messages) != 1 ||
		!strings.Contains(feedbackReceiptSender.messages[0].Text, "received your feedback") ||
		strings.Contains(feedbackReceiptSender.messages[0].Text, "View progress:") {
		t.Fatalf("feedback receipt = %#v", feedbackReceiptSender.messages)
	}
	if err := store.ApplyResendEvent(
		ctx, feedbackReceiptSender.ids[0], "email.complained", "feedback-complaint",
	); err != nil {
		t.Fatalf("apply feedback receipt complaint: %v", err)
	}
	var feedbackReceiptNotify bool
	if err := root.QueryRow(ctx, `SELECT notify_reporter FROM feedbacks WHERE id = $1`, feedbackID).
		Scan(&feedbackReceiptNotify); err != nil || feedbackReceiptNotify {
		t.Fatalf("feedback complaint notify = %t, error = %v", feedbackReceiptNotify, err)
	}
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET notify_reporter = true WHERE id = $1`, feedbackID); err != nil {
		t.Fatalf("restore feedback after complaint: %v", err)
	}
	receiptSender := &sequenceSender{}
	receiptReporter := reporter
	receiptReporter.Sender = receiptSender
	createdEvent := latestIssueEvent(t, ctx, root, createdIssueID)
	if err := receiptReporter.Handler()(ctx, reporterTask(t, createdEvent, tenantID, createdIssueID)); err != nil {
		t.Fatalf("send reporter receipt: %v", err)
	}
	if len(receiptSender.messages) != 1 ||
		!strings.Contains(receiptSender.messages[0].Text, "created issue") ||
		!strings.Contains(receiptSender.messages[0].Text, "View progress:") {
		t.Fatalf("reporter receipt = %#v", receiptSender.messages)
	}
	firstEvent := latestIssueEvent(t, ctx, root, issueID)
	firstTask := reporterTask(t, firstEvent, tenantID, issueID)
	if err := reporter.Handler()(ctx, firstTask); err != nil {
		t.Fatalf("send first reporter notification: %v", err)
	}
	if err := reporter.Handler()(ctx, firstTask); err != nil {
		t.Fatalf("repeat first reporter notification: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0].IdempotencyKey != "reporter:"+firstEvent {
		t.Fatalf("sent messages = %#v", sender.messages)
	}
	firstPortalURL := messageLink(t, sender.messages[0].Text, "View progress: ")
	if err := root.QueryRow(ctx, `SELECT count(*) FROM delivery_logs WHERE outbox_event_id = $1`, firstEvent).
		Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("delivery attempts = %d, error = %v", eventCount, err)
	}

	unsubscribeURL := strings.TrimPrefix(strings.Split(sender.messages[0].Text, "Unsubscribe: ")[1], " ")
	publicRouter := chi.NewRouter()
	RegisterPublicRoutes(publicRouter, PublicHTTPOptions{
		Store: store, Tokens: reporterTokens, Verifier: verifierStub{},
		Begin: db.BeginFromPool(pools.Admin),
	})
	feedbackUnsubscribeURL := messageLink(
		t, feedbackReceiptSender.messages[0].Text, "Unsubscribe: ",
	)
	feedbackUnsubscribe := httptest.NewRecorder()
	publicRouter.ServeHTTP(feedbackUnsubscribe, httptest.NewRequest(
		http.MethodGet, feedbackUnsubscribeURL, nil,
	))
	if feedbackUnsubscribe.Code != http.StatusOK {
		t.Fatalf("feedback unsubscribe status = %d, body = %s",
			feedbackUnsubscribe.Code, feedbackUnsubscribe.Body.String())
	}
	var feedbackNotify bool
	if err := root.QueryRow(ctx, `SELECT notify_reporter FROM feedbacks WHERE id = $1`, feedbackID).
		Scan(&feedbackNotify); err != nil || feedbackNotify {
		t.Fatalf("feedback receipt unsubscribe notify = %t, error = %v", feedbackNotify, err)
	}
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET notify_reporter = true WHERE id = $1`, feedbackID); err != nil {
		t.Fatalf("restore feedback reporter opt-in: %v", err)
	}
	unsubscribeRequest := httptest.NewRequest(http.MethodGet, strings.TrimSpace(unsubscribeURL), nil)
	unsubscribeResponse := httptest.NewRecorder()
	publicRouter.ServeHTTP(unsubscribeResponse, unsubscribeRequest)
	if unsubscribeResponse.Code != http.StatusOK {
		t.Fatalf("unsubscribe status = %d, body = %s", unsubscribeResponse.Code, unsubscribeResponse.Body.String())
	}
	missingToken, _, err := reporterTokens.IssueReporterUnsubscribe(
		"00000000-0000-0000-0000-000000000000", tenantID,
	)
	if err != nil {
		t.Fatalf("issue missing reporter token: %v", err)
	}
	missingUnsubscribe := httptest.NewRecorder()
	publicRouter.ServeHTTP(missingUnsubscribe, httptest.NewRequest(
		http.MethodGet, "/v1/reporter/unsubscribe?token="+url.QueryEscape(missingToken), nil,
	))
	if missingUnsubscribe.Code != http.StatusBadRequest {
		t.Errorf("missing unsubscribe issue status = %d", missingUnsubscribe.Code)
	}
	assertReporterOptIn(t, ctx, root, issueID, feedbackID, false)
	if err := reporter.Handler()(ctx, firstTask); err != nil {
		t.Fatalf("skip unsubscribed reporter notification: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Errorf("unsubscribed reporter received %d messages", len(sender.messages))
	}

	setReporterOptIn(t, ctx, root, issueID, feedbackID, true)
	resolved := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"resolved"}`)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, body = %s", resolved.Code, resolved.Body.String())
	}
	secondEvent := latestIssueEvent(t, ctx, root, issueID)
	if err := reporter.Handler()(ctx, reporterTask(t, secondEvent, tenantID, issueID)); err != nil {
		t.Fatalf("send resolved notification: %v", err)
	}
	complaint := webhookRequest(publicRouter, "email.complained", sender.ids[1], "webhook-complaint")
	if complaint.Code != http.StatusOK {
		t.Fatalf("complaint status = %d, body = %s", complaint.Code, complaint.Body.String())
	}
	assertReporterOptIn(t, ctx, root, issueID, feedbackID, false)
	var deliveryStatus string
	if err := root.QueryRow(ctx, `SELECT status FROM delivery_logs WHERE provider_message_id = $1`, sender.ids[1]).Scan(&deliveryStatus); err != nil {
		t.Fatalf("read complaint delivery: %v", err)
	}
	if deliveryStatus != "sent" {
		t.Errorf("complaint delivery status = %q", deliveryStatus)
	}

	setReporterOptIn(t, ctx, root, issueID, feedbackID, true)
	closed := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"closed"}`)
	if closed.Code != http.StatusOK {
		t.Fatalf("close status = %d, body = %s", closed.Code, closed.Body.String())
	}
	thirdEvent := latestIssueEvent(t, ctx, root, issueID)
	if err := reporter.Handler()(ctx, reporterTask(t, thirdEvent, tenantID, issueID)); err != nil {
		t.Fatalf("send closed notification: %v", err)
	}
	bounce := webhookRequest(publicRouter, "email.bounced", sender.ids[2], "webhook-bounce")
	if bounce.Code != http.StatusOK {
		t.Fatalf("bounce status = %d, body = %s", bounce.Code, bounce.Body.String())
	}
	assertReporterOptIn(t, ctx, root, issueID, feedbackID, false)
	if err := root.QueryRow(ctx, `SELECT status FROM delivery_logs WHERE provider_message_id = $1`, sender.ids[2]).Scan(&deliveryStatus); err != nil {
		t.Fatalf("read bounced delivery: %v", err)
	}
	if deliveryStatus != "bounced" {
		t.Errorf("bounce delivery status = %q", deliveryStatus)
	}

	setReporterOptIn(t, ctx, root, issueID, feedbackID, true)
	reopened := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"open"}`)
	if reopened.Code != http.StatusOK {
		t.Fatalf("reopen status = %d, body = %s", reopened.Code, reopened.Body.String())
	}
	failureEvent := latestIssueEvent(t, ctx, root, issueID)
	failingSender := &senderStub{err: errors.New("provider unavailable")}
	failingReporter := ReporterHandler{
		Store: store, Sender: failingSender, Tokens: reporterTokens,
		PublicBaseURL: "http://api.fidibeki.localhost:8088", BaseDomain: "fidibeki.localhost",
		Now: func() time.Time { return now },
	}
	failureTask := reporterTask(t, failureEvent, tenantID, issueID)
	if err := failingReporter.Handler()(ctx, failureTask); err == nil {
		t.Fatal("failed reporter send error = nil")
	}
	if err := root.QueryRow(ctx, `SELECT status FROM delivery_logs WHERE outbox_event_id = $1`, failureEvent).
		Scan(&deliveryStatus); err != nil {
		t.Fatalf("read failed delivery: %v", err)
	}
	if deliveryStatus != "failed" {
		t.Errorf("failed delivery status = %q", deliveryStatus)
	}
	if err := reporter.Handler()(ctx, failureTask); err != nil {
		t.Fatalf("recover failed reporter delivery: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT status FROM delivery_logs WHERE outbox_event_id = $1`, failureEvent).
		Scan(&deliveryStatus); err != nil || deliveryStatus != "sent" {
		t.Errorf("recovered delivery status = %q, error = %v", deliveryStatus, err)
	}

	missingTask := reporterTask(
		t, "00000000-0000-0000-0000-000000000000", tenantID, issueID,
	)
	if err := reporter.Handler()(ctx, missingTask); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("missing reporter event error = %v, want SkipRetry", err)
	}

	needsInfo := issueRequest(handler, accessToken, http.MethodPatch, "/v1/issues/"+issueID,
		`{"status":"needs_info","reporter_message":"Which promotion code did you use?"}`)
	if needsInfo.Code != http.StatusOK {
		t.Fatalf("request reporter clarification = %d, body = %s", needsInfo.Code, needsInfo.Body.String())
	}
	needsInfoEvent := latestIssueEvent(t, ctx, root, issueID)
	if err := reporter.Handler()(ctx, reporterTask(t, needsInfoEvent, tenantID, issueID)); err != nil {
		t.Fatalf("send needs-info notification: %v", err)
	}
	portalURL := messageLink(t, sender.messages[len(sender.messages)-1].Text, "View progress: ")
	parsedPortalURL, err := url.Parse(portalURL)
	if err != nil {
		t.Fatalf("parse reporter portal URL: %v", err)
	}
	portalToken := strings.TrimPrefix(parsedPortalURL.Path, "/reporter/")
	progressAPIURL := "/v1/reporter/progress/" + portalToken
	progressResponse := httptest.NewRecorder()
	publicRouter.ServeHTTP(progressResponse, httptest.NewRequest(http.MethodGet, progressAPIURL, nil))
	if progressResponse.Code != http.StatusOK ||
		!strings.Contains(progressResponse.Body.String(), "Which promotion code did you use?") {
		t.Fatalf("reporter progress = %d, body = %s", progressResponse.Code, progressResponse.Body.String())
	}
	parsedReceiptPortalURL, err := url.Parse(firstPortalURL)
	if err != nil {
		t.Fatalf("parse receipt reporter portal URL: %v", err)
	}
	previousProgress := httptest.NewRecorder()
	publicRouter.ServeHTTP(previousProgress, httptest.NewRequest(
		http.MethodGet,
		"/v1/reporter/progress/"+strings.TrimPrefix(parsedReceiptPortalURL.Path, "/reporter/"),
		nil,
	))
	if previousProgress.Code != http.StatusOK ||
		!strings.Contains(previousProgress.Body.String(), "needs_info") {
		t.Fatalf("earlier reporter progress link = %d, body = %s",
			previousProgress.Code, previousProgress.Body.String())
	}
	responseRequest := httptest.NewRequest(
		http.MethodPost, progressAPIURL+"/response", strings.NewReader(`{"response":"PROMO-25"}`),
	)
	responseRequest.Header.Set("Content-Type", "application/json")
	responseRecorder := httptest.NewRecorder()
	publicRouter.ServeHTTP(responseRecorder, responseRequest)
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("reporter clarification response = %d, body = %s",
			responseRecorder.Code, responseRecorder.Body.String())
	}
	repeatedResponse := httptest.NewRecorder()
	publicRouter.ServeHTTP(repeatedResponse, httptest.NewRequest(
		http.MethodPost, progressAPIURL+"/response", strings.NewReader(`{"response":"PROMO-OTHER"}`),
	))
	if repeatedResponse.Code != http.StatusNotFound {
		t.Fatalf("repeated reporter response = %d, body = %s",
			repeatedResponse.Code, repeatedResponse.Body.String())
	}
	var reporterResponseActivityCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM issue_activities
		WHERE tenant_id = $1 AND issue_id = $2 AND kind = 'reporter.responded'
	`, tenantID, issueID).Scan(&reporterResponseActivityCount); err != nil || reporterResponseActivityCount != 1 {
		t.Fatalf("reporter response activity count = %d, error = %v, want 1",
			reporterResponseActivityCount, err)
	}
	revoked := issueRequest(handler, accessToken, http.MethodPatch,
		"/v1/issues/"+issueID+"/reporter-access", `{"enabled":false}`)
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke reporter access = %d, body = %s", revoked.Code, revoked.Body.String())
	}
	revokedProgress := httptest.NewRecorder()
	publicRouter.ServeHTTP(revokedProgress, httptest.NewRequest(http.MethodGet, progressAPIURL, nil))
	if revokedProgress.Code != http.StatusNotFound {
		t.Fatalf("revoked reporter progress = %d, body = %s",
			revokedProgress.Code, revokedProgress.Body.String())
	}
	reenabled := issueRequest(handler, accessToken, http.MethodPatch,
		"/v1/issues/"+issueID+"/reporter-access", `{"enabled":true}`)
	if reenabled.Code != http.StatusOK {
		t.Fatalf("re-enable reporter access = %d, body = %s", reenabled.Code, reenabled.Body.String())
	}
	stillRevokedProgress := httptest.NewRecorder()
	publicRouter.ServeHTTP(stillRevokedProgress, httptest.NewRequest(http.MethodGet, progressAPIURL, nil))
	if stillRevokedProgress.Code != http.StatusNotFound {
		t.Fatalf("old reporter token after re-enable = %d, body = %s",
			stillRevokedProgress.Code, stillRevokedProgress.Body.String())
	}
}

func messageLink(t *testing.T, body, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	t.Fatalf("message link %q not found in %q", prefix, body)
	return ""
}

func assertIssueTriageRead(t *testing.T, found Issue, assigneeID string) {
	t.Helper()
	if found.Priority == nil || string(*found.Priority) != "high" || found.Assignee == nil ||
		found.Assignee.ID != assigneeID || found.Assignee.Email != "owner@acme.test" ||
		len(found.Tags) != 2 || found.Tags[0].Name != "Checkout" ||
		found.Tags[1].Name != "Customer impact" {
		t.Fatalf("issue triage metadata = %#v", found)
	}
}

type sequenceSender struct {
	messages []email.Message
	ids      []string
}

func (s *sequenceSender) Send(_ context.Context, message email.Message) (string, error) {
	id := fmt.Sprintf("message-%d", len(s.messages)+1)
	s.messages = append(s.messages, message)
	s.ids = append(s.ids, id)
	return id, nil
}

func issueRequest(handler http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://acme.fidibeki.localhost"+path, strings.NewReader(body))
	request.Host = "acme.fidibeki.localhost"
	request.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func latestIssueEvent(t *testing.T, ctx context.Context, root *pgx.Conn, issueID string) string {
	t.Helper()
	var eventID string
	if err := root.QueryRow(ctx, `
		SELECT id FROM outbox_events WHERE resource_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1
	`, issueID).Scan(&eventID); err != nil {
		t.Fatalf("read latest issue event: %v", err)
	}
	return eventID
}

func reporterTask(t *testing.T, eventID, tenantID, issueID string) *asynq.Task {
	t.Helper()
	payload, err := json.Marshal(ReporterTask{
		OutboxEventID: eventID, TenantID: tenantID, ResourceID: issueID,
	})
	if err != nil {
		t.Fatalf("encode reporter task: %v", err)
	}
	return asynq.NewTask(TaskReporterNotify, payload)
}

func webhookRequest(handler http.Handler, eventType, messageID, webhookID string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"type":%q,"data":{"email_id":%q,"to":["reporter@example.com"]}}`, eventType, messageID)
	request := httptest.NewRequest(http.MethodPost, "/v1/webhooks/resend", strings.NewReader(body))
	request.Header.Set("svix-id", webhookID)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func setReporterOptIn(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	issueID, feedbackID string,
	enabled bool,
) {
	t.Helper()
	if _, err := root.Exec(ctx, `UPDATE issues SET notify_reporter = $2 WHERE id = $1`, issueID, enabled); err != nil {
		t.Fatalf("set issue reporter opt-in: %v", err)
	}
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET notify_reporter = $2 WHERE id = $1`, feedbackID, enabled); err != nil {
		t.Fatalf("set feedback reporter opt-in: %v", err)
	}
}

func assertReporterOptIn(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	issueID, feedbackID string,
	want bool,
) {
	t.Helper()
	var issueOptIn, feedbackOptIn bool
	if err := root.QueryRow(ctx, `SELECT notify_reporter FROM issues WHERE id = $1`, issueID).Scan(&issueOptIn); err != nil {
		t.Fatalf("read issue opt-in: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT notify_reporter FROM feedbacks WHERE id = $1`, feedbackID).Scan(&feedbackOptIn); err != nil {
		t.Fatalf("read feedback opt-in: %v", err)
	}
	if issueOptIn != want || feedbackOptIn != want {
		t.Errorf("opt-in = issue %t feedback %t, want %t", issueOptIn, feedbackOptIn, want)
	}
}

func issueMigrationsPath(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", "..", "infra", "migrations"))
}

func issueRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse direct URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
