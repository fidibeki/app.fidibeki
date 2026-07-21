package ai

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	QueueName             = "ai-pool"
	TaskTextProcess       = "ai.text_process"
	TaskAttachmentProcess = "ai.attachment_process"
	TaskGeminiFileDelete  = "ai.gemini_file_delete"
)

var (
	ErrNotFound       = errors.New("AI resource not found")
	ErrNoWork         = errors.New("AI work is already terminal")
	ErrAlreadyRunning = errors.New("AI work is already processing")
	ErrUnavailable    = errors.New("gemini is not configured")
)

type TaskKind string

const (
	TaskKindText       TaskKind = "text"
	TaskKindAttachment TaskKind = "attachment"
	TaskKindFileDelete TaskKind = "file_delete"
)

type PreparedTask struct {
	Kind       TaskKind
	TenantID   string
	FeedbackID string
	ResourceID string
	Attempt    int
	Generation int64
	EnqueuedAt time.Time
	UsageDate  time.Time
}

type TextWork struct {
	TenantID   string
	FeedbackID string
	Text       string
	Generation int64
}

type AttachmentWork struct {
	TenantID     string
	FeedbackID   string
	AttachmentID string
	Kind         string
	StorageKey   string
	ContentType  string
	Attempt      int
}

type GeminiFileWork struct {
	ID       string
	TenantID string
	FileName string
	Attempt  int
}

type Result struct {
	Transcription   string   `json:"transcription"`
	CleanedText     string   `json:"cleaned_text"`
	SentimentScore  float64  `json:"sentiment_score"`
	ModerationFlags []string `json:"moderation_flags"`
}

func (r *Result) Normalize() error {
	r.Transcription = strings.TrimSpace(r.Transcription)
	r.CleanedText = strings.TrimSpace(r.CleanedText)
	if r.SentimentScore < -1 || r.SentimentScore > 1 {
		return fmt.Errorf("sentiment score must be between -1 and 1")
	}
	seen := make(map[string]struct{}, len(r.ModerationFlags))
	flags := make([]string, 0, len(r.ModerationFlags))
	for _, value := range r.ModerationFlags {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		flags = append(flags, value)
	}
	r.ModerationFlags = flags
	return nil
}

type PermanentError struct {
	Err error
}

func (e PermanentError) Error() string { return e.Err.Error() }
func (e PermanentError) Unwrap() error { return e.Err }

func permanent(err error) error {
	if err == nil {
		return nil
	}
	return PermanentError{Err: err}
}

func IsPermanent(err error) bool {
	var target PermanentError
	return errors.As(err, &target)
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	return message
}
