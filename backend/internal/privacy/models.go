package privacy

import (
	"errors"
	"time"
)

type JobKind string

const (
	JobWorkspaceExport   JobKind = "workspace_export"
	JobReporterExport    JobKind = "reporter_export"
	JobReporterAnonymize JobKind = "reporter_anonymize"
)

type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobProcessing JobStatus = "processing"
	JobCompleted  JobStatus = "completed"
	JobFailed     JobStatus = "failed"
	JobExpired    JobStatus = "expired"
)

var (
	ErrNotFound       = errors.New("privacy job not found")
	ErrHumanRequired  = errors.New("active human member required")
	ErrJobUnavailable = errors.New("privacy job is unavailable")
)

type Job struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"-"`
	Kind            JobKind    `json:"kind"`
	Status          JobStatus  `json:"status"`
	ActorMemberID   *string    `json:"actor_member_id,omitempty"`
	Generation      int        `json:"-"`
	Attempts        int        `json:"attempts"`
	EnqueuedAt      *time.Time `json:"-"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	HeartbeatAt     *time.Time `json:"-"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	ObjectKey       *string    `json:"-"`
	ObjectSizeBytes *int64     `json:"object_size_bytes,omitempty"`
	ErrorMessage    *string    `json:"error_message,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type PreparedJob struct {
	ID         string
	TenantID   string
	Kind       JobKind
	Generation int
}
