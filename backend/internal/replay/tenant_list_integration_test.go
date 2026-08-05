package replay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	var otherTenantID, otherProjectID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Rival', 'rival', 'growth', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create second tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Rival Web', 'pk_live_LISTCCCCCCCCCCCCCCCCCC', '{rival.example}') RETURNING id
	`, otherTenantID).Scan(&otherProjectID); err != nil {
		t.Fatalf("create second tenant project: %v", err)
	}
	var starterTenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Frugal', 'frugal', 'starter', 'active') RETURNING id
	`).Scan(&starterTenantID); err != nil {
		t.Fatalf("create starter tenant: %v", err)
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
	tieOneID := insertSession(projectTwoID, base.Add(30*time.Minute), "")
	tieTwoID := insertSession(projectTwoID, base.Add(30*time.Minute), "")
	if _, err := root.Exec(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, page_url, started_at,
			rrweb_version, max_duration_seconds, max_events, replay_privacy_version
		) VALUES ($1, $2, gen_random_uuid(), 'https://rival.example/', $3, '2.1.1', 1800, 50000, 2)
	`, otherTenantID, otherProjectID, base.Add(4*time.Hour)); err != nil {
		t.Fatalf("insert second tenant session: %v", err)
	}

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

	tieHighID, tieLowID := tieOneID, tieTwoID
	if tieTwoID > tieOneID {
		tieHighID, tieLowID = tieTwoID, tieOneID
	}
	order := []string{recordingID, readyID, failedID, tieHighID, tieLowID, expiredID}
	statuses := []string{"recording", "ready", "failed", "recording", "recording", "expired"}

	t.Run("lists every session of the tenant newest first", func(t *testing.T) {
		recorder := tenantReplayRequest(handler, http.MethodGet, "/v1/sessions", accessToken)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		response := decodeList(t, recorder.Body.String())
		if len(response.Sessions) != len(order) || response.Pagination.HasMore {
			t.Fatalf("list response = %#v", response)
		}
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
		for _, index := range []int{0, 2, 3, 4, 5} {
			if response.Sessions[index].ReplayURL != nil {
				t.Fatalf("session %d has replay_url", index)
			}
			if response.Sessions[index].FeedbackID != nil {
				t.Fatalf("session %d has feedback_id", index)
			}
		}
		if strings.Contains(recorder.Body.String(), "storage_key") ||
			strings.Contains(recorder.Body.String(), "merge_error") ||
			strings.Contains(recorder.Body.String(), "rival.example") {
			t.Fatalf("list leaked internal fields or foreign sessions: %s", recorder.Body.String())
		}
	})

	t.Run("filters by project", func(t *testing.T) {
		recorder := tenantReplayRequest(
			handler, http.MethodGet, "/v1/sessions?project_id="+projectTwoID, accessToken,
		)
		response := decodeList(t, recorder.Body.String())
		if recorder.Code != http.StatusOK || len(response.Sessions) != 3 ||
			response.Sessions[0].ID != failedID ||
			response.Sessions[1].ID != tieHighID || response.Sessions[2].ID != tieLowID {
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
		if recorder.Code != http.StatusOK || len(response.Sessions) != 5 {
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

	t.Run("paginates with a cursor across a started_at tie", func(t *testing.T) {
		// A limit of 4 puts the page boundary between the two sessions that share
		// a started_at, so a cursor that ignored the id tie-breaker would either
		// repeat or skip the second one.
		first := tenantReplayRequest(handler, http.MethodGet, "/v1/sessions?limit=4", accessToken)
		firstResponse := decodeList(t, first.Body.String())
		if first.Code != http.StatusOK || len(firstResponse.Sessions) != 4 ||
			!firstResponse.Pagination.HasMore || firstResponse.Pagination.NextCursor == nil {
			t.Fatalf("first page = %d/%#v", first.Code, firstResponse)
		}
		second := tenantReplayRequest(handler, http.MethodGet,
			"/v1/sessions?limit=4&cursor="+*firstResponse.Pagination.NextCursor, accessToken)
		secondResponse := decodeList(t, second.Body.String())
		if second.Code != http.StatusOK || len(secondResponse.Sessions) != 2 ||
			secondResponse.Pagination.HasMore {
			t.Fatalf("second page = %d/%#v", second.Code, secondResponse)
		}
		var paged []string
		for _, item := range append(firstResponse.Sessions, secondResponse.Sessions...) {
			paged = append(paged, item.ID)
		}
		for index, id := range order {
			if paged[index] != id {
				t.Fatalf("paged order = %#v, want %#v", paged, order)
			}
		}
	})

	t.Run("requires the replay plan feature", func(t *testing.T) {
		var starterMemberID string
		if err := root.QueryRow(ctx, `
			INSERT INTO tenant_members (tenant_id, email, permissions)
			VALUES ($1, 'replay-list@frugal.test', ARRAY['session:read']) RETURNING id
		`, starterTenantID).Scan(&starterMemberID); err != nil {
			t.Fatalf("create starter member: %v", err)
		}
		starterToken, _, err := jwtManager.IssueTenant(starterMemberID, starterTenantID, []string{
			string(auth.PermissionSessionRead),
		})
		if err != nil {
			t.Fatalf("issue starter access token: %v", err)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://frugal.localhost/v1/sessions", nil)
		request.Host = "frugal.localhost"
		request.Header.Set("Authorization", "Bearer "+starterToken)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusPaymentRequired ||
			!strings.Contains(recorder.Body.String(), "plan_feature_unavailable") {
			t.Fatalf("starter list = %d/%s", recorder.Code, recorder.Body.String())
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
