package support

import (
	"context"
	"errors"
	"slices"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type Store struct{}

const conversationColumns = `c.id, c.tenant_id, c.member_id, c.last_sequence,
    c.member_read_sequence, c.admin_read_sequence, c.updated_at,
    COALESCE(NULLIF(m.name, ''), split_part(m.email::text, '@', 1)), m.email::text, t.name`

func scanConversation(row pgx.Row) (Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.TenantID, &c.MemberID, &c.LastSequence,
		&c.MemberReadSequence, &c.AdminReadSequence, &c.UpdatedAt,
		&c.MemberName, &c.MemberEmail, &c.WorkspaceName)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (Store) Find(ctx context.Context, q db.Querier, tenantID, memberID string) (Conversation, error) {
	return scanConversation(q.QueryRow(ctx, `SELECT `+conversationColumns+`
        FROM support_conversations c JOIN tenant_members m ON m.tenant_id=c.tenant_id AND m.id=c.member_id
        JOIN tenants t ON t.id=c.tenant_id WHERE c.tenant_id=$1 AND c.member_id=$2`, tenantID, memberID))
}

func (Store) Get(ctx context.Context, q db.Querier, id string) (Conversation, error) {
	return scanConversation(q.QueryRow(ctx, `SELECT `+conversationColumns+`
        FROM support_conversations c JOIN tenant_members m ON m.tenant_id=c.tenant_id AND m.id=c.member_id
        JOIN tenants t ON t.id=c.tenant_id WHERE c.id=$1`, id))
}

func (s Store) Thread(ctx context.Context, q db.Querier, c Conversation, admin bool, before int64) (Thread, error) {
	thread := Thread{Conversation: &c, Messages: []Message{}}
	read := c.MemberReadSequence
	other := "admin"
	if admin {
		read = c.AdminReadSequence
		other = "member"
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM support_messages WHERE conversation_id=$1
        AND sequence>$2 AND sender_type=$3`, c.ID, read, other).Scan(&c.Unread); err != nil {
		return thread, err
	}
	rows, err := q.Query(ctx, `SELECT id, sequence, client_id, sender_type, sender_name, body, created_at
        FROM support_messages WHERE conversation_id=$1 AND ($2::bigint=0 OR sequence<$2)
        ORDER BY sequence DESC LIMIT $3`, c.ID, before, PageSize+1)
	if err != nil {
		return thread, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Sequence, &m.ClientID, &m.SenderType, &m.SenderName, &m.Body, &m.CreatedAt); err != nil {
			return thread, err
		}
		thread.Messages = append(thread.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return thread, err
	}
	if len(thread.Messages) > PageSize {
		thread.HasMore = true
		thread.Messages = thread.Messages[:PageSize]
	}
	slices.Reverse(thread.Messages)
	return thread, nil
}

// The conversation row lock serializes sequence assignment and commit order.
// A retried send returns the original row without moving read positions.
func (s Store) Send(ctx context.Context, q db.Querier, actor Actor, clientID, body string) (Conversation, Message, bool, error) {
	var c Conversation
	var m Message
	var err error
	if actor.Admin {
		_, err = q.Exec(ctx, `INSERT INTO support_conversations(tenant_id,member_id) VALUES($1,$2)
        ON CONFLICT(tenant_id,member_id) DO NOTHING`, actor.TenantID, actor.MemberID)
		if err != nil {
			return c, m, false, err
		}
	}
	err = q.QueryRow(ctx, `SELECT id,last_sequence FROM support_conversations
        WHERE tenant_id=$1 AND member_id=$2 FOR UPDATE`, actor.TenantID, actor.MemberID).Scan(&c.ID, &c.LastSequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, m, false, ErrNotFound
	}
	if err != nil {
		return c, m, false, err
	}
	err = q.QueryRow(ctx, `SELECT id,sequence,client_id,sender_type,sender_name,body,created_at
        FROM support_messages WHERE conversation_id=$1 AND sender_type=$2 AND sender_id=$3 AND client_id=$4`,
		c.ID, actor.kind(), actor.ID, clientID).Scan(&m.ID, &m.Sequence, &m.ClientID, &m.SenderType, &m.SenderName, &m.Body, &m.CreatedAt)
	if err == nil {
		if m.Body != body {
			return c, m, false, ErrConflict
		}
		c, err = s.Find(ctx, q, actor.TenantID, actor.MemberID)
		return c, m, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return c, m, false, err
	}
	err = q.QueryRow(ctx, `INSERT INTO support_messages(tenant_id,member_id,conversation_id,sequence,
        client_id,sender_type,sender_id,sender_name,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
        RETURNING id,sequence,client_id,sender_type,sender_name,body,created_at`, actor.TenantID, actor.MemberID,
		c.ID, c.LastSequence+1, clientID, actor.kind(), actor.ID, actor.Name, body).
		Scan(&m.ID, &m.Sequence, &m.ClientID, &m.SenderType, &m.SenderName, &m.Body, &m.CreatedAt)
	if err != nil {
		return c, m, false, err
	}
	_, err = q.Exec(ctx, `UPDATE support_conversations SET last_sequence=$2,updated_at=clock_timestamp() WHERE id=$1`, c.ID, m.Sequence)
	if err != nil {
		return c, m, false, err
	}
	c, err = s.Find(ctx, q, actor.TenantID, actor.MemberID)
	return c, m, true, err
}

func (Store) MarkRead(ctx context.Context, q db.Querier, c Conversation, admin bool, sequence int64) error {
	column := "member_read_sequence"
	if admin {
		column = "admin_read_sequence"
	}
	_, err := q.Exec(ctx, `UPDATE support_conversations SET `+column+`=GREATEST(`+column+`,LEAST(last_sequence,$2)) WHERE id=$1`, c.ID, sequence)
	return err
}

func (Store) Inbox(ctx context.Context, q db.Querier, before string) ([]Conversation, error) {
	rows, err := q.Query(ctx, `SELECT `+conversationColumns+`,
        (SELECT count(*) FROM support_messages m WHERE m.conversation_id=c.id AND m.sender_type='member' AND m.sequence>c.admin_read_sequence),
        COALESCE((SELECT left(body,160) FROM support_messages WHERE conversation_id=c.id ORDER BY sequence DESC LIMIT 1),'')
        FROM support_conversations c JOIN tenant_members m ON m.tenant_id=c.tenant_id AND m.id=c.member_id
        JOIN tenants t ON t.id=c.tenant_id
        WHERE ($1::uuid IS NULL OR (c.updated_at,c.id)<(SELECT updated_at,id FROM support_conversations WHERE id=$1))
        ORDER BY c.updated_at DESC,c.id DESC LIMIT $2`, nullable(before), PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.TenantID, &c.MemberID, &c.LastSequence, &c.MemberReadSequence, &c.AdminReadSequence, &c.UpdatedAt, &c.MemberName, &c.MemberEmail, &c.WorkspaceName, &c.Unread, &c.Preview); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
