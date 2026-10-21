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
const (
	DefaultLoggingLevel = "info"
)

// Default values for queue configuration
const (
	DefaultRabbitMQHost           = "localhost"
	DefaultRabbitMQPort           = 5672
	DefaultRabbitMQUser           = "guest"
	DefaultRabbitMQVHost          = "/"
	DefaultRabbitMQQueue          = "enrichment-jobs"
	DefaultRabbitMQReconnectDelay = 5 * time.Second
)

// Default values for observability configuration
const (
	DefaultMetricsEnabled      = true
	DefaultMetricsPath         = "/metrics"
	DefaultMetricsPort         = 9090
	DefaultTracingEnabled      = false
	DefaultTracingSampleRate   = 0.1
	DefaultTracingOTLPInsecure = true
)

// DefaultVocabularyLanguages returns a fresh slice of the default allowed language values.
func DefaultVocabularyLanguages() []string {
	return []string{
		"agnostic",
		"bash",
		"c",
		"cpp",
		"csharp",
		"cql",
		"cypher",
		"dart",
		"delphi",
		"docker",
		"elixir",
		"erlang",
		"go",
		"json",
		"java",
		"javascript",
		"kotlin",
		"lua",
		"markdown",
		"matlab",
		"mql",
		"objective-c",
		"perl",
		"php",
		"plsql",
		"powershell",
		"python",
		"r",
		"react",
		"ruby",
		"rust",
		"scala",
		"shell",
		"sql",
		"swift",
		"terraform",
		"toml",
		"tsql",
		"typescript",
		"visual-basic",
		"yaml",
		"zig",
	}
}

// DefaultVocabularyDomains returns a fresh slice of the default allowed domain values.
func DefaultVocabularyDomains() []string {
	return []string{
		"api-design", "backend", "frontend", "testing", "devops", "cli", "data-design", "documentation", "data-access", "security", "shell-scripting", "configuration", "observability", "source-management",
	}
}

// Configuration file paths
const (
	ProductionConfigPath  = "/etc/mnemonic/config.yaml"
	DevelopmentConfigPath = "./config.yaml"
	EnvConfigFile         = "LIBRARIAN_CONFIG_FILE"
	EnvPrefix             = "LIBRARIAN"
)
