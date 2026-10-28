package samlsso

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/abuse"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestAbuseStartGuardAtomicallyLimitsOneNetworkWithoutExhaustingTheTenant(t *testing.T) {
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
	keyring, err := abuse.NewKeyring(startGuardTestSecret, "")
	if err != nil {
		t.Fatalf("create keyring: %v", err)
	}
	guard := AbuseStartGuard{Evaluator: abuse.Evaluator{
		Limiter: ratelimit.NewRedisLimiter(client),
		Keyring: keyring,
	}}
	attempt := StartAttempt{TenantID: "tenant-secret", Network: "203.0.113.18"}

	const attempts = 20
	start := make(chan struct{})
	results := make(chan struct {
		decision StartDecision
		err      error
	}, attempts)
	var group sync.WaitGroup
	for range attempts {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			decision, allowErr := guard.Allow(ctx, attempt)
			results <- struct {
				decision StartDecision
				err      error
			}{decision: decision, err: allowErr}
		}()
	}
	close(start)
	group.Wait()
	close(results)

	allowed := 0
	suppressed := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("allow concurrent SAML start: %v", result.err)
		}
		switch {
		case result.decision.Allowed:
			allowed++
		case result.decision.Reason == ReasonSAMLStartRateLimited:
			suppressed++
		default:
			t.Errorf("unexpected decision = %#v", result.decision)
		}
	}
	if allowed != 10 || suppressed != attempts-10 {
		t.Fatalf("allowed = %d, suppressed = %d, want 10 and %d", allowed, suppressed, attempts-10)
	}

	keys, err := client.Keys(ctx, "*").Result()
	if err != nil {
		t.Fatalf("list limiter keys: %v", err)
	}
	for _, key := range keys {
		for _, raw := range []string{attempt.TenantID, attempt.Network} {
			if strings.Contains(key, raw) {
				t.Errorf("limiter key %q contains raw identity %q", key, raw)
			}
		}
	}

	additionalAllowed := 0
	for index := range 91 {
		decision, err := guard.Allow(ctx, StartAttempt{
			TenantID: attempt.TenantID,
			Network:  fmt.Sprintf("198.51.100.%d", index+1),
		})
		if err != nil {
			t.Fatalf("allow distinct network %d: %v", index, err)
		}
		if decision.Allowed {
			additionalAllowed++
		}
	}
	if additionalAllowed != 90 {
		t.Fatalf("additional tenant allowance = %d, want 90 after 10 accepted starts", additionalAllowed)
	}
}
