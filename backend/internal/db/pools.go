package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pools struct {
	App   *pgxpool.Pool
	Admin *pgxpool.Pool
}

func ParsePoolConfig(databaseURL, applicationName string, maxConnections int32) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse %s database URL: %w", applicationName, err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	config.MaxConns = maxConnections
	return config, nil
}

func OpenPools(
	ctx context.Context,
	appURL string,
	adminURL string,
	appMaxConnections int32,
	adminMaxConnections int32,
) (*Pools, error) {
	appConfig, err := ParsePoolConfig(appURL, "fidibeki-app", appMaxConnections)
	if err != nil {
		return nil, err
	}
	adminConfig, err := ParsePoolConfig(adminURL, "fidibeki-admin", adminMaxConnections)
	if err != nil {
		return nil, err
	}

	appPool, err := pgxpool.NewWithConfig(ctx, appConfig)
	if err != nil {
		return nil, fmt.Errorf("open app database pool: %w", err)
	}
	if err := appPool.Ping(ctx); err != nil {
		appPool.Close()
		return nil, fmt.Errorf("ping app database pool: %w", err)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		appPool.Close()
		return nil, fmt.Errorf("open admin database pool: %w", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		appPool.Close()
		return nil, fmt.Errorf("ping admin database pool: %w", err)
	}

	return &Pools{App: appPool, Admin: adminPool}, nil
}

func (p *Pools) Close() {
	p.App.Close()
	p.Admin.Close()
}

type PoolCheck struct {
	Pool *pgxpool.Pool
}

type PoolsCheck struct {
	Pools *Pools
}

func (c PoolsCheck) Check(ctx context.Context) error {
	if err := c.Pools.App.Ping(ctx); err != nil {
		return fmt.Errorf("ping app database pool: %w", err)
	}
	if err := c.Pools.Admin.Ping(ctx); err != nil {
		return fmt.Errorf("ping admin database pool: %w", err)
	}
	return nil
}

func (c PoolCheck) Check(ctx context.Context) error {
	return c.Pool.Ping(ctx)
}
