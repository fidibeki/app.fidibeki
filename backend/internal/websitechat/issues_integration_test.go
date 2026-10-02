package websitechat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/issue"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testIssueLinks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, begin db.BeginFunc, jwt *auth.JWTManager, token, tenantID, projectID, visitorID string, request func(string, string, string, any, int, ...map[string]string) map[string]json.RawMessage) {
	t.Helper()
	base := "/v1/projects/" + projectID + "/chat"
	path := base + "/visitors/" + visitorID + "/issues"
	var messages []Message
	thread := request("GET", base+"/visitors/"+visitorID, token, nil, 200)
	if err := json.Unmarshal(thread["messages"], &messages); err != nil {
		t.Fatal(err)
	}
	source := messages[1].ID
	body := map[string]any{"message_id": source, "title": "Checkout needs attention", "description": "Investigate the checkout layout. Internal working note."}
	response := request("POST", path, token, body, 200)
	var issueID string
	if err := json.Unmarshal(response["issue_id"], &issueID); err != nil {
		t.Fatal(err)
	}
	if !validID(issueID) {
		t.Fatal("missing created issue")
	}
	repeated := request("POST", path, token, body, 200)
	if string(repeated["issue_id"]) != string(response["issue_id"]) {
		t.Fatal("retry created another issue")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE resource_id=$1 AND event='issue.created'`, issueID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("outbox count %d: %v", count, err)
	}
	var created issue.Issue
	tx, err := begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenantID); err != nil {
		t.Fatal(err)
	}
	created, err = (issue.Store{}).Get(ctx, tx, tenantID, issueID)
	if err != nil || created.NotifyReporter || created.ReporterEmail != nil || created.FeedbackCount != 0 || created.Title != body["title"] {
		t.Fatalf("invalid issue: %+v %v", created, err)
	}
	// An operator must write the issue summary; no chat transcript was copied.
	if created.Description != body["description"] {
		t.Fatal("description changed")
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	request("POST", path, token, map[string]any{"message_id": source, "title": "Another issue"}, 409)
	request("POST", path, token, map[string]any{"message_id": messages[0].ID, "title": "Operator message"}, 404)
	request("POST", path, token, map[string]any{"message_id": uuid.NewString(), "title": "Unknown message"}, 404)
	request("POST", path, token, map[string]any{"message_id": source, "title": ""}, 400)
	request("POST", path, token, map[string]any{"message_id": source, "issue_id": issueID, "title": "Ambiguous action"}, 400)
	// Changing internal status is reflected for operators without posting to chat.
	if _, err = pool.Exec(ctx, `UPDATE issues SET status='resolved',resolved_at=now() WHERE id=$1`, issueID); err != nil {
		t.Fatal(err)
	}
	linked := request("GET", path, token, nil, 200)
	var links []linkedIssue
	if err = json.Unmarshal(linked["issues"], &links); err != nil || len(links) != 1 || links[0].Status != issue.StatusResolved {
		t.Fatalf("linked status: %s %v", linked["issues"], err)
	}
	after := request("GET", base+"/visitors/"+visitorID, token, nil, 200)
	if string(after["messages"]) != string(thread["messages"]) {
		t.Fatal("link or status update sent chat content")
	}
	sources := request("GET", base+"/issues/"+issueID+"/conversations", token, nil, 200)
	var conversations []Conversation
	if err = json.Unmarshal(sources["conversations"], &conversations); err != nil || len(conversations) != 1 || conversations[0].ID != visitorID {
		t.Fatal("missing conversation backlink")
	}
	// Public RLS scope cannot read operator links, even with a valid visitor ID.
	tx, err = begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.chat_project_id',$2,true),set_config('app.chat_visitor_id',$3,true),set_config('app.chat_operator','false',true)`, tenantID, projectID, visitorID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM website_chat_issue_links`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("public links: %d %v", count, err)
	}
	_ = tx.Rollback(ctx)
	// Membership is authoritative, so even a previously issued full JWT cannot
	// bypass a revoked permission.
	for _, permission := range []auth.Permission{auth.PermissionChatRead, auth.PermissionChatWrite, auth.PermissionProjectRead, auth.PermissionIssueRead, auth.PermissionIssueUpdate} {
		permissions := []string{}
		for _, p := range auth.TenantPermissionStrings() {
			if p != string(permission) {
				permissions = append(permissions, p)
			}
		}
		id := uuid.NewString()
		if _, err = pool.Exec(ctx, `INSERT INTO tenant_members(id,tenant_id,email,status,permissions) VALUES($1,$2,$3,'active',$4)`, id, tenantID, id+"@example.test", auth.TenantPermissionStrings()); err != nil {
			t.Fatal(err)
		}
		restricted, _, err := jwt.IssueTenant(id, tenantID, permissions)
		if err != nil {
			t.Fatal(err)
		}
		request("POST", path, restricted, body, 200)
		full, _, err := jwt.IssueTenant(id, tenantID, auth.TenantPermissionStrings())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE tenant_members SET permissions=$2 WHERE id=$1`, id, permissions); err != nil {
			t.Fatal(err)
		}
		request("POST", path, full, body, 403)
		if permission == auth.PermissionChatRead || permission == auth.PermissionProjectRead || permission == auth.PermissionIssueRead {
			request("GET", path, full, nil, 403)
			request("GET", base+"/issues/"+issueID+"/conversations", full, nil, 403)
		}
	}
	request("DELETE", path+"/"+source, token, nil, 204)
	request("DELETE", path+"/"+source, token, nil, 204)
	// Existing issues must belong to exactly this tenant and project.
	otherKey, err := project.GeneratePublicKey()
	if err != nil {
		t.Fatal(err)
	}
	foreignKey, err := project.GeneratePublicKey()
	if err != nil {
		t.Fatal(err)
	}
	otherProject := uuid.NewString()
	otherIssue := uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO projects(id,tenant_id,name,public_key,allowed_domains) VALUES($1,$2,'Another site',$3,ARRAY['other.test'])`, otherProject, tenantID, otherKey); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO issues(id,tenant_id,project_id,public_id,title) VALUES($1,$2,$3,'CHAT-SHOP-2026-9988','Other site issue')`, otherIssue, tenantID, otherProject); err != nil {
		t.Fatal(err)
	}
	request("POST", path, token, map[string]any{"message_id": source, "issue_id": otherIssue}, 404)
	request("GET", base+"/issues/"+otherIssue+"/conversations", token, nil, 404)
	otherTenant := uuid.NewString()
	foreignProject := uuid.NewString()
	foreignIssue := uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO tenants(id,name,slug,plan,status) VALUES($1,'Foreign','bridge-foreign','growth','active')`, otherTenant); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO projects(id,tenant_id,name,public_key,allowed_domains) VALUES($1,$2,'Foreign site',$3,ARRAY['foreign.test'])`, foreignProject, otherTenant, foreignKey); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO issues(id,tenant_id,project_id,public_id,title) VALUES($1,$2,$3,'BRIDGE-FOREIGN-2026-0001','Foreign issue')`, foreignIssue, otherTenant, foreignProject); err != nil {
		t.Fatal(err)
	}
	request("POST", path, token, map[string]any{"message_id": source, "issue_id": foreignIssue}, 404)
	request("POST", path, token, map[string]any{"message_id": source, "issue_id": issueID}, 200)
	request("POST", path, token, map[string]any{"message_id": source, "issue_id": issueID}, 200)
	// Merged issue backlinks survive and new links cannot target a merged issue.
	target := uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO issues(id,tenant_id,project_id,public_id,title) VALUES($1,$2,$3,'CHAT-SHOP-2026-9989','Merge target')`, target, tenantID, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE issues SET merged_into_id=$2,merged_at=now(),status='closed' WHERE id=$1`, issueID, target); err != nil {
		t.Fatal(err)
	}
	sources = request("GET", base+"/issues/"+target+"/conversations", token, nil, 200)
	if err = json.Unmarshal(sources["conversations"], &conversations); err != nil || len(conversations) != 1 {
		t.Fatal("merged issue lost conversation")
	}
	request("DELETE", path+"/"+source, token, nil, 204)
	request("POST", path, token, map[string]any{"message_id": source, "issue_id": issueID}, 409)
	request("POST", path, token, map[string]any{"message_id": source, "issue_id": target}, 200)
	// Deleting a source message cascades its reference but never deletes the issue.
	// Use an isolated visitor to preserve the surrounding conversation journey.
	v, m := uuid.NewString(), uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO website_chat_visitors(id,tenant_id,project_id,origin,token_hash,expires_at) VALUES($1,$2,$3,'https://shop.test',decode(repeat('00',32),'hex'),$4)`, v, tenantID, projectID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO website_chat_messages(id,tenant_id,project_id,visitor_id,sequence,client_id,sender_type,sender_id,sender_name,body) VALUES($1,$2,$3,$4,1,$5,'visitor',$4,'Visitor','Private source')`, m, tenantID, projectID, v, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	unicodeSummary := strings.Repeat("配", 10000)
	request("POST", base+"/visitors/"+v+"/issues", token, map[string]any{"message_id": m, "title": "Unicode summary", "description": unicodeSummary}, 200)
	request("DELETE", base+"/visitors/"+v+"/issues/"+m, token, nil, 204)
	request("POST", base+"/visitors/"+v+"/issues", token, map[string]any{"message_id": m, "title": "Too long", "description": unicodeSummary + "配"}, 400)
	request("POST", base+"/visitors/"+v+"/issues", token, map[string]any{"message_id": m, "issue_id": target}, 200)
	request("DELETE", base+"/visitors/"+v, token, nil, 204)
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM website_chat_issue_links WHERE message_id=$1`, m).Scan(&count); err != nil || count != 0 {
		t.Fatal("deleted visitor retained link")
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE id=$1`, target).Scan(&count); err != nil || count != 1 {
		t.Fatal("unlink deleted issue")
	}
}
