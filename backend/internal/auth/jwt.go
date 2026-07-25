package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	AudiencePlatform            = "platform"
	AudienceTenant              = "tenant"
	AudienceReporterUnsubscribe = "reporter-unsubscribe"
	// AudienceMemberUnsubscribe is bound to one member in one workspace and
	// grants exactly one action: turning that member's optional product
	// notifications off. It carries no permissions and cannot authenticate a
	// session, so a leaked digest footer link cannot read workspace data.
	AudienceMemberUnsubscribe  = "member-unsubscribe"
	reporterFeedbackPermission = "feedback"
)

var ErrInvalidAccessToken = errors.New("invalid access token")

type Claims struct {
	Permissions          []string `json:"permissions"`
	TenantID             string   `json:"tenant_id,omitempty"`
	AuthorizationVersion *int     `json:"authorization_version,omitempty"`
	// IsAPIKey distinguishes an API-key-authenticated tenant credential from a
	// human session. It is never part of the signed token payload (json:"-")
	// so it cannot be forged by a caller; middleware.AuthenticateTenantCredential
	// sets it fresh on every request based on which credential branch matched.
	IsAPIKey bool `json:"-"`
	jwt.RegisteredClaims
}

type JWTManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

func NewJWTManager(secret, issuer string, ttl time.Duration) (*JWTManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT secret must be at least 32 characters")
	}
	if issuer == "" {
		return nil, errors.New("JWT issuer is required")
	}
	if ttl <= 0 {
		return nil, errors.New("JWT TTL must be positive")
	}
	return &JWTManager{secret: []byte(secret), issuer: issuer, ttl: ttl, now: time.Now}, nil
}

func (m *JWTManager) Issue(subject, audience string, permissions []string) (string, time.Time, error) {
	return m.issue(subject, audience, "", permissions, nil)
}

func (m *JWTManager) IssueTenant(
	subject string,
	tenantID string,
	permissions []string,
) (string, time.Time, error) {
	if tenantID == "" {
		return "", time.Time{}, errors.New("JWT tenant ID is required")
	}
	initialAuthorizationVersion := 0
	return m.issue(subject, AudienceTenant, tenantID, permissions, &initialAuthorizationVersion)
}

func (m *JWTManager) IssueTenantMember(
	subject string,
	tenantID string,
	permissions []string,
	authorizationVersion int,
) (string, time.Time, error) {
	if tenantID == "" {
		return "", time.Time{}, errors.New("JWT tenant ID is required")
	}
	return m.issue(subject, AudienceTenant, tenantID, permissions, &authorizationVersion)
}

func (m *JWTManager) IssueReporterUnsubscribe(
	issueID string,
	tenantID string,
) (string, time.Time, error) {
	if tenantID == "" {
		return "", time.Time{}, errors.New("JWT tenant ID is required")
	}
	return m.issue(issueID, AudienceReporterUnsubscribe, tenantID, nil, nil)
}

func (m *JWTManager) IssueFeedbackReporterUnsubscribe(
	feedbackID string,
	tenantID string,
) (string, time.Time, error) {
	if tenantID == "" {
		return "", time.Time{}, errors.New("JWT tenant ID is required")
	}
	return m.issue(feedbackID, AudienceReporterUnsubscribe, tenantID, []string{reporterFeedbackPermission}, nil)
}

func (m *JWTManager) IssueMemberNotificationUnsubscribe(
	memberID string,
	tenantID string,
) (string, time.Time, error) {
	if tenantID == "" {
		return "", time.Time{}, errors.New("JWT tenant ID is required")
	}
	return m.issue(memberID, AudienceMemberUnsubscribe, tenantID, nil, nil)
}

func IsFeedbackReporterUnsubscribe(claims Claims) bool {
	return len(claims.Permissions) == 1 && claims.Permissions[0] == reporterFeedbackPermission
}

func (m *JWTManager) DeriveRefreshToken(domain, current string) (string, string, error) {
	if domain != AudiencePlatform && domain != AudienceTenant {
		return "", "", errors.New("refresh token domain is invalid")
	}
	if current == "" {
		return "", "", errors.New("current refresh token is required")
	}
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte("fidibeki-refresh-v1\x00"))
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte{'\x00'})
	_, _ = mac.Write([]byte(current))
	raw := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return raw, HashToken(raw), nil
}

func (m *JWTManager) issue(
	subject string,
	audience string,
	tenantID string,
	permissions []string,
	authorizationVersion *int,
) (string, time.Time, error) {
	if subject == "" {
		return "", time.Time{}, errors.New("JWT subject is required")
	}
	if audience != AudiencePlatform && audience != AudienceTenant &&
		audience != AudienceReporterUnsubscribe && audience != AudienceMemberUnsubscribe {
		return "", time.Time{}, errors.New("JWT audience is invalid")
	}
	now := m.now()
	expiresAt := now.Add(m.ttl)
	claims := Claims{
		Permissions:          append([]string(nil), permissions...),
		TenantID:             tenantID,
		AuthorizationVersion: authorizationVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: m.issuer, Subject: subject, Audience: jwt.ClaimStrings{audience},
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return token, expiresAt, nil
}

func (m *JWTManager) Verify(raw, audience string) (Claims, error) {
	claims := Claims{}
	token, err := jwt.ParseWithClaims(
		raw,
		&claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidAccessToken
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(audience),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil || !token.Valid {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidAccessToken, err)
	}
	return claims, nil
}
