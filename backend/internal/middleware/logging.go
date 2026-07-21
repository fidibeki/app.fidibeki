package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type requestLogFieldsContextKey struct{}

type requestLogFields struct {
	tenantID   string
	tenantSlug string
}

type responseObserver struct {
	http.ResponseWriter
	status int
}

func (o *responseObserver) WriteHeader(status int) {
	if o.status != 0 {
		return
	}
	o.status = status
	o.ResponseWriter.WriteHeader(status)
}

func (o *responseObserver) Write(value []byte) (int, error) {
	if o.status == 0 {
		o.WriteHeader(http.StatusOK)
	}
	return o.ResponseWriter.Write(value)
}

func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			observer := &responseObserver{ResponseWriter: w}
			fields := &requestLogFields{}
			ctx := context.WithValue(r.Context(), requestLogFieldsContextKey{}, fields)
			r = r.WithContext(ctx)

			next.ServeHTTP(observer, r)

			status := observer.status
			if status == 0 {
				status = http.StatusOK
			}
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = r.URL.Path
			}
			attributes := []any{
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"route", route,
				"status", status,
				"duration_ms", time.Since(started).Milliseconds(),
			}
			if fields.tenantID != "" {
				attributes = append(attributes, "tenant_id", fields.tenantID, "tenant_slug", fields.tenantSlug)
			}
			logger.InfoContext(r.Context(), "request completed", attributes...)
		})
	}
}

func setTenantLogFields(ctx context.Context, tenantID, tenantSlug string) {
	fields, ok := ctx.Value(requestLogFieldsContextKey{}).(*requestLogFields)
	if !ok {
		return
	}
	fields.tenantID = tenantID
	fields.tenantSlug = tenantSlug
}
