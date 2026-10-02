// Package aiprovider owns workspace AI connections. Credentials never form part
// of its public Connection representation or queued work.
package aiprovider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	Google          = "google_gemini"
	OpenAI          = "openai"
	Anthropic       = "anthropic"
	Bedrock         = "amazon_bedrock"
	CustomOpenAI    = "openai_compatible"
	CustomAnthropic = "anthropic_compatible"
	Managed         = "managed"
	Customer        = "customer"
)

var (
	ErrNotFound    = errors.New("AI connection not found")
	ErrConflict    = errors.New("AI connection has changed; reload before saving")
	ErrInvalid     = errors.New("invalid AI connection")
	ErrDisabled    = errors.New("AI connection is disabled")
	ErrUnvalidated = errors.New("model inputs have not been validated for this connection")
)

type Provider struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Protocol string   `json:"protocol"`
	Inputs   []string `json:"input_categories"`
	Custom   bool     `json:"custom_endpoint"`
	Enabled  bool     `json:"enabled"`
}

func Providers() []Provider {
	return []Provider{
		{Google, "Google Gemini", "gemini", []string{"feedback_text", "screenshot", "audio", "video"}, false, true},
		{OpenAI, "OpenAI", "chat_completions", []string{"feedback_text", "screenshot"}, false, true},
		{Anthropic, "Anthropic", "messages", []string{"feedback_text", "screenshot"}, false, true},
		{Bedrock, "Amazon Bedrock", "converse", []string{"feedback_text", "screenshot"}, false, true},
		{CustomOpenAI, "OpenAI-compatible service", "chat_completions", []string{"feedback_text", "screenshot"}, true, true},
		{CustomAnthropic, "Anthropic-compatible service", "messages", []string{"feedback_text", "screenshot"}, true, true},
	}
}

func Lookup(id string) (Provider, bool) {
	for _, p := range Providers() {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

type Selection struct {
	BillingMode   string `json:"billing_mode,omitempty"`
	ConnectionID  string `json:"connection_id,omitempty"`
	ConfigVersion int64  `json:"config_version,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
}

type Model struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Inputs    []string `json:"input_categories"`
	Validated bool     `json:"validated"`
}

type Connection struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"-"`
	Name                string     `json:"name"`
	Provider            string     `json:"provider"`
	BaseURL             string     `json:"base_url,omitempty"`
	Region              string     `json:"region,omitempty"`
	AuthMode            string     `json:"auth_mode"`
	RoleARN             string     `json:"role_arn,omitempty"`
	ExternalID          string     `json:"external_id,omitempty"`
	CredentialVersion   int64      `json:"credential_version"`
	ConfigVersion       int64      `json:"config_version"`
	Revision            int64      `json:"revision"`
	Enabled             bool       `json:"enabled"`
	Deleted             bool       `json:"deleted"`
	RPM                 int        `json:"rpm"`
	Concurrency         int        `json:"concurrency"`
	DefaultModel        string     `json:"default_model,omitempty"`
	IsDefault           bool       `json:"is_default"`
	Models              []Model    `json:"models"`
	Health              string     `json:"health"`
	CredentialExpiresAt *time.Time `json:"credential_expires_at,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// Secret is deliberately separate from all API and queue representations.
type Secret struct {
	Value    string
	Version  int64
	Envelope []byte
}

type Input struct {
	Name                string     `json:"name"`
	Provider            string     `json:"provider"`
	BaseURL             string     `json:"base_url"`
	Region              string     `json:"region"`
	AuthMode            string     `json:"auth_mode"`
	RoleARN             string     `json:"role_arn"`
	Credential          string     `json:"credential"`
	CredentialExpiresAt *time.Time `json:"credential_expires_at"`
	Revision            int64      `json:"revision"`
	Enabled             bool       `json:"enabled"`
	RPM                 int        `json:"rpm"`
	Concurrency         int        `json:"concurrency"`
	DefaultModel        string     `json:"default_model"`
	IsDefault           bool       `json:"is_default"`
}

var regionPattern = regexp.MustCompile(`^[a-z]{2}(?:-gov)?-[a-z]+-\d$`)
var rolePattern = regexp.MustCompile(`^arn:aws:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$`)

func ValidateInput(input Input) (Input, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Credential = strings.TrimSpace(input.Credential)
	input.DefaultModel = strings.TrimSpace(input.DefaultModel)
	p, ok := Lookup(input.Provider)
	if !ok || input.Name == "" || len(input.Name) > 100 || len(input.Credential) > 16384 || !ValidModelID(input.DefaultModel, true) {
		return input, ErrInvalid
	}
	if input.RPM == 0 {
		input.RPM = 15
	}
	if input.Concurrency == 0 {
		input.Concurrency = 2
	}
	if input.RPM < 1 || input.RPM > 120 || input.Concurrency < 1 || input.Concurrency > 8 {
		return input, fmt.Errorf("%w: limits are out of range", ErrInvalid)
	}
	if p.Custom {
		u, err := url.Parse(strings.TrimSpace(input.BaseURL))
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return input, fmt.Errorf("%w: use a public HTTPS base URL without credentials, query or fragment", ErrInvalid)
		}
		if u.Port() != "" && u.Port() != "443" {
			return input, fmt.Errorf("%w: custom endpoints must use port 443", ErrInvalid)
		}
		input.BaseURL = strings.TrimRight(u.String(), "/")
	} else {
		input.BaseURL = ""
	}
	if input.Provider == Bedrock {
		if !regionPattern.MatchString(input.Region) {
			return input, fmt.Errorf("%w: AWS region is required", ErrInvalid)
		}
		if input.AuthMode == "role" {
			if !rolePattern.MatchString(input.RoleARN) {
				return input, fmt.Errorf("%w: customer IAM role ARN is required", ErrInvalid)
			}
		} else if input.AuthMode == "api_key" {
			if input.CredentialExpiresAt == nil || (input.Enabled && !input.CredentialExpiresAt.After(time.Now())) {
				return input, fmt.Errorf("%w: Bedrock pilot key expiry is required", ErrInvalid)
			}
		} else {
			return input, fmt.Errorf("%w: choose role or api_key authentication", ErrInvalid)
		}
	} else {
		input.AuthMode = "api_key"
		input.Region = ""
		input.RoleARN = ""
		input.CredentialExpiresAt = nil
	}
	return input, nil
}

func ValidModelID(id string, allowEmpty bool) bool {
	if id == "" {
		return allowEmpty
	}
	if len(id) > 512 {
		return false
	}
	for _, r := range id {
		if r < 33 || r > 126 || strings.ContainsRune(`"\\<>?#`, r) {
			return false
		}
	}
	return true
}

func Supports(inputs []string, category string) bool {
	for _, v := range inputs {
		if v == category {
			return true
		}
	}
	return false
}

type Tester interface {
	Models(context.Context, Connection, Secret) ([]Model, error)
	Validate(context.Context, Connection, Secret, string, []string) (Model, error)
}

// Failure contains only a bounded classification, never provider response text.
type Failure struct {
	Code       string
	Status     int
	RetryAfter time.Duration
	Retryable  bool
}

func (e *Failure) Error() string { return "AI provider: " + e.Code }
