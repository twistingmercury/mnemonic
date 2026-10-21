package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"slices"
	"sync"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"

	"github.com/twistingmercury/mnemonic/internal/mcp/config"
	"github.com/twistingmercury/mnemonic/internal/mcp/database"
	"github.com/twistingmercury/mnemonic/internal/mcp/handlers/operations"
	"github.com/twistingmercury/mnemonic/internal/mcp/health"
	"github.com/twistingmercury/mnemonic/internal/mcp/mcpserver"
	"github.com/twistingmercury/mnemonic/internal/mcp/metrics"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/mcp/repository/chunk"
	graphrepo "github.com/twistingmercury/mnemonic/internal/mcp/repository/graph"
	patternrepo "github.com/twistingmercury/mnemonic/internal/mcp/repository/pattern"
	openaisvc "github.com/twistingmercury/mnemonic/internal/mcp/service/openai"
	patternsvc "github.com/twistingmercury/mnemonic/internal/mcp/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic/internal/mcp/service/search"
	"github.com/twistingmercury/mnemonic/internal/mcp/telemetry"
	"github.com/twistingmercury/mnemonic/internal/mcp/version"
	otelxgin "github.com/twistingmercury/otelx/middleware/gin/v2"
	"github.com/twistingmercury/otelx/v2"
)

// ListenAndServe starts the mnemonic server. It initializes telemetry,
// establishes database connections, wires dependencies for MCP search tools,
// and runs the operations HTTP server plus MCP server. It blocks until a
// shutdown signal is received or a component returns a fatal error.
func ListenAndServe(cfg *config.MnemonicConfig) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize telemetry.
	tel, err := telemetry.Initialize(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}
	defer func() {
		if shutdownErr := tel.Shutdown(context.Background()); shutdownErr != nil {
			tel.Logger.Error().Err(shutdownErr).Msg("telemetry shutdown error")
		}
	}()

	logger := tel.Logger
	logger.Info().
		Str("host", cfg.Server.Host).
		Int("admin_port", cfg.Server.Port).
		Int("mcp_port", cfg.MCP.Port).
		Bool("metrics_enabled", cfg.Observability.Metrics.Enabled).
		Bool("tracing_enabled", cfg.Observability.Tracing.Enabled).
		Msg("mnemonic starting")

	// Establish database connections.
	pgPool, neo4jDriver, err := openDatabases(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeDatabases(pgPool, neo4jDriver, logger)

	// Telemetry installs its meter provider globally, so the instruments are
	// created from otel.Meter.
	registry, err := metrics.NewRegistry(otel.Meter("mnemonic"))
	if err != nil {
		return fmt.Errorf("failed to create metrics registry: %w", err)
	}

	// Wire MCP dependencies.
	services := wireDependencies(pgPool, neo4jDriver, cfg, logger, registry)

	// Build operations HTTP server.
	router := setupRouter(tel)
	operations.SetupHandlers(router, health.Descriptors(health.Dependencies{
		PGPool:      pgPool,
		Neo4jDriver: neo4jDriver,
		OpenAI:      cfg.OpenAI,
	}))
	adminServer := newHTTPServer(router, cfg)

	// Build the MCP HTTP server.
	mcpSrv := mcpserver.NewMCPServer(services, logger, cfg.MCP, registry.Tools)
	mcpHandler := mcpserver.NewMCPHTTPHandler(mcpSrv)
	mcpHTTPServer := mcpserver.NewMCPHTTPServer(cfg.MCP, cfg.Server.Host, mcpHandler)

	if err := serve(ctx, cfg, logger, []namedServer{
		{"operations", adminServer, func() error {
			if cfg.Server.TLS.Enabled {
				return adminServer.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
			}
			return adminServer.ListenAndServe()
		}},
		{"mcp", mcpHTTPServer, mcpHTTPServer.ListenAndServe},
	}); err != nil {
		return err
	}

	logger.Info().Msg("mnemonic shutdown complete")
	return nil
}

// openDatabases creates the Postgres pool and Neo4j driver, logging safe
// connection details. Returns both connections or an error if either fails.
func openDatabases(ctx context.Context, cfg *config.MnemonicConfig, logger zerolog.Logger) (*pgxpool.Pool, neo4j.DriverWithContext, error) {
	logger.Info().
		Str("dsn", cfg.Database.Postgres.SafeDSN()).
		Msg("connecting to PostgreSQL")

	pgPool, err := database.NewPostgresPool(ctx, cfg.Database.Postgres)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	logger.Info().Msg("PostgreSQL connected")

	logger.Info().
		Str("uri", cfg.Database.Neo4j.SafeURI()).
		Str("database", cfg.Database.Neo4j.Database).
		Msg("connecting to Neo4j")

	neo4jDriver, err := database.NewNeo4jDriver(ctx, cfg.Database.Neo4j)
	if err != nil {
		pgPool.Close()
		return nil, nil, fmt.Errorf("failed to connect to Neo4j: %w", err)
	}
	logger.Info().Msg("Neo4j connected")

	return pgPool, neo4jDriver, nil
}

// closeDatabases closes both database connections, logging any errors.
func closeDatabases(pgPool *pgxpool.Pool, neo4jDriver neo4j.DriverWithContext, logger zerolog.Logger) {
	pgPool.Close()
	logger.Debug().Msg("PostgreSQL pool closed")

	if err := neo4jDriver.Close(context.Background()); err != nil {
		logger.Error().Err(err).Msg("neo4j driver close error")
	} else {
		logger.Debug().Msg("Neo4j driver closed")
	}
}

// wireDependencies creates the repositories and read-side services required by
// the MCP search tools and returns the service calls behind them.
func wireDependencies(
	pgPool *pgxpool.Pool,
	neo4jDriver neo4j.DriverWithContext,
	cfg *config.MnemonicConfig,
	logger zerolog.Logger,
	registry *metrics.Registry,
) mcpserver.Services {
	patternRepo := patternrepo.NewRepository(pgPool)
	graphRepo := graphrepo.NewRepository(neo4jDriver, cfg.Database.Neo4j.Database, registry.Database)
	chunkRepo := chunkrepo.NewRepository(pgPool)
	embeddingSvc := openaisvc.NewEmbeddingService(cfg.OpenAI)

	searchSvc := searchsvc.New(embeddingSvc, patternRepo, chunkRepo, graphRepo, registry.Patterns, logger)
	patternSvc := patternsvc.New(patternRepo, graphRepo, logger)

	return mcpserver.Services{
		SearchPatterns:      searchSvc.SearchPatterns,
		FindRelatedPatterns: patternSvc.FindRelatedPatterns,
		GetPatternWithGraph: patternSvc.GetPatternWithGraph,
	}
}

// namedServer is an HTTP server with the call that starts it listening.
type namedServer struct {
	name   string
	srv    *http.Server
	listen func() error
}

// serve runs every server until ctx is cancelled or one of them fails to
// listen, then shuts them all down concurrently within
// cfg.Server.ShutdownTimeout. It returns the listener failure, if any,
// otherwise the joined shutdown errors.
func serve(ctx context.Context, cfg *config.MnemonicConfig, logger zerolog.Logger, servers []namedServer) error {
	g, gCtx := errgroup.WithContext(ctx)
	for _, s := range servers {
		g.Go(func() error {
			logger.Info().Str("addr", s.srv.Addr).Str("component", s.name).Msg("HTTP server listening")
			if err := s.listen(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("%s server error: %w", s.name, err)
			}
			return nil
		})
	}
	g.Go(func() error {
		<-gCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()

		errs := make([]error, len(servers))
		var wg sync.WaitGroup
		for i, s := range servers {
			wg.Go(func() {
				logger.Info().Str("component", s.name).Msg("shutting down HTTP server")
				if err := s.srv.Shutdown(shutdownCtx); err != nil {
					errs[i] = fmt.Errorf("%s shutdown error: %w", s.name, err)
				}
			})
		}
		wg.Wait()
		return errors.Join(errs...)
	})
	return g.Wait()
}

// setupRouter creates and configures the Gin router with middleware.
func setupRouter(tel *otelx.Telemetry) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	// Correlation ID middleware: echo X-Request-ID from request to response.
	router.Use(func(c *gin.Context) {
		if rid := c.GetHeader("X-Request-ID"); rid != "" {
			c.Header("X-Request-ID", rid)
		}
		c.Next()
	})

	// /health is polled by probes; keep it out of traces, metrics and logs.
	// /metrics is served on the otelx metrics port, not on this router.
	skipPaths := []string{"/health"}
	router.Use(otelgin.Middleware(version.Service, otelgin.WithFilter(func(r *http.Request) bool {
		return !slices.Contains(skipPaths, r.URL.Path)
	})))
	router.Use(otelxgin.LoggingMiddleware(tel,
		otelxgin.WithSkipPaths(skipPaths...),
		otelxgin.WithRequestHeaders("X-Request-ID", "X-Correlation-ID"),
	))

	return router
}

// newHTTPServer creates the operations http.Server from config.
func newHTTPServer(r *gin.Engine, cfg *config.MnemonicConfig) *http.Server {
	return &http.Server{
		Addr:           cfg.Server.Address(),
		Handler:        r,
		ReadTimeout:    cfg.Server.ReadTimeout,
		WriteTimeout:   cfg.Server.WriteTimeout,
		IdleTimeout:    cfg.Server.IdleTimeout,
		MaxHeaderBytes: 1 << 20,
	}
}
