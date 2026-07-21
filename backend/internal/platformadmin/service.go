package platformadmin

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
)

type AuthStore interface {
	FindAdminByEmail(context.Context, string) (Admin, error)
	CreateMagicLink(context.Context, Admin, string, time.Time) error
	DeleteMagicLink(context.Context, string) error
	ConsumeMagicLink(context.Context, string, string, string, string) (Admin, error)
	RotateSession(context.Context, string, string, string, string) (Admin, error)
	RevokeSession(context.Context, string) error
}

type Service struct {
	Store         AuthStore
	JWT           *auth.JWTManager
	Email         email.Enqueuer
	PublicBaseURL string
	AuthFrom      string
	MagicLinkTTL  time.Duration
	RefreshTTL    time.Duration
	Now           func() time.Time
}

type SessionTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Admin        Admin
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) magicLinkTTL() time.Duration {
	if s.MagicLinkTTL > 0 {
		return s.MagicLinkTTL
	}
	return MagicLinkTTL
}

func (s Service) RequestMagicLink(ctx context.Context, rawEmail string) error {
	emailAddress, err := normalizedEmail(rawEmail)
	if err != nil {
		return err
	}
	admin, err := s.Store.FindAdminByEmail(ctx, emailAddress)
	if errors.Is(err, ErrAdminNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	rawToken, tokenHash, err := auth.NewOpaqueToken()
	if err != nil {
		return err
	}
	if err := s.Store.CreateMagicLink(ctx, admin, tokenHash, s.now().Add(s.magicLinkTTL())); err != nil {
		return err
	}
	link, err := s.magicLinkURL(rawToken)
	if err != nil {
		_ = s.Store.DeleteMagicLink(ctx, tokenHash)
		return err
	}
	message := email.Message{
		From: s.AuthFrom, To: admin.Email, Subject: "Sign in to Fidibeki",
		Text: fmt.Sprintf("Open this link to sign in to Fidibeki: %s\n\nThis link expires in 10 minutes and can only be used once.", link),
		HTML: fmt.Sprintf(`<p>Open this link to sign in to Fidibeki:</p><p><a href="%s">Sign in to Fidibeki</a></p><p>This link expires in 10 minutes and can only be used once.</p>`, link),
	}
	if err := s.Email.Enqueue(ctx, message); err != nil {
		_ = s.Store.DeleteMagicLink(ctx, tokenHash)
		return fmt.Errorf("queue platform admin magic link: %w", err)
	}
	return nil
}

func (s Service) VerifyMagicLink(
	ctx context.Context,
	rawToken string,
	userAgent string,
	ip string,
) (SessionTokens, error) {
	if strings.TrimSpace(rawToken) == "" {
		return SessionTokens{}, ErrMagicLinkInvalid
	}
	refreshToken, refreshHash, err := auth.NewOpaqueToken()
	if err != nil {
		return SessionTokens{}, err
	}
	admin, err := s.Store.ConsumeMagicLink(ctx, auth.HashToken(rawToken), refreshHash, userAgent, ip)
	if err != nil {
		return SessionTokens{}, err
	}
	accessToken, expiresAt, err := s.JWT.Issue(admin.ID, auth.AudiencePlatform, admin.Permissions)
	if err != nil {
		return SessionTokens{}, err
	}
	return SessionTokens{
		AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt, Admin: admin,
	}, nil
}

func (s Service) Refresh(
	ctx context.Context,
	currentToken string,
	userAgent string,
	ip string,
) (SessionTokens, error) {
	if strings.TrimSpace(currentToken) == "" {
		return SessionTokens{}, ErrRefreshInvalid
	}
	nextToken, nextHash, err := auth.NewOpaqueToken()
	if err != nil {
		return SessionTokens{}, err
	}
	admin, err := s.Store.RotateSession(ctx, auth.HashToken(currentToken), nextHash, userAgent, ip)
	if err != nil {
		return SessionTokens{}, err
	}
	accessToken, expiresAt, err := s.JWT.Issue(admin.ID, auth.AudiencePlatform, admin.Permissions)
	if err != nil {
		return SessionTokens{}, err
	}
	return SessionTokens{
		AccessToken: accessToken, RefreshToken: nextToken, ExpiresAt: expiresAt, Admin: admin,
	}, nil
}

func (s Service) Logout(ctx context.Context, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	return s.Store.RevokeSession(ctx, auth.HashToken(refreshToken))
}

func (s Service) magicLinkURL(token string) (string, error) {
	base, err := url.Parse(s.PublicBaseURL)
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", fmt.Errorf("public base URL is invalid")
	}
	base.Path = "/admin/auth/verify"
	query := base.Query()
	query.Set("token", token)
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func normalizedEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return "", fmt.Errorf("email is invalid")
	}
	return value, nil
}
