package ai

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"testing"
	"time"
)

func TestConnectionConcurrencyIsIsolatedAndReleased(t *testing.T) {
	if testing.Short() {
		t.Skip("Redis integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	container, err := testcontainers.Run(ctx, "redis:7.4-alpine", testcontainers.WithExposedPorts("6379/tcp"), testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%s", host, port.Port())})
	defer client.Close()
	leaser := RedisConnectionLeaser{Client: client}
	release, err := leaser.Acquire(ctx, "tenant-a:connection-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = leaser.Acquire(ctx, "tenant-a:connection-a", 1); err == nil {
		t.Fatal("concurrency limit exceeded")
	}
	releaseOther, err := leaser.Acquire(ctx, "tenant-b:connection-b", 1)
	if err != nil {
		t.Fatal("another account was blocked", err)
	}
	releaseOther()
	release()
	release, err = leaser.Acquire(ctx, "tenant-a:connection-a", 1)
	if err != nil {
		t.Fatal("lease was not released", err)
	}
	release()
	// A crashed worker's expired lease must not block the connection forever.
	client.ZAdd(ctx, "ai-concurrency:tenant-a:connection-a", redis.Z{Score: 1, Member: "expired-worker"})
	release, err = leaser.Acquire(ctx, "tenant-a:connection-a", 1)
	if err != nil {
		t.Fatal("expired lease blocked connection", err)
	}
	release()
}
