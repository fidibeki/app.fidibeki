package integration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStateStore is an in-memory stand-in for Redis that keeps the property the callback
// depends on: Consume succeeds at most once per token, even under concurrent callers.
type fakeStateStore struct {
	mutex  sync.Mutex
	states map[string]InstallState
	nextID int
	putErr error
}

func newFakeStateStore() *fakeStateStore {
	return &fakeStateStore{states: map[string]InstallState{}}
}

func (f *fakeStateStore) Create(_ context.Context, state InstallState, _ time.Duration) (string, error) {
	if f.putErr != nil {
		return "", f.putErr
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.nextID++
	token := "state-token-" + string(rune('a'+f.nextID-1))
	f.states[token] = state
	return token, nil
}

func (f *fakeStateStore) Consume(_ context.Context, token string) (InstallState, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	state, ok := f.states[token]
	if !ok {
		return InstallState{}, ErrInstallStateInvalid
	}
	delete(f.states, token)
	return state, nil
}

type stubDoer struct {
	response *http.Response
	err      error
	requests []*http.Request
	bodies   []string
}

func (s *stubDoer) Do(request *http.Request) (*http.Response, error) {
	body := ""
	if request.Body != nil {
		raw, _ := io.ReadAll(request.Body)
		body = string(raw)
	}
	s.requests = append(s.requests, request)
	s.bodies = append(s.bodies, body)
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func testSlackOAuth(doer HTTPDoer, states InstallStateStore) SlackOAuth {
	return SlackOAuth{
		ClientID: "client-id", ClientSecret: "client-secret",
		RedirectURL: "https://api.example.com" + SlackCallbackPath,
		Client:      doer, States: states,
	}
}

func TestSlackBeginBuildsAuthorizeURLAndRefusesWhenUnconfigured(t *testing.T) {
	t.Parallel()
	states := newFakeStateStore()
	oauth := testSlackOAuth(nil, states)
	authorizationVersion := 7

	authorizeURL, err := oauth.Begin(context.Background(), InstallState{
		TenantID: "tenant-1", TenantSlug: "acme", MemberID: "member-1",
		AuthorizationVersion: &authorizationVersion,
		Events:               []string{EventFeedbackCreated},
	})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != slackAuthorizeURL {
		t.Errorf("authorize endpoint = %q, want %q", parsed.Path, slackAuthorizeURL)
	}
	query := parsed.Query()
	if got := query.Get("scope"); got != SlackInstallScope {
		t.Errorf("scope = %q, want %q", got, SlackInstallScope)
	}
	if got := query.Get("client_id"); got != "client-id" {
		t.Errorf("client_id = %q", got)
	}
	if got := query.Get("redirect_uri"); got != oauth.RedirectURL {
		t.Errorf("redirect_uri = %q, want %q", got, oauth.RedirectURL)
	}
	// The tenant must not be discoverable or rewritable from the URL the user carries.
	if strings.Contains(authorizeURL, "tenant-1") || strings.Contains(authorizeURL, "acme") {
		t.Errorf("authorize URL leaks tenant identity: %q", authorizeURL)
	}
	state, err := states.Consume(context.Background(), query.Get("state"))
	if err != nil {
		t.Fatalf("state was not recorded: %v", err)
	}
	if state.TenantID != "tenant-1" || state.MemberID != "member-1" ||
		state.AuthorizationVersion == nil || *state.AuthorizationVersion != authorizationVersion {
		t.Errorf("recorded state = %+v", state)
	}
	if _, err := oauth.Begin(context.Background(), InstallState{
		TenantID: "tenant-1", TenantSlug: "acme", MemberID: "member-1",
	}); !errors.Is(err, ErrInstallAuthorityInvalid) {
		t.Errorf("Begin() without authority version error = %v, want ErrInstallAuthorityInvalid", err)
	}

	unconfigured := SlackOAuth{States: states}
	if _, err := unconfigured.Begin(context.Background(), InstallState{}); !errors.Is(err, ErrSlackNotConfigured) {
		t.Errorf("Begin() unconfigured error = %v, want ErrSlackNotConfigured", err)
	}
}

func TestSlackStateIsSingleUse(t *testing.T) {
	t.Parallel()
	states := newFakeStateStore()
	oauth := testSlackOAuth(nil, states)
	authorizationVersion := 0
	authorizeURL, err := oauth.Begin(context.Background(), InstallState{
		TenantID: "tenant-1", TenantSlug: "acme", MemberID: "member-1",
		AuthorizationVersion: &authorizationVersion,
	})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	token := mustQueryParam(t, authorizeURL, "state")

	if _, err := states.Consume(context.Background(), token); err != nil {
		t.Fatalf("first Consume() error = %v", err)
	}
	// A replayed callback URL must not install a second time.
	if _, err := states.Consume(context.Background(), token); !errors.Is(err, ErrInstallStateInvalid) {
		t.Errorf("second Consume() error = %v, want ErrInstallStateInvalid", err)
	}
	if _, err := states.Consume(context.Background(), "never-issued"); !errors.Is(err, ErrInstallStateInvalid) {
		t.Errorf("forged Consume() error = %v, want ErrInstallStateInvalid", err)
	}
}

func TestSlackExchangeReadsInstallationAndRejectsUnusableResponses(t *testing.T) {
	t.Parallel()
	success := `{"ok":true,"access_token":"xoxb-token","team":{"id":"T123","name":"Acme"},
		"incoming_webhook":{"url":"https://hooks.slack.com/services/T123/B456/secret",
		"channel":"#feedback","channel_id":"C789"}}`

	tests := []struct {
		name      string
		doer      *stubDoer
		wantCode  string
		wantError bool
	}{
		{name: "success", doer: &stubDoer{response: jsonResponse(http.StatusOK, success)}},
		{
			name:      "slack rejects the code",
			doer:      &stubDoer{response: jsonResponse(http.StatusOK, `{"ok":false,"error":"invalid_code"}`)},
			wantCode:  "invalid_code",
			wantError: true,
		},
		{
			// A 200 with ok:false is Slack's normal failure shape, so a bare status check
			// would treat a rejection as a successful install.
			name:      "unexpected error code is not echoed back",
			doer:      &stubDoer{response: jsonResponse(http.StatusOK, `{"ok":false,"error":"<script>x</script>"}`)},
			wantCode:  "unknown_error",
			wantError: true,
		},
		{
			name: "webhook URL from another host is refused",
			doer: &stubDoer{response: jsonResponse(http.StatusOK,
				`{"ok":true,"access_token":"t","team":{"id":"T1"},
				"incoming_webhook":{"url":"https://evil.example.com/hook","channel_id":"C1"}}`)},
			wantCode:  "invalid_webhook_url",
			wantError: true,
		},
		{
			name: "installation without a token is refused",
			doer: &stubDoer{response: jsonResponse(http.StatusOK,
				`{"ok":true,"team":{"id":"T1"},
				"incoming_webhook":{"url":"https://hooks.slack.com/services/a/b/c","channel_id":"C1"}}`)},
			wantCode:  "incomplete_installation",
			wantError: true,
		},
		{
			name:      "malformed body",
			doer:      &stubDoer{response: jsonResponse(http.StatusOK, `not json`)},
			wantCode:  "malformed_response",
			wantError: true,
		},
		{
			name:      "http failure",
			doer:      &stubDoer{response: jsonResponse(http.StatusBadGateway, ``)},
			wantCode:  "http_502",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			oauth := testSlackOAuth(tt.doer, newFakeStateStore())
			installation, err := oauth.Exchange(context.Background(), "the-code")
			if !tt.wantError {
				if err != nil {
					t.Fatalf("Exchange() error = %v", err)
				}
				if installation.WebhookURL != "https://hooks.slack.com/services/T123/B456/secret" {
					t.Errorf("WebhookURL = %q", installation.WebhookURL)
				}
				if installation.ChannelName != "#feedback" || installation.ChannelID != "C789" {
					t.Errorf("channel = %q/%q", installation.ChannelName, installation.ChannelID)
				}
				if installation.TeamID != "T123" || installation.TeamName != "Acme" {
					t.Errorf("team = %q/%q", installation.TeamID, installation.TeamName)
				}
				if installation.AccessToken != "xoxb-token" {
					t.Errorf("AccessToken = %q", installation.AccessToken)
				}
				// The secret must be posted in the body, never in the query string.
				if strings.Contains(tt.doer.requests[0].URL.RawQuery, "client_secret") {
					t.Error("client secret appeared in the query string")
				}
				if !strings.Contains(tt.doer.bodies[0], "code=the-code") {
					t.Errorf("request body = %q", tt.doer.bodies[0])
				}
				return
			}
			var slackErr *SlackError
			if !errors.As(err, &slackErr) {
				t.Fatalf("Exchange() error = %v, want *SlackError", err)
			}
			if slackErr.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", slackErr.Code, tt.wantCode)
			}
		})
	}
}

func TestSlackRevokeTreatsAlreadyRevokedTokensAsSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "revoked", body: `{"ok":true,"revoked":true}`},
		// The desired end state — Slack no longer honours the token — already holds.
		{name: "already invalid", body: `{"ok":false,"error":"invalid_auth"}`},
		{name: "already revoked", body: `{"ok":false,"error":"token_revoked"}`},
		{name: "genuine failure", body: `{"ok":false,"error":"internal_error"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doer := &stubDoer{response: jsonResponse(http.StatusOK, tt.body)}
			oauth := testSlackOAuth(doer, newFakeStateStore())
			err := oauth.Revoke(context.Background(), "xoxb-token")
			if tt.wantErr && err == nil {
				t.Error("Revoke() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Revoke() error = %v", err)
			}
		})
	}

	// An unconfigured deployment has nothing to revoke and must not fail the delete.
	unconfigured := SlackOAuth{}
	if err := unconfigured.Revoke(context.Background(), "xoxb-token"); err != nil {
		t.Errorf("unconfigured Revoke() error = %v", err)
	}
}

// integrations.name has a one-character floor in the schema, so a Slack response with no
// channel or workspace name must still produce a storable name rather than failing the
// install on a database constraint.
func TestSlackInstallNameFallsBackWhenSlackSendsNoNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		channelName string
		teamName    string
		channelID   string
		want        string
	}{
		{name: "channel name preferred", channelName: "#feedback", teamName: "Acme", channelID: "C1", want: "#feedback"},
		{name: "falls back to workspace", teamName: "Acme", channelID: "C1", want: "Acme"},
		{name: "falls back to channel id", channelID: "C1", want: "C1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := firstNonEmpty(tt.channelName, tt.teamName, tt.channelID)
			if got != tt.want {
				t.Errorf("name = %q, want %q", got, tt.want)
			}
			if got == "" {
				t.Error("name is empty, which the schema rejects")
			}
		})
	}
}

func TestSlackWebhookURLValidation(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"https://hooks.slack.com/services/T/B/x": true,
		"https://HOOKS.SLACK.COM/services/T/B/x": true,
		"http://hooks.slack.com/services/T/B/x":  false,
		"https://hooks.slack.com.evil.test/x":    false,
		"https://evil.test/hooks.slack.com":      false,
		"not-a-url":                              false,
		"":                                       false,
	}
	for value, want := range tests {
		if got := validSlackWebhookURL(value); got != want {
			t.Errorf("validSlackWebhookURL(%q) = %v, want %v", value, got, want)
		}
	}
}

// The install callback is reachable without a session, so a Slack-supplied error string is
// never passed through to a browser or a log line unfiltered.
func TestSlackErrorCodesAreConstrained(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"invalid_code":              "invalid_code",
		"":                          "unknown_error",
		"has spaces":                "unknown_error",
		"<script>alert(1)</script>": "unknown_error",
		"UPPER":                     "unknown_error",
		strings.Repeat("a", 61):     "unknown_error",
	}
	for input, want := range tests {
		if got := fallbackSlackCode(input); got != want {
			t.Errorf("fallbackSlackCode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSlackExchangeAndRevokeAgainstAServer(t *testing.T) {
	t.Parallel()
	// One real round trip over HTTP, so the form encoding and headers are exercised for
	// real rather than only against a stub.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", got)
		}
		switch r.URL.Path {
		case "/api/oauth.v2.access":
			if r.PostForm.Get("client_secret") != "client-secret" {
				t.Errorf("client_secret = %q", r.PostForm.Get("client_secret"))
			}
			if r.PostForm.Get("code") != "code" {
				t.Errorf("code = %q", r.PostForm.Get("code"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxb","team":{"id":"T1","name":"Acme"},
				"incoming_webhook":{"url":"https://hooks.slack.com/services/a/b/c",
				"channel":"#general","channel_id":"C1"}}`))
		case "/api/auth.revoke":
			// Revoking authenticates with the bot token itself, not the app secret.
			if r.PostForm.Get("token") != "xoxb" {
				t.Errorf("token = %q", r.PostForm.Get("token"))
			}
			if r.PostForm.Get("client_secret") != "" {
				t.Error("revoke must not carry the app secret")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"revoked":true}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	// Point the client at the test server by overriding the destination through a doer that
	// rewrites scheme and host, keeping the production endpoint constants untouched.
	oauth := testSlackOAuth(
		rewritingDoer{base: server.URL, client: server.Client()}, newFakeStateStore(),
	)
	installation, err := oauth.Exchange(context.Background(), "code")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if installation.ChannelName != "#general" || installation.TeamName != "Acme" {
		t.Errorf("installation = %+v", installation)
	}
	if err := oauth.Revoke(context.Background(), "xoxb"); err != nil {
		t.Errorf("Revoke() error = %v", err)
	}
}

// rewritingDoer sends every request to a test server while leaving the production endpoint
// constants in place.
type rewritingDoer struct {
	base   string
	client *http.Client
}

func (d rewritingDoer) Do(request *http.Request) (*http.Response, error) {
	target, err := url.Parse(d.base)
	if err != nil {
		return nil, err
	}
	request.URL.Scheme = target.Scheme
	request.URL.Host = target.Host
	return d.client.Do(request)
}

func mustQueryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	value := parsed.Query().Get(key)
	if value == "" {
		t.Fatalf("missing %q in %q", key, rawURL)
	}
	return value
}
