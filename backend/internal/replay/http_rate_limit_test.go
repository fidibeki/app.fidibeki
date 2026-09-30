package replay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

func TestReplayRateLimitsAllowRecordingBatchBurst(t *testing.T) {
	// A 30-minute recording flushing every 15 seconds can queue 120 batches
	// behind a slow upload. Both requests per batch must fit the burst budget.
	assertReplayBatchBudget(t, 120, 1024)
}

func TestReplayRateLimitsAllowSmallBatchesUpToSessionEventLimit(t *testing.T) {
	// 50,000 events in 1000 small batches should consume about 1 MiB, not 1000 MiB.
	assertReplayBatchBudget(t, 1000, 1024)
}

func TestReplayRateLimitsStillBoundRequestsAndBytes(t *testing.T) {
	const path = "/v1/replay/sessions/00000000-0000-4000-8000-000000000001/batches"
	t.Run("declaration and completion budgets remain bounded", func(t *testing.T) {
		router := assertReplayBatchBudget(t, 1000, 1024)
		assertReplayBudgetRejected(t, router, path, `{"batch_index":0,"events_count":1,"size_bytes":1024}`)
		assertReplayBudgetRejected(t, router, path+"/0/complete", `{"complete_token":"token"}`)
	})
	t.Run("byte budget rounds up to KiB and retains the 128 MiB ceiling", func(t *testing.T) {
		router := assertReplayBatchBudget(t, 64, maximumBatchSize-1)
		assertReplayBudgetRejected(t, router, path, `{"batch_index":64,"events_count":1,"size_bytes":1}`)
	})
}

func assertReplayBudgetRejected(t *testing.T, router http.Handler, path, body string) {
	t.Helper()
	response := replayRequest(router, http.MethodPost, path+"?key=key", body, "key", "https://example.com")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" ||
		!strings.Contains(response.Body.String(), `"reason":"replay_rate_limited"`) {
		t.Fatalf("exhausted budget returned %d %s", response.Code, response.Body.String())
	}
}

func assertReplayBatchBudget(t *testing.T, batches int, sizeBytes int) http.Handler {
	t.Helper()
	keyring, err := abuse.NewKeyring("replay-rate-limit-test-secret-32-bytes", "")
	if err != nil {
		t.Fatal(err)
	}
	limiter := &replayBudgetLimiter{used: make(map[string]int)}
	admitted := 0
	router := chi.NewRouter()
	RegisterPublicRoutes(router, HTTPOptions{
		Authorizer: projectAuthorizerStub{configuration: project.PublicConfiguration{
			TenantID: "tenant", ProjectID: "project", Plan: tenant.PlanGrowth,
		}},
		Abuse: abuse.Evaluator{Limiter: limiter, Keyring: keyring},
		Begin: func(context.Context) (db.Transaction, error) {
			admitted++
			return nil, errors.New("stop after rate-limit admission")
		},
	})
	const path = "/v1/replay/sessions/00000000-0000-4000-8000-000000000001/batches"
	for index := 0; index < batches; index++ {
		for _, request := range []struct{ path, body string }{
			{path, fmt.Sprintf(`{"batch_index":%d,"events_count":50,"size_bytes":%d}`, index, sizeBytes)},
			{fmt.Sprintf("%s/%d/complete", path, index), `{"complete_token":"token"}`},
		} {
			before := admitted
			response := replayRequest(router, http.MethodPost, request.path+"?key=key",
				request.body, "key", "https://example.com")
			if admitted != before+1 {
				t.Fatalf("batch %d request %s was blocked: %d %s", index, request.path,
					response.Code, response.Body.String())
			}
		}
	}
	return router
}

// A full, non-refilling window tests bursts more strictly than production GCRA.
type replayBudgetLimiter struct{ used map[string]int }

func (limiter *replayBudgetLimiter) Allow(ctx context.Context, key string, window ratelimit.Window) (ratelimit.Decision, error) {
	return limiter.AllowN(ctx, key, window, 1)
}

func (limiter *replayBudgetLimiter) AllowN(_ context.Context, key string, window ratelimit.Window, cost int) (ratelimit.Decision, error) {
	if limiter.used[key]+cost > window.Limit {
		return ratelimit.Decision{Allowed: false, Limit: window.Limit, RetryAfter: time.Hour}, nil
	}
	limiter.used[key] += cost
	return ratelimit.Decision{Allowed: true, Limit: window.Limit, Remaining: window.Limit - limiter.used[key]}, nil
}
