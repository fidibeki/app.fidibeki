package feedback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/project"
)

type PlanLimitError struct {
	Limit int
	Usage int
}

func (err *PlanLimitError) Error() string {
	return "monthly feedback limit reached"
}

type SubmissionOwnership struct {
	Source             string
	AllowExisting      bool
	ExistingFeedbackID *string
}

type CoreSubmissionRequest struct {
	Configuration project.PublicConfiguration
	Values        SubmissionValues
	Ownership     SubmissionOwnership
	BeforeCreate  func(context.Context, db.Querier) error
}

type CoreSubmissionResult struct {
	Submission Submission
	Created    bool
	RawTokens  map[string]string
}

type SubmissionCore struct {
	Store            Store
	Analytics        SubmissionAnalyticsStore
	Now              func() time.Time
	GeneratePublicID func() (string, error)
}

// Submit runs after transport authorization and inside the caller's tenant
// transaction. The lock order is shared across transports:
//
//  1. a transport-specific credential row, when one exists (mobile);
//  2. the project/client_feedback_id advisory lock;
//  3. Feedback and attachment rows.
//
// Web has no credential row, so it starts at step 2 and cannot form a lock
// cycle with mobile.
func (core SubmissionCore) Submit(
	ctx context.Context,
	query db.Querier,
	request CoreSubmissionRequest,
) (CoreSubmissionResult, error) {
	values := request.Values
	if values.Source == "" {
		values.Source = "web"
	}
	if request.Ownership.Source == "" {
		request.Ownership.Source = values.Source
	}
	if values.Source != request.Ownership.Source {
		return CoreSubmissionResult{}, ErrSubmissionCollision
	}
	if err := core.Store.LockSubmission(
		ctx, query, request.Configuration.ProjectID, values.ClientFeedbackID,
	); err != nil {
		return CoreSubmissionResult{}, err
	}
	submission, exists, err := core.Store.FindSubmission(
		ctx, query, request.Configuration.ProjectID, values.ClientFeedbackID,
	)
	if err != nil {
		return CoreSubmissionResult{}, err
	}
	rawTokens := map[string]string{}
	if exists {
		if !request.Ownership.AllowExisting ||
			submission.Source != request.Ownership.Source ||
			(request.Ownership.ExistingFeedbackID != nil &&
				submission.ID != *request.Ownership.ExistingFeedbackID) {
			return CoreSubmissionResult{}, ErrSubmissionCollision
		}
		if err := core.rotateIncompleteTokens(ctx, query, &submission, rawTokens); err != nil {
			return CoreSubmissionResult{}, err
		}
		return CoreSubmissionResult{
			Submission: submission, Created: false, RawTokens: rawTokens,
		}, nil
	}
	if request.Ownership.ExistingFeedbackID != nil {
		return CoreSubmissionResult{}, ErrSubmissionCollision
	}
	if request.BeforeCreate != nil {
		if err := request.BeforeCreate(ctx, query); err != nil {
			return CoreSubmissionResult{}, err
		}
	}
	definition, planExists := config.PlanFor(request.Configuration.Plan)
	if !planExists {
		return CoreSubmissionResult{}, errors.New("unsupported tenant plan")
	}
	now := core.now().UTC()
	usage, err := core.Store.CountMonthly(ctx, query, request.Configuration.TenantID, now)
	if err != nil {
		return CoreSubmissionResult{}, err
	}
	if definition.Limits.FeedbackPerMonth >= 0 && usage >= definition.Limits.FeedbackPerMonth {
		return CoreSubmissionResult{}, &PlanLimitError{
			Limit: definition.Limits.FeedbackPerMonth, Usage: usage,
		}
	}
	feedbackID := NewID()
	prepared, err := prepareAttachments(
		request.Configuration, feedbackID, values.WidgetRuleID, values.Attachments,
		definition.Features.AI, rawTokens, now,
	)
	if err != nil {
		return CoreSubmissionResult{}, err
	}
	publicIDs, err := core.publicIDCandidates()
	if err != nil {
		return CoreSubmissionResult{}, err
	}
	submission, err = core.Store.CreateSubmission(
		ctx, query, request.Configuration, values, feedbackID, publicIDs, prepared,
	)
	if err != nil {
		return CoreSubmissionResult{}, err
	}
	if values.Analytics != nil {
		if core.Analytics == nil {
			return CoreSubmissionResult{}, errors.New("feedback analytics store is not configured")
		}
		analytics := *values.Analytics
		analytics.FeedbackID = &feedbackID
		if _, err := core.Analytics.InsertSubmittedEvent(
			ctx, query, request.Configuration, analytics, now,
		); err != nil {
			return CoreSubmissionResult{}, fmt.Errorf("create submitted widget funnel event: %w", err)
		}
	}
	return CoreSubmissionResult{
		Submission: submission, Created: true, RawTokens: rawTokens,
	}, nil
}

func (core SubmissionCore) rotateIncompleteTokens(
	ctx context.Context,
	query db.Querier,
	submission *Submission,
	rawTokens map[string]string,
) error {
	for index := range submission.Attachments {
		attachment := &submission.Attachments[index]
		if attachment.CompletedAt != nil {
			continue
		}
		raw, tokenHash, err := auth.NewOpaqueToken()
		if err != nil {
			return err
		}
		expiresAt, generation, err := core.Store.RotateCompletionToken(
			ctx, query, attachment.ID, tokenHash, core.now().UTC(),
		)
		if err != nil {
			return err
		}
		rawTokens[attachment.ID] = raw
		attachment.CompleteTokenHash = &tokenHash
		attachment.UploadExpiresAt = expiresAt
		attachment.SafetyStatus = AttachmentSafetyPending
		attachment.VerificationGeneration = generation
	}
	return nil
}

func (core SubmissionCore) publicIDCandidates() ([]string, error) {
	generator := core.GeneratePublicID
	if generator == nil {
		generator = GeneratePublicID
	}
	values := make([]string, 3)
	for index := range values {
		value, err := generator()
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func (core SubmissionCore) now() time.Time {
	if core.Now != nil {
		return core.Now()
	}
	return time.Now()
}

func LatestAttachmentExpiry(submission Submission) time.Time {
	var latest time.Time
	for _, attachment := range submission.Attachments {
		if attachment.UploadExpiresAt.After(latest) {
			latest = attachment.UploadExpiresAt
		}
	}
	return latest
}
