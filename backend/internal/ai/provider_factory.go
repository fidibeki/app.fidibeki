package ai

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/aiprovider"
	"github.com/acidiney/app.fidibeki/backend/internal/outboundhttp"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"google.golang.org/genai"
)

//go:embed fixtures/validation.mp4
var validationVideo []byte

type ProviderFactory struct {
	Connections  aiprovider.Store
	Ledger       Store
	ManagedKey   string
	ManagedModel string
	Logger       *slog.Logger
	Outbound     outboundhttp.Policy
	Allowed      func(string, string) bool
	AWS          AWSCaller
	mu           sync.Mutex
	models       map[string]modelCache
}
type modelCache struct {
	models []aiprovider.Model
	until  time.Time
}

type authorizedTransport struct {
	next  http.RoundTripper
	check func(context.Context) error
}

func (t authorizedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Deleting a remote file is permitted after processing authorization ends.
	if r.Method != http.MethodDelete && t.check != nil {
		if err := t.check(r.Context()); err != nil {
			return nil, err
		}
	}
	return t.next.RoundTrip(r)
}
func (f *ProviderFactory) client(check func(context.Context) error) *http.Client {
	c := f.Outbound.NewClient(2 * time.Minute)
	if check != nil {
		c.Transport = authorizedTransport{c.Transport, check}
	}
	return c
}

func (f *ProviderFactory) analyzer(ctx context.Context, c aiprovider.Connection, key aiprovider.Secret, model string, check func(context.Context) error) (Analyzer, error) {
	if c.Provider == aiprovider.Google {
		return newGeminiAnalyzerWithClient(ctx, key.Value, model, f.Ledger, f.Logger, f.client(check))
	}
	p := &providerHTTP{client: f.client(check), connection: c, secret: key, model: model}
	if c.Provider == aiprovider.Bedrock && c.AuthMode == "role" {
		sign, err := f.AWS.signer(ctx, c, false, f.client(nil))
		if err != nil {
			return nil, err
		}
		p.sign = sign
	}
	return p, nil
}

func (f *ProviderFactory) Resolve(ctx context.Context, tenant string, e Execution, category string, check func(context.Context) error) (Analyzer, aiprovider.Connection, aiprovider.Secret, error) {
	if e.BillingMode == aiprovider.Managed {
		if e.ConnectionID != "" || e.Provider != aiprovider.Google {
			return nil, aiprovider.Connection{}, aiprovider.Secret{}, aiprovider.ErrInvalid
		}
		c := aiprovider.Connection{Provider: aiprovider.Google, Enabled: true, TenantID: tenant}
		key := aiprovider.Secret{Value: f.ManagedKey}
		if f.Connections.Keys != nil {
			key.Version = 1
			encrypted, err := f.Connections.Keys.Encrypt(key.Value, aiprovider.CredentialAAD(tenant, "managed", key.Version))
			if err != nil {
				return nil, c, key, err
			}
			key.Envelope = encrypted
		}
		a, err := f.analyzer(ctx, c, key, e.Model, check)
		return safeAnalyzer{a}, c, key, err
	}
	if e.BillingMode != aiprovider.Customer || f.Allowed == nil || !f.Allowed(tenant, e.Provider) {
		return nil, aiprovider.Connection{}, aiprovider.Secret{}, aiprovider.ErrDisabled
	}
	c, err := f.Connections.Get(ctx, f.Connections.Database, tenant, e.ConnectionID)
	if err != nil {
		return nil, c, aiprovider.Secret{}, err
	}
	if err = aiprovider.ValidateSelection(ctx, f.Connections.Database, tenant, e.Selection, []string{category}); err != nil {
		return nil, c, aiprovider.Secret{}, err
	}
	key, err := f.Connections.Credential(ctx, f.Connections.Database, c)
	if err != nil {
		return nil, c, key, err
	}
	guard := func(ctx context.Context) error {

		var current int64
		if err := f.Connections.Database.QueryRow(ctx, `SELECT credential_version FROM ai_connections WHERE tenant_id=$1 AND id=$2 AND enabled AND NOT deleted`, tenant, c.ID).Scan(&current); err != nil {
			return aiprovider.ErrDisabled
		}
		if current != key.Version {
			return &aiprovider.Failure{Code: "credential_changed", Retryable: true}
		}
		if check != nil {
			if err := check(ctx); err != nil {
				return err
			}
		}
		return nil
	}
	a, err := f.analyzer(ctx, c, key, e.Model, guard)
	return safeAnalyzer{a}, c, key, err
}

type safeAnalyzer struct{ Analyzer }

func (a safeAnalyzer) AnalyzeText(ctx context.Context, text string) (Result, error) {
	r, err := a.Analyzer.AnalyzeText(ctx, text)
	return r, safeProviderError(err)
}
func (a safeAnalyzer) AnalyzeAttachment(ctx context.Context, w AttachmentWork, reader io.Reader) (Result, error) {
	r, err := a.Analyzer.AnalyzeAttachment(ctx, w, reader)
	return r, safeProviderError(err)
}
func safeProviderError(err error) error {
	if err == nil {
		return nil
	}
	var f *aiprovider.Failure
	if errors.As(err, &f) {
		if !f.Retryable {
			return permanent(f)
		}
		return f
	}
	if errors.Is(err, ErrUnsafeAIResult) {
		return permanent(ErrUnsafeAIResult)
	}
	if code := geminiStatusCode(err); code != 0 {
		return safeProviderError(providerFailure(code, "", nil))
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	if IsPermanent(err) {
		return permanent(&aiprovider.Failure{Code: "provider_rejected"})
	}
	return &aiprovider.Failure{Code: "provider_unavailable", Retryable: true}
}

func (f *ProviderFactory) Models(ctx context.Context, c aiprovider.Connection, key aiprovider.Secret) ([]aiprovider.Model, error) {
	cacheKey := c.TenantID + ":" + c.ID + ":" + strconv.FormatInt(c.ConfigVersion, 10) + ":" + strconv.FormatInt(c.CredentialVersion, 10)
	f.mu.Lock()
	cached, ok := f.models[cacheKey]
	f.mu.Unlock()
	if ok && cached.until.After(time.Now()) {
		return mergeValidated(cached.models, c.Models), nil
	}
	p := &providerHTTP{client: f.client(nil), connection: c, secret: key}
	models := []aiprovider.Model{}
	switch c.Provider {
	case aiprovider.Google:
		client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key.Value, Backend: genai.BackendGeminiAPI, HTTPClient: p.client, HTTPOptions: genai.HTTPOptions{BaseURL: "https://generativelanguage.googleapis.com/", APIVersion: "v1beta"}})
		if err != nil {
			return nil, safeProviderError(err)
		}
		page, err := client.Models.List(ctx, &genai.ListModelsConfig{PageSize: 100})
		if err != nil {
			return nil, safeProviderError(err)
		}
		for n := 0; n < 5; n++ {
			for _, m := range page.Items {
				if m != nil && aiprovider.Supports(m.SupportedActions, "generateContent") {
					models = append(models, aiprovider.Model{ID: strings.TrimPrefix(m.Name, "models/"), Name: m.DisplayName, Inputs: []string{}})
				}
			}
			if page.NextPageToken == "" {
				break
			}
			page, err = page.Next(ctx)
			if err != nil {
				return nil, safeProviderError(err)
			}
		}
	case aiprovider.Bedrock:
		if c.AuthMode != "role" {
			return nil, &aiprovider.Failure{Code: "manual_model_required"}
		}
		sign, err := f.AWS.signer(ctx, c, false, p.client)
		if err != nil {
			return nil, err
		}
		p.sign = sign
		var catalog struct {
			Models []struct {
				ID   string `json:"modelId"`
				Name string `json:"modelName"`
			} `json:"modelSummaries"`
		}
		if err = p.request(ctx, http.MethodGet, "https://bedrock."+c.Region+".amazonaws.com/foundation-models", nil, &catalog); err != nil {
			return nil, err
		}
		for _, m := range catalog.Models {
			models = append(models, aiprovider.Model{ID: m.ID, Name: m.Name, Inputs: []string{}})
		}
		// Profiles can have independent permissions. Their absence never changes a
		// selection; the operator can validate an exact profile ID manually.
		var profiles struct {
			Items []struct {
				ID   string `json:"inferenceProfileId"`
				Name string `json:"inferenceProfileName"`
			} `json:"inferenceProfileSummaries"`
		}
		if p.request(ctx, http.MethodGet, "https://bedrock."+c.Region+".amazonaws.com/inference-profiles", nil, &profiles) == nil {
			for _, m := range profiles.Items {
				models = append(models, aiprovider.Model{ID: m.ID, Name: m.Name, Inputs: []string{}})
			}
		}
	default:
		endpoint := p.endpoint() + "/models"
		if c.Provider == aiprovider.Anthropic || c.Provider == aiprovider.CustomAnthropic {
			endpoint += "?limit=100"
		}
		for n := 0; n < 5; n++ {
			var response struct {
				Data []struct {
					ID   string `json:"id"`
					Name string `json:"display_name"`
				} `json:"data"`
				HasMore bool   `json:"has_more"`
				LastID  string `json:"last_id"`
			}
			if err := p.request(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
				return nil, err
			}
			for _, m := range response.Data {
				if !aiprovider.ValidModelID(m.ID, false) {
					continue
				}
				name := m.Name
				if name == "" {
					name = m.ID
				}
				models = append(models, aiprovider.Model{ID: m.ID, Name: name, Inputs: []string{}})
				if len(models) >= 500 {
					break
				}
			}
			if !response.HasMore || response.LastID == "" || len(models) >= 500 {
				break
			}
			endpoint = p.endpoint() + "/models?limit=100&after_id=" + url.QueryEscape(response.LastID)
		}
	}
	if len(models) > 500 {
		models = models[:500]
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	f.mu.Lock()
	if f.models == nil || len(f.models) >= 128 {
		f.models = map[string]modelCache{}
	}
	f.models[cacheKey] = modelCache{models: models, until: time.Now().Add(2 * time.Minute)}
	f.mu.Unlock()
	return mergeValidated(models, c.Models), nil
}
func mergeValidated(models, validated []aiprovider.Model) []aiprovider.Model {
	out := append([]aiprovider.Model{}, models...)
	for i, m := range out {
		for _, v := range validated {
			if v.ID == m.ID {
				out[i].Inputs = append([]string{}, v.Inputs...)
				out[i].Validated = true
			}
		}
	}
	for _, v := range validated {
		found := false
		for _, m := range out {
			if v.ID == m.ID {
				found = true
				break
			}
		}
		if !found {
			out = append(out, v)
		}
	}
	return out
}

func (f *ProviderFactory) Validate(ctx context.Context, c aiprovider.Connection, key aiprovider.Secret, model string, inputs []string) (aiprovider.Model, error) {
	if c.Provider == aiprovider.Bedrock && c.AuthMode == "role" {
		if _, err := f.AWS.signer(ctx, c, true, f.client(nil)); err != nil {
			return aiprovider.Model{}, err
		}
	}
	a, err := f.analyzer(ctx, c, key, model, nil)
	if err != nil {
		return aiprovider.Model{}, safeProviderError(err)
	}
	result, err := a.AnalyzeText(ctx, "The save button does not work. Please fix it.")
	if err != nil {
		return aiprovider.Model{}, safeProviderError(err)
	}
	if _, err = ProtectResult(result); err != nil {
		return aiprovider.Model{}, err
	}
	seen := map[string]bool{}
	verified := []string{}
	for _, category := range inputs {
		if seen[category] {
			continue
		}
		seen[category] = true
		if category == "screenshot" {
			img := validationImage()
			if gemini, ok := a.(*GeminiAnalyzer); ok {
				result, err = gemini.generate(ctx, []*genai.Content{{Role: "user", Parts: []*genai.Part{genai.NewPartFromText(attachmentPrompt("screenshot")), genai.NewPartFromBytes(img, "image/png")}}})
			} else {
				result, err = a.AnalyzeAttachment(ctx, AttachmentWork{Kind: "screenshot", ContentType: "image/png"}, bytes.NewReader(img))
			}
			if err != nil {
				return aiprovider.Model{}, safeProviderError(err)
			}
			if !strings.Contains(strings.ToUpper(result.Transcription), "FIDIBEKI") {
				return aiprovider.Model{}, &aiprovider.Failure{Code: "screenshot_validation_failed"}
			}
		} else if category == "audio" || category == "video" {
			gemini, ok := a.(*GeminiAnalyzer)
			if !ok {
				return aiprovider.Model{}, &aiprovider.Failure{Code: "unsupported_input"}
			}
			data, mime := validationAudio(), "audio/wav"
			if category == "video" {
				data, mime = validationVideo, "video/mp4"
			}
			result, err = gemini.generate(ctx, []*genai.Content{{Role: "user", Parts: []*genai.Part{genai.NewPartFromText(attachmentPrompt(category)), genai.NewPartFromBytes(data, mime)}}})
			if err != nil {
				return aiprovider.Model{}, safeProviderError(err)
			}
		} else if category != "feedback_text" {
			return aiprovider.Model{}, &aiprovider.Failure{Code: "unsupported_input"}
		}
		if _, err = ProtectResult(result); err != nil {
			return aiprovider.Model{}, err
		}
		verified = append(verified, category)
	}
	return aiprovider.Model{ID: model, Name: model, Inputs: verified, Validated: true}, nil
}

func validationImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 240, 60))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(20, 34)}
	d.DrawString("FIDIBEKI")
	var data bytes.Buffer
	_ = png.Encode(&data, img)
	return data.Bytes()
}
func validationAudio() []byte {
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36+16000))
	out.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		_ = binary.Write(&out, binary.LittleEndian, v)
	}
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16000))
	out.Write(make([]byte, 16000))
	return out.Bytes()
}
