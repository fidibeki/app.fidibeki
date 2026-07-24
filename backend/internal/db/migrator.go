package db

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

type Migrator struct {
	migrate *migrate.Migrate
}

func NewMigrator(databaseURL, migrationsPath string) (*Migrator, error) {
	absolutePath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return nil, fmt.Errorf("resolve migrations path: %w", err)
	}
	sourceURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolutePath)}).String()
	database, err := url.Parse(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse migration database URL: %w", err)
	}
	if database.Scheme != "postgres" && database.Scheme != "postgresql" {
		return nil, fmt.Errorf("migration database URL must use postgres or postgresql scheme")
	}
	database.Scheme = "pgx5"

	runner, err := migrate.New(sourceURL, database.String())
	if err != nil {
		return nil, fmt.Errorf("initialize migrations: %w", err)
	}
	return &Migrator{migrate: runner}, nil
}

func (m *Migrator) Up() error {
	if err := m.migrate.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func (m *Migrator) Down() error {
	if err := m.migrate.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back migrations: %w", err)
	}
	return nil
}

func (m *Migrator) Steps(count int) error {
	if err := m.migrate.Steps(count); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate %d steps: %w", count, err)
	}
	return nil
}

func (m *Migrator) Migrate(version uint) error {
	if err := m.migrate.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate to version %d: %w", version, err)
	}
	return nil
}

func (m *Migrator) Version() (uint, bool, error) {
	version, dirty, err := m.migrate.Version()
	if err != nil {
		return 0, false, fmt.Errorf("read migration version: %w", err)
	}
	return version, dirty, nil
}

func (m *Migrator) Close() error {
	sourceErr, databaseErr := m.migrate.Close()
	if err := errors.Join(sourceErr, databaseErr); err != nil {
		return fmt.Errorf("close migration resources: %w", err)
	}
	return nil
}
