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

	"github.com/spf13/viper"
)

// MnemonicConfig is the top-level configuration structure for the Mnemonic server.
type MnemonicConfig struct {
	Server        ServerConfig        `mapstructure:"server"`
	MCP           MCPConfig           `mapstructure:"mcp"`
	Database      DatabaseConfig      `mapstructure:"database"`
	OpenAI        OpenAIConfig        `mapstructure:"openai"`
	Logging       LoggingConfig       `mapstructure:"logging"`
	Observability ObservabilityConfig `mapstructure:"observability"`
}

// MCPConfig contains MCP server settings.
type MCPConfig struct {
	Port                   int           `mapstructure:"port"`
	ReadTimeout            time.Duration `mapstructure:"read_timeout"`
	WriteTimeout           time.Duration `mapstructure:"write_timeout"`
	IdleTimeout            time.Duration `mapstructure:"idle_timeout"`
	DefaultSearchThreshold float64       `mapstructure:"default_search_threshold"`
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
	RetryAttempts       int           `mapstructure:"retry_attempts"`
	RetryDelay          time.Duration `mapstructure:"retry_delay"`
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

// Load loads, unmarshals and validates the configuration that Resolve
// assembles. Going through Resolve keeps the server and any other reader of
// the same sources, such as the --health probe, from drifting apart on
// precedence or file selection.
func Load() (*MnemonicConfig, error) {
	v, err := Resolve()
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
// 1. $LIBRARIAN_CONFIG_FILE (if set; a read error is then fatal)
// 2. /etc/mnemonic/config.yaml (production)
// 3. ./config.yaml (development)
func Resolve() (*viper.Viper, error) {
	v := viper.New()
	SetDefaults(v)

	explicitPath := os.Getenv(EnvConfigFile)
	if configPath := findConfigFile(explicitPath); configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil && explicitPath != "" {
			return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
		}
		// A discovered (not explicit) file that fails to read is ignored.
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

	// MCP server defaults
	v.SetDefault("mcp.port", DefaultMCPPort)
	v.SetDefault("mcp.read_timeout", DefaultMCPReadTimeout)
	v.SetDefault("mcp.write_timeout", DefaultMCPWriteTimeout)
	v.SetDefault("mcp.idle_timeout", DefaultMCPIdleTimeout)
	v.SetDefault("mcp.default_search_threshold", DefaultMCPDefaultSearchThreshold)

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

	// Observability defaults
	v.SetDefault("observability.metrics.enabled", DefaultMetricsEnabled)
	v.SetDefault("observability.metrics.path", DefaultMetricsPath)
	v.SetDefault("observability.metrics.port", DefaultMetricsPort)
	v.SetDefault("observability.tracing.enabled", DefaultTracingEnabled)
	v.SetDefault("observability.tracing.endpoint", "")
	v.SetDefault("observability.tracing.sample_rate", DefaultTracingSampleRate)
	v.SetDefault("observability.tracing.otlp_insecure", DefaultTracingOTLPInsecure)
}

// findConfigFile returns explicitPath when set, otherwise the first existing
// well-known config file, or "" when there is none.
func findConfigFile(explicitPath string) string {
	if explicitPath != "" {
		return explicitPath
	}
	for _, path := range []string{ProductionConfigPath, DevelopmentConfigPath} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// neo4jSchemes are the URI schemes the Neo4j driver accepts: direct (bolt)
// and routed (neo4j), each plain, TLS-verified (+s) or TLS with a self-signed
// certificate (+ssc).
var neo4jSchemes = []string{"bolt", "bolt+s", "bolt+ssc", "neo4j", "neo4j+s", "neo4j+ssc"}

// Validate validates the configuration and returns one error per invalid key.
func (c *MnemonicConfig) Validate() []error {
	var errs []error
	check := func(bad bool, field, message string) {
		if bad {
			errs = append(errs, errors.New(field+": "+message))
		}
	}
	inPortRange := func(port int) bool { return port >= 1 && port <= 65535 }
	portMessage := func(port int) string { return fmt.Sprintf("must be between 1 and 65535, got %d", port) }

	// Server
	s := c.Server
	check(!inPortRange(s.Port), "server.port", portMessage(s.Port))
	check(s.ReadTimeout <= 0, "server.read_timeout", "must be a positive duration")
	check(s.WriteTimeout <= 0, "server.write_timeout", "must be a positive duration")
	check(s.IdleTimeout <= 0, "server.idle_timeout", "must be a positive duration")
	check(s.ShutdownTimeout <= 0, "server.shutdown_timeout", "must be a positive duration")
	if s.TLS.Enabled {
		for _, f := range []struct{ field, path string }{
			{"server.tls.cert_file", s.TLS.CertFile},
			{"server.tls.key_file", s.TLS.KeyFile},
		} {
			if f.path == "" {
				check(true, f.field, "required when TLS is enabled")
			} else if _, err := os.Stat(f.path); err != nil {
				check(true, f.field, fmt.Sprintf("cannot access file: %v", err))
			}
		}
	}

	// MCP
	m := c.MCP
	check(!inPortRange(m.Port), "mcp.port", portMessage(m.Port))
	check(m.ReadTimeout <= 0, "mcp.read_timeout", "must be a positive duration")
	check(m.WriteTimeout <= 0, "mcp.write_timeout", "must be a positive duration")
	check(m.IdleTimeout <= 0, "mcp.idle_timeout", "must be a positive duration")
	check(m.DefaultSearchThreshold < 0 || m.DefaultSearchThreshold > 1, "mcp.default_search_threshold",
		fmt.Sprintf("must be between 0.0 and 1.0, got %f", m.DefaultSearchThreshold))

	// PostgreSQL
	pg := c.Database.Postgres
	check(pg.Host == "", "database.postgres.host", "required")
	check(pg.Database == "", "database.postgres.database", "required")
	check(!inPortRange(pg.Port), "database.postgres.port", portMessage(pg.Port))
	check(pg.MaxOpenConns < 1, "database.postgres.max_open_conns", "must be at least 1")
	check(pg.MinConns < 0, "database.postgres.min_conns", "must be non-negative")
	check(pg.MinConns > pg.MaxOpenConns, "database.postgres.min_conns",
		fmt.Sprintf("must be less than or equal to max_open_conns (%d), got %d", pg.MaxOpenConns, pg.MinConns))
	check(pg.ConnMaxLifetime < 0, "database.postgres.conn_max_lifetime", "must be non-negative")

	// Neo4j
	n := c.Database.Neo4j
	if n.URI == "" {
		check(true, "database.neo4j.uri", "required")
	} else {
		scheme, _, ok := strings.Cut(n.URI, "://")
		check(!ok || !slices.Contains(neo4jSchemes, scheme), "database.neo4j.uri",
			fmt.Sprintf("must start with bolt://, bolt+s://, bolt+ssc://, neo4j://, neo4j+s:// or neo4j+ssc://, got %q", n.URI))
	}
	check(n.MaxConnectionPoolSize < 1, "database.neo4j.max_connection_pool_size", "must be at least 1")
	check(n.ConnectionAcquisitionTimeout <= 0, "database.neo4j.connection_acquisition_timeout", "must be a positive duration")

	// OpenAI. The API key may be empty for development and testing; a missing
	// key fails at runtime when the API is called.
	o := c.OpenAI
	check(o.EmbeddingDimensions < 1, "openai.embedding_dimensions", "must be at least 1")
	check(o.RetryAttempts < 0, "openai.retry_attempts", "must be non-negative")
	check(o.RetryDelay < 0, "openai.retry_delay", "must be non-negative")
	if o.BaseURL != "" {
		u, err := url.Parse(o.BaseURL)
		check(err != nil || u.Scheme == "" || u.Host == "", "openai.base_url",
			fmt.Sprintf("must be an absolute URL with scheme and host, got %q", o.BaseURL))
	}

	// Logging
	switch strings.ToLower(c.Logging.Level) {
	case "debug", "info", "warn", "error":
	default:
		check(true, "logging.level", fmt.Sprintf("must be one of: debug, info, warn, error; got %q", c.Logging.Level))
	}

	// Observability
	obs := c.Observability
	if obs.Metrics.Enabled {
		check(!inPortRange(obs.Metrics.Port), "observability.metrics.port", portMessage(obs.Metrics.Port))
		check(obs.Metrics.Path == "", "observability.metrics.path", "required when metrics are enabled")
	}
	if obs.Tracing.Enabled {
		check(obs.Tracing.Endpoint == "", "observability.tracing.endpoint", "required when tracing is enabled")
		check(obs.Tracing.SampleRate < 0 || obs.Tracing.SampleRate > 1, "observability.tracing.sample_rate",
			fmt.Sprintf("must be between 0 and 1, got %f", obs.Tracing.SampleRate))
	}

	// Cross-configuration
	check(obs.Metrics.Enabled && s.Port == obs.Metrics.Port, "observability.metrics.port",
		fmt.Sprintf("must be different from server.port (%d) to avoid port conflict", s.Port))
	check(s.Port == m.Port, "mcp.port",
		fmt.Sprintf("must be different from server.port (%d) to avoid port conflict", s.Port))

	return errs
}

// Address returns the server address in host:port format.
func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Address returns the MCP server address in host:port format.
// The MCP server binds to the same host as the admin API.
func (c *MCPConfig) Address(host string) string {
	return fmt.Sprintf("%s:%d", host, c.Port)
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
