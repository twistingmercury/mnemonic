package config

import "time"

// Default values for server configuration
const (
	DefaultServerHost            = "0.0.0.0"
	DefaultServerPort            = 8080
	DefaultServerReadTimeout     = 30 * time.Second
	DefaultServerWriteTimeout    = 30 * time.Second
	DefaultServerIdleTimeout     = 120 * time.Second
	DefaultServerShutdownTimeout = 5 * time.Second
	DefaultServerTLSEnabled      = false
)

// Default values for MCP server configuration
const (
	DefaultMCPPort                   = 8081
	DefaultMCPReadTimeout            = 30 * time.Second
	DefaultMCPWriteTimeout           = 30 * time.Second
	DefaultMCPIdleTimeout            = 120 * time.Second
	DefaultMCPDefaultSearchThreshold = 0.5
)

// Default values for PostgreSQL configuration
const (
	DefaultPostgresHost            = "localhost"
	DefaultPostgresPort            = 5432
	DefaultPostgresDatabase        = "mnemonic"
	DefaultPostgresUsername        = "mnemonic"
	DefaultPostgresSSLMode         = "prefer"
	DefaultPostgresMaxOpenConns    = 25
	DefaultPostgresMinConns        = 5
	DefaultPostgresConnMaxLifetime = 5 * time.Minute
)

// Default values for Neo4j configuration
const (
	DefaultNeo4jURI                          = "bolt://localhost:7687"
	DefaultNeo4jUsername                     = "neo4j"
	DefaultNeo4jDatabase                     = "neo4j"
	DefaultNeo4jMaxConnectionPoolSize        = 50
	DefaultNeo4jConnectionAcquisitionTimeout = 60 * time.Second
)

// Default values for OpenAI configuration
const (
	DefaultOpenAIBaseURL             = "https://api.openai.com/v1"
	DefaultOpenAIEmbeddingModel      = "text-embedding-3-large"
	DefaultOpenAIEmbeddingDimensions = 2000
	DefaultOpenAIRetryAttempts       = 3
	DefaultOpenAIRetryDelay          = 1 * time.Second
)

// Default values for logging configuration
const DefaultLoggingLevel = "info"

// Default values for observability configuration
const (
	DefaultMetricsEnabled      = true
	DefaultMetricsPath         = "/metrics"
	DefaultMetricsPort         = 9090
	DefaultHealthPath          = "/health"
	DefaultTracingEnabled      = false
	DefaultTracingSampleRate   = 0.1
	DefaultTracingOTLPInsecure = true
)

// Configuration file paths
const (
	ProductionConfigPath  = "/etc/mnemonic/config.yaml"
	DevelopmentConfigPath = "./config.yaml"
	EnvConfigFile         = "MNEMONIC_CONFIG_FILE"
	EnvPrefix             = "MNEMONIC"
)
