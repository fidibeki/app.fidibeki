package cache_test

import (
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/cache"
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
