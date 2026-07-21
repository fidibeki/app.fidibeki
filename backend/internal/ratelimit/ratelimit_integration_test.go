package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRedisLimiterEnforcesGCRAWindow(t *testing.T) {
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
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve Redis host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("resolve Redis port: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%s", host, port.Port())})
	t.Cleanup(func() { _ = client.Close() })
	limiter := NewRedisLimiter(client)
	window := Window{Limit: 2, Period: time.Minute}
	first, err := limiter.Allow(ctx, "project:test", window)
	if err != nil || !first.Allowed || first.Limit != 2 || first.Remaining != 1 {
		t.Fatalf("first decision = %#v, error = %v", first, err)
	}
	second, err := limiter.Allow(ctx, "project:test", window)
	if err != nil || !second.Allowed || second.Remaining != 0 {
		t.Fatalf("second decision = %#v, error = %v", second, err)
	}
	third, err := limiter.Allow(ctx, "project:test", window)
	if err != nil || third.Allowed || third.RetryAfter <= 0 {
		t.Fatalf("third decision = %#v, error = %v", third, err)
	}
}
