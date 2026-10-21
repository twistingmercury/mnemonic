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
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"

	"github.com/twistingmercury/librarian-enricher/internal/config"
	"github.com/twistingmercury/librarian-enricher/internal/database"
	"github.com/twistingmercury/librarian-enricher/internal/enricher"
	"github.com/twistingmercury/librarian-enricher/internal/handlers/operations"
	"github.com/twistingmercury/librarian-enricher/internal/health"
	"github.com/twistingmercury/librarian-enricher/internal/metrics"
	rabbitmq "github.com/twistingmercury/librarian-enricher/internal/queue/rabbitmq"
	chunkrepo "github.com/twistingmercury/librarian-enricher/internal/repository/chunk"
	enrichmentjobrepo "github.com/twistingmercury/librarian-enricher/internal/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/librarian-enricher/internal/repository/graph"
	patternrepo "github.com/twistingmercury/librarian-enricher/internal/repository/pattern"
	enrichmentsvc "github.com/twistingmercury/librarian-enricher/internal/service/enrichment"
	openaisvc "github.com/twistingmercury/librarian-enricher/internal/service/openai"
	"github.com/twistingmercury/librarian-enricher/internal/telemetry"
	"github.com/twistingmercury/librarian-enricher/internal/version"
	otelxgin "github.com/twistingmercury/otelx/middleware/gin/v2"
	"github.com/twistingmercury/otelx/v2"
)

// ListenAndServe starts the mnemonic server. It initializes telemetry,
// establishes database connections, wires all dependencies, and runs the
// Admin API and enrichment worker concurrently. It blocks until a shutdown
// signal is received or a component returns a fatal error.
func ListenAndServe(cfg *config.MnemonicConfig) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize telemetry.
	tel, err := telemetry.Initialize(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}
	logger := tel.Logger
	defer func() {
		if shutdownErr := tel.Shutdown(context.Background()); shutdownErr != nil {
			logger.Error().Err(shutdownErr).Msg("telemetry shutdown error")
		}
	}()

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
	enrichWorker, err := wireDependencies(pgPool, neo4jDriver, cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to wire dependencies: %w", err)
	}

	// Build the Admin API router.
	router := setupRouter(tel)
	operations.SetupHandlers(router, health.Descriptors(health.Dependencies{
		PGPool:      pgPool,
		Neo4jDriver: neo4jDriver,
		OpenAI:      cfg.OpenAI,
	}))

	// Build the Admin API HTTP server.
	adminServer := CreateHTTPServer(router, cfg)

	// Run all components concurrently.
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return runHTTPServer(gCtx, adminServer, cfg, logger)
	})

	g.Go(func() error {
		logger.Info().
			Int("worker_count", cfg.Enrichment.WorkerCount).
			Msg("starting enrichment worker")
		return enrichWorker.Run(gCtx)
	})

	// Wait for shutdown signal or component failure.
	if err := g.Wait(); err != nil {
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

// wireDependencies creates all repositories, services, and the enrichment
// worker. Returns the enrichment Worker.
func wireDependencies(
	pgPool *pgxpool.Pool,
	neo4jDriver neo4j.DriverWithContext,
	cfg *config.MnemonicConfig,
	logger zerolog.Logger,
) (*enricher.Worker, error) {
	// otelx sets the global meter provider when metrics are enabled.
	meter := otel.Meter("mnemonic")
	jobMetrics, err := metrics.NewJobs(meter)
	if err != nil {
		return nil, fmt.Errorf("job metrics: %w", err)
	}
	dbMetrics, err := metrics.NewDatabase(meter)
	if err != nil {
		return nil, fmt.Errorf("database metrics: %w", err)
	}

	// Repositories.
	patternRepo := patternrepo.NewRepository(pgPool)
	enrichmentJobRepo := enrichmentjobrepo.NewRepository(pgPool)
	graphRepo := graphrepo.NewRepository(neo4jDriver, cfg.Database.Neo4j.Database, dbMetrics)
	chunkRepo := chunkrepo.NewRepository(pgPool)

	// External services.
	embeddingSvc := openaisvc.NewEmbeddingService(cfg.OpenAI)
	extractionSvc := openaisvc.NewExtractionService(cfg.OpenAI)

	// Enrichment service.
	enrichmentSvc := enrichmentsvc.New(
		enrichmentJobRepo, patternRepo, graphRepo,
		embeddingSvc, extractionSvc,
		cfg.Enrichment, chunkRepo, logger,
	)

	// Queue subscriber.
	switch cfg.Queue.Provider {
	case "rabbitmq":
		rmqSub, subErr := rabbitmq.NewSubscriber(cfg.Queue.RabbitMQ)
		if subErr != nil {
			return nil, fmt.Errorf("wire queue subscriber: %w", subErr)
		}
		return enricher.New(enrichmentSvc, rmqSub, cfg.Enrichment, logger, jobMetrics), nil
	default:
		return nil, fmt.Errorf("unknown queue provider: %q", cfg.Queue.Provider)
	}
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

		var err error
		if cfg.Server.TLS.Enabled {
			err = srv.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
		} else {
			err = srv.ListenAndServe()
		}
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

// skipPaths are the routes left out of tracing, HTTP metrics and request
// logs: health probes would otherwise drown out real traffic.
var skipPaths = []string{"/health"}

// setupRouter creates and configures the Gin router with middleware.
func setupRouter(tel *otelx.Telemetry) *gin.Engine {
	// Use gin.New() instead of gin.Default() to avoid duplicate logging
	router := gin.New()

	// Recovery middleware (keep this)
	router.Use(gin.Recovery())

	// Correlation ID middleware: echo X-Request-ID from request to response.
	router.Use(func(c *gin.Context) {
		if rid := c.GetHeader("X-Request-ID"); rid != "" {
			c.Header("X-Request-ID", rid)
		}
		c.Next()
	})

	// Tracing and semconv HTTP metrics, through the global providers.
	router.Use(otelgin.Middleware(version.Service,
		otelgin.WithFilter(func(req *http.Request) bool {
			return !slices.Contains(skipPaths, req.URL.Path)
		}),
	))

	// otelx logging middleware with trace correlation
	router.Use(otelxgin.LoggingMiddleware(tel,
		otelxgin.WithSkipPaths(skipPaths...),
		otelxgin.WithRequestHeaders("X-Request-ID", "X-Correlation-ID"),
	))

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
