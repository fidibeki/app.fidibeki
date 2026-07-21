package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

type Message struct {
	From           string `json:"from"`
	To             string `json:"to"`
	Subject        string `json:"subject"`
	Text           string `json:"text"`
	HTML           string `json:"html,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func (m Message) Validate() error {
	var validationErrors []error
	if _, err := mail.ParseAddress(m.From); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("from: %w", err))
	}
	if _, err := mail.ParseAddress(m.To); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("to: %w", err))
	}
	if strings.TrimSpace(m.Subject) == "" {
		validationErrors = append(validationErrors, errors.New("subject is required"))
	}
	if strings.TrimSpace(m.Text) == "" {
		validationErrors = append(validationErrors, errors.New("text is required"))
	}
	if len(m.IdempotencyKey) > 256 {
		validationErrors = append(validationErrors, errors.New("idempotency key must be at most 256 characters"))
	}
	return errors.Join(validationErrors...)
}

type Sender interface {
	Send(context.Context, Message) (string, error)
}

type Enqueuer interface {
	Enqueue(context.Context, Message) error
}
