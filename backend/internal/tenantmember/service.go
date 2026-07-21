package tenantmember

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

type MemberStore interface {
	FindMemberByEmail(context.Context, db.Querier, string, string) (Member, error)
	CreateMagicLink(context.Context, db.Querier, string, *string, string, []string, string, time.Time) error
	ConsumeMagicLink(context.Context, db.Querier, string, string, string, string, string) (Member, error)
	RotateSession(context.Context, db.Querier, string, string, string, string, string) (Member, error)
	RevokeSession(context.Context, db.Querier, string, string) error
}

type Service struct {
	Store         MemberStore
	JWT           *auth.JWTManager
	PublicBaseURL string
	BaseDomain    string
	AuthFrom      string
	MagicLinkTTL  time.Duration
	Now           func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) magicTTL() time.Duration {
	if s.MagicLinkTTL > 0 {
		return s.MagicLinkTTL
	}
	return 10 * time.Minute
}

func (s Service) RequestMagicLink(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	rawEmail string,
) (*email.Message, error) {
	emailAddress, err := normalizeEmail(rawEmail)
	if err != nil {
		return nil, err
	}
	member, err := s.Store.FindMemberByEmail(ctx, query, resolved.ID, emailAddress)
	if errors.Is(err, ErrMemberNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.createLink(ctx, query, resolved, &member.ID, member.Email, nil)
}

func (s Service) Invite(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	rawEmail string,
	permissions []string,
) (*email.Message, error) {
	emailAddress, err := normalizeEmail(rawEmail)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store.FindMemberByEmail(ctx, query, resolved.ID, emailAddress); err == nil {
		return nil, ErrMemberExists
	} else if !errors.Is(err, ErrMemberNotFound) {
		return nil, err
	}
	return s.createLink(ctx, query, resolved, nil, emailAddress, permissions)
}

func (s Service) createLink(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	memberID *string,
	emailAddress string,
	permissions []string,
) (*email.Message, error) {
	rawToken, tokenHash, err := auth.NewOpaqueToken()
	if err != nil {
		return nil, err
	}
	if err := s.Store.CreateMagicLink(
		ctx, query, resolved.ID, memberID, emailAddress, permissions, tokenHash, s.now().Add(s.magicTTL()),
	); err != nil {
		return nil, err
	}
	link, err := s.loginURL(resolved.Slug, rawToken)
	if err != nil {
		return nil, err
	}
	message := email.Message{
		From: s.AuthFrom, To: emailAddress, Subject: "Sign in to " + resolved.Name + " on Fidibeki",
		Text: fmt.Sprintf("Open this link to sign in: %s\n\nThis link expires in 10 minutes and can only be used once.", link),
		HTML: fmt.Sprintf(`<p>Open this link to sign in:</p><p><a href="%s">Sign in to Fidibeki</a></p><p>This link expires in 10 minutes and can only be used once.</p>`, link),
	}
	return &message, nil
}

func (s Service) Verify(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	rawToken string,
	userAgent string,
	ip string,
) (SessionTokens, error) {
	if rawToken == "" {
		return SessionTokens{}, ErrMagicLinkInvalid
	}
	refreshToken, refreshHash, err := auth.NewOpaqueToken()
	if err != nil {
		return SessionTokens{}, err
	}
	member, err := s.Store.ConsumeMagicLink(
		ctx, query, tenantID, auth.HashToken(rawToken), refreshHash, userAgent, ip,
	)
	if err != nil {
		return SessionTokens{}, err
	}
	accessToken, expiresAt, err := s.JWT.IssueTenant(member.ID, tenantID, member.Permissions)
	if err != nil {
		return SessionTokens{}, err
	}
	return SessionTokens{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt, Member: member}, nil
}

func (s Service) Refresh(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	currentToken string,
	userAgent string,
	ip string,
) (SessionTokens, error) {
	if currentToken == "" {
		return SessionTokens{}, ErrRefreshInvalid
	}
	nextToken, nextHash, err := auth.NewOpaqueToken()
	if err != nil {
		return SessionTokens{}, err
	}
	member, err := s.Store.RotateSession(
		ctx, query, tenantID, auth.HashToken(currentToken), nextHash, userAgent, ip,
	)
	if err != nil {
		return SessionTokens{}, err
	}
	accessToken, expiresAt, err := s.JWT.IssueTenant(member.ID, tenantID, member.Permissions)
	if err != nil {
		return SessionTokens{}, err
	}
	return SessionTokens{AccessToken: accessToken, RefreshToken: nextToken, ExpiresAt: expiresAt, Member: member}, nil
}

func (s Service) Logout(ctx context.Context, query db.Querier, tenantID, token string) error {
	if token == "" {
		return nil
	}
	return s.Store.RevokeSession(ctx, query, tenantID, auth.HashToken(token))
}

func (s Service) loginURL(slug, token string) (string, error) {
	base, err := url.Parse(s.PublicBaseURL)
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", errors.New("public base URL is invalid")
	}
	_, port, _ := net.SplitHostPort(base.Host)
	host := slug + "." + s.BaseDomain
	if port != "" {
		host = net.JoinHostPort(host, port)
	}
	base.Host = host
	base.Path = "/auth/verify"
	base.RawQuery = ""
	base.Fragment = url.Values{"token": []string{token}}.Encode()
	return base.String(), nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return "", errors.New("email is invalid")
	}
	return value, nil
}
