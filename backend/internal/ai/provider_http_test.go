package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/aiprovider"
	"github.com/hibiken/asynq"
)

const validProviderJSON = `{"transcription":"","cleaned_text":"Save fails","sentiment_score":-0.5,"moderation_flags":[],"suggested_title":"Fix save","actionability":"actionable","category":"editor","priority":"high"}`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderAdaptersPreserveModelAuthAndStructuredOutput(t *testing.T) {
	for _, provider := range []string{aiprovider.OpenAI, aiprovider.CustomOpenAI, aiprovider.Anthropic, aiprovider.CustomAnthropic, aiprovider.Bedrock} {
		t.Run(provider, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var payload map[string]any
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if provider != aiprovider.Bedrock && payload["model"] != "test-model" {
					t.Fatalf("wrong model: %#v", payload["model"])
				}
				var response any
				switch provider {
				case aiprovider.Anthropic, aiprovider.CustomAnthropic:
					if req.Header.Get("x-api-key") != "fixture-secret" || req.Header.Get("anthropic-version") == "" {
						t.Fatal("missing Anthropic auth")
					}
					response = map[string]any{"model": "resolved-model", "stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": validProviderJSON}}, "usage": map[string]any{"input_tokens": 12, "output_tokens": 34}}
				case aiprovider.Bedrock:
					if !strings.HasSuffix(req.URL.Path, "/model/test-model/converse") {
						t.Fatal(req.URL.Path)
					}
					response = map[string]any{"stopReason": "tool_use", "output": map[string]any{"message": map[string]any{"content": []any{map[string]any{"toolUse": map[string]any{"name": "feedback_analysis", "input": json.RawMessage(validProviderJSON)}}}}}, "usage": map[string]any{"inputTokens": 12, "outputTokens": 34}}
				default:
					if req.Header.Get("Authorization") != "Bearer fixture-secret" {
						t.Fatal("missing OpenAI auth")
					}
					response = map[string]any{"model": "resolved-model", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": validProviderJSON}}}, "usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 34}}
				}
				body, _ := json.Marshal(response)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: http.Header{}}, nil
			})}
			p := &providerHTTP{client: client, connection: aiprovider.Connection{Provider: provider, BaseURL: "https://custom.example/v1", Region: "eu-west-1"}, secret: aiprovider.Secret{Value: "fixture-secret"}, model: "test-model"}
			result, err := p.AnalyzeText(context.Background(), "Save fails")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.AnalyzeAttachment(context.Background(), AttachmentWork{Kind: "screenshot", ContentType: "image/png"}, strings.NewReader("synthetic-image")); err != nil {
				t.Fatal(err)
			}
			if result.Priority != "high" || result.Usage.Input == nil || *result.Usage.Input != 12 || *result.Usage.Output != 34 {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestStructuredResultRequiresCompleteBoundedSchema(t *testing.T) {
	for _, input := range []string{`{}`, strings.Replace(validProviderJSON, `"priority":"high"`, `"priority":null`, 1), strings.Replace(validProviderJSON, `"priority":"high"`, `"priority":"invented"`, 1), strings.Replace(validProviderJSON, `"sentiment_score":-0.5`, `"sentiment_score":2`, 1), strings.Replace(validProviderJSON, `"moderation_flags":[]`, `"moderation_flags":["invented"]`, 1), validProviderJSON + `{}`, strings.Replace(validProviderJSON, `"cleaned_text":"Save fails"`, `"cleaned_text":"`+strings.Repeat("a", 65537)+`"`, 1)} {
		if _, err := decodeProviderResult(input); !errors.Is(err, ErrUnsafeAIResult) {
			t.Errorf("accepted malformed result or unsafe error: %v", err)
		}
	}
	if _, err := decodeProviderResult(validProviderJSON); err != nil {
		t.Fatal(err)
	}
}

func TestProviderErrorsNeverExposeResponseContent(t *testing.T) {
	err := providerFailure(429, "120", []byte(`{"error":{"code":"insufficient_quota","message":"secret-key reporter@example.com"}}`))
	var failure *aiprovider.Failure
	if !errors.As(err, &failure) || failure.Code != "quota_exhausted" || failure.Retryable || failure.RetryAfter != 2*time.Minute {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "reporter@") {
		t.Fatal("provider response leaked")
	}
	err = providerFailure(503, "999999", nil)
	if !errors.As(err, &failure) || !failure.Retryable || failure.RetryAfter != 5*time.Minute {
		t.Fatal(err)
	}
}

func TestAuthorizationTransportAllowsCleanupAfterDisable(t *testing.T) {
	calls := 0
	transport := authorizedTransport{next: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil
	}), check: func(context.Context) error { return aiprovider.ErrDisabled }}
	post, _ := http.NewRequest(http.MethodPost, "https://provider.example", nil)
	if _, err := transport.RoundTrip(post); err == nil || calls != 0 {
		t.Fatal("disabled inference reached provider")
	}
	cleanup, _ := http.NewRequest(http.MethodDelete, "https://provider.example/files/a", nil)
	if _, err := transport.RoundTrip(cleanup); err != nil || calls != 1 {
		t.Fatal("cleanup was blocked by inference authorization")
	}
}

func TestCustomerConnectionNeverFallsBackToManaged(t *testing.T) {
	f := &ProviderFactory{ManagedKey: "managed-secret", Allowed: func(string, string) bool { return false }}
	_, _, _, err := f.Resolve(context.Background(), "tenant", Execution{Selection: aiprovider.Selection{BillingMode: aiprovider.Customer, Provider: aiprovider.Google, ConnectionID: "missing"}}, "feedback_text", nil)
	if !errors.Is(err, aiprovider.ErrDisabled) {
		t.Fatalf("missing customer connection did not fail closed: %v", err)
	}
}

type legacyInspectorFixture struct{ found bool }

func (l legacyInspectorFixture) GetTaskInfo(string, string) (*asynq.TaskInfo, error) {
	if l.found {
		return &asynq.TaskInfo{}, nil
	}
	return nil, asynq.ErrTaskNotFound
}
func (l legacyInspectorFixture) Close() error { return nil }
func TestQueueDoesNotCloneLegacyWorkDuringDrain(t *testing.T) {
	client := &fakeTaskClient{}
	q := NewQueue(client)
	q.legacy = legacyInspectorFixture{found: true}
	task := PreparedTask{Kind: TaskKindText, ResourceID: "fixture", Generation: 1}
	if err := q.Enqueue(context.Background(), task); err != nil || client.task != nil {
		t.Fatal("legacy task cloned", err)
	}
	q.legacy = legacyInspectorFixture{}
	if err := q.Enqueue(context.Background(), task); err != nil || client.task == nil {
		t.Fatal("new task not queued", err)
	}
}

func TestProviderResponseFailuresDoNotRetainPrivateContent(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{"redirect", 302, `secret response`, "redirect_rejected"},
		{"credentials", 401, `secret response`, "invalid_credentials"},
		{"oversized", 200, strings.Repeat("x", maxProviderResponse+1), "response_too_large"},
		{"malformed", 200, `not JSON secret response`, "invalid_response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := providerHTTP{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}, connection: aiprovider.Connection{Provider: aiprovider.OpenAI}, model: "fixture"}
			_, err := p.AnalyzeText(context.Background(), "private feedback")
			var failure *aiprovider.Failure
			if !errors.As(err, &failure) || failure.Code != tc.code || strings.Contains(err.Error(), "secret") {
				t.Fatal("unexpected or unsafe response", err)
			}
		})
	}
	for _, original := range []error{context.Canceled, context.DeadlineExceeded, &aiprovider.Failure{Code: "credential_changed", Retryable: true}, errors.New("secret network detail")} {
		p := providerHTTP{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, original })}, connection: aiprovider.Connection{Provider: aiprovider.OpenAI}, model: "fixture"}
		_, err := p.AnalyzeText(context.Background(), "private feedback")
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe transport error", err)
		}
	}
	p := providerHTTP{client: &http.Client{}, connection: aiprovider.Connection{Provider: aiprovider.OpenAI}}
	for _, work := range []AttachmentWork{{Kind: "audio"}, {Kind: "screenshot", ContentType: "image/svg+xml"}, {Kind: "screenshot", ContentType: "image/png"}} {
		_, err := p.AnalyzeAttachment(context.Background(), work, strings.NewReader(strings.Repeat("x", maxProviderImage+1)))
		if err == nil {
			t.Fatal("unsupported or oversized attachment accepted")
		}
	}
	p.sign = func(context.Context, *http.Request, []byte) error {
		return &aiprovider.Failure{Code: "credential_expired"}
	}
	if err := p.request(context.Background(), http.MethodPost, "https://bedrock.example", nil, new(any)); err == nil || !strings.Contains(err.Error(), "credential_expired") {
		t.Fatal("unsigned request escaped", err)
	}
	if err := p.request(context.Background(), http.MethodPost, ":bad URL", nil, new(any)); err == nil {
		t.Fatal("invalid URL accepted")
	}
	if err := p.request(context.Background(), http.MethodPost, "https://provider.example", make(chan int), new(any)); err == nil {
		t.Fatal("unencodable body accepted")
	}
}
