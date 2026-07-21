package email

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type CaptureSender struct {
	Directory string
	Now       func() time.Time
}

type capturedMessage struct {
	ID         string    `json:"id"`
	CapturedAt time.Time `json:"captured_at"`
	Message
}

func (s CaptureSender) Send(_ context.Context, message Message) (string, error) {
	if err := message.Validate(); err != nil {
		return "", fmt.Errorf("validate captured email: %w", err)
	}
	if s.Directory == "" {
		return "", fmt.Errorf("capture email directory is required")
	}
	if err := os.MkdirAll(s.Directory, 0o700); err != nil {
		return "", fmt.Errorf("create capture email directory: %w", err)
	}
	id, err := captureID()
	if err != nil {
		return "", err
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	payload, err := json.MarshalIndent(capturedMessage{ID: id, CapturedAt: now, Message: message}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode captured email: %w", err)
	}
	path := filepath.Join(s.Directory, id+".json")
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write captured email: %w", err)
	}
	return id, nil
}

func captureID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate capture email ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
