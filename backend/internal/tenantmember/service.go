package tenantmember

import (
	"context"
	"errors"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

var memberDomainLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type MemberStore interface {
	FindMemberByEmail(context.Context, db.Querier, string, string) (Member, error)
	IsMemberEmailDomainAllowed(context.Context, db.Querier, string, string) (bool, error)
	CreateMagicLink(context.Context, db.Querier, string, *string, string, []string, string, time.Time) error
	ConsumeMagicLink(context.Context, db.Querier, string, string, string, string, string) (Member, error)
	RequestMemberDomainVerification(context.Context, db.Querier, string, string, string, string, time.Time) (MemberDomain, error)
	VerifyMemberDomain(context.Context, db.Querier, string, string) (MemberDomain, error)
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
	allowed, err := s.Store.IsMemberEmailDomainAllowed(ctx, query, resolved.ID, emailAddress)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrMemberDomainNotVerified
	}
	if _, err := s.Store.FindMemberByEmail(ctx, query, resolved.ID, emailAddress); err == nil {
		return nil, ErrMemberExists
	} else if !errors.Is(err, ErrMemberNotFound) {
		return nil, err
	}
	return s.createLink(ctx, query, resolved, nil, emailAddress, permissions)
}

func (s Service) RequestMemberDomainVerification(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	rawEmail string,
) (MemberDomain, *email.Message, error) {
	emailAddress, err := normalizeEmail(rawEmail)
	if err != nil {
		return MemberDomain{}, nil, err
	}
	domain, err := memberEmailDomain(emailAddress)
	if err != nil {
		return MemberDomain{}, nil, err
	}
	rawToken, tokenHash, err := auth.NewOpaqueToken()
	if err != nil {
		return MemberDomain{}, nil, err
	}
	memberDomain, err := s.Store.RequestMemberDomainVerification(
		ctx, query, resolved.ID, domain, emailAddress, tokenHash, s.now().Add(s.magicTTL()),
	)
	if err != nil {
		return MemberDomain{}, nil, err
	}
	link, err := s.tokenURL(resolved.Slug, "/auth/domain-verify", rawToken)
	if err != nil {
		return MemberDomain{}, nil, err
	}
	message, err := s.composeEmail(email.Message{
		From: s.AuthFrom, To: emailAddress, Subject: "Verify " + domain + " for " + resolved.Name,
		IdempotencyKey: "member-domain-verification:" + memberDomain.ID + ":" +
			memberDomain.VerificationExpiresAt.UTC().Format(time.RFC3339Nano),
	}, email.Content{
		Preheader: "Verify who can join " + resolved.Name,
		Heading:   "Verify member email domain", Workspace: resolved.Name,
		Body: []string{
			"Confirm " + domain + " before using it to invite workspace members.",
			"This security link expires in 10 minutes and can only be used once.",
		},
		Metadata: []email.Metadata{{Label: "Domain", Value: domain}},
		Action:   &email.Link{Label: "Verify member domain", URL: link}, ShowActionURL: true,
		Footer:      "You received this because member-domain verification was requested for this workspace.",
		FooterLinks: []email.Link{{Label: "Contact support", URL: "mailto:support@fidibeki.com"}},
	})
	if err != nil {
		return MemberDomain{}, nil, err
	}
	return memberDomain, &message, nil
}

func (s Service) VerifyMemberDomain(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	rawToken string,
) (MemberDomain, error) {
	if rawToken == "" {
		return MemberDomain{}, ErrDomainVerificationInvalid
	}
	return s.Store.VerifyMemberDomain(ctx, query, tenantID, auth.HashToken(rawToken))
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
	link, err := s.tokenURL(resolved.Slug, "/auth/verify", rawToken)
	if err != nil {
		return nil, err
	}
	subject := "Sign in to " + resolved.Name + " on Fidibeki"
	content := email.Content{
		Preheader: "Use your secure link to sign in to " + resolved.Name,
		Heading:   "Sign in to Fidibeki", Workspace: resolved.Name,
		Body: []string{
			"Use the button below to continue to your workspace.",
			"This security link expires in 10 minutes and can only be used once.",
		},
		Action: &email.Link{Label: "Sign in to Fidibeki", URL: link}, ShowActionURL: true,
		Footer:      "You received this because a sign-in link was requested for your Fidibeki account.",
		FooterLinks: []email.Link{{Label: "Contact support", URL: "mailto:support@fidibeki.com"}},
	}
	if memberID == nil {
		subject = "You're invited to " + resolved.Name + " on Fidibeki"
		content.Preheader = "Join " + resolved.Name + " on Fidibeki"
		content.Heading = "You're invited to a workspace"
		content.Body = []string{
			"A workspace administrator invited you to join " + resolved.Name + " on Fidibeki.",
			"Accept the invitation with this one-time link. It expires in 10 minutes.",
		}
		content.Action.Label = "Join " + resolved.Name
		content.Footer = "You received this because a workspace administrator invited this email address."
	}
	message, err := s.composeEmail(email.Message{
		From: s.AuthFrom, To: emailAddress, Subject: subject,
		IdempotencyKey: "tenant-magic-link:" + tokenHash,
	}, content)
	if err != nil {
		return nil, err
	}
	return &message, nil
}

func (s Service) composeEmail(message email.Message, content email.Content) (email.Message, error) {
	logoURL, err := email.BrandLogoURL(s.PublicBaseURL, s.BaseDomain)
	if err != nil {
		return email.Message{}, err
	}
	content.LogoURL = logoURL
	return email.Compose(message, content)
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
	nextToken, nextHash, err := s.JWT.DeriveRefreshToken(auth.AudienceTenant, currentToken)
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

func (s Service) tokenURL(slug, path, token string) (string, error) {
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
	base.Path = path
	base.RawQuery = ""
	base.Fragment = url.Values{"token": []string{token}}.Encode()
	return base.String(), nil
}

func memberEmailDomain(emailAddress string) (string, error) {
	separator := strings.LastIndexByte(emailAddress, '@')
	if separator <= 0 || separator == len(emailAddress)-1 {
		return "", errors.New("email is invalid")
	}
	domain := emailAddress[separator+1:]
	if len(domain) > 253 {
		return "", errors.New("email is invalid")
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return "", errors.New("email is invalid")
	}
	for _, label := range labels {
		if !memberDomainLabelPattern.MatchString(label) {
			return "", errors.New("email is invalid")
		}
	}
	return domain, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return "", errors.New("email is invalid")
	}
	if _, err := memberEmailDomain(value); err != nil {
		return "", err
	}
	return value, nil
}
