// Package database provides factory functions for creating database connections
// used by the mnemonic server. It handles PostgreSQL pool creation and Neo4j
// driver initialization with configuration-driven settings.
package database

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jcfg "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"github.com/twistingmercury/librarian-mcp/internal/config"
)

// NewPostgresPool creates a pgxpool.Pool configured from the provided PostgresConfig.
// Queries are traced and timed with otelpgx, and pool statistics are exported
// as metrics, both through the global OpenTelemetry providers, so telemetry
// must be initialized first.
// The caller is responsible for calling pool.Close() when done.
func NewPostgresPool(ctx context.Context, cfg config.PostgresConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parsing postgres DSN: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxOpenConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime
	poolCfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	if err := otelpgx.RecordStats(pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("recording postgres pool stats: %w", err)
	}

	return pool, nil
}

// NewNeo4jDriver creates a neo4j.DriverWithContext configured from the provided Neo4jConfig.
// The caller is responsible for calling driver.Close(ctx) when done.
func NewNeo4jDriver(ctx context.Context, cfg config.Neo4jConfig) (neo4j.DriverWithContext, error) {

	driver, err := neo4j.NewDriverWithContext(
		cfg.URI,
		neo4j.BasicAuth(cfg.Username, cfg.Password, ""),
		func(driverCfg *neo4jcfg.Config) {
			driverCfg.MaxConnectionPoolSize = cfg.MaxConnectionPoolSize
			driverCfg.ConnectionAcquisitionTimeout = cfg.ConnectionAcquisitionTimeout
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating neo4j driver: %w", err)
	}

	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("verifying neo4j connectivity: %w", err)
	}

	return driver, nil
}
