package db_test

import (
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
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
