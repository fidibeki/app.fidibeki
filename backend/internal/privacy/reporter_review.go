package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

const (
	reviewDecisionReasonMaxLength = 2000
	reviewRedactionPlaceholder    = "[removed for privacy]"
)

type reviewCandidate struct {
	ResourceType          string
	ResourceID            string
	Fields                []string
	DisclosedDestinations map[string]int
}

// captureReporterRequestScope freezes the feedback/issue rows that belong to a reporter while
// their email is still queryable. Structured anonymization nulls that email unconditionally in
// the same job run, so later review and finalization passes (which may run long after, driven
// by human decisions) recheck this fixed id scope instead of the reporter's raw address, which
// is never persisted here.
func captureReporterRequestScope(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	requestID string,
	email string,
	cutoff time.Time,
) error {
	if _, err := query.Exec(ctx, `
		INSERT INTO reporter_privacy_request_scope (tenant_id, reporter_request_id, resource_type, resource_id)
		SELECT $1::uuid, $2::uuid, 'feedback', feedback.id FROM feedbacks AS feedback
		WHERE feedback.tenant_id = $1 AND lower(feedback.reporter_email) = $3 AND feedback.created_at <= $4
		UNION
		SELECT $1::uuid, $2::uuid, 'issue', issue.id FROM issues AS issue
		WHERE issue.tenant_id = $1 AND issue.created_at <= $4 AND (
			lower(issue.reporter_email) = $3 OR EXISTS (
				SELECT 1 FROM feedbacks AS related
				WHERE related.tenant_id = issue.tenant_id AND related.issue_id = issue.id
				  AND lower(related.reporter_email) = $3 AND related.created_at <= $4
			)
		)
		ON CONFLICT DO NOTHING
	`, tenantID, requestID, email, cutoff.UTC()); err != nil {
		return fmt.Errorf("capture reporter privacy scope: %w", err)
	}
	return nil
}

const reviewCandidateQuery = `
	WITH scope_feedback AS (
		SELECT resource_id AS id FROM reporter_privacy_request_scope
		WHERE tenant_id = $1 AND reporter_request_id = $2 AND resource_type = 'feedback'
	), scope_issue AS (
		SELECT resource_id AS id FROM reporter_privacy_request_scope
		WHERE tenant_id = $1 AND reporter_request_id = $2 AND resource_type = 'issue'
	), candidates AS (
		SELECT 'feedback'::text AS resource_type, feedback.id AS resource_id,
		       ARRAY_REMOVE(ARRAY[
		           CASE WHEN feedback.description IS NOT NULL AND btrim(feedback.description) <> ''
		                THEN 'description' END,
		           CASE WHEN feedback.comment IS NOT NULL AND btrim(feedback.comment) <> ''
		                THEN 'comment' END
		       ], NULL) AS fields,
		       feedback.id AS feedback_id, feedback.issue_id AS issue_id
		FROM feedbacks AS feedback
		WHERE feedback.tenant_id = $1 AND feedback.id IN (SELECT id FROM scope_feedback)
		  AND (
		      (feedback.description IS NOT NULL AND btrim(feedback.description) <> '')
		      OR (feedback.comment IS NOT NULL AND btrim(feedback.comment) <> '')
		  )
		UNION ALL
		SELECT 'issue', issue.id, ARRAY['description']::text[], NULL::uuid, issue.id
		FROM issues AS issue
		WHERE issue.tenant_id = $1 AND issue.id IN (SELECT id FROM scope_issue)
		  AND btrim(issue.description) <> ''
		UNION ALL
		SELECT 'issue_comment', comment.id, ARRAY['body']::text[], NULL::uuid, comment.issue_id
		FROM issue_comments AS comment
		WHERE comment.tenant_id = $1 AND comment.issue_id IN (SELECT id FROM scope_issue)
		UNION ALL
		SELECT 'clarification_request', request.id,
		       ARRAY_REMOVE(ARRAY['prompt', CASE WHEN request.response IS NOT NULL THEN 'response' END], NULL),
		       NULL::uuid, request.issue_id
		FROM reporter_clarification_requests AS request
		WHERE request.tenant_id = $1 AND request.issue_id IN (SELECT id FROM scope_issue)
	)
	SELECT candidate.resource_type, candidate.resource_id, candidate.fields,
	       jsonb_strip_nulls(jsonb_build_object(
	           'slack', NULLIF((
	               SELECT count(*) FROM delivery_logs AS delivery
	               JOIN integrations AS integration
	                 ON integration.tenant_id = delivery.tenant_id
	                AND integration.id = delivery.integration_id AND integration.kind = 'slack'
	               WHERE delivery.tenant_id = $1 AND delivery.status = 'sent' AND (
	                   (candidate.feedback_id IS NOT NULL AND delivery.resource_type = 'feedback'
	                       AND delivery.resource_id = candidate.feedback_id)
	                   OR (candidate.issue_id IS NOT NULL AND delivery.resource_type = 'issue'
	                       AND delivery.resource_id = candidate.issue_id)
	               )
	           ), 0),
	           'teams', NULLIF((
	               SELECT count(*) FROM delivery_logs AS delivery
	               JOIN integrations AS integration
	                 ON integration.tenant_id = delivery.tenant_id
	                AND integration.id = delivery.integration_id AND integration.kind = 'teams'
	               WHERE delivery.tenant_id = $1 AND delivery.status = 'sent' AND (
	                   (candidate.feedback_id IS NOT NULL AND delivery.resource_type = 'feedback'
	                       AND delivery.resource_id = candidate.feedback_id)
	                   OR (candidate.issue_id IS NOT NULL AND delivery.resource_type = 'issue'
	                       AND delivery.resource_id = candidate.issue_id)
	               )
	           ), 0),
	           'webhook', NULLIF((
	               SELECT count(*) FROM delivery_logs AS delivery
	               JOIN integrations AS integration
	                 ON integration.tenant_id = delivery.tenant_id
	                AND integration.id = delivery.integration_id AND integration.kind = 'webhook'
	               WHERE delivery.tenant_id = $1 AND delivery.status = 'sent' AND (
	                   (candidate.feedback_id IS NOT NULL AND delivery.resource_type = 'feedback'
	                       AND delivery.resource_id = candidate.feedback_id)
	                   OR (candidate.issue_id IS NOT NULL AND delivery.resource_type = 'issue'
	                       AND delivery.resource_id = candidate.issue_id)
	               )
	           ), 0),
	           'email', NULLIF((
	               SELECT count(*) FROM delivery_logs AS delivery
	               WHERE delivery.tenant_id = $1 AND delivery.status = 'sent'
	                 AND delivery.integration_id IS NULL AND (
	                   (candidate.feedback_id IS NOT NULL AND delivery.resource_type = 'feedback'
	                       AND delivery.resource_id = candidate.feedback_id)
	                   OR (candidate.issue_id IS NOT NULL AND delivery.resource_type = 'issue'
	                       AND delivery.resource_id = candidate.issue_id)
	               )
	           ), 0)
	       )) AS disclosed_destinations
	FROM candidates AS candidate
	ORDER BY candidate.resource_type, candidate.resource_id
`

// reporterReviewCandidates re-derives, from the frozen resource scope, every record that still
// carries free-form content requiring a human decision. It never reads the reporter's email, so
// it works identically for a fresh job run and for a finalize call issued long after anonymize
// nulled that column.
func reporterReviewCandidates(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	requestID string,
) ([]reviewCandidate, error) {
	rows, err := query.Query(ctx, reviewCandidateQuery, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("scan reporter review candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]reviewCandidate, 0)
	for rows.Next() {
		var candidate reviewCandidate
		var destinations []byte
		if err := rows.Scan(
			&candidate.ResourceType, &candidate.ResourceID, &candidate.Fields, &destinations,
		); err != nil {
			return nil, fmt.Errorf("scan reporter review candidate: %w", err)
		}
		if len(destinations) > 0 {
			if err := json.Unmarshal(destinations, &candidate.DisclosedDestinations); err != nil {
				return nil, fmt.Errorf("decode reporter review disclosure: %w", err)
			}
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reporter review candidates: %w", err)
	}
	return candidates, nil
}

// reconcileReviewItems upserts every currently flagged candidate (creating new pending items for
// content detected since the last pass) and auto-resolves pending items whose flagged content is
// no longer present, then returns the fresh pending count. It never touches an item that has
// already been decided by a human. Concurrent edits that introduce new identifying content are
// caught here because the candidate scan always re-reads the live rows.
func reconcileReviewItems(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	requestID string,
	jobID *string,
	now time.Time,
) (int, error) {
	candidates, err := reporterReviewCandidates(ctx, query, tenantID, requestID)
	if err != nil {
		return 0, err
	}
	currentKeys := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		currentKeys = append(currentKeys, candidate.ResourceType+":"+candidate.ResourceID)
		destinations, err := json.Marshal(candidate.DisclosedDestinations)
		if err != nil {
			return 0, fmt.Errorf("encode reporter review disclosure: %w", err)
		}
		if _, err := query.Exec(ctx, `
			INSERT INTO reporter_privacy_review_items (
				tenant_id, reporter_request_id, privacy_job_id, resource_type, resource_id,
				fields, disclosed_destinations
			) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
			ON CONFLICT (tenant_id, reporter_request_id, resource_type, resource_id) DO UPDATE
			SET fields = EXCLUDED.fields, disclosed_destinations = EXCLUDED.disclosed_destinations
			WHERE reporter_privacy_review_items.status = 'pending'
		`, tenantID, requestID, jobID, candidate.ResourceType, candidate.ResourceID,
			candidate.Fields, string(destinations)); err != nil {
			return 0, fmt.Errorf("upsert reporter review item: %w", err)
		}
	}
	if _, err := query.Exec(ctx, `
		UPDATE reporter_privacy_review_items
		SET status = 'redacted', decided_at = $3,
		    decision_reason = 'content no longer present at rescan'
		WHERE tenant_id = $1 AND reporter_request_id = $2 AND status = 'pending'
		  AND resource_type <> 'legacy_unlinked'
		  AND NOT (resource_type || ':' || resource_id::text = ANY($4::text[]))
	`, tenantID, requestID, now.UTC(), currentKeys); err != nil {
		return 0, fmt.Errorf("auto-resolve reporter review items: %w", err)
	}
	var pending int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FROM reporter_privacy_review_items
		WHERE tenant_id = $1 AND reporter_request_id = $2 AND status = 'pending'
	`, tenantID, requestID).Scan(&pending); err != nil {
		return 0, fmt.Errorf("count reporter review pending items: %w", err)
	}
	return pending, nil
}

// FinalizeReporterAnonymization recalculates the pending review queue and, only if nothing
// remains pending, completes both the privacy job and the reporter request. It is safe to call
// repeatedly (after every decision, or on demand) and is idempotent once the request is
// completed.
func (Store) FinalizeReporterAnonymization(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	requestID string,
	now time.Time,
) (completed bool, pending int, err error) {
	var status ReporterRequestStatus
	var jobID *string
	err = query.QueryRow(ctx, `
		SELECT request.status, job.id
		FROM reporter_privacy_requests AS request
		LEFT JOIN privacy_jobs AS job
		  ON job.tenant_id = request.tenant_id AND job.reporter_request_id = request.id
		 AND job.kind = 'reporter_anonymize'
		WHERE request.tenant_id = $1 AND request.id = $2
	`, tenantID, requestID).Scan(&status, &jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, ErrNotFound
	}
	if err != nil {
		return false, 0, fmt.Errorf("load reporter anonymization request: %w", err)
	}
	if status == ReporterCompleted {
		return true, 0, nil
	}
	if status != ReporterAwaitingReview {
		return false, 0, ErrJobUnavailable
	}
	pending, err = reconcileReviewItems(ctx, query, tenantID, requestID, jobID, now)
	if err != nil {
		return false, pending, err
	}
	if pending > 0 {
		return false, pending, nil
	}
	var completedID string
	err = query.QueryRow(ctx, `
		WITH completed_job AS (
			UPDATE privacy_jobs
			SET status = 'completed', heartbeat_at = $3, completed_at = $3,
			    expires_at = $3::timestamptz + interval '30 days',
			    object_key = NULL, object_size_bytes = 0, error_message = NULL
			WHERE tenant_id = $1 AND reporter_request_id = $2 AND kind = 'reporter_anonymize'
			  AND status = 'awaiting_manual_review'
			RETURNING id
		), completed_request AS (
			UPDATE reporter_privacy_requests
			SET status = 'completed', completed_at = $3, manual_review_count = 0, error_message = NULL
			WHERE tenant_id = $1 AND id = $2 AND status = 'awaiting_manual_review'
			RETURNING id
		), audited AS (
			INSERT INTO tenant_access_events (
				tenant_id, kind, actor_member_id, subject_member_id, data
			)
			SELECT $1, 'privacy.reporter_anonymization_completed', NULL, NULL,
			       jsonb_build_object('reporter_request_id', completed_request.id::text)
			       || jsonb_build_object(
			              'privacy_job_id', (SELECT id::text FROM completed_job)
			          )
			FROM completed_request
			RETURNING id
		)
		SELECT completed_request.id FROM completed_request
	`, tenantID, requestID, now.UTC()).Scan(&completedID)
	if errors.Is(err, pgx.ErrNoRows) {
		if statusErr := query.QueryRow(ctx, `
			SELECT status
			FROM reporter_privacy_requests
			WHERE tenant_id = $1 AND id = $2
		`, tenantID, requestID).Scan(&status); statusErr == nil && status == ReporterCompleted {
			return true, 0, nil
		} else if statusErr != nil && !errors.Is(statusErr, pgx.ErrNoRows) {
			return false, 0, fmt.Errorf(
				"recheck reporter anonymization after concurrent completion: %w",
				statusErr,
			)
		}
		return false, 0, ErrJobUnavailable
	}
	if err != nil {
		return false, 0, fmt.Errorf("complete reporter anonymization after review: %w", err)
	}
	return true, 0, nil
}

// ListReviewItems returns every review item for a request, most recently created pending items
// first, scoped to the tenant.
func (Store) ListReviewItems(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	requestID string,
) ([]ReviewItem, error) {
	rows, err := query.Query(ctx, `
		SELECT id, tenant_id, reporter_request_id, privacy_job_id, resource_type, resource_id,
		       fields, status, decision_reason, decided_by_member_id, decided_at,
		       disclosed_destinations, created_at, updated_at
		FROM reporter_privacy_review_items
		WHERE tenant_id = $1 AND reporter_request_id = $2
		ORDER BY (status = 'pending') DESC, created_at, id
	`, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("list reporter review items: %w", err)
	}
	defer rows.Close()
	items := make([]ReviewItem, 0)
	for rows.Next() {
		item, err := scanReviewItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reporter review items: %w", err)
	}
	return items, nil
}

// DecideReviewItem records a human decision for exactly one pending review item. Redacted and
// deleted decisions clear the flagged fields (and any AI text derived from them) on the
// underlying resource and expire already-generated exports so a stale copy cannot be downloaded;
// not-personal-data and retained-exception decisions leave the resource untouched. The item row
// itself becomes immutable the moment this call succeeds.
func (Store) DecideReviewItem(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	requestID string,
	itemID string,
	actorMemberID string,
	decision ReviewDecision,
	now time.Time,
) (ReviewItem, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return ReviewItem{}, err
	}
	switch decision.Status {
	case ReviewRedacted, ReviewDeleted, ReviewNotPersonalData, ReviewRetainedException:
	default:
		return ReviewItem{}, ErrInvalidRequest
	}
	reason := strings.TrimSpace(decision.Reason)
	if len(reason) > reviewDecisionReasonMaxLength {
		return ReviewItem{}, ErrInvalidRequest
	}
	if decision.Status == ReviewRetainedException && reason == "" {
		return ReviewItem{}, ErrReviewReasonMissing
	}
	var reasonArg any
	if reason != "" {
		reasonArg = reason
	}
	var resourceType, resourceID string
	var fields []string
	var currentStatus ReviewItemStatus
	err := query.QueryRow(ctx, `
		SELECT resource_type, resource_id, fields, status
		FROM reporter_privacy_review_items
		WHERE tenant_id = $1 AND reporter_request_id = $2 AND id = $3
	`, tenantID, requestID, itemID).Scan(&resourceType, &resourceID, &fields, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewItem{}, ErrNotFound
	}
	if err != nil {
		return ReviewItem{}, fmt.Errorf("load reporter review item: %w", err)
	}
	if currentStatus != ReviewPending {
		return ReviewItem{}, ErrReviewItemDecided
	}
	if decision.Status == ReviewRedacted || decision.Status == ReviewDeleted {
		if err := applyReviewRedaction(ctx, query, tenantID, resourceType, resourceID, fields); err != nil {
			return ReviewItem{}, err
		}
		if err := expireStaleReporterExports(ctx, query, tenantID, now); err != nil {
			return ReviewItem{}, err
		}
	}
	item, err := scanReviewItem(query.QueryRow(ctx, `
		UPDATE reporter_privacy_review_items
		SET status = $4, decision_reason = $5, decided_by_member_id = $6, decided_at = $7
		WHERE tenant_id = $1 AND reporter_request_id = $2 AND id = $3 AND status = 'pending'
		RETURNING id, tenant_id, reporter_request_id, privacy_job_id, resource_type, resource_id,
		          fields, status, decision_reason, decided_by_member_id, decided_at,
		          disclosed_destinations, created_at, updated_at
	`, tenantID, requestID, itemID, string(decision.Status), reasonArg, actorMemberID, now.UTC()))
	if errors.Is(err, ErrNotFound) {
		return ReviewItem{}, ErrReviewItemDecided
	}
	if err != nil {
		return ReviewItem{}, err
	}
	if err := recordPrivacyRequestAudit(
		ctx, query, tenantID, "privacy.reporter_review_item_decided", actorMemberID, requestID,
		map[string]any{
			"review_item_id": itemID, "resource_type": resourceType, "status": string(decision.Status),
		},
	); err != nil {
		return ReviewItem{}, err
	}
	return item, nil
}

func applyReviewRedaction(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	resourceType string,
	resourceID string,
	fields []string,
) error {
	has := func(name string) bool {
		return slices.Contains(fields, name)
	}
	switch resourceType {
	case "feedback":
		if _, err := query.Exec(ctx, `
			UPDATE feedbacks
			SET description = CASE WHEN $3 THEN NULL ELSE description END,
			    comment = CASE WHEN $4 THEN NULL ELSE comment END,
			    text_cleaned = NULL, ai_suggested_title = NULL,
			    ai_actionability = NULL, ai_category = NULL, ai_priority = NULL
			WHERE tenant_id = $1 AND id = $2
		`, tenantID, resourceID, has("description"), has("comment")); err != nil {
			return fmt.Errorf("redact reporter feedback: %w", err)
		}
	case "issue":
		if _, err := query.Exec(ctx, `
			UPDATE issues SET description = '' WHERE tenant_id = $1 AND id = $2
		`, tenantID, resourceID); err != nil {
			return fmt.Errorf("redact reporter issue: %w", err)
		}
	case "issue_comment":
		if _, err := query.Exec(ctx, `
			UPDATE issue_comments SET body = $3 WHERE tenant_id = $1 AND id = $2
		`, tenantID, resourceID, reviewRedactionPlaceholder); err != nil {
			return fmt.Errorf("redact reporter issue comment: %w", err)
		}
	case "clarification_request":
		if _, err := query.Exec(ctx, `
			UPDATE reporter_clarification_requests
			SET prompt = CASE WHEN $3 THEN $5 ELSE prompt END,
			    response = CASE WHEN $4 THEN NULL ELSE response END
			WHERE tenant_id = $1 AND id = $2
		`, tenantID, resourceID, has("prompt"), has("response"), reviewRedactionPlaceholder); err != nil {
			return fmt.Errorf("redact reporter clarification request: %w", err)
		}
	case "legacy_unlinked":
		// No specific resource can be identified for a pre-migration record; the decision
		// itself (with its required reason for retained_exception) is the remediation.
	}
	return nil
}

// expireStaleReporterExports invalidates already-generated export archives once content they may
// contain has just been redacted or deleted, forcing regeneration before any further download.
// This reuses the same expiry gate the retention worker already relies on to physically remove
// the storage object (see idx_privacy_jobs_expiry / retention.go).
func expireStaleReporterExports(ctx context.Context, query db.Querier, tenantID string, now time.Time) error {
	if _, err := query.Exec(ctx, `
		UPDATE privacy_jobs
		SET expires_at = $2
		WHERE tenant_id = $1 AND kind IN ('workspace_export', 'reporter_export')
		  AND status = 'completed' AND object_key IS NOT NULL
		  AND completed_at < $2 AND expires_at > $2
	`, tenantID, now.UTC()); err != nil {
		return fmt.Errorf("expire stale privacy exports: %w", err)
	}
	return nil
}

func scanReviewItem(row rowScanner) (ReviewItem, error) {
	var item ReviewItem
	var destinations []byte
	err := row.Scan(
		&item.ID, &item.TenantID, &item.ReporterRequestID, &item.PrivacyJobID,
		&item.ResourceType, &item.ResourceID, &item.Fields, &item.Status,
		&item.DecisionReason, &item.DecidedByMemberID, &item.DecidedAt,
		&destinations, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewItem{}, ErrNotFound
	}
	if err != nil {
		return ReviewItem{}, fmt.Errorf("scan reporter review item: %w", err)
	}
	if len(destinations) > 0 {
		if err := json.Unmarshal(destinations, &item.DisclosedDestinations); err != nil {
			return ReviewItem{}, fmt.Errorf("decode reporter review item disclosure: %w", err)
		}
	}
	return item, nil
}
