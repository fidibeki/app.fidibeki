package ai

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/aiprovider"
	"github.com/fidibeki/app.fidibeki/backend/internal/outboundhttp"
	"github.com/hibiken/asynq"
)

type providerResolver struct{}

func (providerResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}
func fixtureProviderFactory(t *testing.T, handler http.HandlerFunc) *ProviderFactory {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &ProviderFactory{Outbound: outboundhttp.Policy{Resolver: providerResolver{}, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}}
}

func TestModelDiscoveryCachesByCredentialAndKeepsManualValidation(t *testing.T) {
	calls := 0
	f := fixtureProviderFactory(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing credential")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"z-model"},{"id":"bad model"}],"has_more":true,"last_id":"z-model"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"a-model","display_name":"Model A"}],"has_more":false}`))
	})
	c := aiprovider.Connection{ID: "account", TenantID: "tenant", ConfigVersion: 1, CredentialVersion: 1, Provider: aiprovider.CustomOpenAI, BaseURL: "http://fixture.example/v1", Models: []aiprovider.Model{{ID: "manual", Validated: true, Inputs: []string{"feedback_text"}}, {ID: "z-model", Validated: true, Inputs: []string{"screenshot"}}}}
	key := aiprovider.Secret{Value: "fixture"}
	models, err := f.Models(context.Background(), c, key)
	if err != nil || len(models) != 3 || models[0].ID != "a-model" || !models[1].Validated || models[2].ID != "manual" {
		t.Fatalf("model merge: %#v %v", models, err)
	}
	if _, err = f.Models(context.Background(), c, key); err != nil || calls != 2 {
		t.Fatal("cache missed", calls, err)
	}
	c.CredentialVersion++
	if _, err = f.Models(context.Background(), c, key); err != nil || calls != 4 {
		t.Fatal("rotated key reused old catalog", calls, err)
	}
	c.Provider = aiprovider.Bedrock
	c.ID = "bedrock"
	c.AuthMode = "api_key"
	if _, err = f.Models(context.Background(), c, key); err == nil {
		t.Fatal("pilot key unexpectedly discovered AWS catalog")
	}
}

func TestSyntheticValidationRequiresVisualEvidenceAndSupportedInputs(t *testing.T) {
	visual := true
	f := fixtureProviderFactory(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "exact-model" {
			t.Error("model selection changed")
		}
		result := validProviderJSON
		if visual {
			result = strings.Replace(result, `"transcription":""`, `"transcription":"FIDIBEKI"`, 1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": result}}}})
	})
	c := aiprovider.Connection{Provider: aiprovider.CustomOpenAI, BaseURL: "http://fixture.example/v1"}
	key := aiprovider.Secret{Value: "fixture"}
	model, err := f.Validate(context.Background(), c, key, "exact-model", []string{"feedback_text", "screenshot", "screenshot"})
	if err != nil || !model.Validated || len(model.Inputs) != 2 {
		t.Fatalf("synthetic validation: %#v %v", model, err)
	}
	visual = false
	if _, err = f.Validate(context.Background(), c, key, "exact-model", []string{"screenshot"}); err == nil || !strings.Contains(err.Error(), "screenshot_validation_failed") {
		t.Fatal("blind model accepted", err)
	}
	for _, kind := range []string{"audio", "video", "unknown"} {
		if _, err = f.Validate(context.Background(), c, key, "exact-model", []string{kind}); err == nil || !strings.Contains(err.Error(), "unsupported_input") {
			t.Fatal("unsupported input accepted", kind, err)
		}
	}
	if !bytes.HasPrefix(validationAudio(), []byte("RIFF")) || len(validationVideo) == 0 {
		t.Fatal("missing media probes")
	}
}

func TestProviderFailuresRemainBoundedAndControlRetries(t *testing.T) {
	for _, status := range []int{301, 400, 401, 403, 404, 408, 429, 500, 504} {
		err := safeProviderError(providerFailure(status, "120", []byte(`{"message":"private feedback"}`)))
		if strings.Contains(err.Error(), "private feedback") {
			t.Fatal("provider text leaked")
		}
		retry := status == 408 || status == 429 || status >= 500
		if IsPermanent(err) == retry {
			t.Fatalf("status %d permanent=%v", status, IsPermanent(err))
		}
	}
	for _, err := range []error{aiprovider.ErrDisabled, aiprovider.ErrNotFound, aiprovider.ErrUnvalidated, aiprovider.ErrInvalid, &aiprovider.Failure{Code: "invalid_credentials"}} {
		if !IsPermanent(classifyConnectionError(err)) {
			t.Fatal("terminal failure was retryable", err)
		}
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		if !errors.Is(safeProviderError(err), err) {
			t.Fatal("cancellation lost")
		}
	}
	if safeProviderError(nil) != nil || !IsPermanent(safeProviderError(ErrUnsafeAIResult)) || !IsPermanent(safeProviderError(permanent(errors.New("secret")))) {
		t.Fatal("unsafe result not terminal")
	}
	if strings.Contains(safeProviderError(errors.New("secret")).Error(), "secret") {
		t.Fatal("unexpected error leaked")
	}
	task := asynq.NewTask("test", nil)
	wait := time.Hour
	if got := RetryDelay(1, &aiprovider.Failure{Code: "rate_limited", Retryable: true, RetryAfter: wait}, task); got < wait {
		t.Fatal("Retry-After ignored")
	}
	if RetryDelay(1, errors.New("ordinary"), task) <= 0 {
		t.Fatal("normal backoff missing")
	}
}

func TestManagedResolutionPinsModelAndEncryptsCleanupCredential(t *testing.T) {
	keys, err := aiprovider.NewKeyring("v1", map[string]string{"v1": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))})
	if err != nil {
		t.Fatal(err)
	}
	f := &ProviderFactory{ManagedKey: "fixture-managed-key", ManagedModel: "new-default", Connections: aiprovider.Store{Keys: keys}}
	selection := Execution{Selection: aiprovider.Selection{BillingMode: aiprovider.Managed, Provider: aiprovider.Google, Model: "pinned-model"}}
	analyzer, c, key, err := f.Resolve(context.Background(), "workspace", selection, "feedback_text", nil)
	if err != nil || analyzer == nil || c.Provider != aiprovider.Google || key.Version != 1 || len(key.Envelope) == 0 {
		t.Fatal("managed resolution failed", err)
	}
	decrypted, err := keys.Decrypt(key.Envelope, aiprovider.CredentialAAD("workspace", "managed", 1))
	if err != nil || decrypted != f.ManagedKey {
		t.Fatal("managed cleanup key not recoverable", err)
	}
	selection.ConnectionID = "unexpected"
	if _, _, _, err = f.Resolve(context.Background(), "workspace", selection, "feedback_text", nil); !errors.Is(err, aiprovider.ErrInvalid) {
		t.Fatal("mixed managed/customer selection accepted", err)
	}
	f.Connections.Keys = nil
	selection.ConnectionID = ""
	if _, _, _, err = f.Resolve(context.Background(), "workspace", selection, "feedback_text", nil); err != nil {
		t.Fatal("legacy managed resolution failed", err)
	}
	wrapped := safeAnalyzer{&fakeAnalyzer{attachmentErr: errors.New("private payload")}}
	if _, err = wrapped.AnalyzeAttachment(context.Background(), AttachmentWork{}, strings.NewReader("")); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("attachment error leaked", err)
	}
}

func TestModelDiscoveryBoundsCatalogAndCache(t *testing.T) {
	f := fixtureProviderFactory(t, func(w http.ResponseWriter, r *http.Request) {
		models := []map[string]string{}
		for i := 0; i < 600; i++ {
			models = append(models, map[string]string{"id": fmt.Sprintf("model-%03d", i)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": models, "has_more": true, "last_id": "cursor"})
	})
	f.models = map[string]modelCache{}
	for i := 0; i < 128; i++ {
		f.models[fmt.Sprint(i)] = modelCache{until: time.Now().Add(time.Hour)}
	}
	c := aiprovider.Connection{ID: "bounded", Provider: aiprovider.CustomAnthropic, BaseURL: "http://fixture.example/v1"}
	models, err := f.Models(context.Background(), c, aiprovider.Secret{Value: "fixture"})
	if err != nil || len(models) != 500 || len(f.models) != 1 {
		t.Fatalf("unbounded discovery/cache: %d %d %v", len(models), len(f.models), err)
	}
}

// SDK clients clone the standard transport. This sequential fixture trusts only
// its own TLS certificate and dials only its local server; no live API is called.
func tlsProviderFactory(t *testing.T, handler http.HandlerFunc) *ProviderFactory {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	original := http.DefaultTransport
	fixture := original.(*http.Transport).Clone()
	fixture.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: server.Certificate().DNSNames[0], MinVersion: tls.VersionTLS12}
	http.DefaultTransport = fixture
	t.Cleanup(func() { http.DefaultTransport = original; fixture.CloseIdleConnections() })
	return &ProviderFactory{Outbound: outboundhttp.Policy{Resolver: providerResolver{}, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}}
}

func TestGoogleDiscoveryAndSyntheticMediaUseNativeAPI(t *testing.T) {
	generations := 0
	f := tlsProviderFactory(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-fixture","displayName":"Gemini fixture","supportedGenerationMethods":["generateContent"]},{"name":"models/embedding","supportedGenerationMethods":["embedContent"]}]}`))
			return
		}
		generations++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !strings.Contains(r.URL.Path, "gemini-fixture:generateContent") {
			t.Error("model changed", r.URL.Path)
		}
		result := strings.Replace(validProviderJSON, `"transcription":""`, `"transcription":"FIDIBEKI"`, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": result}}, "role": "model"}, "finishReason": "STOP"}}, "modelVersion": "gemini-fixture"})
	})
	c := aiprovider.Connection{ID: "google", Provider: aiprovider.Google}
	key := aiprovider.Secret{Value: "fixture-google"}
	models, err := f.Models(context.Background(), c, key)
	if err != nil || len(models) != 1 || models[0].ID != "gemini-fixture" {
		t.Fatalf("native catalog: %#v %v", models, err)
	}
	validated, err := f.Validate(context.Background(), c, key, "gemini-fixture", []string{"feedback_text", "screenshot", "audio", "video"})
	if err != nil || len(validated.Inputs) != 4 || generations != 4 {
		t.Fatalf("native media probes: %#v calls=%d error=%v", validated, generations, err)
	}
}

func TestBedrockCatalogIncludesFoundationModelsAndInferenceProfiles(t *testing.T) {
	c := aiprovider.Connection{ID: "bedrock", Provider: aiprovider.Bedrock, AuthMode: "role", Region: "eu-west-1", RoleARN: "arn:aws:iam::123456789012:role/customer", ExternalID: "workspace-specific"}
	caller := AWSCaller{AccessKey: "caller-key", SecretKey: "caller-secret", ARN: "arn:aws:iam::987654321098:user/fidibeki"}
	f := tlsProviderFactory(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if !strings.Contains(r.Header.Get("Authorization"), "Credential=assumed-key/") {
				t.Error("catalog used wrong AWS credentials")
			}
			if r.URL.Path == "/foundation-models" {
				_, _ = w.Write([]byte(`{"modelSummaries":[{"modelId":"foundation","modelName":"Foundation"}]}`))
				return
			}
			if r.URL.Path == "/inference-profiles" {
				_, _ = w.Write([]byte(`{"inferenceProfileSummaries":[{"inferenceProfileId":"profile","inferenceProfileName":"Profile"}]}`))
				return
			}
			t.Error("unexpected catalog URL", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/xml")
		if r.Form.Get("Action") == "GetCallerIdentity" {
			_, _ = fmt.Fprintf(w, `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><GetCallerIdentityResult><Arn>%s</Arn><Account>987654321098</Account><UserId>fixture</UserId></GetCallerIdentityResult></GetCallerIdentityResponse>`, caller.ARN)
			return
		}
		if r.Form.Get("ExternalId") != c.ExternalID {
			t.Error("missing external ID")
		}
		_, _ = fmt.Fprintf(w, `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><AssumeRoleResult><Credentials><AccessKeyId>assumed-key</AccessKeyId><SecretAccessKey>assumed-secret</SecretAccessKey><SessionToken>assumed-token</SessionToken><Expiration>%s</Expiration></Credentials></AssumeRoleResult></AssumeRoleResponse>`, time.Now().Add(time.Hour).Format(time.RFC3339))
	})
	f.AWS = caller
	models, err := f.Models(context.Background(), c, aiprovider.Secret{})
	if err != nil || len(models) != 2 || models[0].ID != "foundation" || models[1].ID != "profile" {
		t.Fatalf("AWS catalog: %#v %v", models, err)
	}
}
