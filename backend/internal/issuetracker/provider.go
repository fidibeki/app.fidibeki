package issuetracker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// HTTPDoer is the seam every provider client is built on so tests can drive them against an
// httptest server without reaching the real Jira or Linear API.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// providerTimeout bounds a single provider call. The tenant transaction stays open for the
// duration of the call, so this is deliberately short.
const providerTimeout = 15 * time.Second

// maxProviderResponse caps how much of a provider response is read into memory.
const maxProviderResponse = 1 << 20

// ProviderError is a provider failure translated into something a workspace administrator can
// act on. Message never contains the credential, the request body, or a raw provider payload
// beyond the provider's own error text.
type ProviderError struct {
	Code      string
	Message   string
	Status    int
	Retryable bool
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Provider error codes surfaced to the API and mapped to copy in the web app.
const (
	ErrorCodeUnauthorized  = "provider_unauthorized"
	ErrorCodeForbidden     = "provider_forbidden"
	ErrorCodeNotFound      = "provider_not_found"
	ErrorCodeRejected      = "provider_rejected_request"
	ErrorCodeRateLimited   = "provider_rate_limited"
	ErrorCodeUnavailable   = "provider_unavailable"
	ErrorCodeUnreachable   = "provider_unreachable"
	ErrorCodeMalformed     = "provider_response_malformed"
	ErrorCodeNoDestination = "provider_destination_missing"
)

func providerError(code, message string, status int, retryable bool) *ProviderError {
	return &ProviderError{Code: code, Message: truncate(message, 500), Status: status, Retryable: retryable}
}

// statusError maps an HTTP status to the shared error vocabulary so Jira and Linear failures
// read the same way in the UI.
func statusError(status int, detail string) *ProviderError {
	switch {
	case status == http.StatusUnauthorized:
		return providerError(ErrorCodeUnauthorized,
			fallback(detail, "the stored credential was rejected"), status, false)
	case status == http.StatusForbidden:
		return providerError(ErrorCodeForbidden,
			fallback(detail, "the stored credential is not allowed to perform this action"), status, false)
	case status == http.StatusNotFound:
		return providerError(ErrorCodeNotFound,
			fallback(detail, "the requested resource does not exist"), status, false)
	case status == http.StatusTooManyRequests:
		return providerError(ErrorCodeRateLimited,
			fallback(detail, "the provider is rate limiting this workspace"), status, true)
	case status >= 500:
		return providerError(ErrorCodeUnavailable,
			fallback(detail, "the provider is temporarily unavailable"), status, true)
	default:
		return providerError(ErrorCodeRejected,
			fallback(detail, "the provider rejected the request"), status, false)
	}
}

func fallback(value, alternative string) string {
	if strings.TrimSpace(value) == "" {
		return alternative
	}
	return value
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit]))
}

// send performs one provider request with a bounded timeout and reads a bounded response body.
func send(ctx context.Context, client HTTPDoer, request *http.Request) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, nil, providerError(ErrorCodeUnreachable, "the provider did not respond in time", 0, true)
		}
		return 0, nil, providerError(ErrorCodeUnreachable, "the provider could not be reached", 0, true)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponse))
	if err != nil {
		return response.StatusCode, nil, providerError(ErrorCodeMalformed,
			"the provider response could not be read", response.StatusCode, true)
	}
	return response.StatusCode, body, nil
}

func defaultHTTPClient() HTTPDoer {
	return &http.Client{Timeout: providerTimeout}
}

// ValidateSiteURL accepts the Jira site a workspace pastes in. Only HTTPS is allowed outside
// tests; allowInsecure mirrors the integrations package so an httptest server can be used.
func ValidateSiteURL(value string, allowInsecure bool) (string, bool) {
	value = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(value), "/"))
	if value == "" || len(value) > 2048 {
		return "", false
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		parsed.Fragment != "" || parsed.RawQuery != "" || strings.Trim(parsed.Path, "/") != "" {
		return "", false
	}
	switch parsed.Scheme {
	case "https":
		return parsed.Scheme + "://" + parsed.Host, true
	case "http":
		if !allowInsecure {
			return "", false
		}
		// Local and test environments may point Jira at a stub. host.docker.internal is
		// included because the API runs in a container while the stub runs on the host.
		host := parsed.Hostname()
		if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "host.docker.internal") ||
			net.ParseIP(host).IsLoopback() {
			return parsed.Scheme + "://" + parsed.Host, true
		}
		return "", false
	default:
		return "", false
	}
}

// NewClient returns the client for a provider. baseURL overrides the provider endpoint and is
// only set by tests; production callers pass an empty string.
func NewClient(provider Provider, httpClient HTTPDoer, baseURL string) (Client, error) {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	switch provider {
	case ProviderJira:
		return JiraClient{HTTP: httpClient}, nil
	case ProviderLinear:
		return LinearClient{HTTP: httpClient, Endpoint: baseURL}, nil
	default:
		return nil, fmt.Errorf("unsupported issue tracker provider %q", provider)
	}
}

// ClientFactory builds the client for a provider. The HTTP handler holds one so tests can
// substitute fakes for both providers at once.
type ClientFactory func(provider Provider) (Client, error)

// DefaultClientFactory is the production factory.
func DefaultClientFactory(httpClient HTTPDoer) ClientFactory {
	return func(provider Provider) (Client, error) {
		return NewClient(provider, httpClient, "")
	}
}
