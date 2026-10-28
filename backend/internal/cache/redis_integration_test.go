package cache_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/cache"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRedisReadinessAgainstRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Redis integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	container, err := testcontainers.Run(ctx, "redis:7.4-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections")),
	)
	if err != nil {
		t.Fatalf("start Redis container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate Redis container: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve Redis host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("resolve Redis port: %v", err)
	}

	client, err := cache.Open(fmt.Sprintf("redis://%s:%s/0", host, port.Port()))
	if err != nil {
		t.Fatalf("open Redis client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	})
	if err := client.Check(ctx); err != nil {
		t.Fatalf("Redis readiness: %v", err)
	}
}
