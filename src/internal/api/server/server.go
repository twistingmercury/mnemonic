package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"slices"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/rs/zerolog"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"

	"github.com/google/uuid"
	_ "github.com/twistingmercury/librarian/docs/api/swagger"
	"github.com/twistingmercury/librarian/internal/api/config"
	"github.com/twistingmercury/librarian/internal/api/database/graphdb"
	"github.com/twistingmercury/librarian/internal/api/database/vectordb"
	"github.com/twistingmercury/librarian/internal/api/handlers"
	"github.com/twistingmercury/librarian/internal/api/handlers/operations"
	patternhandler "github.com/twistingmercury/librarian/internal/api/handlers/patterns"
	"github.com/twistingmercury/librarian/internal/api/health"
	"github.com/twistingmercury/librarian/internal/api/metrics"
	"github.com/twistingmercury/librarian/internal/api/middleware"
	"github.com/twistingmercury/librarian/internal/api/queue/rabbitmq"
	chunkrepo "github.com/twistingmercury/librarian/internal/api/repository/chunk"
	enrichmentjobrepo "github.com/twistingmercury/librarian/internal/api/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/librarian/internal/api/repository/graph"
	patternrepo "github.com/twistingmercury/librarian/internal/api/repository/pattern"
	openaisvc "github.com/twistingmercury/librarian/internal/api/service/openai"
	patternsvc "github.com/twistingmercury/librarian/internal/api/service/pattern"
	searchsvc "github.com/twistingmercury/librarian/internal/api/service/search"
	"github.com/twistingmercury/librarian/internal/api/telemetry"
	"github.com/twistingmercury/librarian/internal/api/version"
)

// ListenAndServe starts the mnemonic server. It initializes telemetry,
// establishes database connections, wires all dependencies, and runs the
// Admin API. It blocks until a shutdown signal is received or the server
// returns a fatal error.
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
		Bool("metrics_enabled", cfg.Observability.Metrics.Enabled).
		Bool("tracing_enabled", cfg.Observability.Tracing.Enabled).
		Msg("mnemonic starting")

	// Establish database connections.
	pgPool, neo4jDriver, err := openDatabases(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeDatabases(pgPool, neo4jDriver, logger)

	// Wire all dependencies.
	patternHandler, pub, err := wireDependencies(pgPool, neo4jDriver, cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to wire dependencies: %w", err)
	}
	defer func() {
		if closeErr := pub.Close(); closeErr != nil {
			logger.Error().Err(closeErr).Msg("publisher close error")
		}
	}()

	// Build the Admin API router.
	router := setupRouter(logger)
	operations.SetupHandlers(router, health.Descriptors(health.Dependencies{
		PGPool:      pgPool,
		Neo4jDriver: neo4jDriver,
		OpenAI:      cfg.OpenAI,
		Queue:       pub,
	}))
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	patternHandler.RegisterRoutes(router.Group("/v1/api"))

	// Build the Admin API HTTP server.
	adminServer := CreateHTTPServer(router, cfg)

	// Run the Admin API until a shutdown signal or a listener failure.
	if err := runHTTPServer(ctx, adminServer, cfg, logger); err != nil {
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

	pgPool, err := vectordb.NewPostgresPool(ctx, cfg.Database.Postgres)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	logger.Info().Msg("PostgreSQL connected")

	logger.Info().
		Str("uri", cfg.Database.Neo4j.SafeURI()).
		Str("database", cfg.Database.Neo4j.Database).
		Msg("connecting to Neo4j")

	neo4jDriver, err := graphdb.NewNeo4jDriver(ctx, cfg.Database.Neo4j)
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

// wireDependencies creates all repositories, services, and the RabbitMQ
// publisher. Returns the REST API handler and the publisher.
func wireDependencies(
	pgPool *pgxpool.Pool,
	neo4jDriver neo4j.DriverWithContext,
	cfg *config.MnemonicConfig,
	logger zerolog.Logger,
) (*patternhandler.Handler, *rabbitmq.Publisher, error) {
	// Telemetry installs its meter provider globally, so the instruments are
	// created from otel.Meter.
	registry, err := metrics.NewRegistry(otel.Meter("mnemonic"))
	if err != nil {
		return nil, nil, fmt.Errorf("create metrics registry: %w", err)
	}

	// Repositories.
	patternRepo := patternrepo.NewRepository(pgPool)
	enrichmentJobRepo := enrichmentjobrepo.NewRepository(pgPool)
	graphRepo := graphrepo.NewRepository(neo4jDriver, cfg.Database.Neo4j.Database, registry.Database)
	chunkRepo := chunkrepo.NewRepository(pgPool)

	// External services.
	embeddingSvc := openaisvc.NewEmbeddingService(cfg.OpenAI)

	// Publisher.
	pub, err := rabbitmq.NewPublisher(cfg.Queue.RabbitMQ, otel.Meter("mnemonic/queue"))
	if err != nil {
		return nil, nil, fmt.Errorf("wire publisher: %w", err)
	}

	// Domain services.
	searchSvc := searchsvc.New(embeddingSvc, chunkRepo, registry.Patterns, logger)
	patternSvc := patternsvc.New(patternRepo, enrichmentJobRepo, graphRepo, pgPool, chunkRepo, pub, logger)

	return patternhandler.New(patternSvc, searchSvc, cfg.Vocabulary), pub, nil
}

// runHTTPServer starts the admin API HTTP server and gracefully shuts it down
// when the context is cancelled.
func runHTTPServer(ctx context.Context, srv *http.Server, cfg *config.MnemonicConfig, logger zerolog.Logger) error {
	errCh := make(chan error, 1)

	go func() {
		logger.Info().
			Str("addr", srv.Addr).
			Str("component", "admin_api").
			Msg("HTTP server listening")

		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("admin_api server error: %w", err)
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	logger.Info().Str("component", "admin_api").Msg("shutting down HTTP server")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("admin_api shutdown error: %w", err)
	}
	return nil
}

// setupRouter creates and configures the Gin router with middleware.
func setupRouter(logger zerolog.Logger) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		// Only a bounded identifier alphabet may enter headers and private logs.
		rid := c.GetHeader("X-Request-ID")
		valid := len(rid) > 0 && len(rid) <= 128
		for _, r := range rid {
			allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
			if !allowed {
				valid = false
				break
			}
		}
		if !valid {
			rid = uuid.NewString()
		}
		c.Header("X-Request-ID", rid)
		c.Next()
	})
	// /health is polled by probes; keep it out of traces, metrics and logs.
	// /metrics is served on the otelx metrics port, not on this router.
	skipPaths := []string{"/health"}
	router.Use(otelgin.Middleware(version.Service, otelgin.WithFilter(func(r *http.Request) bool {
		return !slices.Contains(skipPaths, r.URL.Path)
	})))
	router.Use(middleware.CompletionLogging(logger, skipPaths))
	// Recovery must be inside every completion wrapper. Do not use Gin's default
	// recovery, which prints raw panic values and request headers to stderr.
	router.Use(func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				handlers.RespondError(c, fmt.Errorf("panic: %v", recovered))
				c.Abort()
			}
		}()
		c.Next()
	})
	return router
}

// CreateHTTPServer creates a new http.Server configured with settings from
// the provided configuration.
func CreateHTTPServer(r *gin.Engine, cfg *config.MnemonicConfig) *http.Server {
	return &http.Server{
		Addr:           cfg.Server.Address(),
		Handler:        r,
		ReadTimeout:    cfg.Server.ReadTimeout,
		WriteTimeout:   cfg.Server.WriteTimeout,
		IdleTimeout:    cfg.Server.IdleTimeout,
		MaxHeaderBytes: 1 << 20,
	}
}
