package replay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

type tenantSessionListItem struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	SessionID   string     `json:"session_id"`
	PageURL     *string    `json:"page_url"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	EventsCount int        `json:"events_count"`
	Status      string     `json:"status"`
	FeedbackID  *string    `json:"feedback_id"`
	ReplayURL   *string    `json:"replay_url"`
}

type tenantSessionListResponse struct {
	Sessions   []tenantSessionListItem `json:"sessions"`
	Pagination struct {
		NextCursor *string `json:"next_cursor"`
		HasMore    bool    `json:"has_more"`
		Limit      int     `json:"limit"`
	} `json:"pagination"`
}

func TestTenantReplaySessionList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, replayMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}

	var tenantID, projectOneID, projectTwoID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_LISTAAAAAAAAAAAAAAAAAA', '{example.com}') RETURNING id
	`, tenantID).Scan(&projectOneID); err != nil {
		t.Fatalf("create first project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Mobile', 'pk_live_LISTBBBBBBBBBBBBBBBBBB', '{example.org}') RETURNING id
	`, tenantID).Scan(&projectTwoID); err != nil {
		t.Fatalf("create second project: %v", err)
	}
	var feedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (tenant_id, project_id, public_id, client_feedback_id, page_url)
		VALUES ($1, $2, 'fb_LIST0001', gen_random_uuid(), 'https://example.com/checkout')
		RETURNING id
	`, tenantID, projectOneID).Scan(&feedbackID); err != nil {
		t.Fatalf("create feedback: %v", err)
	}

	base := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	insertSession := func(projectID string, startedAt time.Time, mutate string) string {
		t.Helper()
		var id string
		if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, page_url, started_at,
				rrweb_version, max_duration_seconds, max_events,
				events_count, batches_count, replay_privacy_version
			) VALUES ($1, $2, gen_random_uuid(), 'https://example.com/checkout', $3,
				'2.1.1', 1800, 50000, 40, 1, 2)
			RETURNING id
		`, tenantID, projectID, startedAt).Scan(&id); err != nil {
			t.Fatalf("insert session: %v", err)
		}
		if mutate != "" {
			if _, err := root.Exec(ctx, mutate, id); err != nil {
				t.Fatalf("mutate session: %v", err)
			}
		}
		return id
	}
	expiredID := insertSession(projectOneID, base, `
		UPDATE sessions SET retention_deleted_at = now() WHERE id = $1`)
	failedID := insertSession(projectTwoID, base.Add(time.Hour), `
		UPDATE sessions SET ended_at = started_at + interval '1 minute',
			merge_requested_at = started_at + interval '1 minute',
			merge_attempts = 12, failure_code = 'processing_failed'
		WHERE id = $1`)
	readyID := insertSession(projectOneID, base.Add(2*time.Hour), `
		UPDATE sessions SET ended_at = started_at + interval '5 minutes',
			merge_requested_at = started_at + interval '5 minutes',
			merged_at = started_at + interval '6 minutes',
			storage_key = 'tenants/acme/sessions/ready/replay.json',
			feedback_id = (SELECT id FROM feedbacks LIMIT 1)
		WHERE id = $1`)
	recordingID := insertSession(projectOneID, base.Add(3*time.Hour), "")

	var listMemberID, restrictedMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'replay-list@acme.test', ARRAY['session:read']) RETURNING id
	`, tenantID).Scan(&listMemberID); err != nil {
		t.Fatalf("create list member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'restricted-list@acme.test', '{}') RETURNING id
	`, tenantID).Scan(&restrictedMemberID); err != nil {
		t.Fatalf("create restricted member: %v", err)
	}
	jwtManager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Hour)
	if err != nil {
		t.Fatalf("create tenant JWT manager: %v", err)
	}
	accessToken, _, err := jwtManager.IssueTenant(listMemberID, tenantID, []string{
		string(auth.PermissionSessionRead),
	})
	if err != nil {
		t.Fatalf("issue tenant access token: %v", err)
	}
	restrictedToken, _, err := jwtManager.IssueTenant(restrictedMemberID, tenantID, nil)
	if err != nil {
		t.Fatalf("issue restricted access token: %v", err)
	}

	pools, err := db.OpenPools(ctx, replayRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	tenantRoutes := chi.NewRouter()
	RegisterTenantRoutes(tenantRoutes, TenantHTTPOptions{Store: Store{}, JWT: jwtManager})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(tenantRoutes)

	decodeList := func(t *testing.T, body string) tenantSessionListResponse {
		t.Helper()
		var response tenantSessionListResponse
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			t.Fatalf("decode list response: %v (%s)", err, body)
		}
		return response
	}

	t.Run("lists every session newest first", func(t *testing.T) {
		recorder := tenantReplayRequest(handler, http.MethodGet, "/v1/sessions", accessToken)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		response := decodeList(t, recorder.Body.String())
		if len(response.Sessions) != 4 || response.Pagination.HasMore {
			t.Fatalf("list response = %#v", response)
		}
		order := []string{recordingID, readyID, failedID, expiredID}
		statuses := []string{"recording", "ready", "failed", "expired"}
		for index, item := range response.Sessions {
			if item.ID != order[index] || item.Status != statuses[index] {
				t.Fatalf("item %d = %#v", index, item)
			}
		}
		ready := response.Sessions[1]
		if ready.ReplayURL == nil || *ready.ReplayURL != "/v1/sessions/"+readyID+"/replay" {
			t.Fatalf("ready replay_url = %#v", ready.ReplayURL)
		}
		if ready.FeedbackID == nil || *ready.FeedbackID != feedbackID {
			t.Fatalf("ready feedback_id = %#v", ready.FeedbackID)
		}
		for _, index := range []int{0, 2, 3} {
			if response.Sessions[index].ReplayURL != nil {
				t.Fatalf("session %d has replay_url", index)
			}
			if response.Sessions[index].FeedbackID != nil {
				t.Fatalf("session %d has feedback_id", index)
			}
		}
		if strings.Contains(recorder.Body.String(), "storage_key") ||
			strings.Contains(recorder.Body.String(), "merge_error") {
			t.Fatalf("list leaked internal fields: %s", recorder.Body.String())
		}
	})

	t.Run("filters by project", func(t *testing.T) {
		recorder := tenantReplayRequest(
			handler, http.MethodGet, "/v1/sessions?project_id="+projectTwoID, accessToken,
		)
		response := decodeList(t, recorder.Body.String())
		if recorder.Code != http.StatusOK || len(response.Sessions) != 1 ||
			response.Sessions[0].ID != failedID {
			t.Fatalf("project filter = %d/%#v", recorder.Code, response)
		}
	})

	t.Run("filters by status", func(t *testing.T) {
		recorder := tenantReplayRequest(
			handler, http.MethodGet, "/v1/sessions?status=ready", accessToken,
		)
		response := decodeList(t, recorder.Body.String())
		if recorder.Code != http.StatusOK || len(response.Sessions) != 1 ||
			response.Sessions[0].ID != readyID {
			t.Fatalf("status filter = %d/%#v", recorder.Code, response)
		}
	})

	t.Run("filters by feedback linkage", func(t *testing.T) {
		recorder := tenantReplayRequest(
			handler, http.MethodGet, "/v1/sessions?feedback=unlinked", accessToken,
		)
		response := decodeList(t, recorder.Body.String())
		if recorder.Code != http.StatusOK || len(response.Sessions) != 3 {
			t.Fatalf("unlinked filter = %d/%#v", recorder.Code, response)
		}
		linked := tenantReplayRequest(
			handler, http.MethodGet, "/v1/sessions?feedback=linked", accessToken,
		)
		linkedResponse := decodeList(t, linked.Body.String())
		if linked.Code != http.StatusOK || len(linkedResponse.Sessions) != 1 ||
			linkedResponse.Sessions[0].ID != readyID {
			t.Fatalf("linked filter = %d/%#v", linked.Code, linkedResponse)
		}
	})

	t.Run("paginates with a cursor", func(t *testing.T) {
		first := tenantReplayRequest(handler, http.MethodGet, "/v1/sessions?limit=3", accessToken)
		firstResponse := decodeList(t, first.Body.String())
		if first.Code != http.StatusOK || len(firstResponse.Sessions) != 3 ||
			!firstResponse.Pagination.HasMore || firstResponse.Pagination.NextCursor == nil {
			t.Fatalf("first page = %d/%#v", first.Code, firstResponse)
		}
		second := tenantReplayRequest(handler, http.MethodGet,
			"/v1/sessions?limit=3&cursor="+*firstResponse.Pagination.NextCursor, accessToken)
		secondResponse := decodeList(t, second.Body.String())
		if second.Code != http.StatusOK || len(secondResponse.Sessions) != 1 ||
			secondResponse.Sessions[0].ID != expiredID || secondResponse.Pagination.HasMore {
			t.Fatalf("second page = %d/%#v", second.Code, secondResponse)
		}
	})

	t.Run("rejects invalid filters", func(t *testing.T) {
		recorder := tenantReplayRequest(
			handler, http.MethodGet, "/v1/sessions?status=bogus", accessToken,
		)
		if recorder.Code != http.StatusUnprocessableEntity ||
			!strings.Contains(recorder.Body.String(), `"status"`) {
			t.Fatalf("invalid filter = %d/%s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("denies members without session read", func(t *testing.T) {
		recorder := tenantReplayRequest(handler, http.MethodGet, "/v1/sessions", restrictedToken)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("restricted status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
	})
}
