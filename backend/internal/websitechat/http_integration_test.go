package websitechat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type allowLimiter struct{ deny bool }

func (l *allowLimiter) Allow(context.Context, string, ratelimit.Window) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: !l.deny}, nil
}

func TestWebsiteChatJourney(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL and Redis integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pg, err := postgrescontainer.Run(ctx, "postgres:16-alpine", postgrescontainer.WithDatabase("fidibeki"), postgrescontainer.WithUsername("postgres"), postgrescontainer.WithPassword("postgres"), postgrescontainer.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(pg) })
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := db.NewMigrator(url, filepath.Join("..", "..", "..", "infra", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err = migrator.Up(); err != nil {
		t.Fatal(err)
	}
	if err = migrator.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err = migrator.Up(); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	t.Run("permission migration grants only full administrators", func(t *testing.T) {
		if err := migrator.Steps(-1); err != nil {
			t.Fatal(err)
		}
		var tid string
		if err := pool.QueryRow(ctx, `INSERT INTO tenants(name,slug,plan,status) VALUES('Permissions','chat-permissions','growth','active') RETURNING id`).Scan(&tid); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, tid) }()
		full := []string{}
		for _, p := range auth.TenantPermissionStrings() {
			if !strings.HasPrefix(p, "chat:") {
				full = append(full, p)
			}
		}
		partial := []string{"project:create", "project:delete", "member:invite", "member:update", "tenant:update", "audit:read"}
		withoutFeedback := []string{}
		for _, p := range full {
			if p != "feedback:read" {
				withoutFeedback = append(withoutFeedback, p)
			}
		}
		ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
		for i, permissions := range [][]string{full, partial, withoutFeedback} {
			if _, err := pool.Exec(ctx, `INSERT INTO tenant_members(id,tenant_id,email,status,permissions) VALUES($1,$2,$3,'active',$4)`, ids[i], tid, "member"+strconv.Itoa(i)+"@shop.test", permissions); err != nil {
				t.Fatal(err)
			}
		}
		if err := migrator.Up(); err != nil {
			t.Fatal(err)
		}
		for i, id := range ids {
			var permissions []string
			var version int
			if err := pool.QueryRow(ctx, `SELECT permissions,authorization_version FROM tenant_members WHERE id=$1`, id).Scan(&permissions, &version); err != nil {
				t.Fatal(err)
			}
			for _, p := range []auth.Permission{auth.PermissionChatRead, auth.PermissionChatWrite, auth.PermissionChatDelete} {
				if auth.HasPermission(permissions, p) != (i == 0) {
					t.Fatalf("unexpected permission upgrade for role %d: %v", i, permissions)
				}
			}
			if i == 0 && version != 1 || i > 0 && version != 0 {
				t.Fatalf("role %d authorization version=%d", i, version)
			}
		}
	})

	rc, err := testcontainers.Run(ctx, "redis:7.4-alpine", testcontainers.WithExposedPorts("6379/tcp"), testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(rc) })
	host, err := rc.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := rc.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClient(&redis.Options{Addr: host + ":" + port.Port()})
	t.Cleanup(func() { _ = cache.Close() })
	var tenantID, projectID, memberID string
	key, err := project.GeneratePublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO tenants(name,slug,plan,status) VALUES('Shop','chat-shop','growth','active') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO projects(tenant_id,name,public_key,allowed_domains) VALUES($1,'Website',$2,ARRAY['shop.test']) RETURNING id`, tenantID, key).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO tenant_members(tenant_id,email,name,status,permissions) VALUES($1,'owner@shop.test','Shop owner','active',$2) RETURNING id`, tenantID, auth.TenantPermissionStrings()).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	jwt, err := auth.NewJWTManager(strings.Repeat("j", 32), "test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := jwt.IssueTenant(memberID, tenantID, auth.TenantPermissionStrings())
	if err != nil {
		t.Fatal(err)
	}
	begin := func(ctx context.Context) (db.Transaction, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `SET LOCAL ROLE fidibeki_app`); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		return tx, nil
	}
	limiter := &allowLimiter{}
	h := NewHandler(Options{Begin: begin, Resolver: tenant.PostgresResolver{Admin: pool}, BaseDomain: "localhost", JWT: jwt, Authorizer: widgetapi.Authorizer{Store: project.Store{}, Database: pool}, Redis: cache, Limiter: limiter})
	routes := chi.NewRouter()
	h.RegisterTenantRoutes(routes)
	h.RegisterPublicRoutes(routes)
	server := httptest.NewServer(routes)
	t.Cleanup(server.Close)
	request := func(method, path, credential string, body any, status int, overrides ...map[string]string) map[string]json.RawMessage {
		t.Helper()
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "chat-shop.localhost"
		req.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(path, "/v1/widget/") {
			req.Header.Set("Origin", "https://shop.test")
			req.Header.Set(widgetapi.PublicKeyHeader, key)
			req.Header.Set("X-Fidibeki-Chat-Token", credential)
		} else {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		for _, headers := range overrides {
			for name, value := range headers {
				if name == "Host" {
					req.Host = value
				} else {
					req.Header.Set(name, value)
				}
			}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("%s %s = %d %s; want %d", method, path, resp.StatusCode, data, status)
		}
		var result map[string]json.RawMessage
		if len(data) > 0 {
			if err = json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	base := "/v1/projects/" + projectID + "/chat"
	request("POST", "/v1/widget/chat/session", "", map[string]any{}, 403)
	request("PUT", base+"/settings", token, map[string]any{"enabled": true}, 200)
	session := request("POST", "/v1/widget/chat/session", "", map[string]any{}, 201)
	var visitorToken, visitorID string
	if err = json.Unmarshal(session["token"], &visitorToken); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(session["visitor_id"], &visitorID); err != nil {
		t.Fatal(err)
	}
	payload := func(body string) any { return map[string]any{"client_id": uuid.NewString(), "body": body} }
	request("POST", "/v1/widget/chat/messages", visitorToken, payload("Visitor cannot start"), 403)
	first := payload("How is checkout going?")
	request("POST", base+"/visitors/"+visitorID+"/messages", token, first, 200)
	request("POST", base+"/visitors/"+visitorID+"/messages", token, first, 200)
	thread := request("GET", "/v1/widget/chat/conversation", visitorToken, nil, 200)
	var messages []map[string]any
	if err = json.Unmarshal(thread["messages"], &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0]["body"] != "How is checkout going?" {
		t.Fatalf("unexpected thread: %s", thread["messages"])
	}
	request("POST", "/v1/widget/chat/messages", visitorToken, payload("I need help"), 200)
	thread = request("GET", base+"/visitors/"+visitorID, token, nil, 200)
	if err = json.Unmarshal(thread["messages"], &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1]["body"] != "I need help" {
		t.Fatalf("unexpected reply: %s", thread["messages"])
	}
	request("GET", "/v1/widget/chat/conversation", visitorID, nil, 401)
	t.Run("presence, read markers and inbox are scoped to the website", func(t *testing.T) {
		tab := uuid.NewString()
		request("PUT", "/v1/widget/chat/presence", visitorToken, map[string]any{"tab_id": tab, "page_url": "https://shop.test/orders/private?token=secret", "state": "active"}, 204)
		online := request("GET", base+"/online", token, nil, 200)
		var visitors []map[string]any
		if err = json.Unmarshal(online["visitors"], &visitors); err != nil {
			t.Fatal(err)
		}
		if len(visitors) != 1 || visitors[0]["id"] != visitorID || visitors[0]["page_url"] != "https://shop.test/" {
			t.Fatalf("bad presence: %s", online["visitors"])
		}
		inbox := request("GET", base+"/conversations", token, nil, 200)
		if err = json.Unmarshal(inbox["conversations"], &visitors); err != nil {
			t.Fatal(err)
		}
		if len(visitors) != 1 || visitors[0]["unread"] != float64(1) {
			t.Fatalf("bad inbox: %s", inbox["conversations"])
		}
		request("POST", base+"/visitors/"+visitorID+"/read", token, map[string]any{"sequence": 2}, 204)
		thread = request("GET", base+"/visitors/"+visitorID, token, nil, 200)
		var c Conversation
		if err = json.Unmarshal(thread["conversation"], &c); err != nil {
			t.Fatal(err)
		}
		if c.Unread != 0 {
			t.Fatalf("unread = %d", c.Unread)
		}
		request("DELETE", "/v1/widget/chat/presence/"+tab, visitorToken, nil, 204)
		online = request("GET", base+"/online", token, nil, 200)
		if string(online["visitors"]) != "[]" {
			t.Fatalf("presence not removed: %s", online["visitors"])
		}
	})

	t.Run("website receives live invalidation without holding a transaction", func(t *testing.T) {
		liveCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		req, err := http.NewRequestWithContext(liveCtx, "GET", server.URL+"/v1/widget/chat/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://shop.test")
		req.Header.Set(widgetapi.PublicKeyHeader, key)
		req.Header.Set(tokenHeader, visitorToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("stream status %d", resp.StatusCode)
		}
		scanner := bufio.NewScanner(resp.Body)
		event := func() {
			t.Helper()
			for scanner.Scan() {
				if scanner.Text() == "event: change" {
					return
				}
			}
			t.Fatalf("stream ended: %v", scanner.Err())
		}
		event()
		request("POST", base+"/visitors/"+visitorID+"/messages", token, payload("A live update"), 200)
		event()
	})

	t.Run("tenant project visitor origin and permission boundaries", func(t *testing.T) {
		otherProject := uuid.NewString()
		otherKey, err := project.GeneratePublicKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO projects(id,tenant_id,name,public_key,allowed_domains,chat_enabled) VALUES($1,$2,'Other site',$3,ARRAY['shop.test'],true)`, otherProject, tenantID, otherKey); err != nil {
			t.Fatal(err)
		}
		request("GET", "/v1/widget/chat/conversation", visitorToken, nil, 403, map[string]string{"Origin": "https://evil.test"})
		request("GET", "/v1/widget/chat/conversation", visitorToken, nil, 403, map[string]string{widgetapi.PublicKeyHeader: ""})
		request("GET", "/v1/widget/chat/conversation?key=different", visitorToken, nil, 403)
		request("GET", "/v1/widget/chat/conversation", visitorToken, nil, 401, map[string]string{widgetapi.PublicKeyHeader: otherKey})
		request("GET", "/v1/projects/"+otherProject+"/chat/visitors/"+visitorID, token, nil, 404)
		request("GET", base+"/visitors/"+visitorID, "fk_live_"+strings.Repeat("a", 32), nil, 403)
		request("GET", base+"/visitors/"+visitorID, "", nil, 401)
		request("GET", base+"/visitors/"+visitorID, token, nil, 403, map[string]string{"Host": "missing.localhost"})
		request("GET", "/v1/widget/chat/conversation", visitorID+"."+strings.Repeat("b", 43), nil, 401)
		request("OPTIONS", "/v1/widget/chat/events?key="+key, "", nil, 204)
		request("OPTIONS", "/v1/widget/chat/events?key="+key, "", nil, 403, map[string]string{"Origin": "https://evil.test"})
		second := request("POST", "/v1/widget/chat/session", "", map[string]any{}, 201)
		var secondToken string
		if err = json.Unmarshal(second["token"], &secondToken); err != nil {
			t.Fatal(err)
		}
		empty := request("GET", "/v1/widget/chat/conversation", secondToken, nil, 200)
		if string(empty["conversation"]) != "null" || string(empty["messages"]) != "[]" {
			t.Fatal("visitor accessed another conversation")
		}
		request("POST", "/v1/widget/chat/messages", secondToken, map[string]any{"visitor_id": visitorID, "client_id": uuid.NewString(), "body": "spoof"}, 400)
		if _, err = pool.Exec(ctx, `UPDATE tenant_members SET permissions=ARRAY['chat:read','project:read'] WHERE id=$1`, memberID); err != nil {
			t.Fatal(err)
		}
		request("GET", base+"/visitors/"+visitorID, token, nil, 200)
		request("POST", base+"/visitors/"+visitorID+"/messages", token, payload("cannot send"), 403)
		request("PUT", base+"/settings", token, map[string]any{"enabled": false}, 403)
		if _, err = pool.Exec(ctx, `UPDATE tenant_members SET permissions=$2 WHERE id=$1`, memberID, auth.TenantPermissionStrings()); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE tenants SET two_factor_policy='required' WHERE id=$1`, tenantID); err != nil {
			t.Fatal(err)
		}
		request("GET", base+"/online", token, nil, 403)
		if _, err = pool.Exec(ctx, `UPDATE tenants SET two_factor_policy='optional' WHERE id=$1`, tenantID); err != nil {
			t.Fatal(err)
		}
		request("PUT", base+"/settings", token, map[string]any{"enabled": false}, 200)
		request("GET", "/v1/widget/chat/conversation", visitorToken, nil, 403)
		request("POST", base+"/visitors/"+visitorID+"/messages", token, payload("disabled"), 403)
		request("GET", base+"/visitors/"+visitorID, token, nil, 200)
		request("PUT", base+"/settings", token, map[string]any{"enabled": true}, 200)
	})
	t.Run("message validation pagination read positions and revocation", func(t *testing.T) {
		for _, body := range []any{map[string]any{}, map[string]any{"client_id": uuid.NewString(), "body": "  "}, map[string]any{"client_id": uuid.NewString(), "body": strings.Repeat("界", 4001)}, map[string]any{"client_id": uuid.NewString(), "body": "bad\x00content"}} {
			request("POST", "/v1/widget/chat/messages", visitorToken, body, 400)
		}
		request("GET", "/v1/widget/chat/conversation?before=bad", visitorToken, nil, 400)
		request("GET", base+"/conversations?before=bad", token, nil, 400)
		request("POST", "/v1/widget/chat/read", visitorToken, map[string]any{"sequence": 999}, 400)
		request("POST", "/v1/widget/chat/read", visitorToken, map[string]any{"sequence": -1}, 400)
		request("POST", "/v1/widget/chat/read", visitorToken, map[string]any{"sequence": 3}, 204)
		request("POST", "/v1/widget/chat/read", visitorToken, map[string]any{"sequence": 1}, 204)
		conflict := payload("First version").(map[string]any)
		request("POST", "/v1/widget/chat/messages", visitorToken, conflict, 200)
		conflict["body"] = "Changed retry"
		request("POST", "/v1/widget/chat/messages", visitorToken, conflict, 409)
		for i := 0; i < 51; i++ {
			request("POST", base+"/visitors/"+visitorID+"/messages", token, payload("Pagination message"), 200)
		}
		latest := request("GET", "/v1/widget/chat/conversation", visitorToken, nil, 200)
		var rows []Message
		if err = json.Unmarshal(latest["messages"], &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 50 || string(latest["has_more"]) != "true" {
			t.Fatal("history not paginated")
		}
		older := request("GET", "/v1/widget/chat/conversation?before="+strconv.FormatInt(rows[0].Sequence, 10), visitorToken, nil, 200)
		if string(older["has_more"]) != "false" {
			t.Fatal("older page incomplete")
		}
		request("POST", "/v1/widget/chat/session/revoke", visitorToken, map[string]any{}, 204)
		request("GET", "/v1/widget/chat/conversation", visitorToken, nil, 401)
		request("POST", base+"/visitors/"+visitorID+"/messages", token, payload("expired visitor"), 409)
		request("GET", base+"/visitors/"+visitorID, token, nil, 200)
	})

	t.Run("another tenant cannot read visitors or receive their presence", func(t *testing.T) {
		tid, pid, mid := uuid.NewString(), uuid.NewString(), uuid.NewString()
		key2, err := project.GeneratePublicKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO tenants(id,name,slug,plan,status) VALUES($1,'Other','other-chat','growth','active')`, tid); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO projects(id,tenant_id,name,public_key,allowed_domains,chat_enabled) VALUES($1,$2,'Other',$3,ARRAY['shop.test'],true)`, pid, tid, key2); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO tenant_members(id,tenant_id,email,status,permissions) VALUES($1,$2,'other@shop.test','active',$3)`, mid, tid, auth.TenantPermissionStrings()); err != nil {
			t.Fatal(err)
		}
		otherToken, _, err := jwt.IssueTenant(mid, tid, auth.TenantPermissionStrings())
		if err != nil {
			t.Fatal(err)
		}
		otherBase := "/v1/projects/" + pid + "/chat"
		request("GET", base+"/visitors/"+visitorID, otherToken, nil, 403)
		request("GET", base+"/visitors/"+visitorID, otherToken, nil, 404, map[string]string{"Host": "other-chat.localhost"})
		request("GET", otherBase+"/visitors/"+visitorID, otherToken, nil, 404, map[string]string{"Host": "other-chat.localhost"})
		online := request("GET", otherBase+"/online", otherToken, nil, 200, map[string]string{"Host": "other-chat.localhost"})
		if string(online["visitors"]) != "[]" {
			t.Fatal("tenant presence leaked")
		}
		inbox := request("GET", otherBase+"/conversations", otherToken, nil, 200, map[string]string{"Host": "other-chat.localhost"})
		if string(inbox["conversations"]) != "[]" {
			t.Fatal("tenant inbox leaked")
		}
		tx, err := begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.chat_project_id',$2,true),set_config('app.chat_operator','true',true)`, tid, pid); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM website_chat_messages WHERE visitor_id=$1`, visitorID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("RLS crossed tenant: %d %v", count, err)
		}
	})
	t.Run("limits bad input and retention preserve legal holds", func(t *testing.T) {
		limiter.deny = true
		request("PUT", base+"/settings", token, map[string]any{"enabled": true}, 429)
		limiter.deny = false
		request("PUT", base+"/settings", token, map[string]any{}, 400)
		request("GET", "/v1/projects/bad/chat/settings", token, nil, 404)
		request("GET", base+"/visitors/bad", token, nil, 404)
		if _, err = pool.Exec(ctx, `UPDATE website_chat_visitors SET updated_at=now()-interval '181 days' WHERE id=$1`, visitorID); err != nil {
			t.Fatal(err)
		}
		var caseID string
		if err = pool.QueryRow(ctx, `INSERT INTO privacy_cases(tenant_id,public_id,subject_type,right_type,controller_role,status,subject_member_id,requested_by_member_id,verification_method,verified_at,received_at,deadline_at,idempotency_key,request_fingerprint) VALUES($1,'PR-CHAT','member','access','controller','received',$2,$2,'workspace_membership',now(),now(),now()+interval '30 days','chat-hold',decode(repeat('00',32),'hex')) RETURNING id`, tenantID, memberID).Scan(&caseID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO privacy_case_legal_holds(tenant_id,privacy_case_id,reason,placed_by_member_id,placed_at,idempotency_key,request_fingerprint) VALUES($1,$2,'Preserve website conversation',$3,now(),'chat-hold',decode(repeat('00',32),'hex'))`, tenantID, caseID, memberID); err != nil {
			t.Fatal(err)
		}
		request("DELETE", base+"/visitors/"+visitorID, token, nil, 409)
		maintainer := Maintainer{Database: pool, Health: retentionhealth.Store{Database: pool}}
		if err = maintainer.MaintainOnce(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		request("GET", base+"/visitors/"+visitorID, token, nil, 200)
		if _, err = pool.Exec(ctx, `DELETE FROM privacy_cases WHERE id=$1`, caseID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE website_chat_visitors SET expires_at=now()-interval '2 days' WHERE last_sequence=0`); err != nil {
			t.Fatal(err)
		}
		if err = maintainer.MaintainOnce(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		request("GET", base+"/visitors/"+visitorID, token, nil, 404)
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM website_chat_messages WHERE visitor_id=$1`, visitorID).Scan(&count); err != nil || count != 0 {
			t.Fatal("retention did not cascade")
		}
		fresh := request("POST", "/v1/widget/chat/session", "", map[string]any{}, 201)
		var id, cred string
		_ = json.Unmarshal(fresh["visitor_id"], &id)
		_ = json.Unmarshal(fresh["token"], &cred)
		request("DELETE", base+"/visitors/"+id, token, nil, 204)
		request("GET", "/v1/widget/chat/conversation", cred, nil, 401)
		request("DELETE", base+"/visitors/"+id, token, nil, 404)
		runCtx, stop := context.WithCancel(ctx)
		stop()
		if err = maintainer.Run(runCtx, 0); err != nil {
			t.Fatal(err)
		}
	})

}
