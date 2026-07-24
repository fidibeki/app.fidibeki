package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrTurnstile = errors.New("turnstile verification failed")

type SignupVerifier interface {
	Verify(context.Context, string, string, string) error
}

type TurnstileVerifier struct {
	SecretKey       string
	ExpectedHost    string
	HTTPClient      *http.Client
	VerificationURL string
}

func (v TurnstileVerifier) Verify(ctx context.Context, token, remoteIP, idempotencyKey string) error {
	if len(token) == 0 || len(token) > 2048 {
		return ErrTurnstile
	}
	endpoint := v.VerificationURL
	if endpoint == "" {
		endpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	}
	form := url.Values{"secret": {v.SecretKey}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	if idempotencyKey != "" {
		form.Set("idempotency_key", idempotencyKey)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create Turnstile request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := v.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("verify Turnstile token: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("verify Turnstile token: unexpected status %d", response.StatusCode)
	}
	var result struct {
		Success  bool     `json:"success"`
		Hostname string   `json:"hostname"`
		Action   string   `json:"action"`
		Errors   []string `json:"error-codes"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode Turnstile response: %w", err)
	}
	if !result.Success || result.Action != "signup" || !v.hostAllowed(result.Hostname) {
		return ErrTurnstile
	}
	return nil
}

// hostAllowed accepts the base domain and its subdomains, since signup is
// served from the app subdomain while ExpectedHost carries BASE_DOMAIN.
func (v TurnstileVerifier) hostAllowed(hostname string) bool {
	if v.ExpectedHost == "" {
		return true
	}
	return strings.EqualFold(hostname, v.ExpectedHost) ||
		strings.HasSuffix(strings.ToLower(hostname), "."+strings.ToLower(v.ExpectedHost))
}

type LocalSignupVerifier struct{}

func (LocalSignupVerifier) Verify(context.Context, string, string, string) error { return nil }
