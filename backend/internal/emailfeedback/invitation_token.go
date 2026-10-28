package emailfeedback

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/google/uuid"
)

const invitationTokenDomain = "fidibeki-email-feedback-invitation-v1\x00"

type InvitationTokenManager struct {
	secret []byte
}

func NewInvitationTokenManager(secret string) (*InvitationTokenManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("email feedback token secret must be at least 32 characters")
	}
	return &InvitationTokenManager{secret: []byte(secret)}, nil
}

func (manager *InvitationTokenManager) Token(invitationID string) (string, string, error) {
	if manager == nil {
		return "", "", errors.New("email feedback token manager is required")
	}
	parsed, err := uuid.Parse(invitationID)
	if err != nil {
		return "", "", errors.New("email feedback invitation ID is invalid")
	}
	mac := hmac.New(sha256.New, manager.secret)
	_, _ = mac.Write([]byte(invitationTokenDomain))
	_, _ = mac.Write([]byte(parsed.String()))
	raw := "efi_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return raw, auth.HashToken(raw), nil
}
