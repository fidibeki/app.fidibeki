package testimonial

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

// Store is the persistence boundary for the testimonial domain. Every method
// takes a db.Querier so callers can supply either a request transaction or the
// worker's pool, matching the convention used across the other domains.
type Store struct{}

const candidateColumns = `
	candidate.feedback_id, candidate.project_id, candidate.base_score, candidate.decayed_score,
	candidate.score_version, candidate.eligibility, candidate.reasons, candidate.ineligible_reasons,
	candidate.signals, candidate.suggested_excerpt, candidate.suggested_headline,
	candidate.dismissed_at, candidate.computed_at
`

const testimonialColumns = `
	id, public_id, project_id, source, source_feedback_id, request_id, consented_text, quote,
	headline, author_name, author_role, author_company, author_company_url, rating, status,
	consent_state, consent_scopes, review_note, collected_at, approved_at, revoked_at,
	created_at, updated_at
`

const wallColumns = `
	id, public_id, project_id, name, slug, layout, theme, show_rating, show_avatar, show_company,
	show_video, max_items, allowed_origins, status, published_at, has_unpublished_changes,
	created_at, updated_at
`

// ListCandidates returns the Suggested queue: eligible, not dismissed, best
// first. It joins the feedback text so the caller renders an excerpt without a
// second round trip.
func (Store) ListCandidates(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	limit int,
) ([]Candidate, error) {
	rows, err := query.Query(ctx, `
		SELECT `+candidateColumns+`, feedback.public_id, feedback.text, feedback.rating,
		       feedback.reporter_email, feedback.notify_reporter, feedback.created_at
		FROM testimonial_candidates AS candidate
		JOIN feedbacks AS feedback
		  ON feedback.tenant_id = candidate.tenant_id AND feedback.id = candidate.feedback_id
		WHERE candidate.tenant_id = $1
		  AND candidate.eligibility = 'eligible'
		  AND candidate.dismissed_at IS NULL
		ORDER BY candidate.decayed_score DESC, feedback.created_at DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list testimonial candidates: %w", err)
	}
	defer rows.Close()

	candidates := []Candidate{}
	for rows.Next() {
		var candidate Candidate
		var signals []byte
		var text *string
		if err := rows.Scan(
			&candidate.FeedbackID, &candidate.ProjectID, &candidate.BaseScore, &candidate.DecayedScore,
			&candidate.ScoreVersion, &candidate.Eligibility, &candidate.Reasons,
			&candidate.IneligibleReasons, &signals, &candidate.SuggestedExcerpt,
			&candidate.SuggestedHeadline, &candidate.DismissedAt, &candidate.ComputedAt,
			&candidate.PublicID, &text, &candidate.Rating, &candidate.ReporterEmail,
			&candidate.ContactPermitted, &candidate.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan testimonial candidate: %w", err)
		}
		if text != nil {
			candidate.Text = *text
		}
		if len(signals) > 0 {
			_ = json.Unmarshal(signals, &candidate.Signals)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate testimonial candidates: %w", err)
	}
	return candidates, nil
}

// UpsertCandidate stores a scoring result. It is idempotent by
// (tenant_id, feedback_id) so a rescore never duplicates a row, and it preserves
// an existing dismissal: a customer who said "never suggest this" should not be
// overruled by the next scoring pass.
func (Store) UpsertCandidate(
	ctx context.Context,
	query db.Querier,
	tenantID, feedbackID, projectID string,
	assessment Assessment,
	decayed float64,
	now time.Time,
) error {
	signals, err := json.Marshal(assessment.Signals)
	if err != nil {
		return fmt.Errorf("encode candidate signals: %w", err)
	}
	reasons := assessment.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	ineligible := assessment.IneligibleReasons
	if ineligible == nil {
		ineligible = []string{}
	}
	_, err = query.Exec(ctx, `
		INSERT INTO testimonial_candidates (
			tenant_id, feedback_id, project_id, base_score, decayed_score, score_version,
			eligibility, reasons, ineligible_reasons, signals, computed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (tenant_id, feedback_id) DO UPDATE SET
			project_id = EXCLUDED.project_id,
			base_score = EXCLUDED.base_score,
			decayed_score = EXCLUDED.decayed_score,
			score_version = EXCLUDED.score_version,
			eligibility = EXCLUDED.eligibility,
			reasons = EXCLUDED.reasons,
			ineligible_reasons = EXCLUDED.ineligible_reasons,
			signals = EXCLUDED.signals,
			computed_at = EXCLUDED.computed_at
	`, tenantID, feedbackID, projectID, assessment.BaseScore, decayed, assessment.ScoreVersion,
		string(assessment.Eligibility), reasons, ineligible, string(signals), now)
	if err != nil {
		return fmt.Errorf("upsert testimonial candidate: %w", err)
	}
	return nil
}

// DismissCandidate records "never suggest this again".
func (Store) DismissCandidate(
	ctx context.Context,
	query db.Querier,
	tenantID, feedbackID, memberID string,
	now time.Time,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE testimonial_candidates
		SET dismissed_at = $4, dismissed_by = $3
		WHERE tenant_id = $1 AND feedback_id = $2 AND dismissed_at IS NULL
	`, tenantID, feedbackID, memberID, now)
	if err != nil {
		return fmt.Errorf("dismiss testimonial candidate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateRequest stores a collection or re-consent invitation. Only the token
// digest is persisted; the caller holds the plaintext for the email.
// ErrNotContactable reports that a feedback carries no address its reporter
// agreed to be reached on.
var ErrNotContactable = errors.New("testimonial: feedback has no contactable reporter")

// FeedbackRecipient returns the address a testimonial request for this feedback
// may be sent to.
//
// It is the reporter's own address, and only when they asked to be contacted
// about this feedback. That consent is what makes asking them about the same
// feedback a compatible purpose; without it there is no address here to use,
// whatever the caller supplies.
func (Store) FeedbackRecipient(
	ctx context.Context,
	query db.Querier,
	tenantID, feedbackID string,
) (string, error) {
	var email *string
	var permitted bool
	err := query.QueryRow(ctx, `
		SELECT reporter_email, notify_reporter
		FROM feedbacks WHERE tenant_id = $1 AND id = $2
	`, tenantID, feedbackID).Scan(&email, &permitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("load feedback recipient: %w", err)
	}
	if email == nil || *email == "" || !permitted {
		return "", ErrNotContactable
	}
	return *email, nil
}

func (Store) CreateRequest(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	values RequestValues,
) (Request, error) {
	var request Request
	err := query.QueryRow(ctx, `
		INSERT INTO testimonial_requests (
			tenant_id, project_id, feedback_id, testimonial_id, kind, token_hash, recipient_email,
			prompt, locale, proposed_quote, expires_at, sent_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, project_id, feedback_id, testimonial_id, kind, recipient_email, status,
		          prompt, locale, proposed_quote, expires_at, sent_at, opened_at, submitted_at,
		          created_at
	`, tenantID, values.ProjectID, values.FeedbackID, values.TestimonialID, string(values.Kind),
		values.TokenHash, values.RecipientEmail, values.Prompt, values.Locale, values.ProposedQuote,
		values.ExpiresAt, values.SentAt, values.CreatedBy).
		Scan(&request.ID, &request.ProjectID, &request.FeedbackID, &request.TestimonialID,
			&request.Kind, &request.RecipientEmail, &request.Status, &request.Prompt,
			&request.Locale, &request.ProposedQuote, &request.ExpiresAt, &request.SentAt,
			&request.OpenedAt, &request.SubmittedAt, &request.CreatedAt)
	if err != nil {
		return Request{}, fmt.Errorf("create testimonial request: %w", err)
	}
	return request, nil
}

// RequestValues is a validated invitation ready to store.
type RequestValues struct {
	ProjectID      *string
	FeedbackID     *string
	TestimonialID  *string
	Kind           RequestKind
	TokenHash      string
	RecipientEmail string
	Prompt         *string
	Locale         *string
	ProposedQuote  *string
	ExpiresAt      time.Time
	SentAt         *time.Time
	CreatedBy      *string
}

// ResolveRequestTenant finds which workspace a collection token belongs to,
// without a tenant context. It must be called on the admin pool, mirroring how
// the widget resolves a project from its public key, and its result is what the
// caller passes to set_config before reading anything else.
func (Store) ResolveRequestTenant(
	ctx context.Context,
	query db.Querier,
	tokenHash string,
) (string, error) {
	var tenantID string
	err := query.QueryRow(ctx, `
		SELECT tenant_id FROM testimonial_requests WHERE token_hash = $1
	`, tokenHash).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("resolve testimonial request tenant: %w", err)
	}
	return tenantID, nil
}

// LoadRequestByToken reads an invitation under tenant context.
func (Store) LoadRequestByToken(
	ctx context.Context,
	query db.Querier,
	tenantID, tokenHash string,
) (Request, error) {
	var request Request
	err := query.QueryRow(ctx, `
		SELECT id, project_id, feedback_id, testimonial_id, kind, recipient_email, status, prompt,
		       locale, proposed_quote, expires_at, sent_at, opened_at, submitted_at, created_at
		FROM testimonial_requests
		WHERE tenant_id = $1 AND token_hash = $2
		FOR UPDATE
	`, tenantID, tokenHash).
		Scan(&request.ID, &request.ProjectID, &request.FeedbackID, &request.TestimonialID,
			&request.Kind, &request.RecipientEmail, &request.Status, &request.Prompt,
			&request.Locale, &request.ProposedQuote, &request.ExpiresAt, &request.SentAt,
			&request.OpenedAt, &request.SubmittedAt, &request.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrTokenInvalid
	}
	if err != nil {
		return Request{}, fmt.Errorf("load testimonial request: %w", err)
	}
	return request, nil
}

// MarkRequestOpened is idempotent: opening a link twice is normal.
func (Store) MarkRequestOpened(
	ctx context.Context,
	query db.Querier,
	tenantID, requestID string,
	now time.Time,
) error {
	_, err := query.Exec(ctx, `
		UPDATE testimonial_requests
		SET status = 'opened', opened_at = COALESCE(opened_at, $3)
		WHERE tenant_id = $1 AND id = $2 AND status = 'pending'
	`, tenantID, requestID, now)
	if err != nil {
		return fmt.Errorf("mark testimonial request opened: %w", err)
	}
	return nil
}

// SubmissionValues is what the author supplied through the public form.
type SubmissionValues struct {
	ProjectID  *string
	FeedbackID *string
	RequestID  string
	Source     Source
	Quote      string
	Headline   *string
	Author     AuthorDetails
	// Rating overrides the stars carried over from the source feedback. It is only
	// needed for a submission that has no feedback behind it; leaving it nil is
	// the normal case, not an omission.
	Rating  *int
	Scopes  ConsentScopes
	Locale  *string
	Consent ConsentEvidence
}

// ConsentEvidence is the auditable record that permission was given. It carries
// no raw IP address: a user-agent family and a coarse region are sufficient
// proof and far less sensitive.
type ConsentEvidence struct {
	NoticeVersion            string         `json:"notice_version"`
	ControllerProfileVersion int            `json:"controller_profile_version"`
	Locale                   string         `json:"locale"`
	UserAgentFamily          string         `json:"user_agent_family"`
	Region                   string         `json:"region"`
	NoticeSnapshotHash       string         `json:"notice_snapshot_hash"`
	Extra                    map[string]any `json:"extra,omitempty"`
}

// SubmitCollection creates the testimonial and closes the request in one
// transaction. The unique index on (tenant_id, request_id) is what makes a
// double submit impossible; checking the request status alone would let two
// concurrent submissions both read 'pending' and both insert.
func (s Store) SubmitCollection(
	ctx context.Context,
	query db.Querier,
	tenantID, publicID, revocationTokenHash string,
	values SubmissionValues,
	now time.Time,
) (Testimonial, error) {
	scopes, err := json.Marshal(values.Scopes)
	if err != nil {
		return Testimonial{}, fmt.Errorf("encode consent scopes: %w", err)
	}
	evidence, err := json.Marshal(values.Consent)
	if err != nil {
		return Testimonial{}, fmt.Errorf("encode consent evidence: %w", err)
	}
	var id string
	err = query.QueryRow(ctx, `
		INSERT INTO testimonials (
			tenant_id, public_id, project_id, source, source_feedback_id, request_id,
			consented_text, quote, headline, author_name, author_role, author_company,
			author_company_url, author_locale, rating, status, consent_state, consent_scopes,
			consent_notice_version, consent_controller_profile_version, consent_locale,
			consent_granted_at, consent_evidence, revocation_token_hash, collected_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $7, $8, $9, $10, $11, $12, $13,
			-- The stars come from the feedback the author already gave. Deriving
			-- them here rather than in the caller means no submission path can
			-- silently publish a testimonial with no rating on it.
			COALESCE($14, (SELECT rating FROM feedbacks WHERE tenant_id = $1 AND id = $5)),
			'pending_review', 'granted', $15, $16, $17, $18, $19, $20, $21, $19
		)
		RETURNING id
	`, tenantID, publicID, values.ProjectID, string(values.Source), values.FeedbackID,
		values.RequestID, values.Quote, values.Headline, nullable(values.Author.Name),
		nullable(values.Author.Role), nullable(values.Author.Company),
		nullable(values.Author.CompanyURL), values.Locale, values.Rating, string(scopes),
		values.Consent.NoticeVersion, values.Consent.ControllerProfileVersion,
		values.Consent.Locale, now, string(evidence), revocationTokenHash).Scan(&id)
	if err != nil {
		return Testimonial{}, fmt.Errorf("create testimonial: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE testimonial_requests
		SET status = 'submitted', submitted_at = $3, testimonial_id = $4
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, values.RequestID, now, id); err != nil {
		return Testimonial{}, fmt.Errorf("close testimonial request: %w", err)
	}
	return s.GetTestimonial(ctx, query, tenantID, id)
}

// GetTestimonial reads one aggregate with its tags.
func (Store) GetTestimonial(
	ctx context.Context,
	query db.Querier,
	tenantID, id string,
) (Testimonial, error) {
	var item Testimonial
	var scopes []byte
	var author authorColumns
	err := query.QueryRow(ctx, `
		SELECT `+testimonialColumns+`
		FROM testimonials WHERE tenant_id = $1 AND id = $2
	`, tenantID, id).Scan(
		&item.ID, &item.PublicID, &item.ProjectID, &item.Source, &item.FeedbackID, &item.RequestID,
		&item.ConsentedText, &item.Quote, &item.Headline, &author.name, &author.role,
		&author.company, &author.companyURL, &item.Rating, &item.Status,
		&item.ConsentState, &scopes, &item.ReviewNote, &item.CollectedAt, &item.ApprovedAt,
		&item.RevokedAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Testimonial{}, ErrNotFound
	}
	if err != nil {
		return Testimonial{}, fmt.Errorf("load testimonial: %w", err)
	}
	item.Author = author.details()
	if len(scopes) > 0 {
		_ = json.Unmarshal(scopes, &item.Scopes)
	}
	tags, err := loadTags(ctx, query, tenantID, id)
	if err != nil {
		return Testimonial{}, err
	}
	item.TagIDs = tags
	return item, nil
}

// ListTestimonials returns the library, newest first.
func (Store) ListTestimonials(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	status string,
	limit int,
) ([]Testimonial, error) {
	rows, err := query.Query(ctx, `
		SELECT `+testimonialColumns+`
		FROM testimonials
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2::testimonial_status)
		ORDER BY created_at DESC
		LIMIT $3
	`, tenantID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list testimonials: %w", err)
	}
	defer rows.Close()

	items := []Testimonial{}
	for rows.Next() {
		var item Testimonial
		var scopes []byte
		var author authorColumns
		if err := rows.Scan(
			&item.ID, &item.PublicID, &item.ProjectID, &item.Source, &item.FeedbackID,
			&item.RequestID, &item.ConsentedText, &item.Quote, &item.Headline, &author.name,
			&author.role, &author.company, &author.companyURL, &item.Rating,
			&item.Status, &item.ConsentState, &scopes, &item.ReviewNote, &item.CollectedAt,
			&item.ApprovedAt, &item.RevokedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan testimonial: %w", err)
		}
		item.Author = author.details()
		if len(scopes) > 0 {
			_ = json.Unmarshal(scopes, &item.Scopes)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate testimonials: %w", err)
	}
	return items, nil
}

// ApproveTestimonial moves a reviewed testimonial to approved. It refuses when
// consent is not granted, so the editorial and consent axes cannot be crossed by
// a race between an approval and a revocation.
func (Store) ApproveTestimonial(
	ctx context.Context,
	query db.Querier,
	tenantID, id, memberID string,
	now time.Time,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE testimonials
		SET status = 'approved', approved_at = $4, approved_by = $3
		WHERE tenant_id = $1 AND id = $2
		  AND status IN ('draft', 'pending_review')
		  AND consent_state = 'granted'
	`, tenantID, id, memberID, now)
	if err != nil {
		return fmt.Errorf("approve testimonial: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotPublishable
	}
	return nil
}

// UpdateQuote applies a customer edit. If the new wording is not what the author
// consented to, consent moves to pending_reconsent and the testimonial stops
// being publishable until the author approves it.
func (Store) UpdateQuote(
	ctx context.Context,
	query db.Querier,
	tenantID, id, quote string,
) (ConsentState, error) {
	var consented string
	var state ConsentState
	err := query.QueryRow(ctx, `
		SELECT consented_text, consent_state FROM testimonials
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE
	`, tenantID, id).Scan(&consented, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("load testimonial for edit: %w", err)
	}
	next := state
	if state == ConsentGranted && !QuoteMatchesConsent(consented, quote) {
		next = ConsentPendingReconsent
	}
	if _, err := query.Exec(ctx, `
		UPDATE testimonials SET quote = $3, consent_state = $4
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, quote, string(next)); err != nil {
		return "", fmt.Errorf("update testimonial quote: %w", err)
	}
	return next, nil
}

// SetTags replaces the tag set, reusing the workspace tag catalogue that
// feedback and issues already share.
func (Store) SetTags(
	ctx context.Context,
	query db.Querier,
	tenantID, id string,
	tagIDs []string,
) error {
	if _, err := query.Exec(ctx, `
		DELETE FROM testimonial_tags WHERE tenant_id = $1 AND testimonial_id = $2
	`, tenantID, id); err != nil {
		return fmt.Errorf("clear testimonial tags: %w", err)
	}
	for _, tagID := range tagIDs {
		if _, err := query.Exec(ctx, `
			INSERT INTO testimonial_tags (tenant_id, testimonial_id, tag_id)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
		`, tenantID, id, tagID); err != nil {
			return fmt.Errorf("apply testimonial tag: %w", err)
		}
	}
	return nil
}

func loadTags(ctx context.Context, query db.Querier, tenantID, id string) ([]string, error) {
	rows, err := query.Query(ctx, `
		SELECT tag_id FROM testimonial_tags WHERE tenant_id = $1 AND testimonial_id = $2
	`, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("load testimonial tags: %w", err)
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tagID string
		if err := rows.Scan(&tagID); err != nil {
			return nil, fmt.Errorf("scan testimonial tag: %w", err)
		}
		tags = append(tags, tagID)
	}
	return tags, rows.Err()
}

// ResolveRevocationTenant finds the workspace behind a revoke link. Like the
// collection token, it runs without tenant context on the admin pool.
func (Store) ResolveRevocationTenant(
	ctx context.Context,
	query db.Querier,
	tokenHash string,
) (string, string, error) {
	var tenantID, id string
	err := query.QueryRow(ctx, `
		SELECT tenant_id, id FROM testimonials WHERE revocation_token_hash = $1
	`, tokenHash).Scan(&tenantID, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrTokenInvalid
	}
	if err != nil {
		return "", "", fmt.Errorf("resolve testimonial revocation tenant: %w", err)
	}
	return tenantID, id, nil
}

// Revoke withdraws consent and marks every affected snapshot stale in the same
// transaction.
//
// Marking stale is what makes revocation immediate: the public endpoint refuses
// to serve a stale snapshot, so the next read is already blocked before any
// rebuild runs. Rebuilding here instead would hold a connection for the length
// of a full wall projection under a 20-connection pool, which is a
// self-inflicted denial of service on the tenant's whole API.
func (Store) Revoke(
	ctx context.Context,
	query db.Querier,
	tenantID, id, reason string,
	now time.Time,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE testimonials
		SET consent_state = 'revoked', revoked_at = $3,
		    revocation_reason = NULLIF($4, '')
		WHERE tenant_id = $1 AND id = $2 AND consent_state <> 'revoked'
	`, tenantID, id, now, reason)
	if err != nil {
		return fmt.Errorf("revoke testimonial consent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := query.Exec(ctx, `
		UPDATE testimonial_wall_snapshots AS snapshot
		SET is_stale = true
		WHERE snapshot.tenant_id = $1 AND snapshot.is_current
		  AND EXISTS (
			SELECT 1 FROM testimonial_wall_items AS item
			WHERE item.tenant_id = snapshot.tenant_id
			  AND item.wall_id = snapshot.wall_id
			  AND item.testimonial_id = $2
		  )
	`, tenantID, id); err != nil {
		return fmt.Errorf("mark affected snapshots stale: %w", err)
	}
	return nil
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// authorColumns holds the nullable author fields during a scan.
//
// The columns are nullable because every author detail is optional and
// independently consented; AuthorDetails uses plain strings because an absent
// name and an empty name mean the same thing to a renderer.
type authorColumns struct {
	name       *string
	role       *string
	company    *string
	companyURL *string
}

func (c authorColumns) details() AuthorDetails {
	return AuthorDetails{
		Name:       derefString(c.name),
		Role:       derefString(c.role),
		Company:    derefString(c.company),
		CompanyURL: derefString(c.companyURL),
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// hashPayload derives the ETag for a published snapshot.
func hashPayload(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// PendingScoringWork returns feedback that needs a candidate row: never scored,
// or scored by an older version of the weighting.
//
// It joins the AI signals the engine reads, and the tenant's supported locales,
// so scoring stays a pure function over data rather than reaching for the
// database itself.
func (Store) PendingScoringWork(
	ctx context.Context,
	query db.Querier,
	scoreVersion int,
	limit int,
) ([]ScoringWork, error) {
	rows, err := query.Query(ctx, `
		SELECT feedback.tenant_id, feedback.project_id, feedback.id,
		       COALESCE(feedback.text, ''), feedback.rating, feedback.status::text,
		       feedback.text_process_status::text, feedback.text_sentiment_score,
		       feedback.text_moderation_flags,
		       (feedback.reporter_email IS NOT NULL AND feedback.notify_reporter),
		       COALESCE(feedback.language, ''),
		       COALESCE(project.default_locale, ''),
		       EXISTS (
		           SELECT 1 FROM testimonials AS existing
		           WHERE existing.tenant_id = feedback.tenant_id
		             AND existing.source_feedback_id = feedback.id
		             AND existing.status <> 'archived'
		       )
		FROM feedbacks AS feedback
		JOIN projects AS project
		  ON project.tenant_id = feedback.tenant_id AND project.id = feedback.project_id
		LEFT JOIN testimonial_candidates AS candidate
		  ON candidate.tenant_id = feedback.tenant_id AND candidate.feedback_id = feedback.id
		WHERE candidate.feedback_id IS NULL OR candidate.score_version < $1
		ORDER BY feedback.created_at DESC
		LIMIT $2
	`, scoreVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("load pending testimonial scoring work: %w", err)
	}
	defer rows.Close()

	work := []ScoringWork{}
	for rows.Next() {
		var item ScoringWork
		var sentiment *float64
		var defaultLocale string
		if err := rows.Scan(
			&item.TenantID, &item.ProjectID, &item.FeedbackID, &item.Text, &item.Rating,
			&item.FeedbackStatus, &item.ModerationStatus, &sentiment, &item.ModerationFlags,
			&item.AuthorReachable, &item.Language, &defaultLocale, &item.HasTestimonial,
		); err != nil {
			return nil, fmt.Errorf("scan testimonial scoring work: %w", err)
		}
		item.SentimentScore = sentiment
		if defaultLocale != "" {
			item.SupportedLocales = []string{defaultLocale}
		}
		work = append(work, item)
	}
	return work, rows.Err()
}

// RefreshDecayedScores reapplies age decay to eligible candidates.
//
// Only eligible, undismissed rows are touched: an ineligible candidate carries
// no score to decay, and a dismissed one will never be shown again, so both
// would be wasted writes on every sweep.
func (Store) RefreshDecayedScores(
	ctx context.Context,
	query db.Querier,
	now time.Time,
) (int64, error) {
	tag, err := query.Exec(ctx, `
		UPDATE testimonial_candidates AS candidate
		SET decayed_score = GREATEST(0, LEAST(1,
			candidate.base_score * exp(
				-GREATEST(0, EXTRACT(EPOCH FROM ($1::timestamptz - feedback.created_at)) / 86400.0) / 90.0
			)
		))
		FROM feedbacks AS feedback
		WHERE feedback.tenant_id = candidate.tenant_id
		  AND feedback.id = candidate.feedback_id
		  AND candidate.eligibility = 'eligible'
		  AND candidate.dismissed_at IS NULL
	`, now)
	if err != nil {
		return 0, fmt.Errorf("refresh testimonial decayed scores: %w", err)
	}
	return tag.RowsAffected(), nil
}
