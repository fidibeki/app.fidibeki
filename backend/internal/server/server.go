package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

type ReadinessCheck interface {
	Check(context.Context) error
}

type Options struct {
	Readiness           map[string]ReadinessCheck
	ReadinessTimeout    time.Duration
	Logger              *slog.Logger
	BaseDomain          string
	PlatformHandler     http.Handler
	TenantHandler       http.Handler
	WidgetHandler       http.Handler
	PanicReporter       appmiddleware.PanicReporter
	InternalProxySecret string
}

func New(options Options) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}

	router := chi.NewRouter()
	router.Use(appmiddleware.TrustedProxyHost(options.InternalProxySecret))
	router.Use(appmiddleware.RequestID)
	router.Use(appmiddleware.RequestLogger(logger))
	router.Use(appmiddleware.Recovery(logger, options.PanicReporter))
	router.Get("/healthz", healthz)
	router.Get("/readyz", readyz(options))
	dispatch := hostDispatcher(options)
	router.NotFound(dispatch.ServeHTTP)
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteError(w, http.StatusMethodNotAllowed, api.CodeBadRequest, "method not allowed", nil)
	})

	return router
}

func hostDispatcher(options Options) http.Handler {
	if options.BaseDomain == "" || (options.PlatformHandler == nil && options.TenantHandler == nil) {
		return apiNotFoundHandler()
	}
	platformHandler := options.PlatformHandler
	if platformHandler == nil {
		platformHandler = apiNotFoundHandler()
	}
	tenantHandler := options.TenantHandler
	if tenantHandler == nil {
		tenantHandler = apiNotFoundHandler()
	}
	widgetHandler := options.WidgetHandler
	if widgetHandler == nil {
		widgetHandler = apiNotFoundHandler()
	}
	baseDomain := strings.ToLower(strings.TrimSuffix(options.BaseDomain, "."))
	platformHost := "api." + baseDomain
	widgetHost := "widget." + baseDomain

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostname := requestHostname(r.Host)
		switch {
		case hostname == platformHost:
			platformHandler.ServeHTTP(w, r)
		case hostname == widgetHost:
			widgetHandler.ServeHTTP(w, r)
		case hostname == baseDomain:
			apiNotFoundHandler().ServeHTTP(w, r)
		default:
			if _, err := tenant.SlugFromHost(r.Host, baseDomain); err != nil {
				api.WriteError(w, http.StatusNotFound, api.CodeUnknownTenant, "unknown tenant", nil)
				return
			}
			tenantHandler.ServeHTTP(w, r)
		}
	})
}

func requestHostname(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		return parsed
	}
	if strings.Contains(host, ":") {
		return ""
	}
	return host
}

func apiNotFoundHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	})
}

func readyz(options Options) http.HandlerFunc {
	timeout := options.ReadinessTimeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}

	return func(w http.ResponseWriter, r *http.Request) {
		checks := options.Readiness
		if len(checks) == 0 {
			api.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "unavailable",
				"checks": map[string]string{"configuration": "unavailable"},
			})
			return
		}

		type checkResult struct {
			name string
			err  error
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		results := make(chan checkResult, len(checks))
		for name, check := range checks {
			go func() {
				results <- checkResult{name: name, err: check.Check(ctx)}
			}()
		}

		statuses := make(map[string]string, len(checks))
		allReady := true
		for range checks {
			select {
			case result := <-results:
				if result.err != nil {
					statuses[result.name] = "unavailable"
					allReady = false
				} else {
					statuses[result.name] = "ok"
				}
			case <-ctx.Done():
				allReady = false
				for name := range checks {
					if _, exists := statuses[name]; !exists {
						statuses[name] = "unavailable"
					}
				}
				api.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
					"status": "unavailable",
					"checks": statuses,
				})
				return
			}
		}

		statusCode := http.StatusOK
		status := "ok"
		if !allReady {
			statusCode = http.StatusServiceUnavailable
			status = "unavailable"
		}
		api.WriteJSON(w, statusCode, map[string]any{"status": status, "checks": statuses})
	}
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
