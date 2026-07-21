package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/httpx"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

type tenantContextKey struct{}
type querierContextKey struct{}
type afterCommitContextKey struct{}
type transactionControlContextKey struct{}

type afterCommitHooks struct {
	hooks []func(context.Context) error
}

type transactionControl struct {
	commitOnError bool
}

type TenantTransactionOptions struct {
	BaseDomain string
	Resolver   tenant.Resolver
	Begin      db.BeginFunc
	Logger     *slog.Logger
}

func TenantTransaction(options TenantTransactionOptions) func(http.Handler) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slug, err := tenant.SlugFromHost(r.Host, options.BaseDomain)
			if err != nil {
				api.WriteError(w, http.StatusNotFound, api.CodeUnknownTenant, "unknown tenant", nil)
				return
			}

			resolved, err := options.Resolver.Resolve(r.Context(), slug)
			if errors.Is(err, tenant.ErrUnknownTenant) {
				api.WriteError(w, http.StatusNotFound, api.CodeUnknownTenant, "unknown tenant", nil)
				return
			}
			if err != nil {
				logger.ErrorContext(r.Context(), "tenant resolution failed", "error", err, "tenant_slug", slug)
				api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
				return
			}
			setTenantLogFields(r.Context(), resolved.ID, resolved.Slug)

			tx, err := options.Begin(r.Context())
			if err != nil {
				logger.ErrorContext(r.Context(), "tenant transaction begin failed", "error", err, "tenant_id", resolved.ID)
				api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
				return
			}

			rollback := func() {
				if err := tx.Rollback(r.Context()); err != nil {
					logger.WarnContext(r.Context(), "tenant transaction rollback failed", "error", err, "tenant_id", resolved.ID)
				}
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					rollback()
					panic(recovered)
				}
			}()

			var configuredTenant string
			if err := tx.QueryRow(r.Context(),
				`SELECT set_config('app.tenant_id', $1, true)`, resolved.ID,
			).Scan(&configuredTenant); err != nil || configuredTenant != resolved.ID {
				rollback()
				logger.ErrorContext(r.Context(), "tenant context setup failed", "error", err, "tenant_id", resolved.ID)
				api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
				return
			}

			ctx := context.WithValue(r.Context(), tenantContextKey{}, resolved)
			ctx = context.WithValue(ctx, querierContextKey{}, db.Querier(tx))
			hooks := &afterCommitHooks{}
			ctx = context.WithValue(ctx, afterCommitContextKey{}, hooks)
			control := &transactionControl{}
			ctx = context.WithValue(ctx, transactionControlContextKey{}, control)
			buffer := httpx.NewResponseBuffer()
			next.ServeHTTP(buffer, r.WithContext(ctx))

			if buffer.Status() >= http.StatusBadRequest && !control.commitOnError {
				rollback()
				buffer.FlushTo(w)
				return
			}
			if err := tx.Commit(r.Context()); err != nil {
				rollback()
				logger.ErrorContext(r.Context(), "tenant transaction commit failed", "error", err, "tenant_id", resolved.ID)
				api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
				return
			}
			for _, hook := range hooks.hooks {
				if err := hook(context.WithoutCancel(r.Context())); err != nil {
					logger.ErrorContext(r.Context(), "tenant post-commit hook failed", "error", err, "tenant_id", resolved.ID)
					api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
					return
				}
			}
			buffer.FlushTo(w)
		})
	}
}

func AfterTenantCommit(ctx context.Context, hook func(context.Context) error) bool {
	hooks, ok := ctx.Value(afterCommitContextKey{}).(*afterCommitHooks)
	if !ok || hook == nil {
		return false
	}
	hooks.hooks = append(hooks.hooks, hook)
	return true
}

func CommitTenantTransactionOnError(ctx context.Context) bool {
	control, ok := ctx.Value(transactionControlContextKey{}).(*transactionControl)
	if !ok {
		return false
	}
	control.commitOnError = true
	return true
}

func TenantFromContext(ctx context.Context) (tenant.Tenant, bool) {
	resolved, ok := ctx.Value(tenantContextKey{}).(tenant.Tenant)
	return resolved, ok
}

func QuerierFromContext(ctx context.Context) (db.Querier, bool) {
	querier, ok := ctx.Value(querierContextKey{}).(db.Querier)
	return querier, ok
}

func RequireActive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolved, ok := TenantFromContext(r.Context())
		if !ok {
			api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
			return
		}

		active := resolved.Status == tenant.StatusActive
		validTrial := resolved.Status == tenant.StatusTrial &&
			resolved.TrialEndsAt != nil && resolved.TrialEndsAt.After(time.Now())
		if !active && !validTrial {
			api.WriteError(w, http.StatusForbidden, api.CodeTenantSuspended, "tenant is not active", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireWritable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolved, ok := TenantFromContext(r.Context())
		if !ok {
			api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
			return
		}
		if resolved.DeletionRequestedAt != nil {
			api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "tenant is pending deletion and is read-only", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
