package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian-api/internal/config"
	"github.com/twistingmercury/librarian-api/internal/telemetry"
)

func createTestConfig() *config.MnemonicConfig {
	return &config.MnemonicConfig{
		Server: config.ServerConfig{
			Host:            "localhost",
			Port:            8080,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Database: config.DatabaseConfig{
			Postgres: config.PostgresConfig{
				Host:            "localhost",
				Port:            5432,
				Database:        "mnemonic",
				Username:        "mnemonic",
				SSLMode:         "disable",
				MaxOpenConns:    25,
				MinConns:        5,
				ConnMaxLifetime: 5 * time.Minute,
			},
			Neo4j: config.Neo4jConfig{
				URI:                          "bolt://localhost:7687",
				Username:                     "neo4j",
				Database:                     "neo4j",
				MaxConnectionPoolSize:        50,
				ConnectionAcquisitionTimeout: 60 * time.Second,
			},
		},
		OpenAI: config.OpenAIConfig{
			EmbeddingModel:      "text-embedding-3-small",
			EmbeddingDimensions: 1536,
			RetryAttempts:       3,
			RetryDelay:          time.Second,
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		Observability: config.ObservabilityConfig{
			Metrics: config.MetricsConfig{
				Enabled: false, // Disable for tests
				Path:    "/metrics",
				Port:    9090,
			},
			Tracing: config.TracingConfig{
				Enabled:      false, // Disable for tests
				Endpoint:     "",
				SampleRate:   0.1,
				OTLPInsecure: true,
			},
		},
	}
}

func TestInitialize(t *testing.T) {
	cfg := createTestConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)

	// Clean shutdown
	err = tel.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestInitializeWithMetricsEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Observability.Metrics.Enabled = true
	cfg.Observability.Metrics.Port = 19090 // Use a high port to avoid conflicts

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)

	defer func() {
		_ = tel.Shutdown(ctx)
	}()
}

func TestInitializeWithTracingEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Observability.Tracing.Enabled = true
	cfg.Observability.Tracing.Endpoint = "localhost:4317"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)

	defer func() {
		_ = tel.Shutdown(ctx)
	}()
}

func TestShutdown(t *testing.T) {
	cfg := createTestConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)

	// Shutdown should not error
	err = tel.Shutdown(ctx)
	assert.NoError(t, err)
}
