package feedback

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
)

type AttachmentCompletionFailure string

const (
	AttachmentCompletionNotFound AttachmentCompletionFailure = "not_found"
	AttachmentCompletionComplete AttachmentCompletionFailure = "already_complete"
	AttachmentCompletionExpired  AttachmentCompletionFailure = "expired"
	AttachmentCompletionDenied   AttachmentCompletionFailure = "denied"
	AttachmentCompletionMissing  AttachmentCompletionFailure = "upload_missing"
	AttachmentCompletionMismatch AttachmentCompletionFailure = "upload_mismatch"
)

type AttachmentCompletionError struct {
	Failure AttachmentCompletionFailure
	Fields  []FieldError
}

func (err *AttachmentCompletionError) Error() string {
	return "attachment completion " + string(err.Failure)
}

type AttachmentCompletionService struct {
	Store   Store
	Storage ObjectStorage
	Now     func() time.Time
}

func (service AttachmentCompletionService) Complete(
	ctx context.Context,
	query db.Querier,
	projectID string,
	attachmentID string,
	expectedFeedbackID *string,
	completeToken string,
) (Attachment, error) {
	attachment, err := service.Store.LockAttachment(ctx, query, projectID, attachmentID)
	if errors.Is(err, ErrNotFound) {
		return Attachment{}, &AttachmentCompletionError{Failure: AttachmentCompletionNotFound}
	}
	if err != nil {
		return Attachment{}, err
	}
	if expectedFeedbackID != nil && attachment.FeedbackID != *expectedFeedbackID {
		return Attachment{}, &AttachmentCompletionError{Failure: AttachmentCompletionNotFound}
	}
	if attachment.CompletedAt != nil || attachment.CompleteTokenHash == nil {
		return attachment, &AttachmentCompletionError{Failure: AttachmentCompletionComplete}
	}
	now := service.now().UTC()
	if !now.Before(attachment.UploadExpiresAt) {
		if err := service.Store.ExpireAttachment(ctx, query, attachment.ID, now); err != nil {
			return Attachment{}, err
		}
		return attachment, &AttachmentCompletionError{Failure: AttachmentCompletionExpired}
	}
	wantHash := auth.HashToken(completeToken)
	if subtle.ConstantTimeCompare([]byte(wantHash), []byte(*attachment.CompleteTokenHash)) != 1 {
		return attachment, &AttachmentCompletionError{Failure: AttachmentCompletionDenied}
	}
	object, err := service.Storage.Stat(ctx, attachment.StorageKey)
	if err != nil {
		return attachment, &AttachmentCompletionError{Failure: AttachmentCompletionMissing}
	}
	fields := make([]FieldError, 0, 2)
	if object.Size != attachment.SizeBytes {
		fields = append(fields, field("size_bytes", "uploaded object size does not match the declaration"))
	}
	if normalizeContentType(object.ContentType) != normalizeContentType(attachment.ContentType) {
		fields = append(fields, field(
			"content_type", "uploaded object MIME type does not match the declaration",
		))
	}
	if len(fields) > 0 {
		return attachment, &AttachmentCompletionError{
			Failure: AttachmentCompletionMismatch, Fields: fields,
		}
	}
	if err := service.Store.CompleteAttachment(ctx, query, attachment.ID, now); err != nil {
		return Attachment{}, err
	}
	return attachment, nil
}

func (service AttachmentCompletionService) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}
