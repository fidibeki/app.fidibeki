package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultResendEndpoint = "https://api.resend.com/emails"

type ResendSender struct {
	APIKey   string
	Endpoint string
	Client   *http.Client
}

func (s ResendSender) Send(ctx context.Context, message Message) (string, error) {
	if err := message.Validate(); err != nil {
		return "", fmt.Errorf("validate Resend email: %w", err)
	}
	if strings.TrimSpace(s.APIKey) == "" {
		return "", fmt.Errorf("Resend API key is required")
	}
	payload, err := json.Marshal(map[string]any{
		"from": message.From, "to": []string{message.To}, "subject": message.Subject,
		"text": message.Text, "html": message.HTML,
	})
	if err != nil {
		return "", fmt.Errorf("encode Resend email: %w", err)
	}
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = defaultResendEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create Resend request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+s.APIKey)
	request.Header.Set("Content-Type", "application/json")
	if message.IdempotencyKey != "" {
		request.Header.Set("Idempotency-Key", message.IdempotencyKey)
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("send Resend email: %w", err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if readErr != nil {
		return "", fmt.Errorf("read Resend response: %w", readErr)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("Resend returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode Resend response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("Resend response did not include a message ID")
	}
	return result.ID, nil
}
