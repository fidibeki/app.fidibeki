package replay

import (
	"errors"
	"time"
)

const RRWebVersion = "2.1.1"

var (
	ErrNotFound          = errors.New("replay session not found")
	ErrBatchComplete     = errors.New("replay batch is already complete")
	ErrSessionFinalized  = errors.New("replay session is already finalized")
	ErrIncompleteBatches = errors.New("replay session has incomplete batches")
	ErrEventLimit        = errors.New("replay session event limit exceeded")
)

type Session struct {
	ID                 string     `json:"id"`
	TenantID           string     `json:"tenant_id"`
	ProjectID          string     `json:"project_id"`
	FeedbackID         *string    `json:"feedback_id,omitempty"`
	ClientSessionID    string     `json:"session_id"`
	PageURL            *string    `json:"page_url,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	EventsCount        int        `json:"events_count"`
	BatchesCount       int        `json:"batches_count"`
	StorageKey         *string    `json:"-"`
	RRWebVersion       string     `json:"rrweb_version"`
	MergedAt           *time.Time `json:"merged_at,omitempty"`
	MergeRequestedAt   *time.Time `json:"-"`
	MergeEnqueuedAt    *time.Time `json:"-"`
	MergeAttempts      int        `json:"merge_attempts"`
	MergeError         *string    `json:"merge_error,omitempty"`
	MaxDurationSeconds int        `json:"-"`
	MaxEvents          int        `json:"-"`
	CreatedAt          time.Time  `json:"created_at"`
}

func (s Session) Status() string {
	if s.MergedAt != nil && s.StorageKey != nil {
		return "ready"
	}
	if s.MergeError != nil && s.MergeAttempts >= 12 {
		return "failed"
	}
	if s.EndedAt != nil {
		return "processing"
	}
	return "recording"
}

type Batch struct {
	ID                string
	TenantID          string
	ProjectID         string
	SessionID         string
	Index             int
	StorageKey        string
	EventsCount       int
	SizeBytes         int64
	ContentType       string
	CompleteTokenHash *string
	CompletedAt       *time.Time
}

type BatchUpload struct {
	BatchIndex    int               `json:"batch_index"`
	PresignedURL  string            `json:"presigned_url"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers"`
	CompleteToken string            `json:"complete_token"`
	ExpiresAt     time.Time         `json:"expires_at"`
}
