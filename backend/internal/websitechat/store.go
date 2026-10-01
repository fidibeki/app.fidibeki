package websitechat

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/jackc/pgx/v5"
)

const visitorColumns = `id,project_id,last_sequence,visitor_read_sequence,operator_read_sequence,updated_at,expires_at`

func scanVisitor(row pgx.Row) (Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.ProjectID, &c.LastSequence, &c.VisitorReadSequence, &c.OperatorReadSequence, &c.UpdatedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, missing()
	}
	if len(c.ID) >= 8 {
		c.Name = "Visitor " + c.ID[:8]
	}
	return c, err
}
func (h *Handler) thread(r *http.Request, s *scope) (any, error) {
	before := int64(0)
	if value := r.URL.Query().Get("before"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 {
			return nil, invalid()
		}
		before = n
	}
	c, err := scanVisitor(s.tx.QueryRow(r.Context(), `SELECT `+visitorColumns+` FROM website_chat_visitors WHERE id=$1`, s.visitorID))
	if err != nil {
		return nil, err
	}
	thread := Thread{Messages: []Message{}}
	if c.LastSequence == 0 {
		return thread, nil
	}
	thread.Conversation = &c
	if err = h.audit(r, s, "accessed", auth.PermissionChatRead); err != nil {
		return nil, err
	}
	read, other := c.VisitorReadSequence, "operator"
	if s.operator {
		read, other = c.OperatorReadSequence, "visitor"
	}
	if err = s.tx.QueryRow(r.Context(), `SELECT count(*) FROM website_chat_messages WHERE visitor_id=$1 AND sequence>$2 AND sender_type=$3`, s.visitorID, read, other).Scan(&c.Unread); err != nil {
		return nil, err
	}
	rows, err := s.tx.Query(r.Context(), `SELECT id,sequence,client_id,sender_type,sender_name,body,created_at FROM website_chat_messages WHERE visitor_id=$1 AND ($2::bigint=0 OR sequence<$2) ORDER BY sequence DESC LIMIT $3`, s.visitorID, before, pageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Message
		if err = rows.Scan(&m.ID, &m.Sequence, &m.ClientID, &m.SenderType, &m.SenderName, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		thread.Messages = append(thread.Messages, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(thread.Messages) > pageSize {
		thread.HasMore = true
		thread.Messages = thread.Messages[:pageSize]
	}
	slices.Reverse(thread.Messages)
	return thread, nil
}
func (h *Handler) send(r *http.Request, s *scope) (any, error) {
	clientID, body, err := messageInput(r)
	if err != nil {
		return nil, err
	}
	if !s.enabled {
		return nil, denied()
	}
	c, err := scanVisitor(s.tx.QueryRow(r.Context(), `SELECT `+visitorColumns+` FROM website_chat_visitors WHERE id=$1 FOR UPDATE`, s.visitorID))
	if err != nil {
		return nil, err
	}
	if !time.Now().Before(c.ExpiresAt) {
		return nil, failure{409, api.CodeConflict, "visitor session has ended"}
	}
	if !s.operator && c.LastSequence == 0 {
		return nil, denied()
	}
	kind := "visitor"
	if s.operator {
		kind = "operator"
	}
	var m Message
	err = s.tx.QueryRow(r.Context(), `SELECT id,sequence,client_id,sender_type,sender_name,body,created_at FROM website_chat_messages WHERE visitor_id=$1 AND sender_type=$2 AND sender_id=$3 AND client_id=$4`, s.visitorID, kind, s.actorID, clientID).Scan(&m.ID, &m.Sequence, &m.ClientID, &m.SenderType, &m.SenderName, &m.Body, &m.CreatedAt)
	if err == nil {
		if m.Body != body {
			return nil, failure{409, api.CodeConflict, "message retry content changed"}
		}
		s.notify = true
		return map[string]any{"message": m}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	err = s.tx.QueryRow(r.Context(), `INSERT INTO website_chat_messages(tenant_id,project_id,visitor_id,sequence,client_id,sender_type,sender_id,sender_name,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id,sequence,client_id,sender_type,sender_name,body,created_at`, s.tenantID, s.projectID, s.visitorID, c.LastSequence+1, clientID, kind, s.actorID, s.name, body).Scan(&m.ID, &m.Sequence, &m.ClientID, &m.SenderType, &m.SenderName, &m.Body, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err = s.tx.Exec(r.Context(), `UPDATE website_chat_visitors SET last_sequence=$2,updated_at=clock_timestamp() WHERE id=$1`, s.visitorID, m.Sequence); err != nil {
		return nil, err
	}
	if err = h.audit(r, s, "message_sent", auth.PermissionChatWrite); err != nil {
		return nil, err
	}
	s.notify = true
	return map[string]any{"message": m}, nil
}

func (h *Handler) inbox(r *http.Request, s *scope) (any, error) {
	before := r.URL.Query().Get("before")
	if before != "" && !validID(before) {
		return nil, invalid()
	}
	var cursor any
	if before != "" {
		cursor = before
	}
	rows, err := s.tx.Query(r.Context(), `SELECT `+visitorColumns+`,
 (SELECT count(*) FROM website_chat_messages m WHERE m.visitor_id=c.id AND m.sender_type='visitor' AND m.sequence>c.operator_read_sequence),
 COALESCE((SELECT left(body,160) FROM website_chat_messages WHERE visitor_id=c.id ORDER BY sequence DESC LIMIT 1),'')
 FROM website_chat_visitors c WHERE last_sequence>0 AND ($1::uuid IS NULL OR (updated_at,id)<(SELECT updated_at,id FROM website_chat_visitors WHERE id=$1)) ORDER BY updated_at DESC,id DESC LIMIT $2`, cursor, pageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err = rows.Scan(&c.ID, &c.ProjectID, &c.LastSequence, &c.VisitorReadSequence, &c.OperatorReadSequence, &c.UpdatedAt, &c.ExpiresAt, &c.Unread, &c.Preview); err != nil {
			return nil, err
		}
		c.Name = "Visitor " + c.ID[:8]
		result = append(result, c)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	next := ""
	if len(result) > pageSize {
		result = result[:pageSize]
		next = result[len(result)-1].ID
	}
	return map[string]any{"conversations": result, "next_cursor": next}, nil
}
func (h *Handler) read(r *http.Request, s *scope) (any, error) {
	var input struct {
		Sequence int64 `json:"sequence"`
	}
	if decode(r, &input) != nil || input.Sequence < 0 {
		return nil, invalid()
	}
	c, err := scanVisitor(s.tx.QueryRow(r.Context(), `SELECT `+visitorColumns+` FROM website_chat_visitors WHERE id=$1`, s.visitorID))
	if err != nil {
		return nil, err
	}
	if input.Sequence > c.LastSequence {
		return nil, invalid()
	}
	column, previous := "visitor_read_sequence", c.VisitorReadSequence
	if s.operator {
		column, previous = "operator_read_sequence", c.OperatorReadSequence
	}
	if input.Sequence <= previous {
		return nil, nil
	}
	_, err = s.tx.Exec(r.Context(), `UPDATE website_chat_visitors SET `+column+`=GREATEST(`+column+`,LEAST(last_sequence,$2)) WHERE id=$1`, s.visitorID, input.Sequence)
	s.notify = err == nil
	return nil, err
}

func (h *Handler) revoke(r *http.Request, s *scope) (any, error) {
	_, err := s.tx.Exec(r.Context(), `UPDATE website_chat_visitors SET expires_at=LEAST(expires_at,now()) WHERE id=$1`, s.visitorID)
	s.notify = err == nil
	return nil, err
}
func (h *Handler) remove(r *http.Request, s *scope) (any, error) {
	var held bool
	if err := s.tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM privacy_case_legal_holds WHERE tenant_id=$1 AND active)`, s.tenantID).Scan(&held); err != nil {
		return nil, err
	}
	if held {
		return nil, failure{409, api.CodeConflict, "an active privacy hold prevents deletion"}
	}
	result, err := s.tx.Exec(r.Context(), `DELETE FROM website_chat_visitors WHERE id=$1`, s.visitorID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, missing()
	}
	if err = h.audit(r, s, "deleted", auth.PermissionChatDelete); err != nil {
		return nil, err
	}
	s.notify = true
	return nil, nil
}
