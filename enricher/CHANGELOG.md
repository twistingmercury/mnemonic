# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- 2026-10-21: Renamed the project back to Mnemonic in the monorepo, dropping the placeholder name "Librarian": the Go module path is `github.com/twistingmercury/mnemonic`, the image is `ghcr.io/twistingmercury/mnemonic-enricher`, every environment variable uses the `MNEMONIC_` prefix and the host ports are the original ones (gateway 8080, 8081 and 8180).

## [0.5.0] - 2026-10-16

### Added

- A `--config <path>` flag. It takes precedence over `MNEMONIC_CONFIG_FILE`.

### Changed

- **Breaking:** `database.postgres.max_idle_conns` (`MNEMONIC_DATABASE_POSTGRES_MAX_IDLE_CONNS`) is renamed `database.postgres.min_conns` (`MNEMONIC_DATABASE_POSTGRES_MIN_CONNS`). It always set the pool's minimum open connections, not an idle cap. The default is still 5; it must be between 0 and `max_open_conns`.
- Config validation errors list one `field: message` per line under the header, without a leading dash.
- The embedding and extraction clients share one HTTP client and retry loop. The extraction error "unmarshal chat response" now reads "unmarshal response".

### Removed

- **Breaking:** the `mnemonic.http.request.count`, `mnemonic.http.request.duration` and `mnemonic.http.request.in_flight` metrics. Use otelgin's standard `http.server.request.duration`.
- Checks for states callers cannot produce, test-only config getters, and the health package's global state.

### Fixed

- Neo4j URIs using `bolt+s://`, `bolt+ssc://`, `neo4j+s://` or `neo4j+ssc://` are accepted. They used to fail validation at startup.
- `--health` reads `server.port` from the config file. It used to see only defaults and environment variables.
- The RabbitMQ URL escapes the user, password and vhost, so a password containing `@`, `/` or `:` works.
- `--version` prints to stdout instead of stderr.

## [0.4.2] - 2026-10-16

### Changed

- `database.postgres.max_open_conns` and `database.postgres.max_idle_conns` are loaded as 32-bit integers, the type the PostgreSQL pool takes. A value above 2147483647 now fails config loading with an error. It used to be silently capped at that limit.

## [0.4.1] - 2026-10-16

### Changed

- The service reports its own name, `mnemonic-enricher`, as the OpenTelemetry `service.name`, the `/health` `name`, the `/version` `service` and the HTTP tracing middleware name. All three services used to report `mnemonic`, so their traces and metrics could not be told apart.

## [0.4.0] - 2026-10-15

### Fixed

- Failed jobs are retried. A failed job was set back to `pending` with a retry time, but its message was already acked, so nothing ever ran it again. The same was true of stale jobs that `ReclaimStale` reset. The worker now claims jobs due for a retry every 15 seconds (`FOR UPDATE SKIP LOCKED`, up to the number of free workers), along with new jobs left pending for over 2 minutes because the API saved them but failed to publish. Retries get a `retry enrichment job` span and the usual job metrics.
- A queue delivery only starts a job that is pending and due. A redelivered message, a job the retry sweep already took, or one waiting out its retry delay is now acked and skipped instead of being processed again.

### Added

- Distributed tracing for enrichment jobs. Each delivery gets a consumer span that continues the API's trace from the W3C `traceparent` message header, with child spans for PostgreSQL queries (otelpgx), Neo4j operations and OpenAI embedding and extraction calls (otelhttp). Retried OpenAI calls add an `openai.embed.retry` or `openai.extract.retry` span event.
- `mnemonic.enrichment.jobs` (by `outcome`: completed, failed, error, rejected) and `mnemonic.enrichment.job_duration` for every handled delivery.
- PostgreSQL client metrics from otelpgx: `db.client.operation.duration` per operation and `pgxpool.*` pool statistics.
- `mnemonic.db.query_latency` and `mnemonic.db.query_errors` are now recorded for every Neo4j operation. Previously these instruments were created but never recorded.
- Worker log lines for a job carry its `trace_id` and `span_id`.

### Changed

- `ProcessJob` returns an error wrapping `enrichment.ErrJobFailed` when a step fails and the failure is recorded, instead of nil. The worker still acks these messages, and now marks the job's span as failed.
- `queue.Delivery` carries the message's string headers.
- Raised the service and E2E suite modules to Go 1.27.2 and pinned the build and E2E runner images to `golang-tooling:go1.27.2`
- Upgraded `golang.org/x/net` to v0.60.0 to clear five govulncheck findings in v0.59.0
- Custom latency histograms record fractional milliseconds. Previously durations were truncated to whole milliseconds, so sub-millisecond calls recorded 0.
- A chunk job makes 5 PostgreSQL round trips instead of 9: starting a job is one `UPDATE ... RETURNING` (was a SELECT then an UPDATE), the chunk is loaded with its pattern's name and tags in one join, the embedding and enriched status are written in one UPDATE, and one aggregate query replaces the separate any-failed and all-enriched checks.
- The full pattern row (including its content) is loaded once per pattern, by the job that finishes its last chunk, instead of by every chunk job.
- The Neo4j graph sync (pattern node, concepts, RELATED_TO edges) runs in one session and write transaction instead of three, so a failure part way leaves the graph unchanged. It is traced as one `neo4j SyncPattern` span.

### Removed

- The unused `mnemonic.patterns.*` instruments and the unused connection pool gauges, replaced by the otelpgx pool statistics.
- Code copied from the API and MCP server that `main` never reached: the `mcpserver`, `handlers/patterns`, `service/pattern` and `service/search` packages, the unused repository methods (pattern CRUD and list, chunk create/list/search, job create/claim/list, graph reads and deletes), the Neo4j session/runner/result adapter interfaces, and unused telemetry, middleware and version helpers. The `github.com/modelcontextprotocol/go-sdk` dependency is gone with them.
- Config nothing read: the `mcp`, `rate_limit` and `vocabulary` sections, `openai.max_requests_per_minute`, `enrichment.max_attempts` (the job row carries its own max), `logging.format`, `logging.include_caller` and `observability.health`. Setting them is now ignored.
- Graph integration test scripts that pointed at a `migrations/` directory this repo doesn't have. `make tests-db-graph` now runs the graph integration test against `NEO4J_URI`.

## [0.3.2] - 2026-09-09

### Changed

- Raised the service and E2E suite modules to Go 1.27.1
- Pinned the service build and E2E runner images to `golang-tooling:go1.27.1`
- Upgraded direct and indirect service dependencies, including OpenTelemetry, heartbeat, otelx, and testify
- Updated CI to `actions/checkout@v5` and `docker/login-action@v4`

### Fixed

- Retained `pgx/v5` v5.10.0 for compatibility with `pgxmock/v4` v4.9.0, avoiding the missing `pgx.Rows.TypeMap` method errors introduced by pgx v5.11.0
- Pulled E2E infrastructure images before startup and reused them during Compose startup to address Neo4j CI failures
- Prevented E2E Compose from pulling the locally built enricher image

### Removed

- Removed the root Docker Compose file; E2E infrastructure remains in `src/tests/docker-compose.yaml`

## [0.3.1] - 2026-08-19

### Changed

- Raised the service module to Go 1.26.6 and upgraded direct and indirect runtime dependencies
- Aligned the E2E suite, OpenAI stub module, and stub builder image with the service's Go baseline
- Updated the build stage to use the `golang-tooling:latest` image
- Modernized the root and build documentation for project usage, architecture, development, CI, Docker Compose, and RabbitMQ operations
- Updated `make local` to create `.bin` and inject build metadata through the `internal/version` package
- Restored normal registry pull behavior for `dev_api` while retaining `pull_policy: never` for the locally built `dev_enricher` image

### Fixed

- Corrected the example logging keys to `MNEMONIC_LOGGING_LEVEL` and `MNEMONIC_LOGGING_FORMAT`

### Removed

- Removed tracked compiled binaries and added ignore rules to prevent local Go artifacts from being committed

## [0.3.0] - 2026-04-06

### Changed

- Relicensed the repository from proprietary terms to the Apache License 2.0

## [0.2.0] - 2026-03-21

### Added

- E2E test suite for enrichment pipeline (`tests/e2e/pipeline/`) validating happy path, malformed message, job-not-found, and OpenAI failure scenarios against a live Docker Compose environment
- OpenAI HTTP stub (`tests/e2e/openai-stub/`) serving deterministic embeddings and chat completions for E2E tests, with a `/control/fail-next` endpoint to simulate API failures
- Neo4j helper (`tests/e2e/helpers/neo4j.go`) for asserting graph node existence and cleaning up test data
- AMQP helper (`tests/e2e/helpers/amqp.go`) for publishing enrichment job messages to RabbitMQ during E2E tests
- Database helpers (`tests/e2e/helpers/db.go`) for seeding patterns, chunks, and enrichment jobs, and for polling job status

### Fixed

- OpenAI embedding and extraction services now correctly append `/embeddings` and `/chat/completions` path suffixes to `cfg.BaseURL`, so that stub and production URLs are constructed correctly from the base URL configured via `MNEMONIC_OPENAI_BASE_URL`
- Docker Compose E2E configuration (`tests/docker-compose.yaml`): enricher now waits for Postgres and Neo4j to be healthy before starting, preventing connection errors on slow infra startup
- OpenAI stub `fail-next` counter uses a decrement-based mechanism so that all embedding retry attempts within a single operation fail, matching the test intent of simulating a fully unavailable API

## [0.1.0] - 2026-03-20

### Added

- Initial release of `mnemonic-enricher` as a standalone service
- Queue abstraction layer (`internal/queue/queue.go`) with `Subscriber` interface for pluggable queue providers
- RabbitMQ subscriber implementation (`internal/queue/rabbitmq/`) with connection pooling and automatic reconnection
- Enrichment worker pool for concurrent processing of embedding and graph synchronization jobs
- Configuration system (`internal/config/`) with environment variable support and layered defaults
- OpenAI API integration for vector embeddings (text-embedding-3-small by default)
- Neo4j graph synchronization for knowledge graph updates
- PostgreSQL backing store for pattern metadata and embeddings
- HTTP health check endpoint (`GET /health`) and version endpoint (`GET /version`)
- Docker and Docker Compose setup for local development and deployment
- GitHub Actions CI/CD workflows for automated testing and image building
- Comprehensive unit and integration tests for all components
- Structured logging with request tracing support
- Graceful shutdown with configurable drain timeout for in-flight jobs

### Changed

- Extracted from monolithic `mnemonic` service to standalone worker service
- Refactored enrichment processing from polling to queue-driven subscription model
- Moved queue interaction behind abstraction layer for provider independence

### Removed

- Admin REST API handlers for agents, patterns, skills, and skillfiles
- MCP server (pattern search) — now provided by separate service
- Agent routing logic and policy engine
- Pattern search REST endpoints

[Unreleased]: https://github.com/twistingmercury/librarian-enricher/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/twistingmercury/librarian-enricher/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/twistingmercury/librarian-enricher/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/twistingmercury/librarian-enricher/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/twistingmercury/librarian-enricher/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/twistingmercury/librarian-enricher/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/twistingmercury/librarian-enricher/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/twistingmercury/librarian-enricher/releases/tag/v0.1.0
