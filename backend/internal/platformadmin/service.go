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
	// BackofficeURL is the exact browser origin that may receive a platform
	// administrator magic-link verification. It is deployment configuration,
	// never request input, so a login request cannot create an open redirect.
	BackofficeURL string
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
	return s.requestMagicLink(ctx, rawEmail, s.magicLinkURL)
}

// RequestBackofficeMagicLink sends the same non-enumerating platform-admin
// magic link as RequestMagicLink, but verifies it through the separately
// deployed backoffice browser origin. The existing admin routes retain their
// current API-origin behavior.
func (s Service) RequestBackofficeMagicLink(ctx context.Context, rawEmail string) error {
	return s.requestMagicLink(ctx, rawEmail, s.backofficeMagicLinkURL)
}

func (s Service) requestMagicLink(
	ctx context.Context,
	rawEmail string,
	linkURL func(string) (string, error),
) error {
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
	link, err := linkURL(rawToken)
	if err != nil {
		_ = s.Store.DeleteMagicLink(ctx, tokenHash)
		return err
	}
	logoURL, err := email.BrandLogoURL(s.PublicBaseURL)
	if err != nil {
		_ = s.Store.DeleteMagicLink(ctx, tokenHash)
		return err
	}
	message, err := email.Compose(email.Message{
		From: s.AuthFrom, To: admin.Email, Subject: "Sign in to Fidibeki administration",
		IdempotencyKey: "platform-magic-link:" + tokenHash,
	}, email.Content{
		Preheader: "Use your secure link to access Fidibeki administration",
		Heading:   "Sign in to administration",
		Body: []string{
			"Use the button below to continue to the Fidibeki administration workspace.",
			"This security link expires in 10 minutes and can only be used once.",
		},
		Action: &email.Link{Label: "Sign in to Fidibeki", URL: link}, ShowActionURL: true,
		Footer:      "You received this because an administrator sign-in link was requested for this address.",
		FooterLinks: []email.Link{{Label: "Contact support", URL: "mailto:support@fidibeki.com"}},
		LogoURL:     logoURL,
	})
	if err != nil {
		_ = s.Store.DeleteMagicLink(ctx, tokenHash)
		return err
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
	nextToken, nextHash, err := s.JWT.DeriveRefreshToken(auth.AudiencePlatform, currentToken)
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

func (s Service) backofficeMagicLinkURL(token string) (string, error) {
	return backofficeMagicLinkURL(s.BackofficeURL, token)
}

func backofficeMagicLinkURL(origin string, token string) (string, error) {
	base, err := url.Parse(origin)
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", fmt.Errorf("magic link origin is invalid")
	}
	if base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return "", fmt.Errorf("magic link origin must not contain a path, query, or fragment")
	}
	base.Path = "/auth/verify"
	base.RawPath = ""
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
