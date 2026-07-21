package db

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
)

type runtimeRole struct {
	name      string
	password  string
	bypassRLS bool
}

// ConfigureRuntimeRoles enables the two least-privilege roles created by the
// migrations using credentials already present in their runtime database URLs.
func ConfigureRuntimeRoles(ctx context.Context, directURL, appURL, adminURL string) error {
	roles := make([]runtimeRole, 0, 2)
	for _, roleURL := range []struct {
		name      string
		url       string
		bypassRLS bool
	}{
		{name: "fidibeki_app", url: appURL, bypassRLS: false},
		{name: "fidibeki_admin", url: adminURL, bypassRLS: true},
	} {
		password, err := runtimeRolePassword(roleURL.url, roleURL.name)
		if err != nil {
			return err
		}
		roles = append(roles, runtimeRole{
			name: roleURL.name, password: password, bypassRLS: roleURL.bypassRLS,
		})
	}

	connection, err := pgx.Connect(ctx, directURL)
	if err != nil {
		return fmt.Errorf("connect for runtime role configuration: %w", err)
	}
	defer func() { _ = connection.Close(ctx) }()

	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin runtime role configuration: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	for _, role := range roles {
		bypassRLS := "NOBYPASSRLS"
		if role.bypassRLS {
			bypassRLS = "BYPASSRLS"
		}
		var statement string
		if err := transaction.QueryRow(ctx, `
			SELECT format(
				'ALTER ROLE %I WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION ' || $3::text,
				$1::text,
				$2::text
			)
		`, role.name, role.password, bypassRLS).Scan(&statement); err != nil {
			return fmt.Errorf("build %s role configuration: %w", role.name, err)
		}
		if _, err := transaction.Exec(ctx, statement); err != nil {
			return fmt.Errorf("configure %s role: %w", role.name, err)
		}
	}

	var grantConnect string
	if err := transaction.QueryRow(ctx, `
		SELECT format(
			'GRANT CONNECT ON DATABASE %I TO fidibeki_app, fidibeki_admin',
			current_database()
		)
	`).Scan(&grantConnect); err != nil {
		return fmt.Errorf("build runtime role database grant: %w", err)
	}
	if _, err := transaction.Exec(ctx, grantConnect); err != nil {
		return fmt.Errorf("grant runtime role database access: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit runtime role configuration: %w", err)
	}
	return nil
}

func runtimeRolePassword(databaseURL, expectedRole string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User == nil {
		return "", fmt.Errorf("DATABASE_URL for %s must be an absolute URL with credentials", expectedRole)
	}
	if parsed.User.Username() != expectedRole {
		return "", fmt.Errorf("DATABASE_URL for %s must use the %s role", expectedRole, expectedRole)
	}
	password, ok := parsed.User.Password()
	if !ok || password == "" {
		return "", fmt.Errorf("DATABASE_URL for %s must include a non-empty password", expectedRole)
	}
	return password, nil
}
