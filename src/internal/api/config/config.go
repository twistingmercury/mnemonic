package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
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
	Logging       LoggingConfig       `mapstructure:"logging"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Vocabulary    VocabularyConfig    `mapstructure:"vocabulary"`
	Queue         QueueConfig         `mapstructure:"queue"`
}

// VocabularyConfig holds the allowed values for pattern language and domain fields.
type VocabularyConfig struct {
	Languages []string `mapstructure:"languages"`
	Domains   []string `mapstructure:"domains"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
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
	RetryAttempts       int           `mapstructure:"retry_attempts"`
	RetryDelay          time.Duration `mapstructure:"retry_delay"`
}

// QueueConfig contains message queue settings.
type QueueConfig struct {
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

// fieldError reports an invalid configuration key.
func fieldError(field, message string) error {
	return errors.New(field + ": " + message)
}

// LoadWithFlags loads, unmarshals and validates the configuration that
// Resolve assembles from flags. Going through Resolve keeps the server and any
// other reader of the same sources, such as the --health probe, from drifting
// apart on precedence or file selection.
func LoadWithFlags(flags *pflag.FlagSet) (*MnemonicConfig, error) {
	v, err := Resolve(flags)
	if err != nil {
		return nil, err
	}

	cfg := &MnemonicConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("configuration validation failed:\n%w", errors.Join(errs...))
	}

	return cfg, nil
}

// Resolve layers the configuration sources into a viper instance without
// unmarshalling or validating them, for callers that need a few keys and must
// not fail on unrelated sections (for example missing credentials).
//
// Precedence, lowest to highest:
// 1. Compiled defaults
// 2. Configuration file
// 3. Environment variables
//
// The config file is discovered in the following order:
// 1. The "config" flag in flags, if flags is non-nil and the flag was set
// 2. $MNEMONIC_CONFIG_FILE (if set)
// 3. /etc/mnemonic/config.yaml (production)
// 4. ./config.yaml (development)
//
// Finding no file at all is fine and yields defaults plus environment, but
// once a file is selected by any of these it must be readable and parse, so a
// broken file never silently falls back to defaults.
func Resolve(flags *pflag.FlagSet) (*viper.Viper, error) {
	v := viper.New()
	SetDefaults(v)

	if configPath := findConfigFile(flags); configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
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
	v.SetDefault("openai.base_url", DefaultOpenAIBaseURL)
	v.SetDefault("openai.embedding_model", DefaultOpenAIEmbeddingModel)
	v.SetDefault("openai.embedding_dimensions", DefaultOpenAIEmbeddingDimensions)
	v.SetDefault("openai.retry_attempts", DefaultOpenAIRetryAttempts)
	v.SetDefault("openai.retry_delay", DefaultOpenAIRetryDelay)

	// Logging defaults
	v.SetDefault("logging.level", DefaultLoggingLevel)

	// Vocabulary defaults
	v.SetDefault("vocabulary.languages", DefaultVocabularyLanguages())
	v.SetDefault("vocabulary.domains", DefaultVocabularyDomains())

	// Observability defaults
	v.SetDefault("observability.metrics.enabled", DefaultMetricsEnabled)
	v.SetDefault("observability.metrics.path", DefaultMetricsPath)
	v.SetDefault("observability.metrics.port", DefaultMetricsPort)
	v.SetDefault("observability.tracing.enabled", DefaultTracingEnabled)
	v.SetDefault("observability.tracing.endpoint", "")
	v.SetDefault("observability.tracing.sample_rate", DefaultTracingSampleRate)
	v.SetDefault("observability.tracing.otlp_insecure", DefaultTracingOTLPInsecure)

	// Queue defaults
	v.SetDefault("queue.rabbitmq.host", DefaultRabbitMQHost)
	v.SetDefault("queue.rabbitmq.port", DefaultRabbitMQPort)
	v.SetDefault("queue.rabbitmq.user", DefaultRabbitMQUser)
	v.SetDefault("queue.rabbitmq.password", "")
	v.SetDefault("queue.rabbitmq.vhost", DefaultRabbitMQVHost)
	v.SetDefault("queue.rabbitmq.queue", DefaultRabbitMQQueue)
	v.SetDefault("queue.rabbitmq.reconnect_delay", DefaultRabbitMQReconnectDelay)
}

// findConfigFile determines which config file to use based on the discovery order.
func findConfigFile(flags *pflag.FlagSet) string {
	// 1. Check --config flag
	if flags != nil {
		if configFlag := flags.Lookup("config"); configFlag != nil && configFlag.Changed {
			return configFlag.Value.String()
		}
	}

	// 2. Check MNEMONIC_CONFIG_FILE environment variable
	if envPath := os.Getenv(EnvConfigFile); envPath != "" {
		return envPath
	}

	// 3. Check /etc/mnemonic/config.yaml (production)
	if _, err := os.Stat(ProductionConfigPath); err == nil {
		return ProductionConfigPath
	}

	// 4. Check ./config.yaml (development)
	if _, err := os.Stat(DevelopmentConfigPath); err == nil {
		return DevelopmentConfigPath
	}

	return ""
}

// Validate validates the configuration and returns one error per invalid key.
func (c *MnemonicConfig) Validate() []error {
	var errs []error

	// Server validation
	errs = append(errs, c.Server.validate()...)

	// Database validation
	errs = append(errs, c.Database.validate()...)

	// OpenAI validation
	errs = append(errs, c.OpenAI.validate()...)

	// Logging validation
	errs = append(errs, c.Logging.validate()...)

	// Observability validation
	errs = append(errs, c.Observability.validate()...)

	// Vocabulary validation
	errs = append(errs, c.Vocabulary.validate()...)

	// Cross-configuration validation
	if c.Observability.Metrics.Enabled && c.Server.Port == c.Observability.Metrics.Port {
		errs = append(errs, fieldError("observability.metrics.port", fmt.Sprintf("must be different from server.port (%d) to avoid port conflict", c.Server.Port)))
	}
	return errs
}

func (c *ServerConfig) validate() []error {
	var errs []error

	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fieldError("server.port", fmt.Sprintf("must be between 1 and 65535, got %d", c.Port)))
	}

	if c.ReadTimeout <= 0 {
		errs = append(errs, fieldError("server.read_timeout", "must be a positive duration"))
	}

	if c.WriteTimeout <= 0 {
		errs = append(errs, fieldError("server.write_timeout", "must be a positive duration"))
	}

	if c.IdleTimeout <= 0 {
		errs = append(errs, fieldError("server.idle_timeout", "must be a positive duration"))
	}

	if c.ShutdownTimeout <= 0 {
		errs = append(errs, fieldError("server.shutdown_timeout", "must be a positive duration"))
	}

	return errs
}

// neo4jSchemes are the URI schemes the Neo4j driver accepts: direct (bolt)
// and routed (neo4j), each plain, TLS-verified (+s) or TLS with a self-signed
// certificate (+ssc).
var neo4jSchemes = []string{"bolt", "bolt+s", "bolt+ssc", "neo4j", "neo4j+s", "neo4j+ssc"}

func (c *DatabaseConfig) validate() []error {
	var errs []error

	// PostgreSQL validation
	if c.Postgres.Host == "" {
		errs = append(errs, fieldError("database.postgres.host", "required"))
	}

	if c.Postgres.Database == "" {
		errs = append(errs, fieldError("database.postgres.database", "required"))
	}

	if c.Postgres.Port < 1 || c.Postgres.Port > 65535 {
		errs = append(errs, fieldError("database.postgres.port", fmt.Sprintf("must be between 1 and 65535, got %d", c.Postgres.Port)))
	}

	if c.Postgres.MaxOpenConns < 1 {
		errs = append(errs, fieldError("database.postgres.max_open_conns", "must be at least 1"))
	}

	if c.Postgres.MinConns < 0 {
		errs = append(errs, fieldError("database.postgres.min_conns", "must be non-negative"))
	}

	if c.Postgres.MinConns > c.Postgres.MaxOpenConns {
		errs = append(errs, fieldError("database.postgres.min_conns", fmt.Sprintf("must be less than or equal to max_open_conns (%d), got %d", c.Postgres.MaxOpenConns, c.Postgres.MinConns)))
	}

	if c.Postgres.ConnMaxLifetime < 0 {
		errs = append(errs, fieldError("database.postgres.conn_max_lifetime", "must be non-negative"))
	}

	// Neo4j validation
	if c.Neo4j.URI == "" {
		errs = append(errs, fieldError("database.neo4j.uri", "required"))
	} else if scheme, _, ok := strings.Cut(c.Neo4j.URI, "://"); !ok || !slices.Contains(neo4jSchemes, scheme) {
		errs = append(errs, fieldError("database.neo4j.uri", fmt.Sprintf("must start with bolt://, bolt+s://, bolt+ssc://, neo4j://, neo4j+s:// or neo4j+ssc://, got %q", c.Neo4j.URI)))
	}

	if c.Neo4j.MaxConnectionPoolSize < 1 {
		errs = append(errs, fieldError("database.neo4j.max_connection_pool_size", "must be at least 1"))
	}

	if c.Neo4j.ConnectionAcquisitionTimeout <= 0 {
		errs = append(errs, fieldError("database.neo4j.connection_acquisition_timeout", "must be a positive duration"))
	}

	return errs
}

func (c *OpenAIConfig) validate() []error {
	var errs []error

	// API key is required for production, but we allow empty for development/testing
	// The actual check will happen at runtime when trying to use the API

	if c.EmbeddingDimensions < 1 {
		errs = append(errs, fieldError("openai.embedding_dimensions", "must be at least 1"))
	}

	if c.RetryAttempts < 0 {
		errs = append(errs, fieldError("openai.retry_attempts", "must be non-negative"))
	}

	if c.RetryDelay < 0 {
		errs = append(errs, fieldError("openai.retry_delay", "must be non-negative"))
	}

	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fieldError("openai.base_url", fmt.Sprintf("must be an absolute URL with scheme and host, got %q", c.BaseURL)))
		}
	}

	return errs
}

func (c *LoggingConfig) validate() []error {
	var errs []error

	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}

	if !validLevels[strings.ToLower(c.Level)] {
		errs = append(errs, fieldError("logging.level", fmt.Sprintf("must be one of: debug, info, warn, error; got %q", c.Level)))
	}

	return errs
}

func (c *ObservabilityConfig) validate() []error {
	var errs []error

	// Metrics validation
	if c.Metrics.Enabled {
		if c.Metrics.Port < 1 || c.Metrics.Port > 65535 {
			errs = append(errs, fieldError("observability.metrics.port", fmt.Sprintf("must be between 1 and 65535, got %d", c.Metrics.Port)))
		}

		if c.Metrics.Path == "" {
			errs = append(errs, fieldError("observability.metrics.path", "required when metrics are enabled"))
		}
	}

	// Tracing validation
	if c.Tracing.Enabled {
		if c.Tracing.Endpoint == "" {
			errs = append(errs, fieldError("observability.tracing.endpoint", "required when tracing is enabled"))
		}

		if c.Tracing.SampleRate < 0 || c.Tracing.SampleRate > 1 {
			errs = append(errs, fieldError("observability.tracing.sample_rate", fmt.Sprintf("must be between 0 and 1, got %f", c.Tracing.SampleRate)))
		}
	}

	return errs
}

func (c *VocabularyConfig) validate() []error {
	var errs []error

	if len(c.Languages) == 0 {
		errs = append(errs, fieldError("vocabulary.languages", "required"))
	}

	if len(c.Domains) == 0 {
		errs = append(errs, fieldError("vocabulary.domains", "required"))
	}

	return errs
}

// Address returns the server address in host:port format.
func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// url builds the PostgreSQL connection URL. url.URL escapes the credentials
// and the sslmode value.
func (c *PostgresConfig) url() *url.URL {
	return &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.Username, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     c.Database,
		RawQuery: url.Values{"sslmode": {c.SSLMode}}.Encode(),
	}
}

// DSN returns the PostgreSQL connection URL.
func (c *PostgresConfig) DSN() string {
	return c.url().String()
}

// SafeDSN returns the PostgreSQL connection URL with the password redacted,
// for logging.
func (c *PostgresConfig) SafeDSN() string {
	return c.url().Redacted()
}

// SafeURI returns the Neo4j URI with any embedded password redacted, for
// logging.
func (c *Neo4jConfig) SafeURI() string {
	u, err := url.Parse(c.URI)
	if err != nil {
		return "[unparseable neo4j uri]"
	}
	return u.Redacted()
}
