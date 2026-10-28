package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestTestimonialMigrationsRoundTrip proves 000116-000120 can be rolled back and
// reapplied. A down migration that only fails when someone needs it is worse
// than no down migration at all.
func TestTestimonialMigrationsRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	migrator, err := db.NewMigrator(dsn, migrationsPath(t))
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })

	for round := 1; round <= 2; round++ {
		if err := migrator.Migrate(120); err != nil {
			t.Fatalf("round %d: migrate up to 120: %v", round, err)
		}
		if err := migrator.Migrate(115); err != nil {
			t.Fatalf("round %d: migrate down to 115: %v", round, err)
		}
		version, dirty, err := migrator.Version()
		if err != nil || dirty || version != 115 {
			t.Fatalf("round %d: version = %d dirty = %t err = %v", round, version, dirty, err)
		}
	}
}
