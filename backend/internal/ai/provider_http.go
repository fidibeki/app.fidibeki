package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/aiprovider"
)

const maxProviderResponse = 1 << 20
const maxProviderImage = 5 << 20

type providerHTTP struct {
	client     *http.Client
	connection aiprovider.Connection
	secret     aiprovider.Secret
	model      string
	sign       func(context.Context, *http.Request, []byte) error
}

func (p *providerHTTP) endpoint() string {
	switch p.connection.Provider {
	case aiprovider.OpenAI:
		return "https://api.openai.com/v1"
	case aiprovider.Anthropic:
		return "https://api.anthropic.com/v1"
	case aiprovider.Bedrock:
		return "https://bedrock-runtime." + p.connection.Region + ".amazonaws.com"
	default:
		return p.connection.BaseURL
	}
}

func (p *providerHTTP) request(ctx context.Context, method, endpoint string, payload any, out any) error {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return &aiprovider.Failure{Code: "invalid_endpoint"}
	}
	req.Header.Set("Content-Type", "application/json")
	if p.connection.Provider == aiprovider.Anthropic || p.connection.Provider == aiprovider.CustomAnthropic {
		req.Header.Set("x-api-key", p.secret.Value)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+p.secret.Value)
	}
	if p.sign != nil {
		req.Header.Del("Authorization")
		if err := p.sign(ctx, req, body); err != nil {
			return err
		}
	}
	response, err := p.client.Do(req)
	if err != nil {
		var classified *aiprovider.Failure
		if errors.As(err, &classified) {
			return classified
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &aiprovider.Failure{Code: "network_error", Retryable: true}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponse+1))
	if err != nil {
		return &aiprovider.Failure{Code: "network_error", Retryable: true}
	}
	if len(data) > maxProviderResponse {
		return &aiprovider.Failure{Code: "response_too_large"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerFailure(response.StatusCode, response.Header.Get("Retry-After"), data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &aiprovider.Failure{Code: "invalid_response"}
	}
	return nil
}

func providerFailure(status int, retryHeader string, data []byte) error {
	code := "provider_rejected"
	retry := false
	switch {
	case status == 401:
		code = "invalid_credentials"
	case status == 403:
		code = "permission_denied"
	case status == 404:
		code = "model_unavailable"
	case status == 408 || status == 504:
		code = "provider_timeout"
		retry = true
	case status == 429:
		code = "rate_limited"
		retry = true
	case status >= 500:
		code = "provider_unavailable"
		retry = true
	case status >= 300 && status < 400:
		code = "redirect_rejected"
	}
	// Inspect only a known machine-readable error code. Never retain its message.
	var details struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &details) == nil && (details.Error.Code == "insufficient_quota" || details.Error.Type == "insufficient_quota" || details.Error.Type == "billing_error") {
		code = "quota_exhausted"
		retry = false
	}
	delay := time.Duration(0)
	if n, err := strconv.Atoi(retryHeader); err == nil && n > 0 {
		delay = time.Duration(n) * time.Second
	} else if date, err := http.ParseTime(retryHeader); err == nil {
		delay = time.Until(date)
	}
	if delay < 0 {
		delay = 0
	}
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	return &aiprovider.Failure{Code: code, Status: status, RetryAfter: delay, Retryable: retry}
}

func (p *providerHTTP) AnalyzeText(ctx context.Context, text string) (Result, error) {
	text, redactions := MinimizeProviderText(text)
	result, err := p.generate(ctx, "Analyze this written feedback. Leave transcription empty.\n<feedback>\n"+text+"\n</feedback>", nil, "")
	result.Redactions = redactions
	return result, err
}

func (p *providerHTTP) AnalyzeAttachment(ctx context.Context, work AttachmentWork, reader io.Reader) (Result, error) {
	if work.Kind != "screenshot" {
		return Result{}, &aiprovider.Failure{Code: "unsupported_input"}
	}
	if work.ContentType != "image/png" && work.ContentType != "image/jpeg" && work.ContentType != "image/webp" {
		return Result{}, &aiprovider.Failure{Code: "unsupported_media_type"}
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxProviderImage+1))
	if err != nil {
		return Result{}, err
	}
	if len(data) > maxProviderImage {
		return Result{}, &aiprovider.Failure{Code: "input_too_large"}
	}
	return p.generate(ctx, attachmentPrompt(work.Kind), data, work.ContentType)
}

func portableSchema() map[string]any {
	// Provider schema dialects differ. All constraints are enforced locally too.
	return map[string]any{"type": "object", "properties": map[string]any{
		"transcription": map[string]any{"type": "string"}, "cleaned_text": map[string]any{"type": "string"},
		"sentiment_score": map[string]any{"type": "number"}, "moderation_flags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"suggested_title": map[string]any{"type": "string"}, "actionability": map[string]any{"type": "string", "enum": []string{"actionable", "not_actionable", "uncertain"}},
		"category": map[string]any{"type": "string"}, "priority": map[string]any{"type": "string", "enum": []string{"low", "medium", "high", "urgent"}},
	}, "required": []string{"transcription", "cleaned_text", "sentiment_score", "moderation_flags", "suggested_title", "actionability", "category", "priority"}, "additionalProperties": false}
}

func (p *providerHTTP) generate(ctx context.Context, prompt string, image []byte, mime string) (Result, error) {
	var raw, model string
	usage := Usage{}
	switch p.connection.Provider {
	case aiprovider.Anthropic, aiprovider.CustomAnthropic:
		content := []any{map[string]any{"type": "text", "text": prompt}}
		if image != nil {
			content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mime, "data": base64.StdEncoding.EncodeToString(image)}})
		}
		body := map[string]any{"model": p.model, "max_tokens": 2048, "system": analysisInstruction, "messages": []any{map[string]any{"role": "user", "content": content}}, "output_config": map[string]any{"format": map[string]any{"type": "json_schema", "schema": portableSchema()}}}
		var response struct {
			Model      string `json:"model"`
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Usage struct {
				Input     *int64 `json:"input_tokens"`
				Output    *int64 `json:"output_tokens"`
				CacheRead *int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if err := p.request(ctx, http.MethodPost, p.endpoint()+"/messages", body, &response); err != nil {
			return Result{}, err
		}
		if response.StopReason != "end_turn" {
			return Result{}, &aiprovider.Failure{Code: "incomplete_output"}
		}
		for _, part := range response.Content {
			if part.Type == "text" {
				raw += part.Text
			}
		}
		model = response.Model
		usage = Usage{Input: response.Usage.Input, Output: response.Usage.Output, Cache: response.Usage.CacheRead}
	case aiprovider.Bedrock:
		content := []any{map[string]any{"text": prompt}}
		if image != nil {
			content = append(content, map[string]any{"image": map[string]any{"format": strings.TrimPrefix(mime, "image/"), "source": map[string]any{"bytes": base64.StdEncoding.EncodeToString(image)}}})
		}
		body := map[string]any{"system": []any{map[string]any{"text": analysisInstruction}}, "messages": []any{map[string]any{"role": "user", "content": content}}, "inferenceConfig": map[string]any{"maxTokens": 2048}, "toolConfig": map[string]any{"tools": []any{map[string]any{"toolSpec": map[string]any{"name": "feedback_analysis", "description": "Return the feedback analysis", "inputSchema": map[string]any{"json": portableSchema()}}}}, "toolChoice": map[string]any{"tool": map[string]any{"name": "feedback_analysis"}}}}
		var response struct {
			StopReason string `json:"stopReason"`
			Output     struct {
				Message struct {
					Content []struct {
						ToolUse *struct {
							Name  string          `json:"name"`
							Input json.RawMessage `json:"input"`
						} `json:"toolUse"`
					} `json:"content"`
				} `json:"message"`
			} `json:"output"`
			Usage struct {
				Input  *int64 `json:"inputTokens"`
				Output *int64 `json:"outputTokens"`
			} `json:"usage"`
		}
		if err := p.request(ctx, http.MethodPost, p.endpoint()+"/model/"+url.PathEscape(p.model)+"/converse", body, &response); err != nil {
			return Result{}, err
		}
		if response.StopReason != "tool_use" {
			return Result{}, &aiprovider.Failure{Code: "incomplete_output"}
		}
		for _, part := range response.Output.Message.Content {
			if part.ToolUse != nil && part.ToolUse.Name == "feedback_analysis" {
				if raw != "" {
					return Result{}, &aiprovider.Failure{Code: "invalid_output"}
				}
				raw = string(part.ToolUse.Input)
			}
		}
		usage = Usage{Input: response.Usage.Input, Output: response.Usage.Output}
	default:
		var content any = prompt
		if image != nil {
			content = []any{map[string]any{"type": "text", "text": prompt}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)}}}
		}
		body := map[string]any{"model": p.model, "messages": []any{map[string]any{"role": "system", "content": analysisInstruction}, map[string]any{"role": "user", "content": content}}, "max_completion_tokens": 2048, "response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "feedback_analysis", "strict": true, "schema": portableSchema()}}}
		if p.connection.Provider == aiprovider.CustomOpenAI {
			delete(body, "max_completion_tokens")
			body["max_tokens"] = 2048
		}

		var response struct {
			Model   string `json:"model"`
			Choices []struct {
				Finish  string `json:"finish_reason"`
				Message struct {
					Content string  `json:"content"`
					Refusal *string `json:"refusal"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				Input         *int64 `json:"prompt_tokens"`
				Output        *int64 `json:"completion_tokens"`
				PromptDetails struct {
					Cache *int64 `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionDetails struct {
					Reasoning *int64 `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
		}
		if err := p.request(ctx, http.MethodPost, p.endpoint()+"/chat/completions", body, &response); err != nil {
			return Result{}, err
		}
		if len(response.Choices) != 1 || response.Choices[0].Finish != "stop" || response.Choices[0].Message.Refusal != nil {
			return Result{}, &aiprovider.Failure{Code: "incomplete_output"}
		}
		raw = response.Choices[0].Message.Content
		model = response.Model
		usage = Usage{Input: response.Usage.Input, Output: response.Usage.Output, Cache: response.Usage.PromptDetails.Cache, Reasoning: response.Usage.CompletionDetails.Reasoning}
	}
	result, err := decodeProviderResult(raw)
	result.Usage = usage
	result.ResolvedModel = model
	return result, err
}

func decodeProviderResult(raw string) (Result, error) {
	invalid := func() (Result, error) { return Result{}, permanent(ErrUnsafeAIResult) }
	if len(raw) > maxProviderResponse {
		return invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || len(fields) != 8 {
		return invalid()
	}
	for _, key := range []string{"transcription", "cleaned_text", "sentiment_score", "moderation_flags", "suggested_title", "actionability", "category", "priority"} {
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return invalid()
		}
	}
	var result Result
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return invalid()
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return invalid()
	}
	if result.Actionability == "" || result.Priority == "" || len(result.Transcription) > 65536 || len(result.CleanedText) > 65536 {
		return invalid()
	}
	if err := result.Normalize(); err != nil {
		return invalid()
	}
	return result, nil
}
