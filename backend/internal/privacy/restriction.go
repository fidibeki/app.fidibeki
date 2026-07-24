package privacy

import (
	"context"
	"fmt"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

func EnsureFeedbackProcessingAllowed(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackID string,
	purpose ProcessingPurpose,
) error {
	return ensureRestrictionResult(ctx, query, `
		SELECT fidibeki_privacy_feedback_restricted($1, $2, $3)
	`, tenantID, feedbackID, purpose)
}

func EnsureIssueProcessingAllowed(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	purpose ProcessingPurpose,
) error {
	return ensureRestrictionResult(ctx, query, `
		SELECT fidibeki_privacy_issue_restricted($1, $2, $3)
	`, tenantID, issueID, purpose)
}

func EnsureSessionProcessingAllowed(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	sessionID string,
	purpose ProcessingPurpose,
) error {
	return ensureRestrictionResult(ctx, query, `
		SELECT fidibeki_privacy_session_restricted($1, $2, $3)
	`, tenantID, sessionID, purpose)
}

func EnsureEmailProcessingAllowed(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	reporterEmail string,
	purpose ProcessingPurpose,
) error {
	if strings.TrimSpace(reporterEmail) == "" {
		return ErrProcessingRestricted
	}
	return ensureRestrictionResult(ctx, query, `
		SELECT fidibeki_privacy_email_restricted($1, $2, $3)
	`, tenantID, reporterEmail, purpose)
}

func EnsureResourceProcessingAllowed(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	resourceType string,
	resourceID string,
	purpose ProcessingPurpose,
) error {
	switch resourceType {
	case "feedback":
		return EnsureFeedbackProcessingAllowed(ctx, query, tenantID, resourceID, purpose)
	case "issue":
		return EnsureIssueProcessingAllowed(ctx, query, tenantID, resourceID, purpose)
	default:
		return ErrProcessingRestricted
	}
}

func ensureRestrictionResult(
	ctx context.Context,
	query db.Querier,
	statement string,
	arguments ...any,
) error {
	if query == nil {
		return ErrProcessingRestricted
	}
	var restricted bool
	if err := query.QueryRow(ctx, statement, arguments...).Scan(&restricted); err != nil {
		return fmt.Errorf("check privacy processing restriction: %w", err)
	}
	if restricted {
		return ErrProcessingRestricted
	}
	return nil
}
