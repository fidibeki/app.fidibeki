package cache_test

import (
	"crypto/tls"
	"testing"

	"github.com/fidibeki/app.fidibeki/backend/internal/cache"
)

func TestOpenValidatesRedisURL(t *testing.T) {
	t.Parallel()

	if _, err := cache.Open("not a redis URL"); err == nil {
		t.Fatal("Open() error = nil, want invalid URL error")
	}
	client, err := cache.Open("redis://localhost:6379/3")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if client.Client() == nil {
		t.Fatal("Client() = nil")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestOpenSecureURLConfiguresVerifiedTLS(t *testing.T) {
	t.Parallel()

	client, err := cache.Open("rediss://redis.fidibeki.test:6380/3")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	options := client.Client().Options()
	if options.TLSConfig == nil {
		t.Fatal("TLSConfig = nil")
	}
	if options.TLSConfig.ServerName != "redis.fidibeki.test" {
		t.Errorf("TLS server name = %q", options.TLSConfig.ServerName)
	}
	if options.TLSConfig.MinVersion < tls.VersionTLS12 {
		t.Errorf("TLS minimum version = %d, want TLS 1.2 or newer", options.TLSConfig.MinVersion)
	}
}
