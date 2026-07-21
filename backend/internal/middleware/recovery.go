package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/httpx"
)

type PanicReporter interface {
	CapturePanic(context.Context, any) string
}

func Recovery(logger *slog.Logger, reporters ...PanicReporter) func(http.Handler) http.Handler {
	var reporter PanicReporter
	if len(reporters) > 0 {
		reporter = reporters[0]
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buffer := httpx.NewResponseBuffer()
			defer func() {
				if recovered := recover(); recovered != nil {
					eventID := ""
					if reporter != nil {
						eventID = reporter.CapturePanic(r.Context(), recovered)
					}
					logger.ErrorContext(r.Context(), "panic recovered",
						"request_id", RequestIDFromContext(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
						"sentry_event_id", eventID,
					)
					api.WriteErrorWithEventID(
						w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil, eventID,
					)
					return
				}
				buffer.FlushTo(w)
			}()

			next.ServeHTTP(buffer, r)
		})
	}
}
