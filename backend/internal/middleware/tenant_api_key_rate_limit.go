package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
)

const tenantAPIKeyRateLimitPeriod = time.Minute

var (
	ErrTenantAPIKeyRateLimited        = errors.New("tenant API key rate limit exceeded")
	ErrTenantAPIKeyLimiterUnavailable = errors.New("tenant API key rate limiter unavailable")
)

type tenantAPIKeyRateLimitContextKey struct{}

type TenantAPIKeyRateLimitOptions struct {
	Limiter ratelimit.Limiter
	Logger  *slog.Logger
}

type tenantAPIKeyRateLimitState struct {
	limiter ratelimit.Limiter
	logger  *slog.Logger

	mu       sync.Mutex
	checked  bool
	keyID    string
	limit    int
	decision ratelimit.Decision
	err      error
}

// TenantAPIKeyRateLimit installs one request-scoped quota decision shared by
// regular tenant routes and the direct attachment/replay streaming handlers.
func TenantAPIKeyRateLimit(options TenantAPIKeyRateLimitOptions) func(http.Handler) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := &tenantAPIKeyRateLimitState{limiter: options.Limiter, logger: logger}
			ctx := context.WithValue(r.Context(), tenantAPIKeyRateLimitContextKey{}, state)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func enforceTenantAPIKeyRateLimit(
	ctx context.Context,
	tenantID string,
	keyID string,
	limit int,
) error {
	state, ok := ctx.Value(tenantAPIKeyRateLimitContextKey{}).(*tenantAPIKeyRateLimitState)
	if !ok || state == nil {
		return ErrTenantAPIKeyLimiterUnavailable
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.checked {
		if state.keyID != keyID || state.limit != limit {
			return ErrTenantAPIKeyLimiterUnavailable
		}
		return state.err
	}
	state.checked = true
	state.keyID = keyID
	state.limit = limit
	if state.limiter == nil {
		state.err = ErrTenantAPIKeyLimiterUnavailable
		return state.err
	}
	decision, err := state.limiter.Allow(
		ctx,
		"tenant-api-key:"+keyID,
		ratelimit.Window{Limit: limit, Period: tenantAPIKeyRateLimitPeriod},
	)
	state.decision = decision
	if err != nil {
		state.logger.WarnContext(ctx, "tenant API key rate limiter unavailable",
			"error", err, "tenant_id", tenantID, "api_key_id", keyID)
		state.err = ErrTenantAPIKeyLimiterUnavailable
		return state.err
	}
	if !decision.Allowed {
		state.logger.WarnContext(ctx, "tenant API key rate limit exceeded",
			"tenant_id", tenantID, "api_key_id", keyID, "rate_limit_per_min", limit)
		state.err = ErrTenantAPIKeyRateLimited
		return state.err
	}
	return nil
}

func WriteTenantAPIKeyRateLimitHeaders(w http.ResponseWriter, ctx context.Context) {
	state, ok := ctx.Value(tenantAPIKeyRateLimitContextKey{}).(*tenantAPIKeyRateLimitState)
	if !ok || state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.checked {
		return
	}
	limit := state.decision.Limit
	if limit <= 0 {
		limit = state.limit
	}
	remaining := state.decision.Remaining
	if remaining < 0 || state.err != nil {
		remaining = 0
	}
	if remaining > limit {
		remaining = limit
	}
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
}

// WriteTenantAPIKeyRateLimitError translates only API-key quota failures. Other
// credential errors keep their existing authentication/authorization handling.
func WriteTenantAPIKeyRateLimitError(
	w http.ResponseWriter,
	ctx context.Context,
	err error,
) bool {
	switch {
	case errors.Is(err, ErrTenantAPIKeyRateLimited):
		WriteTenantAPIKeyRateLimitHeaders(w, ctx)
		retryAfter := tenantAPIKeyRetryAfter(ctx)
		w.Header().Set("Retry-After", strconv.FormatInt(roundedUpSeconds(retryAfter), 10))
		api.WriteError(
			w, http.StatusTooManyRequests, api.CodeRateLimitExceeded,
			"API key rate limit exceeded", nil,
		)
		return true
	case errors.Is(err, ErrTenantAPIKeyLimiterUnavailable):
		WriteTenantAPIKeyRateLimitHeaders(w, ctx)
		w.Header().Set("Retry-After", "1")
		api.WriteError(
			w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			"service unavailable", nil,
		)
		return true
	default:
		return false
	}
}

func tenantAPIKeyRetryAfter(ctx context.Context) time.Duration {
	state, _ := ctx.Value(tenantAPIKeyRateLimitContextKey{}).(*tenantAPIKeyRateLimitState)
	if state == nil {
		return time.Second
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.decision.RetryAfter > 0 {
		return state.decision.RetryAfter
	}
	return time.Second
}

func roundedUpSeconds(duration time.Duration) int64 {
	seconds := int64((duration + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}
