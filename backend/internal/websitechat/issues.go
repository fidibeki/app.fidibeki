package websitechat

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/issue"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// Includes JSON-escaped supplementary Unicode characters at the 10,000-rune limit.
const issueLinkBodyLimit int64 = 128 * 1024

// Bridge endpoints are operator-only and require permissions from the verified
// credential's current membership. Widget threads never include these data.
func (s *scope) canLinkIssues(write bool) bool {
	permissions := []auth.Permission{auth.PermissionChatRead, auth.PermissionProjectRead, auth.PermissionIssueRead}
	if write {
		permissions = append(permissions, auth.PermissionChatWrite, auth.PermissionIssueUpdate)
	}
	for _, permission := range permissions {
		if !s.operator || !auth.HasPermission(s.permissions, permission) {
			return false
		}
	}
	return true
}

type linkedIssue struct {
	MessageID    string       `json:"message_id"`
	ID           string       `json:"id"`
	PublicID     string       `json:"public_id"`
	Title        string       `json:"title"`
	Status       issue.Status `json:"status"`
	MergedIntoID *string      `json:"merged_into_id,omitempty"`
}

func (h *Handler) linkedIssues(r *http.Request, s *scope) (any, error) {
	if !s.canLinkIssues(false) {
		return nil, denied()
	}
	if _, err := scanVisitor(s.tx.QueryRow(r.Context(), `SELECT `+visitorColumns+` FROM website_chat_visitors WHERE id=$1`, s.visitorID)); err != nil {
		return nil, err
	}
	rows, err := s.tx.Query(r.Context(), `SELECT l.message_id,i.id,i.public_id,i.title,i.status,i.merged_into_id
  FROM website_chat_issue_links l JOIN issues i ON i.id=l.issue_id AND i.tenant_id=l.tenant_id
  WHERE l.visitor_id=$1 ORDER BY l.created_at,l.message_id LIMIT 501`, s.visitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := []linkedIssue{}
	for rows.Next() {
		var link linkedIssue
		if err = rows.Scan(&link.MessageID, &link.ID, &link.PublicID, &link.Title, &link.Status, &link.MergedIntoID); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	truncated := len(links) > 500
	if truncated {
		links = links[:500]
	}
	return map[string]any{"issues": links, "truncated": truncated}, h.audit(r, s, "accessed", auth.PermissionChatRead)
}

func (h *Handler) linkIssue(r *http.Request, s *scope) (any, error) {
	if !s.canLinkIssues(true) {
		return nil, denied()
	}
	var input struct {
		MessageID   string `json:"message_id"`
		IssueID     string `json:"issue_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decodeBounded(r, &input, issueLinkBodyLimit); err != nil {
		return nil, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if !validID(input.MessageID) || strings.ContainsRune(input.Title+input.Description, 0) {
		return nil, invalid()
	}
	if input.IssueID != "" {
		if !validID(input.IssueID) || input.Title != "" || input.Description != "" {
			return nil, invalid()
		}
	} else if utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 80 || utf8.RuneCountInString(input.Description) > 10000 {
		return nil, invalid()
	}
	// Serialize source operations with deletion and duplicate/retried requests.
	if _, err := scanVisitor(s.tx.QueryRow(r.Context(), `SELECT `+visitorColumns+` FROM website_chat_visitors WHERE id=$1 FOR UPDATE`, s.visitorID)); err != nil {
		return nil, err
	}
	var exists bool
	if err := s.tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM website_chat_messages WHERE id=$1 AND visitor_id=$2 AND sender_type='visitor')`, input.MessageID, s.visitorID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, missing()
	}
	payload, _ := json.Marshal(input)
	hash := sha256.Sum256(payload)
	var issueID string
	var previousHash []byte
	err := s.tx.QueryRow(r.Context(), `SELECT issue_id,request_hash FROM website_chat_issue_links WHERE message_id=$1`, input.MessageID).Scan(&issueID, &previousHash)
	if err == nil {
		if !bytes.Equal(previousHash, hash[:]) {
			return nil, failure{409, api.CodeConflict, "message is already linked to an issue"}
		}
		return map[string]string{"issue_id": issueID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if input.IssueID != "" {
		var merged *string
		err = s.tx.QueryRow(r.Context(), `SELECT id,merged_into_id FROM issues WHERE tenant_id=$1 AND project_id=$2 AND id=$3 FOR SHARE`, s.tenantID, s.projectID, input.IssueID).Scan(&issueID, &merged)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, missing()
		}
		if err != nil {
			return nil, err
		}
		if merged != nil {
			return nil, failure{409, api.CodeConflict, "choose the issue this issue was merged into"}
		}
	} else {
		created, err := (issue.Store{}).CreateFromConversation(r.Context(), s.tx, s.tenantID, s.projectID, s.tenantSlug, s.actorID, input.Title, input.Description, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		issueID = created.ID
	}
	if _, err = s.tx.Exec(r.Context(), `INSERT INTO website_chat_issue_links(tenant_id,project_id,visitor_id,message_id,issue_id,request_hash) VALUES($1,$2,$3,$4,$5,$6)`, s.tenantID, s.projectID, s.visitorID, input.MessageID, issueID, hash[:]); err != nil {
		return nil, err
	}
	s.notify = true
	return map[string]string{"issue_id": issueID}, h.audit(r, s, "issue_linked", auth.PermissionIssueUpdate)
}

func (h *Handler) unlinkIssue(r *http.Request, s *scope) (any, error) {
	if !s.canLinkIssues(true) {
		return nil, denied()
	}
	messageID := chi.URLParam(r, "messageID")
	if !validID(messageID) {
		return nil, invalid()
	}
	if _, err := scanVisitor(s.tx.QueryRow(r.Context(), `SELECT `+visitorColumns+` FROM website_chat_visitors WHERE id=$1 FOR UPDATE`, s.visitorID)); err != nil {
		return nil, err
	}
	result, err := s.tx.Exec(r.Context(), `DELETE FROM website_chat_issue_links WHERE visitor_id=$1 AND message_id=$2`, s.visitorID, messageID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, nil
	}
	s.notify = true
	return nil, h.audit(r, s, "issue_unlinked", auth.PermissionIssueUpdate)
}

func (h *Handler) issueConversations(r *http.Request, s *scope) (any, error) {
	if !s.canLinkIssues(false) {
		return nil, denied()
	}
	issueID := chi.URLParam(r, "issueID")
	if !validID(issueID) {
		return nil, invalid()
	}
	var exists bool
	if err := s.tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM issues WHERE tenant_id=$1 AND project_id=$2 AND id=$3)`, s.tenantID, s.projectID, issueID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, missing()
	}
	// Include sources from merged issues without changing or copying chat data.
	rows, err := s.tx.Query(r.Context(), `WITH RECURSIVE family AS (
  SELECT id FROM issues WHERE tenant_id=$1 AND project_id=$2 AND id=$3
  UNION SELECT i.id FROM issues i JOIN family f ON i.merged_into_id=f.id WHERE i.tenant_id=$1 AND i.project_id=$2
 ) SELECT DISTINCT v.id,v.project_id,v.last_sequence,v.visitor_read_sequence,v.operator_read_sequence,v.updated_at,v.expires_at,v.locale
 FROM website_chat_issue_links l JOIN family f ON f.id=l.issue_id JOIN website_chat_visitors v ON v.id=l.visitor_id
 ORDER BY v.updated_at DESC,v.id LIMIT 501`, s.tenantID, s.projectID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	conversations := []Conversation{}
	for rows.Next() {
		c, err := scanVisitor(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, c)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	truncated := len(conversations) > 500
	if truncated {
		conversations = conversations[:500]
	}
	return map[string]any{"conversations": conversations, "truncated": truncated}, h.audit(r, s, "accessed", auth.PermissionChatRead)
}
