package db_test

import (
	"testing"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

func TestPoolConfigIsSafeForPgbouncerTransactionMode(t *testing.T) {
	t.Parallel()

	config, err := db.ParsePoolConfig(
		"postgres://fidibeki_app:secret@localhost:6432/fidibeki?sslmode=disable",
		"fidibeki-app",
		12,
	)
	if err != nil {
		t.Fatalf("ParsePoolConfig() error = %v", err)
	}
	if config.ConnConfig.DefaultQueryExecMode != pgx.QueryExecModeExec {
		t.Errorf("query mode = %v, want exec", config.ConnConfig.DefaultQueryExecMode)
	}
	if got := config.ConnConfig.RuntimeParams["application_name"]; got != "fidibeki-app" {
		t.Errorf("application_name = %q, want fidibeki-app", got)
	}
	if config.MaxConns != 12 {
		t.Errorf("MaxConns = %d, want 12", config.MaxConns)
	}
}

func TestSecurePoolModesNeverFallBackToPlaintext(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			config, err := db.ParsePoolConfig(
				"postgres://fidibeki_app:secret@database.fidibeki.test/fidibeki?sslmode="+mode,
				"fidibeki-app",
				12,
			)
			if err != nil {
				t.Fatalf("ParsePoolConfig() error = %v", err)
			}
			if config.ConnConfig.TLSConfig == nil {
				t.Fatal("primary TLSConfig = nil")
			}
			for index, fallback := range config.ConnConfig.Fallbacks {
				if fallback.TLSConfig == nil {
					t.Fatalf("fallback %d permits plaintext", index)
				}
			}
			if mode == "verify-full" && config.ConnConfig.TLSConfig.InsecureSkipVerify {
				t.Fatal("verify-full disabled standard certificate verification")
			}
		})
	}
}
