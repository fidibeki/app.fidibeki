package integration

// Slack one-click install. Requesting only the incoming-webhook scope makes Slack render
// its own channel picker during the install and hand back a channel-scoped webhook URL —
// exactly what integrations.webhook_url already holds, so nothing downstream of the install
// changes. The bot token is kept solely so an uninstall can revoke it.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	slackAuthorizeURL = "https://slack.com/oauth/v2/authorize"
	slackAccessURL    = "https://slack.com/api/oauth.v2.access"
	slackRevokeURL    = "https://slack.com/api/auth.revoke"

	// SlackInstallScope is the only scope requested. It cannot read messages, and it is
	// what triggers the channel picker in Slack's own install screen.
	SlackInstallScope = "incoming-webhook"

	// slackWebhookHost is the only host Slack issues incoming webhooks on. Checking it
	// keeps a malformed or unexpected response from becoming a delivery destination.
	slackWebhookHost = "hooks.slack.com"

	slackStateTTL       = 10 * time.Minute
	slackRequestTimeout = 15 * time.Second
	maxSlackResponse    = 1 << 20
)

var (
	// ErrInstallStateInvalid covers a forged, expired, or already-used state token. The
	// three are deliberately indistinguishable to the caller.
	ErrInstallStateInvalid = errors.New("slack install state is invalid or expired")
	// ErrInstallAuthorityInvalid means the member or workspace no longer has
	// the authority captured when the OAuth flow began.
	ErrInstallAuthorityInvalid = errors.New("slack install authority is no longer valid")
	// ErrSlackNotConfigured means the deployment has no Slack app credentials, so the
	// one-click path is unavailable while the manual path keeps working.
	ErrSlackNotConfigured = errors.New("slack one-click install is not configured")
)

// SlackError is a Slack-side rejection translated into something an administrator can act
// on. It never carries the code, the token, or a raw Slack payload.
type SlackError struct {
	Code string
}

func (e *SlackError) Error() string { return "slack rejected the install: " + e.Code }

// InstallState is the tenant-scoped intent recorded before the browser leaves for Slack.
// It is held server-side and referenced by an opaque token, so nothing in the URL the user
// carries to Slack and back can be tampered with to reach another workspace.
type InstallState struct {
	TenantID   string `json:"tenant_id"`
	TenantSlug string `json:"tenant_slug"`
	MemberID   string `json:"member_id"`
	// AuthorizationVersion is required so any permission or lifecycle mutation
	// invalidates an already-issued OAuth state.
	AuthorizationVersion *int     `json:"authorization_version"`
	Events               []string `json:"events"`
	PrivacyFields        []string `json:"privacy_fields"`
}

// InstallStateStore hands out single-use state tokens. Consume must return a given token at
// most once, however many callers race for it — that is what makes the callback replay-proof.
type InstallStateStore interface {
	Create(ctx context.Context, state InstallState, ttl time.Duration) (string, error)
	Consume(ctx context.Context, token string) (InstallState, error)
}

// SlackOAuth performs the install handshake against Slack.
type SlackOAuth struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Client       HTTPDoer
	States       InstallStateStore
}

// Configured reports whether this deployment can offer one-click install at all.
func (s SlackOAuth) Configured() bool {
	return s.ClientID != "" && s.ClientSecret != "" && s.RedirectURL != "" && s.States != nil
}

// SlackInstallation is what Slack returns once the user has picked a channel.
type SlackInstallation struct {
	TeamID      string
	TeamName    string
	ChannelID   string
	ChannelName string
	WebhookURL  string
	AccessToken string
}

// Begin records the install intent and returns the Slack URL to send the browser to.
func (s SlackOAuth) Begin(ctx context.Context, state InstallState) (string, error) {
	if !s.Configured() {
		return "", ErrSlackNotConfigured
	}
	if state.TenantID == "" || state.TenantSlug == "" || state.MemberID == "" ||
		state.AuthorizationVersion == nil {
		return "", ErrInstallAuthorityInvalid
	}
	token, err := s.States.Create(ctx, state, slackStateTTL)
	if err != nil {
		return "", fmt.Errorf("record slack install state: %w", err)
	}
	query := url.Values{}
	query.Set("client_id", s.ClientID)
	query.Set("scope", SlackInstallScope)
	query.Set("redirect_uri", s.RedirectURL)
	query.Set("state", token)
	return slackAuthorizeURL + "?" + query.Encode(), nil
}

// Exchange trades the one-time code for the installation. A Slack-level rejection arrives as
// HTTP 200 with ok:false, so the body is always inspected rather than just the status.
func (s SlackOAuth) Exchange(ctx context.Context, code string) (SlackInstallation, error) {
	if !s.Configured() {
		return SlackInstallation{}, ErrSlackNotConfigured
	}
	form := url.Values{}
	form.Set("client_id", s.ClientID)
	form.Set("client_secret", s.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", s.RedirectURL)

	var payload struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		Team        struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
		IncomingWebhook struct {
			URL       string `json:"url"`
			Channel   string `json:"channel"`
			ChannelID string `json:"channel_id"`
		} `json:"incoming_webhook"`
	}
	if err := s.post(ctx, slackAccessURL, form, &payload); err != nil {
		return SlackInstallation{}, err
	}
	if !payload.OK {
		return SlackInstallation{}, &SlackError{Code: fallbackSlackCode(payload.Error)}
	}
	installation := SlackInstallation{
		TeamID:      strings.TrimSpace(payload.Team.ID),
		TeamName:    strings.TrimSpace(payload.Team.Name),
		ChannelID:   strings.TrimSpace(payload.IncomingWebhook.ChannelID),
		ChannelName: strings.TrimSpace(payload.IncomingWebhook.Channel),
		WebhookURL:  strings.TrimSpace(payload.IncomingWebhook.URL),
		AccessToken: strings.TrimSpace(payload.AccessToken),
	}
	if !validSlackWebhookURL(installation.WebhookURL) {
		return SlackInstallation{}, &SlackError{Code: "invalid_webhook_url"}
	}
	if installation.TeamID == "" || installation.ChannelID == "" || installation.AccessToken == "" {
		return SlackInstallation{}, &SlackError{Code: "incomplete_installation"}
	}
	return installation, nil
}

// Revoke releases the bot token so uninstalling in Fidibeki also uninstalls in Slack. A
// token Slack has already forgotten is not an error — the desired end state is reached.
func (s SlackOAuth) Revoke(ctx context.Context, accessToken string) error {
	if !s.Configured() || accessToken == "" {
		return nil
	}
	form := url.Values{}
	form.Set("token", accessToken)
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := s.post(ctx, slackRevokeURL, form, &payload); err != nil {
		return err
	}
	if !payload.OK && payload.Error != "invalid_auth" && payload.Error != "token_revoked" {
		return &SlackError{Code: fallbackSlackCode(payload.Error)}
	}
	return nil
}

func (s SlackOAuth) post(ctx context.Context, endpoint string, form url.Values, out any) error {
	ctx, cancel := context.WithTimeout(ctx, slackRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("create slack request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: slackRequestTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("call slack: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &SlackError{Code: fmt.Sprintf("http_%d", response.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSlackResponse))
	if err != nil {
		return fmt.Errorf("read slack response: %w", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &SlackError{Code: "malformed_response"}
	}
	return nil
}

func validSlackWebhookURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), slackWebhookHost)
}

func fallbackSlackCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "unknown_error"
	}
	// Slack error codes are short snake_case tokens; anything else is not echoed back.
	if len(code) > 60 {
		return "unknown_error"
	}
	for _, character := range code {
		letter := character >= 'a' && character <= 'z'
		if !letter && character != '_' {
			return "unknown_error"
		}
	}
	return code
}

// NewInstallStateToken mints the opaque, unguessable reference held in the OAuth state
// parameter. The token is the only thing that travels through the browser and Slack.
func NewInstallStateToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", fmt.Errorf("generate slack install state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
