package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	AudiencePlatform            = "platform"
	AudienceTenant              = "tenant"
	AudienceReporterUnsubscribe = "reporter-unsubscribe"
)

var ErrInvalidAccessToken = errors.New("invalid access token")

type Claims struct {
	Permissions []string `json:"permissions"`
	TenantID    string   `json:"tenant_id,omitempty"`
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
	return m.issue(subject, audience, "", permissions)
}

func (m *JWTManager) IssueTenant(
	subject string,
	tenantID string,
	permissions []string,
) (string, time.Time, error) {
	if tenantID == "" {
		return "", time.Time{}, errors.New("JWT tenant ID is required")
	}
	return m.issue(subject, AudienceTenant, tenantID, permissions)
}

func (m *JWTManager) IssueReporterUnsubscribe(
	issueID string,
	tenantID string,
) (string, time.Time, error) {
	if tenantID == "" {
		return "", time.Time{}, errors.New("JWT tenant ID is required")
	}
	return m.issue(issueID, AudienceReporterUnsubscribe, tenantID, nil)
}

func (m *JWTManager) issue(
	subject string,
	audience string,
	tenantID string,
	permissions []string,
) (string, time.Time, error) {
	if subject == "" {
		return "", time.Time{}, errors.New("JWT subject is required")
	}
	if audience != AudiencePlatform && audience != AudienceTenant &&
		audience != AudienceReporterUnsubscribe {
		return "", time.Time{}, errors.New("JWT audience is invalid")
	}
	now := m.now()
	expiresAt := now.Add(m.ttl)
	claims := Claims{
		Permissions: append([]string(nil), permissions...),
		TenantID:    tenantID,
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
