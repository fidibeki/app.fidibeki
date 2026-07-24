package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestOpaqueTokensAreRandomAndStoredAsHashes(t *testing.T) {
	t.Parallel()

	first, firstHash, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("NewOpaqueToken() error = %v", err)
	}
	second, secondHash, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("NewOpaqueToken() error = %v", err)
	}
	if first == second || firstHash == secondHash {
		t.Fatal("opaque tokens are not unique")
	}
	if strings.Contains(firstHash, first) || len(firstHash) != 64 || HashToken(first) != firstHash {
		t.Errorf("raw token = %q, hash = %q", first, firstHash)
	}
}

func TestJWTManagerDerivesStableSecretBoundRefreshSuccessor(t *testing.T) {
	t.Parallel()

	manager, _ := NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Minute)
	first, firstHash, err := manager.DeriveRefreshToken(AudienceTenant, "current-token")
	if err != nil {
		t.Fatalf("DeriveRefreshToken() error = %v", err)
	}
	retry, retryHash, err := manager.DeriveRefreshToken(AudienceTenant, "current-token")
	if err != nil || retry != first || retryHash != firstHash {
		t.Fatalf("retry token/hash = %q/%q, error = %v", retry, retryHash, err)
	}
	if len(first) != 43 || firstHash != HashToken(first) {
		t.Fatalf("derived token/hash = %q/%q", first, firstHash)
	}
	next, _, _ := manager.DeriveRefreshToken(AudienceTenant, "other-token")
	otherManager, _ := NewJWTManager(strings.Repeat("o", 32), "fidibeki", time.Minute)
	otherSecret, _, _ := otherManager.DeriveRefreshToken(AudienceTenant, "current-token")
	if next == first || otherSecret == first {
		t.Fatal("derived refresh token is not bound to both the current token and server secret")
	}
	platform, _, _ := manager.DeriveRefreshToken(AudiencePlatform, "current-token")
	if platform == first {
		t.Fatal("derived refresh token is not domain separated")
	}
	if _, _, err := manager.DeriveRefreshToken(AudienceTenant, ""); err == nil {
		t.Error("DeriveRefreshToken(empty) error = nil")
	}
	if _, _, err := manager.DeriveRefreshToken("other", "current-token"); err == nil {
		t.Error("DeriveRefreshToken(other domain) error = nil")
	}
}

func TestJWTManagerIssuesAndVerifiesAudienceBoundTokens(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	manager, err := NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	manager.now = func() time.Time { return now }
	raw, expiresAt, err := manager.Issue("admin-id", AudiencePlatform, []string{"platform:tenant:read"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if !expiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Errorf("expiresAt = %s", expiresAt)
	}
	claims, err := manager.Verify(raw, AudiencePlatform)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.Subject != "admin-id" || !HasPermission(claims.Permissions, PermissionPlatformTenantRead) {
		t.Errorf("claims = %#v", claims)
	}
	if _, err := manager.Verify(raw, AudienceTenant); !errors.Is(err, ErrInvalidAccessToken) {
		t.Errorf("tenant Verify() error = %v", err)
	}
	manager.now = func() time.Time { return now.Add(11 * time.Minute) }
	if _, err := manager.Verify(raw, AudiencePlatform); !errors.Is(err, ErrInvalidAccessToken) {
		t.Errorf("expired Verify() error = %v", err)
	}
}

func TestJWTManagerRejectsUnexpectedAlgorithm(t *testing.T) {
	t.Parallel()

	manager, _ := NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "fidibeki", Subject: "admin-id", Audience: jwt.ClaimStrings{AudiencePlatform},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none token: %v", err)
	}
	if _, err := manager.Verify(raw, AudiencePlatform); !errors.Is(err, ErrInvalidAccessToken) {
		t.Errorf("Verify() error = %v", err)
	}
}

func TestJWTManagerValidatesConstructionAndIssueInputs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		secret string
		issuer string
		ttl    time.Duration
	}{
		{name: "short secret", secret: "short", issuer: "fidibeki", ttl: time.Minute},
		{name: "missing issuer", secret: strings.Repeat("s", 32), ttl: time.Minute},
		{name: "invalid ttl", secret: strings.Repeat("s", 32), issuer: "fidibeki"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewJWTManager(test.secret, test.issuer, test.ttl); err == nil {
				t.Fatal("NewJWTManager() error = nil")
			}
		})
	}
	manager, _ := NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Minute)
	if _, _, err := manager.Issue("", AudiencePlatform, nil); err == nil {
		t.Error("Issue(empty subject) error = nil")
	}
	if _, _, err := manager.Issue("id", "other", nil); err == nil {
		t.Error("Issue(other audience) error = nil")
	}
}

func TestPlatformPermissionsAreExplicit(t *testing.T) {
	t.Parallel()
	permissions := PlatformPermissionStrings()
	if len(permissions) != 9 {
		t.Fatalf("platform permission count = %d, want 9", len(permissions))
	}
	if HasPermission(permissions, Permission("platform:*")) {
		t.Error("wildcard permission was accepted")
	}
}

func TestTenantPermissionsAreExplicitAndValidated(t *testing.T) {
	t.Parallel()
	permissions := TenantPermissionStrings()
	if len(permissions) != 33 {
		t.Fatalf("tenant permission count = %d, want 33", len(permissions))
	}
	if !ValidTenantPermissions(permissions) {
		t.Error("full tenant permission set is invalid")
	}
	if ValidTenantPermissions([]string{"project:read", "project:read"}) {
		t.Error("duplicate permission was accepted")
	}
	if ValidTenantPermissions([]string{"project:*"}) {
		t.Error("wildcard permission was accepted")
	}
}

func TestJWTManagerIssuesTenantBoundToken(t *testing.T) {
	t.Parallel()
	manager, _ := NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Minute)
	raw, _, err := manager.IssueTenant("member-id", "tenant-id", []string{"project:read"})
	if err != nil {
		t.Fatalf("IssueTenant() error = %v", err)
	}
	claims, err := manager.Verify(raw, AudienceTenant)
	if err != nil || claims.TenantID != "tenant-id" || claims.Subject != "member-id" ||
		claims.AuthorizationVersion == nil || *claims.AuthorizationVersion != 0 {
		t.Fatalf("tenant claims = %#v, error = %v", claims, err)
	}
	if _, _, err := manager.IssueTenant("member-id", "", nil); err == nil {
		t.Error("IssueTenant(empty tenant) error = nil")
	}
}

func TestJWTManagerIssuesVersionedTenantMemberToken(t *testing.T) {
	t.Parallel()
	manager, _ := NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Minute)
	raw, _, err := manager.IssueTenantMember("member-id", "tenant-id", []string{"project:read"}, 7)
	if err != nil {
		t.Fatalf("IssueTenantMember() error = %v", err)
	}
	claims, err := manager.Verify(raw, AudienceTenant)
	if err != nil || claims.AuthorizationVersion == nil || *claims.AuthorizationVersion != 7 {
		t.Fatalf("versioned tenant claims = %#v, error = %v", claims, err)
	}
}

func TestJWTManagerIssuesReporterUnsubscribeToken(t *testing.T) {
	t.Parallel()
	manager, _ := NewJWTManager(strings.Repeat("s", 32), "fidibeki", 90*24*time.Hour)
	raw, expiresAt, err := manager.IssueReporterUnsubscribe("issue-id", "tenant-id")
	if err != nil {
		t.Fatalf("IssueReporterUnsubscribe() error = %v", err)
	}
	claims, err := manager.Verify(raw, AudienceReporterUnsubscribe)
	if err != nil || claims.Subject != "issue-id" || claims.TenantID != "tenant-id" {
		t.Fatalf("reporter claims = %#v, error = %v", claims, err)
	}
	if time.Until(expiresAt) < 89*24*time.Hour {
		t.Errorf("reporter token expiry = %s", expiresAt)
	}
	if _, err := manager.Verify(raw, AudienceTenant); !errors.Is(err, ErrInvalidAccessToken) {
		t.Errorf("tenant Verify(reporter token) error = %v", err)
	}
	if _, _, err := manager.IssueReporterUnsubscribe("issue-id", ""); err == nil {
		t.Error("IssueReporterUnsubscribe(empty tenant) error = nil")
	}
	feedbackToken, _, err := manager.IssueFeedbackReporterUnsubscribe("feedback-id", "tenant-id")
	if err != nil {
		t.Fatalf("IssueFeedbackReporterUnsubscribe() error = %v", err)
	}
	feedbackClaims, err := manager.Verify(feedbackToken, AudienceReporterUnsubscribe)
	if err != nil || feedbackClaims.Subject != "feedback-id" ||
		!IsFeedbackReporterUnsubscribe(feedbackClaims) {
		t.Fatalf("feedback reporter claims = %#v, error = %v", feedbackClaims, err)
	}
}
