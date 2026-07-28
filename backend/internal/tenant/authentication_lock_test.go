package tenant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestLockForAuthenticationEvaluatesTheCurrentLockedLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 14, 0, 0, 0, time.UTC)
	databaseErr := errors.New("database unavailable")

	for _, test := range []struct {
		name    string
		status  Status
		scanErr error
		wantErr error
	}{
		{name: "active", status: StatusActive},
		{name: "inactive", status: StatusSuspended, wantErr: ErrInactive},
		{name: "missing", scanErr: pgx.ErrNoRows, wantErr: ErrUnknownTenant},
		{name: "database failure", scanErr: databaseErr, wantErr: databaseErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			querier := &resolverQuerier{row: resolverRow{scan: func(destinations ...any) error {
				if test.scanErr != nil {
					return test.scanErr
				}
				if len(destinations) != 3 {
					return errors.New("unexpected destination count")
				}
				*destinations[0].(*string) = string(test.status)
				*destinations[1].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
				*destinations[2].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
				return nil
			}}}

			err := LockForAuthentication(context.Background(), querier, "tenant-id", now)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("LockForAuthentication() error = %v, want %v", err, test.wantErr)
			}
			if !strings.Contains(querier.query, "FOR SHARE") {
				t.Fatalf("query does not lock tenant lifecycle: %s", querier.query)
			}
			if querier.slug != "tenant-id" {
				t.Fatalf("tenant argument = %q, want tenant-id", querier.slug)
			}
		})
	}
}
