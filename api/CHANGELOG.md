# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.5.0] - 2026-10-16

### Changed

- **Breaking:** `database.postgres.max_idle_conns` (`MNEMONIC_DATABASE_POSTGRES_MAX_IDLE_CONNS`) is renamed `database.postgres.min_conns` (`MNEMONIC_DATABASE_POSTGRES_MIN_CONNS`). It always set the pool's minimum open connections, not an idle cap. The default is still 5; it must be between 0 and `max_open_conns`.
- Config validation errors list one `field: message` per line under the header, without a leading dash.
- Logged connection strings mask the password as `xxxxx` instead of `*****`.
- The create and update request bodies share one OpenAPI schema, `patterns.patternRequest`. The fields are unchanged.
- Listing patterns no longer computes a total row count, which nothing returned.

### Removed

- **Breaking:** the `mnemonic.http.request.count`, `mnemonic.http.request.duration` and `mnemonic.http.request.in_flight` metrics. Use otelgin's standard `http.server.request.duration`.
- Dead code: the unrouted FindRelated service and repository path, unused types and filters, test-only config getters, and the health package's global state.

### Fixed

- Neo4j URIs using `bolt+s://`, `bolt+ssc://`, `neo4j+s://` or `neo4j+ssc://` are accepted. They used to fail validation at startup.
- `--version` prints to stdout instead of stderr.

## [v0.4.1] - 2026-10-16

### Changed

- The service reports its own name, `mnemonic-api`, as the OpenTelemetry `service.name`, the `/health` `name`, the `/version` `service` and the HTTP tracing middleware name. All three services used to report `mnemonic`, so their traces and metrics could not be told apart.
- Content with no `[//]: pattern` section is stored as a single chunk titled by its first heading (or `Content`), so it gets an enrichment job. Such patterns used to get no chunks and stay `pending` forever.

### Removed

- Config keys that were validated but never used: `logging.format`, `logging.include_caller`, `observability.health.enabled`, `observability.health.path`, `openai.extraction_model`, `openai.max_requests_per_minute` and `queue.provider`. Viper ignores them if they are still set.
- The stale `internal/repository/tests/run-*-integration-tests.sh` scripts, which pointed at a compose file that no longer exists. `make tests-integration` replaces them.

### Fixed

- The pattern repository integration test expected a 10KB content CHECK that migration 000005 in mnemonic-dbs dropped. It now checks that 100,000 bytes, the API's limit, are stored.

## [v0.4.0] - 2026-10-15

### Added

- An `openai.base_url` setting (`MNEMONIC_OPENAI_BASE_URL`, default `https://api.openai.com/v1`) used by the embedding client and the `/health` model check. The E2E suite points it at a local OpenAI stub, so search tests now run instead of skipping and no test reaches api.openai.com.
- Bounded failure diagnostics for unexpected request errors: a single private request-completion log records the status, route template, duration, request ID, active trace and span IDs, and a redacted cause, and the active span records the exception.
- `X-Request-ID` validation. The API echoes identifiers of up to 128 ASCII letters, digits, dots, underscores, or hyphens, and generates a UUID when the header is missing or invalid.
- Distributed tracing past the HTTP handler. PostgreSQL queries (otelpgx), Neo4j operations, OpenAI embedding calls (otelhttp) and RabbitMQ publishes now produce child spans, and the publish injects W3C trace context into the message headers so the enricher can continue the same trace.
- PostgreSQL client metrics from otelpgx: `db.client.operation.duration` per operation and `pgxpool.*` pool statistics.
- `mnemonic.db.query_latency` and `mnemonic.db.query_errors` are now recorded for every Neo4j operation, and `mnemonic.patterns.query_latency` and `mnemonic.patterns.returned` for every vector search. Previously these instruments were created but never recorded.
- A `openai.embed.retry` span event for each retried embedding call.
- A RabbitMQ entry in `/health` that reports Warning while the job publisher is disconnected. Pattern writes still succeed then, and their job rows stay pending until published, so it never fails the probe.
- A `--config <path>` flag that selects the configuration file and takes priority over `MNEMONIC_CONFIG_FILE`. Previously the loader looked for the flag but the binary never registered it, so passing it failed as an unknown flag.

### Changed

- Custom latency histograms, including `mnemonic.http.request.duration`, record fractional milliseconds. Previously durations were truncated to whole milliseconds, so sub-millisecond calls recorded 0.
- Creating or updating a pattern now writes the pattern, its chunks and one enrichment job per chunk in a single transaction. Previously `Create` used no transaction at all, so a chunk failure returned 500 with the pattern already stored, and retrying the same name returned 409 with no way forward. A failed write now persists nothing and the name stays free.
- A failed enrichment-job insert now fails the request instead of returning success. Previously the response was 2xx and the affected chunks were never embedded, with no row to find them by.
- Enrichment jobs are published to RabbitMQ only after the transaction commits, so a rolled-back write cannot enqueue work for rows that no longer exist. Publication remains best-effort: a queue failure leaves the job rows pending for recovery.
- Error response `traceId` now identifies the active OpenTelemetry trace rather than the incoming `X-Request-ID`, and is omitted when no valid trace context exists. Request identity and trace identity are now separate values.
- Request-completion logging moved from the `otelx` Gin middleware to an in-repository implementation that carries the failure cause.
- `database.postgres.max_open_conns` and `max_idle_conns` are typed `int32`, matching the pool settings they configure. A value too large is now rejected while loading, naming the key, instead of being silently clamped.
- Creating or updating a pattern inserts all of its chunks in one statement and all of its enrichment jobs in another, so the write costs two round trips however many sections the pattern has. Previously each chunk and each job was its own round trip.
- `GET /v1/api/patterns/:id` reads the pattern row once and fetches related patterns and concepts in one Neo4j session, and the chunk list it embeds no longer loads chunk content. Previously the pattern was read twice, Neo4j was opened twice, and every chunk body was read and discarded.
- `GET /v1/api/patterns` no longer reads pattern content, which the list response never returned.
- Deleting a pattern removes only the concepts that pattern left unreferenced. Previously every delete also scanned every `:Concept` node in the graph for orphans.
- A RabbitMQ publisher waiting to reconnect no longer holds its lock during `reconnect_delay`, so other requests keep publishing meanwhile, and concurrent failures trigger one reconnect instead of one each.
- A discovered `/etc/mnemonic/config.yaml` or `./config.yaml` that exists but cannot be read or parsed now stops startup with `failed to read config file <path>`. Previously only an explicitly selected file failed; a broken discovered file was skipped and the service started on defaults and environment variables.

### Removed

- The `mnemonic.db.connection_pool.size` and `mnemonic.db.connection_pool.in_use` gauges, which were never recorded. otelpgx's `pgxpool.*` metrics replace them.
- `rate_limit.*` and `enrichment.*` configuration. The API never read either block: it has no rate limiter, and enrichment runs in mnemonic-enricher, which has its own settings. Setting these keys no longer has any effect, and invalid values no longer stop startup.
- Repository methods the API never calls: the enrichment-job lifecycle (claim, mark, reclaim, list, purge), chunk and pattern single-row reads and status updates, and graph writes (`SyncPattern`, `SyncConcepts`, `ComputeRelatedToEdges`, `HealthCheck`). These belong to the enricher.
- The Neo4j session, transaction and result adapter interfaces in the graph repository. Each query now runs through `neo4j.ExecuteQuery`, which keeps the driver's retry behavior.
- In-process TLS support and `server.tls.*` configuration. TLS terminates at the reverse proxy and the API serves plain HTTP behind it, which is what the Docker health probe already assumed.

### Fixed

- A failed enrichment publish now logs the pattern ID alongside the job ID and cause, so a job left pending can be traced back to the pattern it belongs to.
- Panic recovery now runs inside the observability middleware. A recovered panic produces a 500 completion log, an error span, and request count and duration metrics, and returns the in-flight count to zero; previously those frames unwound before the outer recovery wrote the response.
- The `--health` probe now resolves `server.port` from the same config file as the server, selected by `--config`, `MNEMONIC_CONFIG_FILE` or discovery, with environment variables still on top. Previously it read only defaults and environment variables, so a port set only in the config file sent the Docker health check to the wrong port. It still skips full validation and needs no service credentials.
- The `--health` probe now fails with a configuration error, without sending a request, when the config file cannot be read or `server.port` is not a number in 1-65535. Previously a non-numeric port was read as 0 and the probe dialled `localhost:0`.

### Security

- Public 503 responses return a stable `service temporarily unavailable` detail instead of the upstream failure text, which could disclose upstream response bodies to API clients.
- Private diagnostics redact upstream response bodies, URLs, quoted values, and credential or content fields, and exclude request bodies and query strings. Gin's default recovery, which prints raw panic values and request headers to stderr, is no longer used.

## [v0.3.4] - 2026-09-09

### Changed

- Updated the API and E2E Go modules and Docker build images to Go 1.27.1.
- Updated runtime and development dependencies.

### Fixed

- Disabled test analysis in golangci-lint with `run.tests: false`; unit tests remain covered by `go test`.
- Moved graph repository and OpenAI embedding test constructors into `export_test.go` so production lint analysis excludes test-only helpers.
- Retained `pgx` 5.10.0 for compatibility with `pgxmock` 4.9.0, which lacks the `TypeMap` method required by `pgx` 5.11.0.

## [v0.3.3] 2026-08-24

### Removed

- The embedded MCP listener, its API configuration, and the API-local MCP implementation dependency.
- API-owned MCP integration tests and test-stack wiring; `mnemonic-mcp` exclusively serves MCP.

## [v0.3.1] - 2026-08-19

### Added

- Local Docker Compose integration for the standalone enrichment service

### Changed

- Updated the Go toolchain and runtime dependencies
- Hardened the Docker build and runtime image, including non-root execution and image metadata
- Unified Docker-first CI so successful main and develop builds publish the image they tested
- Removed obsolete standalone database-test Make targets in favor of the full E2E build gate
- Cleaned Docker build contexts and removed obsolete generated artifacts

### Fixed

- Aligned local Compose services on shared database and RabbitMQ configuration
- Made CI and E2E image selection deterministic and validated the supported Compose configurations

## [v0.2.1]

### Added

- REST Admin API (port 8080) for pattern CRUD and semantic search operations
- Pattern enrichment pipeline: automatic embedding generation and concept extraction via OpenAI LLM
- PostgreSQL data persistence with PGVector support for vector similarity search
- Neo4j backing store for pattern concept relationships and graph traversal
- API-local MCP serving; MCP protocol serving is owned by `mnemonic-mcp`
- OpenTelemetry observability: distributed tracing, metrics collection, and structured logging
- Gin HTTP server framework with middleware for tracing and request metrics
- Configuration management (`internal/config`): layered loading from defaults, files, and environment variables
- Server integration with configurable timeouts, TLS support, and graceful shutdown
- Telemetry package (`internal/telemetry`) with otelx integration for unified OpenTelemetry setup
- Middleware package (`internal/middleware`) with tracing and request metrics for Gin
- Metrics package (`internal/metrics`) with domain-specific counters and histograms for patterns and database operations
- Distributed tracing support via otelgin middleware with trace ID correlation
- Request metrics: operation counts, duration histograms, and in-flight request counters
- Version package (`internal/version`) for build metadata and release information
- Repository layer (`internal/repository`) with PostgreSQL implementation for:
  - Patterns: CRUD, similarity search, and enrichment status tracking
  - Skills: definition management and versioning
  - Skill files: content persistence and metadata
  - Chunks: text segmentation for pattern processing
  - Enrichment jobs: status tracking and result storage
  - Agents: metadata and configuration
  - Graph: Neo4j pattern relationships and concept linkage
- Database schema migrations for all entity types
- Repository error types: domain-specific errors for conflict, not found, validation, and persistence failures
- List options for pagination support in repository queries
- Service layer (`internal/service`) for business logic:
  - Pattern service: enrichment orchestration, search, and lifecycle management
  - Skill and skill file services for capability management
  - Agent service for user/agent tracking
  - Enrichment service for LLM pipeline coordination
  - Search service for semantic similarity queries
- OpenAI integration service (`internal/service/openai`):
  - Embedding generation using text-embedding-3-large model
  - Concept extraction and entity identification via structured chat completions
  - Token usage tracking and error handling
- Health check endpoint for service readiness and dependency status
- Docker multi-stage build for optimized image size
- E2E test suite via Docker Compose:
  - Tests for all API endpoints (agents, skills, skill files, patterns, enrichment operations)
  - REST API integration tests
  - Database and dependency initialization
- GitHub Actions CI/CD workflows for automated testing and image publication
- Comprehensive unit and integration tests with pgxmock for database isolation
- Makefile targets for building, testing, and documentation generation
- Swagger UI at `/swagger/index.html` with a Swagger 2.0 specification
- Build script with cleanup traps for Docker Compose teardown

### Fixed

- E2E test Docker Compose configuration naming (mnemonic_tests container reference)
- CI config and E2E Docker Compose setup for proper service initialization
- E2E test execution and assertions for all API endpoints

### Changed

- API version path structure: `/v1/api/` prefix for all endpoints
- Server startup now initializes telemetry and observability middleware by default
- Configuration validation includes log level and timeout validation with fail-fast error reporting
- Build and CI/CD workflows optimized for mnemonic-api specific requirements
