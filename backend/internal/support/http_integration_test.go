package support

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
	appserver "github.com/acidiney/app.fidibeki/backend/internal/server"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type testLimiter struct{ deny bool }

func (l *testLimiter) Allow(context.Context, string, ratelimit.Window) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: !l.deny}, nil
}

func TestSupportHTTPIntegration(t *testing.T) {
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
	redisContainer, err := testcontainers.Run(ctx, "redis:7.4-alpine", testcontainers.WithExposedPorts("6379/tcp"), testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(redisContainer) })
	host, err := redisContainer.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := redisContainer.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClient(&redis.Options{Addr: host + ":" + port.Port()})
	t.Cleanup(func() { _ = cache.Close() })
	var tenantID, otherTenant, member, peer, outsider, admin string
	for i, slug := range []string{"support-one", "support-two"} {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO tenants(name,slug,plan,status) VALUES($1,$1,'growth','active') RETURNING id`, slug).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			tenantID = id
		} else {
			otherTenant = id
		}
	}
	for i, tid := range []string{tenantID, tenantID, otherTenant} {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO tenant_members(tenant_id,email,name,status,permissions) VALUES($1,$2,'Test member','active',$3) RETURNING id`, tid, uuid.NewString()+"@example.com", auth.TenantPermissionStrings()).Scan(&id); err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0:
			member = id
		case 1:
			peer = id
		case 2:
			outsider = id
		}
	}
	if err = pool.QueryRow(ctx, `INSERT INTO platform_admins(email,name,permissions)VALUES('staff@example.com','Ada',$1)RETURNING id`, auth.PlatformPermissionStrings()).Scan(&admin); err != nil {
		t.Fatal(err)
	}
	jwt, err := auth.NewJWTManager(strings.Repeat("s", 32), "test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, _ := jwt.Issue(admin, auth.AudiencePlatform, auth.PlatformPermissionStrings())
	memberToken, _, _ := jwt.IssueTenant(member, tenantID, auth.TenantPermissionStrings())
	peerToken, _, _ := jwt.IssueTenant(peer, tenantID, auth.TenantPermissionStrings())
	outsiderToken, _, _ := jwt.IssueTenant(outsider, otherTenant, auth.TenantPermissionStrings())
	appBegin := func(ctx context.Context) (db.Transaction, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, "SET LOCAL ROLE fidibeki_app"); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		return tx, nil
	}
	limiter := &testLimiter{}
	h := NewHandler(HTTPOptions{AppBegin: appBegin, AdminBegin: db.BeginFromPool(pool), Resolver: tenant.PostgresResolver{Admin: pool}, BaseDomain: "localhost", JWT: jwt, Live: Live{Redis: cache}, Limiter: limiter})
	router := chi.NewRouter()
	h.RegisterTenantRoutes(router)
	h.RegisterPlatformRoutes(router)
	server := httptest.NewServer(appserver.New(appserver.Options{BaseDomain: "localhost", PlatformHandler: router, TenantHandler: router}))
	t.Cleanup(server.Close)
	request := func(method, path, token string, body any) (int, []byte) {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "support-one.localhost"
		if strings.HasPrefix(path, "/admin/") {
			req.Host = "api.localhost"
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		result, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, result
	}
	expect := func(method, path, token string, body any, status int) []byte {
		t.Helper()
		got, data := request(method, path, token, body)
		if got != status {
			t.Fatalf("%s %s = %d: %s, want %d", method, path, got, data, status)
		}
		return data
	}
	messageBody := func(text string) map[string]any {
		return map[string]any{"tenant_id": tenantID, "member_id": member, "client_id": uuid.NewString(), "body": text}
	}
	var conversationID string
	t.Run("staff starts and member replies, retries do not duplicate", func(t *testing.T) {
		expect("GET", "/v1/support/conversation", memberToken, nil, 200)
		expect("POST", "/v1/support/messages", memberToken, map[string]any{"client_id": uuid.NewString(), "body": "cannot start"}, 404)
		input := messageBody("How was setup?")
		first := expect("POST", "/admin/backoffice/support/messages", adminToken, input, 200)
		var sent struct {
			ConversationID string  `json:"conversation_id"`
			Message        Message `json:"message"`
		}
		if err = json.Unmarshal(first, &sent); err != nil {
			t.Fatal(err)
		}
		conversationID = sent.ConversationID
		second := expect("POST", "/admin/backoffice/support/messages", adminToken, input, 200)
		if string(first) != string(second) {
			t.Fatalf("retry differs: %s / %s", first, second)
		}
		input["body"] = "Changed"
		expect("POST", "/admin/backoffice/support/messages", adminToken, input, 409)
		expect("POST", "/v1/support/messages", memberToken, map[string]any{"client_id": uuid.NewString(), "body": "<script>alert('plain text')</script>"}, 200)
		var thread Thread
		data := expect("GET", "/v1/support/conversation", memberToken, nil, 200)
		if err = json.Unmarshal(data, &thread); err != nil {
			t.Fatal(err)
		}
		if len(thread.Messages) != 2 || thread.Conversation.Unread != 1 || thread.Messages[0].SenderName != "Ada" {
			t.Fatalf("thread=%+v", thread)
		}
		expect("POST", "/v1/support/read", memberToken, map[string]any{"sequence": 2}, 204)
		expect("POST", "/v1/support/read", memberToken, map[string]any{"sequence": 1}, 204)
		expect("POST", "/v1/support/read", memberToken, map[string]any{"sequence": 999}, 400)
		data = expect("GET", "/v1/support/conversation", memberToken, nil, 200)
		_ = json.Unmarshal(data, &thread)
		if thread.Conversation.Unread != 0 || thread.Conversation.MemberReadSequence != 2 {
			t.Fatalf("read regressed: %+v", thread.Conversation)
		}
		expect("GET", "/admin/backoffice/support/conversations/"+conversationID, adminToken, nil, 200)
		expect("POST", "/admin/backoffice/support/conversations/"+conversationID+"/read", adminToken, map[string]any{"sequence": 2}, 204)
	})
	t.Run("auth isolation and database policies", func(t *testing.T) {
		expect("GET", "/v1/support/conversation", "", nil, 401)
		expect("GET", "/v1/support/conversation", adminToken, nil, 401)
		expect("GET", "/v1/support/conversation", outsiderToken, nil, 403)
		expect("GET", "/admin/backoffice/support/conversations", memberToken, nil, 401)
		expect("POST", "/v1/support/messages", peerToken, map[string]any{"client_id": uuid.NewString(), "body": "no existing thread"}, 404)
		expect("POST", "/v1/support/messages", memberToken, messageBody("forged recipient"), 400)
		var empty Thread
		_ = json.Unmarshal(expect("GET", "/v1/support/conversation", peerToken, nil, 200), &empty)
		if empty.Conversation != nil {
			t.Fatal("peer sees another member's thread")
		}
		tx, err := appBegin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.support_member_id',$2,true)`, tenantID, peer)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM support_messages").Scan(&count); err != nil || count != 0 {
			t.Fatalf("RLS leaked %d: %v", count, err)
		}
		if _, err = pool.Exec(ctx, `UPDATE platform_admins SET permissions='{}' WHERE id=$1`, admin); err != nil {
			t.Fatal(err)
		}
		expect("GET", "/admin/backoffice/support/conversations", adminToken, nil, 403)
		if _, err = pool.Exec(ctx, `UPDATE platform_admins SET permissions=$2 WHERE id=$1`, admin, auth.PlatformPermissionStrings()); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE tenant_members SET authorization_version=authorization_version+1 WHERE id=$1`, member); err != nil {
			t.Fatal(err)
		}
		expect("GET", "/v1/support/events", memberToken, nil, 401)
		memberToken, _, _ = jwt.IssueTenantMember(member, tenantID, auth.TenantPermissionStrings(), 1)
	})
	t.Run("machine credentials, read-only staff, suspension and two-factor policy", func(t *testing.T) {
		key := "fk_live_" + strings.Repeat("a", 43)
		if _, err = pool.Exec(ctx, `INSERT INTO api_keys(tenant_id,name,token_hash,token_prefix,permissions) VALUES($1,'Support test',$2,'fk_live_test',$3)`, tenantID, auth.HashToken(key), auth.TenantPermissionStrings()); err != nil {
			t.Fatal(err)
		}
		expect("GET", "/v1/support/conversation", key, nil, 403)
		expect("GET", "/v1/support/events", key, nil, 403)
		readerToken, _, _ := jwt.Issue(admin, auth.AudiencePlatform, []string{string(auth.PermissionPlatformSupportRead)})
		expect("GET", "/admin/backoffice/support/conversations", readerToken, nil, 200)
		expect("POST", "/admin/backoffice/support/messages", readerToken, messageBody("not permitted"), 403)
		if _, err = pool.Exec(ctx, `UPDATE tenants SET status='suspended' WHERE id=$1`, tenantID); err != nil {
			t.Fatal(err)
		}
		expect("GET", "/v1/support/conversation", memberToken, nil, 403)
		expect("POST", "/admin/backoffice/support/messages", adminToken, messageBody("inactive target"), 404)
		if _, err = pool.Exec(ctx, `UPDATE tenants SET status='active',two_factor_policy='required' WHERE id=$1`, tenantID); err != nil {
			t.Fatal(err)
		}
		expect("GET", "/v1/support/conversation", memberToken, nil, 403)
		if _, err = pool.Exec(ctx, `UPDATE tenants SET two_factor_policy='optional' WHERE id=$1`, tenantID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("presence deduplication, activity and expiry", func(t *testing.T) {
		first, second := uuid.NewString(), uuid.NewString()
		expect("PUT", "/v1/support/presence", memberToken, map[string]any{"tab_id": first, "section": "projects", "state": "active"}, 204)
		expect("PUT", "/v1/support/presence", memberToken, map[string]any{"tab_id": second, "section": "settings", "state": "idle"}, 204)
		var online struct {
			Users []Presence `json:"users"`
		}
		_ = json.Unmarshal(expect("GET", "/admin/backoffice/support/online", adminToken, nil, 200), &online)
		if len(online.Users) != 1 || online.Users[0].State != "active" || online.Users[0].Section != "projects" {
			t.Fatalf("presence=%+v", online)
		}
		expect("PUT", "/v1/support/presence", memberToken, map[string]any{"tab_id": first, "section": "/private?secret=1", "state": "active"}, 400)
		expect("DELETE", "/v1/support/presence/"+first, memberToken, nil, 204)
		if err = cache.Del(ctx, presenceKey(tenantID, member, second)).Err(); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(expect("GET", "/admin/backoffice/support/online", adminToken, nil, 200), &online)
		if len(online.Users) != 0 {
			t.Fatal("expired session still online")
		}
	})
	t.Run("real stream invalidation and reconnect snapshot", func(t *testing.T) {
		streamCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		req, _ := http.NewRequestWithContext(streamCtx, "GET", server.URL+"/v1/support/events", nil)
		req.Host = "support-one.localhost"
		req.Header.Set("Authorization", "Bearer "+memberToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 {
			t.Fatal(resp.Status)
		}
		reader := bufio.NewReader(resp.Body)
		readEvent := func() {
			t.Helper()
			var event string
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				event += line
				if line == "\n" {
					break
				}
			}
			if event != "event: change\ndata: {}\n\n" {
				t.Fatalf("event contains unexpected data: %q", event)
			}
		}
		readEvent()
		if pool.Stat().AcquiredConns() != 0 {
			t.Fatal("stream holds database connection")
		}
		expect("POST", "/admin/backoffice/support/messages", adminToken, messageBody("Live message"), 200)
		readEvent()
		stop()
		var thread Thread
		_ = json.Unmarshal(expect("GET", "/v1/support/conversation", memberToken, nil, 200), &thread)
		if thread.Messages[len(thread.Messages)-1].Body != "Live message" {
			t.Fatal("snapshot lost latest message")
		}
	})
	t.Run("concurrent ordering and bounded history", func(t *testing.T) {
		var group sync.WaitGroup
		for i := 0; i < 55; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				code, body := request("POST", "/admin/backoffice/support/messages", adminToken, messageBody("Concurrent message"))
				if code != 200 {
					t.Errorf("send=%d %s", code, body)
				}
			}()
		}
		group.Wait()
		var thread Thread
		_ = json.Unmarshal(expect("GET", "/v1/support/conversation", memberToken, nil, 200), &thread)
		if len(thread.Messages) != PageSize || !thread.HasMore {
			t.Fatalf("pagination: %d %v", len(thread.Messages), thread.HasMore)
		}
		for i := 1; i < len(thread.Messages); i++ {
			if thread.Messages[i].Sequence != thread.Messages[i-1].Sequence+1 {
				t.Fatal("sequence gap")
			}
		}
		expect("GET", "/v1/support/conversation?before=9", memberToken, nil, 200)
		expect("GET", "/v1/support/conversation?before=-1", memberToken, nil, 400)
		expect("GET", "/admin/backoffice/support/conversations?before=invalid", adminToken, nil, 400)
	})
	t.Run("bounds and rate limiting", func(t *testing.T) {
		expect("POST", "/admin/backoffice/support/messages", adminToken, messageBody(strings.Repeat("é", 4001)), 400)
		expect("POST", "/admin/backoffice/support/messages", adminToken, messageBody(" \n "), 400)
		input := messageBody("Bad ID")
		input["client_id"] = "bad"
		expect("POST", "/admin/backoffice/support/messages", adminToken, input, 400)
		limiter.deny = true
		expect("POST", "/admin/backoffice/support/messages", adminToken, messageBody("Limited"), 429)
		limiter.deny = false
	})
	t.Run("member removal cascades conversation and messages", func(t *testing.T) {
		if _, err = pool.Exec(ctx, "DELETE FROM tenant_members WHERE id=$1", member); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM support_messages WHERE member_id=$1", member).Scan(&count); err != nil || count != 0 {
			t.Fatalf("messages survived deletion: %d %v", count, err)
		}
	})
	t.Run("retention removes old threads and records health", func(t *testing.T) {
		input := messageBody("Old support thread")
		input["member_id"] = peer
		expect("POST", "/admin/backoffice/support/messages", adminToken, input, 200)
		if _, err = pool.Exec(ctx, `UPDATE support_conversations SET updated_at=now()-interval '181 days' WHERE member_id=$1`, peer); err != nil {
			t.Fatal(err)
		}
		var caseID string
		if err = pool.QueryRow(ctx, `INSERT INTO privacy_cases(tenant_id,public_id,subject_type,right_type,controller_role,status,subject_member_id,requested_by_member_id,verification_method,verified_at,received_at,deadline_at,idempotency_key,request_fingerprint)
            VALUES($1,'PR-SUPPORT','member','access','controller','received',$2,$2,'workspace_membership',now(),now(),now()+interval '30 days','support-hold',decode(repeat('00',32),'hex')) RETURNING id`, tenantID, peer).Scan(&caseID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO privacy_case_legal_holds(tenant_id,privacy_case_id,reason,placed_by_member_id,placed_at,idempotency_key,request_fingerprint)
            VALUES($1,$2,'Preserve conversation',$3,now(),'support-hold',decode(repeat('00',32),'hex'))`, tenantID, caseID, peer); err != nil {
			t.Fatal(err)
		}
		maintainer := Maintainer{Database: pool, Health: retentionhealth.Store{Database: pool}}
		sweepTime := time.Now()
		if err = maintainer.MaintainOnce(ctx, sweepTime); err != nil {
			t.Fatal(err)
		}
		var heldCount int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM support_messages WHERE member_id=$1`, peer).Scan(&heldCount); err != nil || heldCount != 1 {
			t.Fatalf("hold did not protect messages: %d %v", heldCount, err)
		}
		if _, err = pool.Exec(ctx, `DELETE FROM privacy_cases WHERE id=$1`, caseID); err != nil {
			t.Fatal(err)
		}

		if err = maintainer.MaintainOnce(ctx, sweepTime.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM support_messages WHERE member_id=$1`, peer).Scan(&count); err != nil || count != 0 {
			t.Fatalf("expired messages=%d: %v", count, err)
		}
		var processed int
		if err = pool.QueryRow(ctx, `SELECT last_processed FROM retention_maintenance_status WHERE operation_key='support_conversations'`).Scan(&processed); err != nil || processed != 1 {
			t.Fatalf("retention health=%d: %v", processed, err)
		}
	})
}
