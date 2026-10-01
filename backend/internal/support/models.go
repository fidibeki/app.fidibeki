// Package support implements first-party conversations between platform staff
// and authenticated workspace members. It is not a public widget API.
package support

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("support conversation not found")
	ErrConflict = errors.New("message idempotency key reused with different content")
)

const MessageLimit = 4000
const PageSize = 50

type Conversation struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	MemberID           string    `json:"member_id"`
	LastSequence       int64     `json:"last_sequence"`
	MemberReadSequence int64     `json:"member_read_sequence"`
	AdminReadSequence  int64     `json:"admin_read_sequence"`
	UpdatedAt          time.Time `json:"updated_at"`
	MemberName         string    `json:"member_name"`
	MemberEmail        string    `json:"member_email,omitempty"`
	WorkspaceName      string    `json:"workspace_name,omitempty"`
	Unread             int       `json:"unread"`
	Preview            string    `json:"preview"`
}

type Message struct {
	ID         string    `json:"id"`
	Sequence   int64     `json:"sequence"`
	ClientID   string    `json:"client_id"`
	SenderType string    `json:"sender_type"`
	SenderName string    `json:"sender_name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type Thread struct {
	Conversation *Conversation `json:"conversation"`
	Messages     []Message     `json:"messages"`
	HasMore      bool          `json:"has_more"`
}

type Actor struct {
	TenantID string
	MemberID string
	ID       string
	Name     string
	Admin    bool
}

func (a Actor) kind() string {
	if a.Admin {
		return "admin"
	}
	return "member"
}
