package platformadmin

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
)

func TestRequestMagicLinkIsNonEnumeratingAndQueuesKnownAdmin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeAuthStore{admin: Admin{ID: "admin-id", Email: "admin@example.com"}}
	queue := &captureEnqueuer{}
	service := Service{
		Store: store, Email: queue, PublicBaseURL: "https://api.fidibeki.com",
		AuthFrom: "Fidibeki <auth@fidibeki.com>", MagicLinkTTL: 10 * time.Minute,
		Now: func() time.Time { return now },
	}

	if err := service.RequestMagicLink(context.Background(), " ADMIN@example.com "); err != nil {
		t.Fatalf("RequestMagicLink() error = %v", err)
	}
	if store.lookupEmail != "admin@example.com" {
		t.Errorf("lookup email = %q", store.lookupEmail)
	}
	if store.magicHash == "" || !store.magicExpiry.Equal(now.Add(10*time.Minute)) {
		t.Errorf("magic hash = %q, expiry = %s", store.magicHash, store.magicExpiry)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("queued messages = %d, want 1", len(queue.messages))
	}
	link := strings.Fields(queue.messages[0].Text)[8]
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse magic link: %v", err)
	}
	rawToken := parsed.Query().Get("token")
	if rawToken == "" || auth.HashToken(rawToken) != store.magicHash {
		t.Errorf("magic link token does not match stored hash")
	}

	store.findErr = ErrAdminNotFound
	if err := service.RequestMagicLink(context.Background(), "missing@example.com"); err != nil {
		t.Fatalf("unknown RequestMagicLink() error = %v", err)
	}
	if len(queue.messages) != 1 {
		t.Errorf("unknown admin queued an email")
	}
}

func TestRequestMagicLinkDeletesLinkWhenQueueFails(t *testing.T) {
	t.Parallel()

	store := &fakeAuthStore{admin: Admin{ID: "admin-id", Email: "admin@example.com"}}
	service := Service{
		Store: store, Email: &captureEnqueuer{err: errors.New("redis unavailable")},
		PublicBaseURL: "https://api.fidibeki.com", AuthFrom: "Fidibeki <auth@fidibeki.com>",
	}
	if err := service.RequestMagicLink(context.Background(), "admin@example.com"); err == nil {
		t.Fatal("RequestMagicLink() error = nil")
	}
	if store.deletedHash == "" || store.deletedHash != store.magicHash {
		t.Errorf("deleted hash = %q, created hash = %q", store.deletedHash, store.magicHash)
	}
}

func TestVerifyRefreshAndLogoutIssueAudienceBoundTokens(t *testing.T) {
	t.Parallel()

	admin := Admin{
		ID: "admin-id", Email: "admin@example.com",
		Permissions: []string{string(auth.PermissionPlatformTenantRead)},
	}
	store := &fakeAuthStore{admin: admin}
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	service := Service{Store: store, JWT: jwtManager}

	verified, err := service.VerifyMagicLink(context.Background(), "magic-token", "agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("VerifyMagicLink() error = %v", err)
	}
	if store.consumedHash != auth.HashToken("magic-token") || store.nextRefreshHash != auth.HashToken(verified.RefreshToken) {
		t.Errorf("consume hash = %q, refresh hash = %q", store.consumedHash, store.nextRefreshHash)
	}
	claims, err := jwtManager.Verify(verified.AccessToken, auth.AudiencePlatform)
	if err != nil || claims.Subject != admin.ID {
		t.Fatalf("verify access token claims = %#v, error = %v", claims, err)
	}

	refreshed, err := service.Refresh(context.Background(), verified.RefreshToken, "agent-2", "127.0.0.2")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.RefreshToken == verified.RefreshToken || store.currentRefreshHash != auth.HashToken(verified.RefreshToken) {
		t.Error("refresh token was not rotated")
	}
	if err := service.Logout(context.Background(), refreshed.RefreshToken); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if store.revokedHash != auth.HashToken(refreshed.RefreshToken) {
		t.Errorf("revoked hash = %q", store.revokedHash)
	}
}

type fakeAuthStore struct {
	admin              Admin
	findErr            error
	lookupEmail        string
	magicHash          string
	magicExpiry        time.Time
	deletedHash        string
	consumedHash       string
	nextRefreshHash    string
	currentRefreshHash string
	revokedHash        string
	consumeErr         error
	rotateErr          error
}

func (s *fakeAuthStore) FindAdminByEmail(_ context.Context, emailAddress string) (Admin, error) {
	s.lookupEmail = emailAddress
	return s.admin, s.findErr
}

func (s *fakeAuthStore) CreateMagicLink(_ context.Context, _ Admin, hash string, expiry time.Time) error {
	s.magicHash = hash
	s.magicExpiry = expiry
	return nil
}

func (s *fakeAuthStore) DeleteMagicLink(_ context.Context, hash string) error {
	s.deletedHash = hash
	return nil
}

func (s *fakeAuthStore) ConsumeMagicLink(
	_ context.Context, magicHash, refreshHash, _, _ string,
) (Admin, error) {
	s.consumedHash = magicHash
	s.nextRefreshHash = refreshHash
	return s.admin, s.consumeErr
}

func (s *fakeAuthStore) RotateSession(
	_ context.Context, currentHash, nextHash, _, _ string,
) (Admin, error) {
	s.currentRefreshHash = currentHash
	s.nextRefreshHash = nextHash
	return s.admin, s.rotateErr
}

func (s *fakeAuthStore) RevokeSession(_ context.Context, hash string) error {
	s.revokedHash = hash
	return nil
}

type captureEnqueuer struct {
	messages []email.Message
	err      error
}

func (e *captureEnqueuer) Enqueue(_ context.Context, message email.Message) error {
	e.messages = append(e.messages, message)
	return e.err
}
