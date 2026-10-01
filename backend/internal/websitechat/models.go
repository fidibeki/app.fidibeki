// Package websitechat connects tenant operators with visitors to their own websites.
package websitechat

import "time"

const pageSize = 50
const tokenHeader = "X-Fidibeki-Chat-Token"

type Conversation struct {
	ID                   string    `json:"id"`
	ProjectID            string    `json:"project_id"`
	Name                 string    `json:"name"`
	LastSequence         int64     `json:"last_sequence"`
	VisitorReadSequence  int64     `json:"visitor_read_sequence"`
	OperatorReadSequence int64     `json:"operator_read_sequence"`
	UpdatedAt            time.Time `json:"updated_at"`
	ExpiresAt            time.Time `json:"expires_at"`
	Unread               int       `json:"unread"`
	Preview              string    `json:"preview"`
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
