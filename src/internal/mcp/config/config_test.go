package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/mcp/config"
)

// TestDefaultValues verifies that all default values are set correctly.
func TestDefaultValues(t *testing.T) {
	// Clear any environment variables that might interfere
	clearMnemonicEnvVars(t)

	v := viper.New()
	config.SetDefaults(v)

	cfg := &config.MnemonicConfig{}
	err := v.Unmarshal(cfg)
	require.NoError(t, err)

	// Server defaults
	assert.Equal(t, config.DefaultServerHost, cfg.Server.Host)
	assert.Equal(t, config.DefaultServerPort, cfg.Server.Port)
	assert.Equal(t, config.DefaultServerReadTimeout, cfg.Server.ReadTimeout)
	assert.Equal(t, config.DefaultServerWriteTimeout, cfg.Server.WriteTimeout)
	assert.Equal(t, config.DefaultServerIdleTimeout, cfg.Server.IdleTimeout)
	assert.Equal(t, config.DefaultServerShutdownTimeout, cfg.Server.ShutdownTimeout)
	assert.Equal(t, config.DefaultServerTLSEnabled, cfg.Server.TLS.Enabled)

	// MCP defaults
	assert.Equal(t, config.DefaultMCPPort, cfg.MCP.Port)
	assert.Equal(t, config.DefaultMCPReadTimeout, cfg.MCP.ReadTimeout)
	assert.Equal(t, config.DefaultMCPWriteTimeout, cfg.MCP.WriteTimeout)
	assert.Equal(t, config.DefaultMCPIdleTimeout, cfg.MCP.IdleTimeout)
	assert.Equal(t, config.DefaultMCPDefaultSearchThreshold, cfg.MCP.DefaultSearchThreshold)

	// PostgreSQL defaults
	assert.Equal(t, config.DefaultPostgresHost, cfg.Database.Postgres.Host)
	assert.Equal(t, config.DefaultPostgresPort, cfg.Database.Postgres.Port)
	assert.Equal(t, config.DefaultPostgresDatabase, cfg.Database.Postgres.Database)
	assert.Equal(t, config.DefaultPostgresUsername, cfg.Database.Postgres.Username)
	assert.Equal(t, config.DefaultPostgresSSLMode, cfg.Database.Postgres.SSLMode)
	assert.Equal(t, int32(config.DefaultPostgresMaxOpenConns), cfg.Database.Postgres.MaxOpenConns)
	assert.Equal(t, int32(config.DefaultPostgresMinConns), cfg.Database.Postgres.MinConns)
	assert.Equal(t, config.DefaultPostgresConnMaxLifetime, cfg.Database.Postgres.ConnMaxLifetime)

	// Neo4j defaults
	assert.Equal(t, config.DefaultNeo4jURI, cfg.Database.Neo4j.URI)
	assert.Equal(t, config.DefaultNeo4jUsername, cfg.Database.Neo4j.Username)
	assert.Equal(t, config.DefaultNeo4jDatabase, cfg.Database.Neo4j.Database)
	assert.Equal(t, config.DefaultNeo4jMaxConnectionPoolSize, cfg.Database.Neo4j.MaxConnectionPoolSize)
	assert.Equal(t, config.DefaultNeo4jConnectionAcquisitionTimeout, cfg.Database.Neo4j.ConnectionAcquisitionTimeout)

	// OpenAI defaults
	assert.Equal(t, config.DefaultOpenAIEmbeddingModel, cfg.OpenAI.EmbeddingModel)
	assert.Equal(t, config.DefaultOpenAIEmbeddingDimensions, cfg.OpenAI.EmbeddingDimensions)
	assert.Equal(t, config.DefaultOpenAIRetryAttempts, cfg.OpenAI.RetryAttempts)
	assert.Equal(t, config.DefaultOpenAIRetryDelay, cfg.OpenAI.RetryDelay)

	// Logging defaults
	assert.Equal(t, config.DefaultLoggingLevel, cfg.Logging.Level)

	// Observability defaults
	assert.Equal(t, config.DefaultMetricsEnabled, cfg.Observability.Metrics.Enabled)
	assert.Equal(t, config.DefaultMetricsPath, cfg.Observability.Metrics.Path)
	assert.Equal(t, config.DefaultMetricsPort, cfg.Observability.Metrics.Port)
	assert.Equal(t, config.DefaultTracingEnabled, cfg.Observability.Tracing.Enabled)
	assert.Equal(t, config.DefaultTracingSampleRate, cfg.Observability.Tracing.SampleRate)
	assert.Equal(t, config.DefaultTracingOTLPInsecure, cfg.Observability.Tracing.OTLPInsecure)
}

// TestYAMLFileLoading verifies that configuration can be loaded from a YAML file.
func TestYAMLFileLoading(t *testing.T) {
	clearMnemonicEnvVars(t)

	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  host: 127.0.0.1
  port: 9000
  read_timeout: 45s
  write_timeout: 45s
  idle_timeout: 180s

database:
  postgres:
    host: pghost
    port: 5433
    database: testdb
    username: testuser
    password: testpass
    ssl_mode: require

  neo4j:
    uri: bolt://neo4jhost:7688
    username: neo4juser
    password: neo4jpass
    database: testgraph

openai:
  api_key: sk-test-key
  embedding_model: text-embedding-ada-002
  embedding_dimensions: 1536

logging:
  level: debug
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	t.Setenv(config.EnvConfigFile, configPath)

	cfg, err := config.Load()
	require.NoError(t, err)

	// Verify file values override defaults
	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 9000, cfg.Server.Port)
	assert.Equal(t, 45*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 45*time.Second, cfg.Server.WriteTimeout)
	assert.Equal(t, 180*time.Second, cfg.Server.IdleTimeout)

	assert.Equal(t, "pghost", cfg.Database.Postgres.Host)
	assert.Equal(t, 5433, cfg.Database.Postgres.Port)
	assert.Equal(t, "testdb", cfg.Database.Postgres.Database)
	assert.Equal(t, "testuser", cfg.Database.Postgres.Username)
	assert.Equal(t, "testpass", cfg.Database.Postgres.Password)
	assert.Equal(t, "require", cfg.Database.Postgres.SSLMode)

	assert.Equal(t, "bolt://neo4jhost:7688", cfg.Database.Neo4j.URI)
	assert.Equal(t, "neo4juser", cfg.Database.Neo4j.Username)
	assert.Equal(t, "neo4jpass", cfg.Database.Neo4j.Password)
	assert.Equal(t, "testgraph", cfg.Database.Neo4j.Database)

	assert.Equal(t, "sk-test-key", cfg.OpenAI.APIKey)
	assert.Equal(t, "text-embedding-ada-002", cfg.OpenAI.EmbeddingModel)
	assert.Equal(t, 1536, cfg.OpenAI.EmbeddingDimensions)

	assert.Equal(t, "debug", cfg.Logging.Level)
}

// TestEnvironmentVariableOverrides verifies that environment variables override file values.
func TestEnvironmentVariableOverrides(t *testing.T) {
	clearMnemonicEnvVars(t)

	// Create a config file with some values
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  host: 127.0.0.1
  port: 9000

database:
  postgres:
    host: pghost
    password: filepassword

logging:
  level: info

mcp:
  default_search_threshold: 0.7
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Set environment variables to override
	t.Setenv("LIBRARIAN_SERVER_HOST", "env-host")
	t.Setenv("LIBRARIAN_SERVER_PORT", "8888")
	t.Setenv("LIBRARIAN_DATABASE_POSTGRES_PASSWORD", "envpassword")
	t.Setenv("LIBRARIAN_LOGGING_LEVEL", "error")

	t.Setenv(config.EnvConfigFile, configPath)

	cfg, err := config.Load()
	require.NoError(t, err)

	// Verify environment variables override file values
	assert.Equal(t, "env-host", cfg.Server.Host)
	assert.Equal(t, 8888, cfg.Server.Port)
	assert.Equal(t, "envpassword", cfg.Database.Postgres.Password)
	assert.Equal(t, "error", cfg.Logging.Level)

	// Verify file values are retained when no env override
	assert.Equal(t, "pghost", cfg.Database.Postgres.Host)
	assert.InDelta(t, 0.7, cfg.MCP.DefaultSearchThreshold, 1e-9)
}

// TestEnvironmentVariableNaming verifies correct environment variable naming conventions.
func TestEnvironmentVariableNaming(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		value  string
		check  func(t *testing.T, cfg *config.MnemonicConfig)
	}{
		{
			name:   "server.port",
			envVar: "LIBRARIAN_SERVER_PORT",
			value:  "3000",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 3000, cfg.Server.Port)
			},
		},
		{
			name:   "database.postgres.max_open_conns",
			envVar: "LIBRARIAN_DATABASE_POSTGRES_MAX_OPEN_CONNS",
			value:  "50",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, int32(50), cfg.Database.Postgres.MaxOpenConns)
			},
		},
		{
			name:   "database.neo4j.max_connection_pool_size",
			envVar: "LIBRARIAN_DATABASE_NEO4J_MAX_CONNECTION_POOL_SIZE",
			value:  "100",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 100, cfg.Database.Neo4j.MaxConnectionPoolSize)
			},
		},
		{
			name:   "openai.api_key",
			envVar: "LIBRARIAN_OPENAI_API_KEY",
			value:  "sk-test-123",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, "sk-test-123", cfg.OpenAI.APIKey)
			},
		},
		{
			name:   "mcp.default_search_threshold",
			envVar: "LIBRARIAN_MCP_DEFAULT_SEARCH_THRESHOLD",
			value:  "0.25",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 0.25, cfg.MCP.DefaultSearchThreshold)
			},
		},
		{
			name:   "observability.metrics.enabled",
			envVar: "LIBRARIAN_OBSERVABILITY_METRICS_ENABLED",
			value:  "false",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.False(t, cfg.Observability.Metrics.Enabled)
			},
		},
		{
			name:   "observability.tracing.sample_rate",
			envVar: "LIBRARIAN_OBSERVABILITY_TRACING_SAMPLE_RATE",
			value:  "0.5",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 0.5, cfg.Observability.Tracing.SampleRate)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearMnemonicEnvVars(t)
			t.Setenv(tt.envVar, tt.value)

			cfg, err := config.Load()
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

// TestPostgresPoolSizeOverflow verifies that pool sizes too large for int32
// are rejected when the config is loaded rather than silently wrapped.
func TestPostgresPoolSizeOverflow(t *testing.T) {
	clearMnemonicEnvVars(t)
	t.Setenv("LIBRARIAN_DATABASE_POSTGRES_MAX_OPEN_CONNS", "3000000000")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_open_conns")
}

// TestValidation_ServerConfig tests server configuration validation.
func TestValidation_ServerConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "invalid port - zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 0
			},
			expectError: "server.port",
		},
		{
			name: "invalid port - too high",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 70000
			},
			expectError: "server.port",
		},
		{
			name: "invalid read_timeout - zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.ReadTimeout = 0
			},
			expectError: "server.read_timeout",
		},
		{
			name: "invalid write_timeout - negative",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.WriteTimeout = -1 * time.Second
			},
			expectError: "server.write_timeout",
		},
		{
			name: "invalid idle_timeout - zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.IdleTimeout = 0
			},
			expectError: "server.idle_timeout",
		},
		{
			name: "invalid shutdown_timeout - zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.ShutdownTimeout = 0
			},
			expectError: "server.shutdown_timeout",
		},
		{
			name: "invalid shutdown_timeout - negative",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.ShutdownTimeout = -1 * time.Second
			},
			expectError: "server.shutdown_timeout",
		},
		{
			name: "TLS enabled without cert_file",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.TLS.Enabled = true
				cfg.Server.TLS.CertFile = ""
				cfg.Server.TLS.KeyFile = "/some/key.pem"
			},
			expectError: "server.tls.cert_file",
		},
		{
			name: "TLS enabled without key_file",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.TLS.Enabled = true
				cfg.Server.TLS.CertFile = "/some/cert.pem"
				cfg.Server.TLS.KeyFile = ""
			},
			expectError: "server.tls.key_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			assert.ErrorContains(t, errors.Join(errs...), tt.expectError)
		})
	}
}

// TestValidation_MCPConfig tests MCP server configuration validation.
func TestValidation_MCPConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "default_search_threshold below zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.MCP.DefaultSearchThreshold = -0.1
			},
			expectError: "mcp.default_search_threshold",
		},
		{
			name: "default_search_threshold above one",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.MCP.DefaultSearchThreshold = 1.1
			},
			expectError: "mcp.default_search_threshold",
		},
		{
			name: "default_search_threshold at zero is valid",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.MCP.DefaultSearchThreshold = 0.0
			},
			expectError: "",
		},
		{
			name: "default_search_threshold at one is valid",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.MCP.DefaultSearchThreshold = 1.0
			},
			expectError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			if tt.expectError == "" {
				assert.Empty(t, errs, "expected no validation errors")
			} else {
				assert.ErrorContains(t, errors.Join(errs...), tt.expectError)
			}
		})
	}
}

// TestValidation_DatabaseConfig tests database configuration validation.
func TestValidation_DatabaseConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "empty postgres host",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.Host = ""
			},
			expectError: "database.postgres.host",
		},
		{
			name: "empty postgres database",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.Database = ""
			},
			expectError: "database.postgres.database",
		},
		{
			name: "invalid postgres port",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.Port = 0
			},
			expectError: "database.postgres.port",
		},
		{
			name: "invalid postgres max_open_conns",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.MaxOpenConns = 0
			},
			expectError: "database.postgres.max_open_conns",
		},
		{
			name: "negative postgres min_conns",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.MinConns = -1
			},
			expectError: "database.postgres.min_conns",
		},
		{
			name: "min_conns greater than max_open_conns",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.MaxOpenConns = 10
				cfg.Database.Postgres.MinConns = 20
			},
			expectError: "database.postgres.min_conns",
		},
		{
			name: "negative postgres conn_max_lifetime",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.ConnMaxLifetime = -1 * time.Second
			},
			expectError: "database.postgres.conn_max_lifetime",
		},
		{
			name: "empty neo4j uri",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.URI = ""
			},
			expectError: "database.neo4j.uri",
		},
		{
			name: "invalid neo4j uri prefix - http",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.URI = "http://localhost:7687"
			},
			expectError: "database.neo4j.uri",
		},
		{
			name: "invalid neo4j uri prefix - no scheme",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.URI = "localhost:7687"
			},
			expectError: "database.neo4j.uri",
		},
		{
			name: "invalid neo4j max_connection_pool_size",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.MaxConnectionPoolSize = 0
			},
			expectError: "database.neo4j.max_connection_pool_size",
		},
		{
			name: "invalid neo4j connection_acquisition_timeout",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.ConnectionAcquisitionTimeout = 0
			},
			expectError: "database.neo4j.connection_acquisition_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			assert.ErrorContains(t, errors.Join(errs...), tt.expectError)
		})
	}
}

// TestValidation_Neo4jValidURIs tests that valid Neo4j URI prefixes are accepted.
func TestValidation_Neo4jValidURIs(t *testing.T) {
	validURIs := []string{
		"bolt://localhost:7687",
		"bolt://neo4j-server:7687",
		"neo4j://localhost:7687",
		"neo4j://neo4j-cluster:7687",
		"bolt+s://secure-host:7687",
		"bolt+ssc://self-signed-host:7687",
		"neo4j+s://xxxx.databases.neo4j.io",
		"neo4j+ssc://self-signed-cluster:7687",
	}

	for _, uri := range validURIs {
		t.Run(uri, func(t *testing.T) {
			cfg := validConfig()
			cfg.Database.Neo4j.URI = uri
			errs := cfg.Validate()
			assert.NotContains(t, fmt.Sprint(errs), "database.neo4j.uri", "URI %q should be valid", uri)
		})
	}
}

// TestValidation_OpenAIConfig tests OpenAI configuration validation.
func TestValidation_OpenAIConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "invalid embedding_dimensions",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.OpenAI.EmbeddingDimensions = 0
			},
			expectError: "openai.embedding_dimensions",
		},
		{
			name: "negative retry_attempts",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.OpenAI.RetryAttempts = -1
			},
			expectError: "openai.retry_attempts",
		},
		{
			name: "negative retry_delay",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.OpenAI.RetryDelay = -1 * time.Second
			},
			expectError: "openai.retry_delay",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			assert.ErrorContains(t, errors.Join(errs...), tt.expectError)
		})
	}
}

// TestValidation_LoggingLevels tests all valid log levels.
func TestValidation_LoggingLevels(t *testing.T) {
	validLevels := []string{"debug", "info", "warn", "error", "DEBUG", "INFO", "WARN", "ERROR"}

	for _, level := range validLevels {
		t.Run(level, func(t *testing.T) {
			cfg := validConfig()
			cfg.Logging.Level = level
			errs := cfg.Validate()
			assert.NotContains(t, fmt.Sprint(errs), "logging.level")
		})
	}
}

// TestValidation_ObservabilityConfig tests observability configuration validation.
func TestValidation_ObservabilityConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "metrics enabled with invalid port",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Metrics.Enabled = true
				cfg.Observability.Metrics.Port = 0
			},
			expectError: "observability.metrics.port",
		},
		{
			name: "metrics enabled with empty path",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Metrics.Enabled = true
				cfg.Observability.Metrics.Path = ""
			},
			expectError: "observability.metrics.path",
		},
		{
			name: "tracing enabled with empty endpoint",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Tracing.Enabled = true
				cfg.Observability.Tracing.Endpoint = ""
			},
			expectError: "observability.tracing.endpoint",
		},
		{
			name: "tracing enabled with invalid sample_rate - negative",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Tracing.Enabled = true
				cfg.Observability.Tracing.Endpoint = "localhost:4317"
				cfg.Observability.Tracing.SampleRate = -0.1
			},
			expectError: "observability.tracing.sample_rate",
		},
		{
			name: "tracing enabled with invalid sample_rate - over 1",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Tracing.Enabled = true
				cfg.Observability.Tracing.Endpoint = "localhost:4317"
				cfg.Observability.Tracing.SampleRate = 1.5
			},
			expectError: "observability.tracing.sample_rate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			assert.ErrorContains(t, errors.Join(errs...), tt.expectError)
		})
	}
}

// TestValidation_PortConflict tests cross-configuration validation for port conflicts.
func TestValidation_PortConflict(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError bool
	}{
		{
			name: "metrics enabled with same port as server - conflict",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 8080
				cfg.Observability.Metrics.Enabled = true
				cfg.Observability.Metrics.Port = 8080
			},
			expectError: true,
		},
		{
			name: "metrics enabled with different port - no conflict",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 8080
				cfg.Observability.Metrics.Enabled = true
				cfg.Observability.Metrics.Port = 9090
			},
			expectError: false,
		},
		{
			name: "metrics disabled with same port - no conflict",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 8080
				cfg.Observability.Metrics.Enabled = false
				cfg.Observability.Metrics.Port = 8080
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()

			conflict := "observability.metrics.port: must be different from server.port"
			if tt.expectError {
				assert.ErrorContains(t, errors.Join(errs...), conflict)
			} else {
				// Other validation errors may exist; only the conflict must not.
				assert.NotContains(t, fmt.Sprint(errs), conflict,
					"unexpected port conflict error when none expected")
			}
		})
	}
}

// TestValidation_MultipleErrors verifies multiple errors are collected.
func TestValidation_MultipleErrors(t *testing.T) {
	cfg := validConfig()
	cfg.Server.Port = 0
	cfg.Database.Postgres.Port = 0

	errs := cfg.Validate()
	require.NotEmpty(t, errs, "expected validation errors")

	err := errors.Join(errs...)
	assert.ErrorContains(t, err, "server.port")
	assert.ErrorContains(t, err, "database.postgres.port")
}

// TestServerAddress tests the Address() helper method.
func TestServerAddress(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		expected string
	}{
		{
			name:     "default values",
			host:     "0.0.0.0",
			port:     8080,
			expected: "0.0.0.0:8080",
		},
		{
			name:     "localhost",
			host:     "localhost",
			port:     3000,
			expected: "localhost:3000",
		},
		{
			name:     "empty host",
			host:     "",
			port:     8080,
			expected: ":8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ServerConfig{
				Host: tt.host,
				Port: tt.port,
			}
			assert.Equal(t, tt.expected, cfg.Address())
		})
	}
}

// TestPostgresDSN tests the DSN() helper method.
func TestPostgresDSN(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.PostgresConfig
		expected string
	}{
		{
			name: "simple credentials",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "testpass", SSLMode: "require",
			},
			expected: "postgres://testuser:testpass@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "password with @ symbol",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "p@ss", SSLMode: "require",
			},
			expected: "postgres://testuser:p%40ss@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "password with colon and slash",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "p:a/ss", SSLMode: "require",
			},
			expected: "postgres://testuser:p%3Aa%2Fss@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "password with percent",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "100%pass", SSLMode: "require",
			},
			expected: "postgres://testuser:100%25pass@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "username with special characters",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "user@domain", Password: "testpass", SSLMode: "require",
			},
			expected: "postgres://user%40domain:testpass@localhost:5432/testdb?sslmode=require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cfg.DSN())
		})
	}
}

// TestPostgresSafeDSN tests the SafeDSN() helper method.
func TestPostgresSafeDSN(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.PostgresConfig
		expected string
	}{
		{
			name: "simple credentials",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "supersecretpassword", SSLMode: "require",
			},
			expected: "postgres://testuser:xxxxx@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "username with special characters",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "user@domain", Password: "supersecretpassword", SSLMode: "require",
			},
			expected: "postgres://user%40domain:xxxxx@localhost:5432/testdb?sslmode=require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.SafeDSN()
			assert.Equal(t, tt.expected, result)
			assert.NotContains(t, result, tt.cfg.Password)
		})
	}
}

// TestNeo4jSafeURI tests the SafeURI() helper method.
func TestNeo4jSafeURI(t *testing.T) {
	tests := []struct {
		name          string
		uri           string
		expected      string
		shouldNotHave string
	}{
		{
			name:     "no embedded credentials",
			uri:      "bolt://localhost:7687",
			expected: "bolt://localhost:7687",
		},
		{
			name:     "neo4j scheme no credentials",
			uri:      "neo4j://neo4j-cluster:7687",
			expected: "neo4j://neo4j-cluster:7687",
		},
		{
			name:          "embedded username and password",
			uri:           "bolt://myuser:secretpass@localhost:7687",
			expected:      "bolt://myuser:xxxxx@localhost:7687",
			shouldNotHave: "secretpass",
		},
		{
			name:          "neo4j scheme with credentials",
			uri:           "neo4j://admin:supersecret@cluster.example.com:7687",
			expected:      "neo4j://admin:xxxxx@cluster.example.com:7687",
			shouldNotHave: "supersecret",
		},
		{
			name:     "username only no password",
			uri:      "bolt://myuser@localhost:7687",
			expected: "bolt://myuser@localhost:7687",
		},
		{
			name:          "bolt+s with credentials",
			uri:           "bolt+s://user:pass123@secure-host:7687",
			expected:      "bolt+s://user:xxxxx@secure-host:7687",
			shouldNotHave: "pass123",
		},
		{
			name:          "complex password with special chars",
			uri:           "bolt://user:p@ss:word!@localhost:7687",
			expected:      "bolt://user:xxxxx@localhost:7687",
			shouldNotHave: "p@ss:word!",
		},
		{
			name:          "unparseable uri is not echoed",
			uri:           "bolt://user:secret@[::1",
			expected:      "[unparseable neo4j uri]",
			shouldNotHave: "secret",
		},
		{
			name:     "invalid uri no scheme",
			uri:      "localhost:7687",
			expected: "localhost:7687",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Neo4jConfig{
				URI: tt.uri,
			}
			result := cfg.SafeURI()
			assert.Equal(t, tt.expected, result)
			if tt.shouldNotHave != "" {
				assert.NotContains(t, result, tt.shouldNotHave)
			}
		})
	}
}

// TestValidConfig verifies a fully valid config passes validation.
func TestValidConfig(t *testing.T) {
	cfg := validConfig()
	errs := cfg.Validate()
	assert.Empty(t, errs, "expected no validation errors, got: %v", errs)
}

// Helper functions

// clearMnemonicEnvVars clears all LIBRARIAN_ prefixed environment variables.
func clearMnemonicEnvVars(t *testing.T) {
	t.Helper()
	for _, env := range os.Environ() {
		key, _, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "LIBRARIAN_") {
			// t.Setenv handles cleanup automatically when test completes
			t.Setenv(key, "")
		}
	}
}

// validConfig returns the compiled defaults, which form a valid configuration.
func validConfig() *config.MnemonicConfig {
	v := viper.New()
	config.SetDefaults(v)
	cfg := &config.MnemonicConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		panic(err)
	}
	return cfg
}

// TestLoad_ExplicitConfigFileMustBeReadable verifies that a config file named
// by LIBRARIAN_CONFIG_FILE fails loading when it cannot be read.
func TestLoad_ExplicitConfigFileMustBeReadable(t *testing.T) {
	clearMnemonicEnvVars(t)
	t.Setenv(config.EnvConfigFile, filepath.Join(t.TempDir(), "missing.yaml"))

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestOpenAIBaseURL_EnvOverride(t *testing.T) {
	clearMnemonicEnvVars(t)
	t.Setenv("LIBRARIAN_OPENAI_BASE_URL", "http://localhost:8090/v1")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "http://localhost:8090/v1", cfg.OpenAI.BaseURL)
}

func TestOpenAIBaseURL_ValidateRejectsBareString(t *testing.T) {
	clearMnemonicEnvVars(t)

	v := viper.New()
	config.SetDefaults(v)
	v.Set("openai.base_url", "not-a-url")

	cfg := &config.MnemonicConfig{}
	require.NoError(t, v.Unmarshal(cfg))

	assert.ErrorContains(t, errors.Join(cfg.Validate()...), "openai.base_url: ")
}
