package logging

import (
	"io"
	"log/slog"
	"strings"
)

const redacted = "[REDACTED]"

func New(output io.Writer, level slog.Leveler) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			if isSensitiveKey(attribute.Key) {
				return slog.String(attribute.Key, redacted)
			}
			return attribute
		},
	})
	return slog.New(handler)
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	if strings.Contains(key, "email") || strings.Contains(key, "token") ||
		strings.Contains(key, "secret") || strings.Contains(key, "authorization") {
		return true
	}
	switch key {
	case "text", "transcription", "page_url", "referrer", "user_agent",
		"element_selector", "dom_snapshot_html", "dom_snapshot_styles", "storage_key":
		return true
	default:
		return false
	}
}
