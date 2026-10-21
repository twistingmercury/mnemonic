package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// MnemonicConfig is the top-level configuration structure for the Mnemonic server.
type MnemonicConfig struct {
	Server        ServerConfig        `mapstructure:"server"`
	Database      DatabaseConfig      `mapstructure:"database"`
	OpenAI        OpenAIConfig        `mapstructure:"openai"`
	Enrichment    EnrichmentConfig    `mapstructure:"enrichment"`
	Queue         QueueConfig         `mapstructure:"queue"`
	Logging       LoggingConfig       `mapstructure:"logging"`
	Observability ObservabilityConfig `mapstructure:"observability"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	TLS             TLSConfig     `mapstructure:"tls"`
}

// TLSConfig contains TLS settings for the server.
type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

// DatabaseConfig contains database connection settings.
type DatabaseConfig struct {
	Postgres PostgresConfig `mapstructure:"postgres"`
	Neo4j    Neo4jConfig    `mapstructure:"neo4j"`
}

// PostgresConfig contains PostgreSQL connection settings.
type PostgresConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Database        string        `mapstructure:"database"`
	Username        string        `mapstructure:"username"`
	Password        string        `mapstructure:"password"` // #nosec G117 -- credentials loaded from config/env, not serialized
	SSLMode         string        `mapstructure:"ssl_mode"`
	MaxOpenConns    int32         `mapstructure:"max_open_conns"`
	MinConns        int32         `mapstructure:"min_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// Neo4jConfig contains Neo4j connection settings.
type Neo4jConfig struct {
	URI                          string        `mapstructure:"uri"`
	Username                     string        `mapstructure:"username"`
	Password                     string        `mapstructure:"password"` // #nosec G117 -- credentials loaded from config/env, not serialized
	Database                     string        `mapstructure:"database"`
	MaxConnectionPoolSize        int           `mapstructure:"max_connection_pool_size"`
	ConnectionAcquisitionTimeout time.Duration `mapstructure:"connection_acquisition_timeout"`
}

// OpenAIConfig contains OpenAI API settings.
type OpenAIConfig struct {
	APIKey              string        `mapstructure:"api_key"` // #nosec G117 -- credentials loaded from config/env, not serialized
	BaseURL             string        `mapstructure:"base_url"`
	EmbeddingModel      string        `mapstructure:"embedding_model"`
	EmbeddingDimensions int           `mapstructure:"embedding_dimensions"`
	ExtractionModel     string        `mapstructure:"extraction_model"`
	RetryAttempts       int           `mapstructure:"retry_attempts"`
	RetryDelay          time.Duration `mapstructure:"retry_delay"`
}

// EnrichmentConfig contains enrichment worker settings.
type EnrichmentConfig struct {
	WorkerCount            int           `mapstructure:"worker_count"`
	RetryDelay             time.Duration `mapstructure:"retry_delay"`
	JobTimeout             time.Duration `mapstructure:"job_timeout"`
	DrainTimeout           time.Duration `mapstructure:"drain_timeout"`
	CompletedRetention     time.Duration `mapstructure:"completed_retention"`
	FailedRetention        time.Duration `mapstructure:"failed_retention"`
	RelatedToMinSimilarity float64       `mapstructure:"related_to_min_similarity"`
}

// QueueConfig contains message queue settings.
type QueueConfig struct {
	Provider string         `mapstructure:"provider"`
	RabbitMQ RabbitMQConfig `mapstructure:"rabbitmq"`
}

// RabbitMQConfig contains RabbitMQ connection settings.
type RabbitMQConfig struct {
	Host           string        `mapstructure:"host"`
	Port           int           `mapstructure:"port"`
	User           string        `mapstructure:"user"`
	Password       string        `mapstructure:"password"` // #nosec G117 -- credentials loaded from config/env, not serialized
	VHost          string        `mapstructure:"vhost"`
	Queue          string        `mapstructure:"queue"`
	PrefetchCount  int           `mapstructure:"prefetch_count"`
	ReconnectDelay time.Duration `mapstructure:"reconnect_delay"`
}

// LoggingConfig contains logging settings.
type LoggingConfig struct {
	Level string `mapstructure:"level"`
}

// ObservabilityConfig contains observability settings.
type ObservabilityConfig struct {
	Metrics MetricsConfig `mapstructure:"metrics"`
	Tracing TracingConfig `mapstructure:"tracing"`
}

// MetricsConfig contains metrics settings.
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
	Port    int    `mapstructure:"port"`
}

// TracingConfig contains distributed tracing settings.
type TracingConfig struct {
	Enabled      bool    `mapstructure:"enabled"`
	Endpoint     string  `mapstructure:"endpoint"`
	SampleRate   float64 `mapstructure:"sample_rate"`
	OTLPInsecure bool    `mapstructure:"otlp_insecure"`
}

// Load loads configuration from all sources with the following precedence:
// 1. Compiled defaults (lowest priority)
// 2. Configuration file
// 3. Environment variables (highest priority)
//
// The config file is discovered in the following order:
// 1. The "config" flag in flags (if flags is non-nil and the flag was set)
// 2. $MNEMONIC_CONFIG_FILE (if set)
// 3. /etc/mnemonic/config.yaml (production)
// 4. ./config.yaml (development)
func Load(flags *pflag.FlagSet) (*MnemonicConfig, error) {
	v, err := Resolve(flags)
	if err != nil {
		return nil, err
	}

	cfg := &MnemonicConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Resolve layers defaults, the config file and environment variables into a
// viper instance without unmarshalling or validating them, for callers that
// need a few keys and must not fail on unrelated sections (for example the
// --health probe, which must not need database credentials). Load uses it, so
// both read the same sources with the same precedence.
//
// A config file that cannot be read is an error only when it was named
// explicitly (by flag or $MNEMONIC_CONFIG_FILE); otherwise defaults and
// environment variables are used.
func Resolve(flags *pflag.FlagSet) (*viper.Viper, error) {
	v := viper.New()
	SetDefaults(v)

	if configPath, explicit := findConfigFile(flags); configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil && explicit {
			return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
		}
	}

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	return v, nil
}

// SetDefaults sets all default values in the viper instance.
// This function is exported to allow tests to use the same defaults as production code.
func SetDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.host", DefaultServerHost)
	v.SetDefault("server.port", DefaultServerPort)
	v.SetDefault("server.read_timeout", DefaultServerReadTimeout)
	v.SetDefault("server.write_timeout", DefaultServerWriteTimeout)
	v.SetDefault("server.idle_timeout", DefaultServerIdleTimeout)
	v.SetDefault("server.shutdown_timeout", DefaultServerShutdownTimeout)
	v.SetDefault("server.tls.enabled", DefaultServerTLSEnabled)
	v.SetDefault("server.tls.cert_file", "")
	v.SetDefault("server.tls.key_file", "")

	// PostgreSQL defaults
	v.SetDefault("database.postgres.host", DefaultPostgresHost)
	v.SetDefault("database.postgres.port", DefaultPostgresPort)
	v.SetDefault("database.postgres.database", DefaultPostgresDatabase)
	v.SetDefault("database.postgres.username", DefaultPostgresUsername)
	v.SetDefault("database.postgres.password", "")
	v.SetDefault("database.postgres.ssl_mode", DefaultPostgresSSLMode)
	v.SetDefault("database.postgres.max_open_conns", DefaultPostgresMaxOpenConns)
	v.SetDefault("database.postgres.min_conns", DefaultPostgresMinConns)
	v.SetDefault("database.postgres.conn_max_lifetime", DefaultPostgresConnMaxLifetime)

	// Neo4j defaults
	v.SetDefault("database.neo4j.uri", DefaultNeo4jURI)
	v.SetDefault("database.neo4j.username", DefaultNeo4jUsername)
	v.SetDefault("database.neo4j.password", "")
	v.SetDefault("database.neo4j.database", DefaultNeo4jDatabase)
	v.SetDefault("database.neo4j.max_connection_pool_size", DefaultNeo4jMaxConnectionPoolSize)
	v.SetDefault("database.neo4j.connection_acquisition_timeout", DefaultNeo4jConnectionAcquisitionTimeout)

	// OpenAI defaults
	v.SetDefault("openai.api_key", "")
	v.SetDefault("openai.embedding_model", DefaultOpenAIEmbeddingModel)
	v.SetDefault("openai.embedding_dimensions", DefaultOpenAIEmbeddingDimensions)
	v.SetDefault("openai.extraction_model", DefaultOpenAIExtractionModel)
	v.SetDefault("openai.retry_attempts", DefaultOpenAIRetryAttempts)
	v.SetDefault("openai.retry_delay", DefaultOpenAIRetryDelay)
	v.SetDefault("openai.base_url", DefaultOpenAIBaseURL)

	// Enrichment defaults
	v.SetDefault("enrichment.worker_count", DefaultEnrichmentWorkerCount)
	v.SetDefault("enrichment.retry_delay", DefaultEnrichmentRetryDelay)
	v.SetDefault("enrichment.job_timeout", DefaultEnrichmentJobTimeout)
	v.SetDefault("enrichment.drain_timeout", DefaultEnrichmentDrainTimeout)
	v.SetDefault("enrichment.completed_retention", DefaultEnrichmentCompletedRetention)
	v.SetDefault("enrichment.failed_retention", DefaultEnrichmentFailedRetention)
	v.SetDefault("enrichment.related_to_min_similarity", DefaultEnrichmentRelatedToMinSimilarity)

	// Queue defaults
	v.SetDefault("queue.provider", DefaultQueueProvider)
	v.SetDefault("queue.rabbitmq.host", DefaultRabbitMQHost)
	v.SetDefault("queue.rabbitmq.port", DefaultRabbitMQPort)
	v.SetDefault("queue.rabbitmq.user", DefaultRabbitMQUser)
	v.SetDefault("queue.rabbitmq.password", "")
	v.SetDefault("queue.rabbitmq.vhost", DefaultRabbitMQVHost)
	v.SetDefault("queue.rabbitmq.queue", DefaultRabbitMQQueue)
	v.SetDefault("queue.rabbitmq.prefetch_count", DefaultRabbitMQPrefetchCount)
	v.SetDefault("queue.rabbitmq.reconnect_delay", DefaultRabbitMQReconnectDelay)

	// Logging defaults
	v.SetDefault("logging.level", DefaultLoggingLevel)

	// Observability defaults
	v.SetDefault("observability.metrics.enabled", DefaultMetricsEnabled)
	v.SetDefault("observability.metrics.path", DefaultMetricsPath)
	v.SetDefault("observability.metrics.port", DefaultMetricsPort)
	v.SetDefault("observability.tracing.enabled", DefaultTracingEnabled)
	v.SetDefault("observability.tracing.endpoint", "")
	v.SetDefault("observability.tracing.sample_rate", DefaultTracingSampleRate)
	v.SetDefault("observability.tracing.otlp_insecure", DefaultTracingOTLPInsecure)
}

// findConfigFile returns the config file to use and whether it was named
// explicitly (by flag or environment variable) rather than discovered.
func findConfigFile(flags *pflag.FlagSet) (path string, explicit bool) {
	if flags != nil {
		if f := flags.Lookup("config"); f != nil && f.Changed {
			return f.Value.String(), true
		}
	}

	if envPath := os.Getenv(EnvConfigFile); envPath != "" {
		return envPath, true
	}

	for _, p := range []string{ProductionConfigPath, DevelopmentConfigPath} {
		if _, err := os.Stat(p); err == nil {
			return p, false
		}
	}

	return "", false
}

// Validate validates the configuration and returns an error describing every
// problem found, or nil.
func (c *MnemonicConfig) Validate() error {
	var errs []error
	errs = append(errs, c.Server.validate()...)
	errs = append(errs, c.Database.validate()...)
	errs = append(errs, c.OpenAI.validate()...)
	errs = append(errs, c.Enrichment.validate()...)
	errs = append(errs, c.Logging.validate()...)
	errs = append(errs, c.Observability.validate()...)

	// Cross-configuration validation
	if c.Observability.Metrics.Enabled && c.Server.Port == c.Observability.Metrics.Port {
		errs = append(errs, fmt.Errorf("observability.metrics.port: must be different from server.port (%d) to avoid port conflict", c.Server.Port))
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("configuration validation failed:\n%w", errors.Join(errs...))
}

// invalid formats one validation problem as "field: message".
func invalid(field, format string, args ...any) error {
	return fmt.Errorf("%s: %s", field, fmt.Sprintf(format, args...))
}

func (c *ServerConfig) validate() []error {
	var errs []error

	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, invalid("server.port", "must be between 1 and 65535, got %d", c.Port))
	}
	if c.ReadTimeout <= 0 {
		errs = append(errs, invalid("server.read_timeout", "must be a positive duration"))
	}
	if c.WriteTimeout <= 0 {
		errs = append(errs, invalid("server.write_timeout", "must be a positive duration"))
	}
	if c.IdleTimeout <= 0 {
		errs = append(errs, invalid("server.idle_timeout", "must be a positive duration"))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, invalid("server.shutdown_timeout", "must be a positive duration"))
	}

	if c.TLS.Enabled {
		if c.TLS.CertFile == "" {
			errs = append(errs, invalid("server.tls.cert_file", "required when TLS is enabled"))
		} else if _, err := os.Stat(c.TLS.CertFile); err != nil {
			errs = append(errs, invalid("server.tls.cert_file", "cannot access file: %v", err))
		}

		if c.TLS.KeyFile == "" {
			errs = append(errs, invalid("server.tls.key_file", "required when TLS is enabled"))
		} else if _, err := os.Stat(c.TLS.KeyFile); err != nil {
			errs = append(errs, invalid("server.tls.key_file", "cannot access file: %v", err))
		}
	}

	return errs
}

// neo4jSchemes are the URI schemes the Neo4j driver accepts.
var neo4jSchemes = []string{"bolt://", "bolt+s://", "bolt+ssc://", "neo4j://", "neo4j+s://", "neo4j+ssc://"}

func (c *DatabaseConfig) validate() []error {
	var errs []error

	if c.Postgres.Host == "" {
		errs = append(errs, invalid("database.postgres.host", "required"))
	}
	if c.Postgres.Database == "" {
		errs = append(errs, invalid("database.postgres.database", "required"))
	}
	if c.Postgres.Port < 1 || c.Postgres.Port > 65535 {
		errs = append(errs, invalid("database.postgres.port", "must be between 1 and 65535, got %d", c.Postgres.Port))
	}
	if c.Postgres.MaxOpenConns < 1 {
		errs = append(errs, invalid("database.postgres.max_open_conns", "must be at least 1"))
	}
	if c.Postgres.MinConns < 0 {
		errs = append(errs, invalid("database.postgres.min_conns", "must be non-negative"))
	}
	if c.Postgres.MinConns > c.Postgres.MaxOpenConns {
		errs = append(errs, invalid("database.postgres.min_conns", "must be less than or equal to max_open_conns (%d), got %d", c.Postgres.MaxOpenConns, c.Postgres.MinConns))
	}
	if c.Postgres.ConnMaxLifetime < 0 {
		errs = append(errs, invalid("database.postgres.conn_max_lifetime", "must be non-negative"))
	}

	if c.Neo4j.URI == "" {
		errs = append(errs, invalid("database.neo4j.uri", "required"))
	} else if !slices.ContainsFunc(neo4jSchemes, func(s string) bool { return strings.HasPrefix(c.Neo4j.URI, s) }) {
		errs = append(errs, invalid("database.neo4j.uri", "must start with one of %s, got %q", strings.Join(neo4jSchemes, ", "), c.Neo4j.URI))
	}
	if c.Neo4j.MaxConnectionPoolSize < 1 {
		errs = append(errs, invalid("database.neo4j.max_connection_pool_size", "must be at least 1"))
	}
	if c.Neo4j.ConnectionAcquisitionTimeout <= 0 {
		errs = append(errs, invalid("database.neo4j.connection_acquisition_timeout", "must be a positive duration"))
	}

	return errs
}

func (c *OpenAIConfig) validate() []error {
	var errs []error

	// API key is required for production, but we allow empty for development/testing
	// The actual check will happen at runtime when trying to use the API

	if c.EmbeddingDimensions < 1 {
		errs = append(errs, invalid("openai.embedding_dimensions", "must be at least 1"))
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, invalid("openai.retry_attempts", "must be non-negative"))
	}
	if c.RetryDelay < 0 {
		errs = append(errs, invalid("openai.retry_delay", "must be non-negative"))
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, invalid("openai.base_url", "must be an absolute URL with scheme and host, got %q", c.BaseURL))
		}
	}

	return errs
}

func (c *EnrichmentConfig) validate() []error {
	var errs []error

	if c.WorkerCount < 1 {
		errs = append(errs, invalid("enrichment.worker_count", "must be at least 1"))
	}
	if c.RetryDelay < 0 {
		errs = append(errs, invalid("enrichment.retry_delay", "must be non-negative"))
	}
	if c.JobTimeout <= 0 {
		errs = append(errs, invalid("enrichment.job_timeout", "must be a positive duration"))
	}
	if c.DrainTimeout <= 0 {
		errs = append(errs, invalid("enrichment.drain_timeout", "must be a positive duration"))
	}
	if c.CompletedRetention <= 0 {
		errs = append(errs, invalid("enrichment.completed_retention", "must be a positive duration"))
	}
	if c.FailedRetention <= 0 {
		errs = append(errs, invalid("enrichment.failed_retention", "must be a positive duration"))
	}
	if c.RelatedToMinSimilarity < 0 || c.RelatedToMinSimilarity > 1 {
		errs = append(errs, invalid("enrichment.related_to_min_similarity", "must be between 0 and 1, got %f", c.RelatedToMinSimilarity))
	}

	return errs
}

func (c *LoggingConfig) validate() []error {
	switch strings.ToLower(c.Level) {
	case "debug", "info", "warn", "error":
		return nil
	}
	return []error{invalid("logging.level", "must be one of: debug, info, warn, error; got %q", c.Level)}
}

func (c *ObservabilityConfig) validate() []error {
	var errs []error

	if c.Metrics.Enabled {
		if c.Metrics.Port < 1 || c.Metrics.Port > 65535 {
			errs = append(errs, invalid("observability.metrics.port", "must be between 1 and 65535, got %d", c.Metrics.Port))
		}
		if c.Metrics.Path == "" {
			errs = append(errs, invalid("observability.metrics.path", "required when metrics are enabled"))
		}
	}

	if c.Tracing.Enabled {
		if c.Tracing.Endpoint == "" {
			errs = append(errs, invalid("observability.tracing.endpoint", "required when tracing is enabled"))
		}
		if c.Tracing.SampleRate < 0 || c.Tracing.SampleRate > 1 {
			errs = append(errs, invalid("observability.tracing.sample_rate", "must be between 0 and 1, got %f", c.Tracing.SampleRate))
		}
	}

	return errs
}

// Address returns the server address in host:port format.
func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// DSN returns the PostgreSQL DSN for use with database/sql.
// Username, password, and SSLMode are URL-encoded to handle special characters safely.
func (c *PostgresConfig) DSN() string {
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.Username, c.Password),
		Host:     fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:     c.Database,
		RawQuery: fmt.Sprintf("sslmode=%s", url.QueryEscape(c.SSLMode)),
	}
	return u.String()
}

// SafeDSN returns the PostgreSQL DSN with the password masked.
// Use this method for logging to prevent secret exposure.
func (c *PostgresConfig) SafeDSN() string {
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.User(c.Username),
		Host:     fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:     c.Database,
		RawQuery: fmt.Sprintf("sslmode=%s", url.QueryEscape(c.SSLMode)),
	}
	// Build the DSN with the URL-encoded username, then inject the mask.
	// url.User encodes the username safely; we append the fixed mask ourselves
	// so that the literal "*****" is not percent-encoded.
	safe := u.String()
	// Replace "postgres://encodeduser@" with "postgres://encodeduser:*****@"
	encoded := u.User.String()
	safe = strings.Replace(safe, encoded+"@", encoded+":*****@", 1)
	return safe
}

// SafeURI returns the Neo4j URI with any embedded credentials masked for logging.
// If the URI contains embedded credentials (user:pass@host), the password is masked.
// Use this method for logging to prevent secret exposure.
func (c *Neo4jConfig) SafeURI() string {
	// Check for embedded credentials in the URI (e.g., bolt://user:pass@host:port)
	// Format: scheme://[user[:password]@]host[:port]
	uri := c.URI

	// Find the scheme separator
	schemeEnd := strings.Index(uri, "://")
	if schemeEnd == -1 {
		return uri
	}

	// Get the part after the scheme
	rest := uri[schemeEnd+3:]

	// Find the LAST @ symbol which separates credentials from host
	// This handles passwords that contain @ symbols
	atIndex := strings.LastIndex(rest, "@")
	if atIndex == -1 {
		// No embedded credentials
		return uri
	}

	// Extract the credentials part (before the last @)
	credentials := rest[:atIndex]

	// Find the FIRST colon which separates username from password
	colonIndex := strings.Index(credentials, ":")
	if colonIndex == -1 {
		// Only username, no password to mask
		return uri
	}

	// Build the safe URI with masked password
	scheme := uri[:schemeEnd+3]
	username := credentials[:colonIndex]
	hostPart := rest[atIndex+1:]

	return fmt.Sprintf("%s%s:*****@%s", scheme, username, hostPart)
}
