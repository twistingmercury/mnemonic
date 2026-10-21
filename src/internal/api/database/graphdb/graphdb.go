package graphdb

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jcfg "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"github.com/twistingmercury/mnemonic/internal/api/config"
)

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
