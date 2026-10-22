# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- 2026-10-21: Renamed the project back to Mnemonic in the monorepo, dropping the placeholder name "Librarian": the Go module path is `github.com/twistingmercury/mnemonic`, the image is `ghcr.io/twistingmercury/mnemonic-mcp`, every environment variable uses the `MNEMONIC_` prefix and the host ports are the original ones (gateway 8080, 8081 and 8180).

### Removed

- 2026-10-22: The Go integration tests (build tag `integration`); the end-to-end suite in `tests/integration` covers the behavior a client can observe. Scenarios no client can create are no longer tested.

## [0.6.0] - 2026-10-16

### Changed

- **Breaking:** `database.postgres.max_idle_conns` (`MNEMONIC_DATABASE_POSTGRES_MAX_IDLE_CONNS`) is renamed `database.postgres.min_conns` (`MNEMONIC_DATABASE_POSTGRES_MIN_CONNS`). It always set the pool's minimum open connections, not an idle cap. The default is still 5; it must be between 0 and `max_open_conns`.
- The `search_patterns` tool schema declares the minimum, maximum and default of `limit` and `threshold`, and the SDK enforces them. Out-of-range values and an explicit `null` are rejected with the SDK's `validating "arguments"` message.
- The MCP server shuts down within `server.shutdown_timeout` instead of a fixed 5 seconds. The default is the same.
- `find_related_patterns` output no longer shows a shared-concept count; the concept names are still listed.
- Config validation errors list one `field: message` per line under the header, without a leading dash.
- Logged connection strings mask the password as `xxxxx` instead of `*****`.

### Removed

- **Breaking:** the `mnemonic.http.request.count`, `mnemonic.http.request.duration` and `mnemonic.http.request.in_flight` metrics. Use otelgin's standard `http.server.request.duration`.
- The Neo4j session adapter layer; the graph repository uses `neo4j.ExecuteQuery`.

### Fixed

- Tool calls with invalid arguments are counted as `invalid_input` instead of `unavailable`.
- An all-zero pattern ID passed to `find_related_patterns` returns "pattern not found". It used to report the service as unavailable.
- Neo4j URIs using `bolt+s://`, `bolt+ssc://`, `neo4j+s://` or `neo4j+ssc://` are accepted. They used to fail validation at startup.
- `--health` reads `server.port` from the config file. It used to see only defaults and environment variables.
- `--version` prints to stdout instead of stderr.

## [0.5.1] - 2026-10-16

### Changed

- `database.postgres.max_open_conns` and `database.postgres.max_idle_conns` are loaded as 32-bit integers, the type the PostgreSQL pool takes. A value above 2147483647 now fails config loading with an error. It used to be silently capped at that limit.

## [0.5.0] - 2026-10-16

### Added

- Support for MCP protocol version 2026-07-28, served by go-sdk v1.8.0: clients can call `server/discover` and send each request on its own with the protocol version and client capabilities in `_meta`, without an `initialize` handshake. Mismatched `Mcp-Method`/`Mcp-Name` headers get 400 with `-32020` (HeaderMismatch) and unknown versions get 400 with `-32022`. Clients on 2025-11-25 and earlier keep working through `initialize`.
- A tool call whose `_meta` carries `traceparent` (and optionally `tracestate` and `baggage`) gets its `tools/call <tool>` span in that trace, linked to the `/mcp` HTTP span.
- `mcp.method.name` and `mcp.protocol.version` attributes on the `/mcp` HTTP span, read from the `Mcp-Method` and `MCP-Protocol-Version` headers.

### Changed

- `/mcp` rejects cross-origin browser requests with 403, as the 2026-07-28 spec requires servers to validate `Origin`. Clients that send no `Origin` header, such as Claude Code, are unaffected.
- The server advertises only the `tools` capability, without `listChanged`. It used to also advertise `logging`, which the spec now deprecates and the server never used, and `tools.listChanged`, although the tool list never changes at runtime.
- `tools/list` and `server/discover` results carry `ttlMs: 3600000` and `cacheScope: "public"`, so clients may cache the tool list for an hour. They used to send `ttlMs: 0`.

## [0.4.1] - 2026-10-16

### Changed

- The service reports its own name, `mnemonic-mcp`, as the OpenTelemetry `service.name`, the `/health` `name`, the `/version` `service` and the HTTP tracing middleware name. All three services used to report `mnemonic`, so their traces and metrics could not be told apart.

## [0.4.0] - 2026-10-15

### Added

- An `openai.base_url` setting (`MNEMONIC_OPENAI_BASE_URL`, default `https://api.openai.com/v1`) used by the embedding client and the `/health` model check. The E2E suite points it at a local OpenAI stub, so the search tests now require a successful result instead of accepting an embedding error.
- Tracing for the `/mcp` endpoint. Each request gets an otelhttp server span (continuing any incoming W3C trace context) and each tool call a `tools/call <tool>` span beneath it, with child spans for PostgreSQL queries (otelpgx), Neo4j operations and OpenAI embedding calls (otelhttp). Retried embedding calls add an `openai.embed.retry` span event.
- `mnemonic.mcp.tool_calls` (by `tool` and `outcome`: ok, invalid_input, not_found, unavailable) and `mnemonic.mcp.tool_duration`, plus the standard `http.server.*` metrics for `/mcp`.
- PostgreSQL client metrics from otelpgx: `db.client.operation.duration` per operation and `pgxpool.*` pool statistics.
- `mnemonic.db.query_latency` and `mnemonic.db.query_errors` are now recorded for every Neo4j operation, and `mnemonic.patterns.query_latency` and `mnemonic.patterns.returned` for every vector search. Previously these instruments were created but never recorded.
- Tool-call log lines carry `trace_id` and `span_id`.

### Changed

- Updated Go modules and Docker build images to Go 1.27.2.
- Upgraded `golang.org/x/net` to v0.60.0 to clear five govulncheck findings in v0.59.0.
- Custom latency histograms record fractional milliseconds. Previously durations were truncated to whole milliseconds, so sub-millisecond calls recorded 0.
- `find_related_patterns` reads the source pattern's name with one `SELECT name` run alongside the Neo4j query, instead of an existence check followed by a full `get_pattern` lookup (a PostgreSQL row read plus two more Neo4j queries).
- `get_pattern` runs its two Neo4j queries (related patterns, concepts) concurrently and reads only the columns it shows.
- `search_patterns` queries Neo4j for its graph-expansion seeds concurrently, and no longer loads chunk or pattern columns the response does not use (pattern `content` for graph candidates in particular).
- OpenAI embedding calls are not retried after a 4xx response other than 429; those fail the same way on every attempt.

### Removed

- The unused connection pool gauges, replaced by the otelpgx pool statistics.
- Code carried over from the API and enricher that the MCP server never runs: the enricher, enrichment service, enrichment job repository, concept extraction client, pattern and chunk write paths, Neo4j write queries, and their tests.
- Unused configuration: the `rate_limit` and `enrichment` sections, `logging.format`, `logging.include_caller`, `observability.health`, `openai.extraction_model` and `openai.max_requests_per_minute`. Setting them has no effect.

## [0.3.2] - 2026-09-09

### Changed

- Updated Go modules and Docker build images to Go 1.27.1 and refreshed dependencies.
- Disabled test-file analysis in golangci-lint.

### Fixed

- Moved test-only constructors into `export_test.go` files to avoid unused-function
  lint errors when test analysis is disabled.
- Kept pgx at v5.10.0 for compatibility with pgxmock v4.9.0, restoring unit-test
  compilation during `make build`.

## [0.3.1]

### Changed

- Updated build to use latest go version 1.26.6
- Updated packages as needed
- Updated docker build to use latest golang-tooling image

## [0.3.0]

### Added

- GitHub Actions CI workflow for Mnemonic service (`mnemonic-ci.yaml`)
- GitHub Actions CD workflow for Mnemonic service (`mnemonic-cd.yaml`)
- Docker image artifact passing between CI and CD workflows
- E2E test execution during CI builds
- Cleanup trap in build script for docker compose
- Configuration package implementation (`internal/config`)
- Layered configuration loading (defaults → file → environment variables)
- Comprehensive validation with clear error messages for all configuration sections
- Server integration with configurable timeouts, TLS, and graceful shutdown
- Telemetry package (`internal/telemetry`) with otelx integration for unified OpenTelemetry initialization
- Middleware package (`internal/middleware`) with tracing and request metrics middleware for Gin
- Metrics package (`internal/metrics`) with domain-specific metrics for routing, patterns, and database operations
- Distributed tracing support via otelgin middleware
- Structured logging with trace correlation via otelx
- Request metrics: count, duration histograms, in-flight counters
- Version package (`internal/version`) for build metadata, eliminating upward dependencies
- Agent repository package (`internal/repository`) with PostgreSQL implementation
- Agent schema migrations (002_create_agents.up.sql and 002_create_agents.down.sql)
- Repository error types: ErrAgentExists, ErrAgentNotFound, ErrAgentInUse
- List options for pagination support in repository queries
- Comprehensive unit tests for agent repository with pgxmock
- RegexMatcher implementation with compiled pattern caching and sliding TTL
- Background cleanup goroutine for expired regex patterns
- `Close()` method added to `RuleMatcher` interface for resource cleanup
- `CloseAll()` method added to `MatcherRegistry` for graceful matcher shutdown
- `PatternIDs []uuid.UUID` field on `SimilarityOptions` for filtering similarity search to specific pattern IDs
- Unit and integration tests for PatternIDs filter on `FindSimilar`
- Startup timeout for rule cache (`routing.cache.startup_timeout`) bounds
  initial rule load to prevent indefinite hangs (default 30s)

### Changed

- Build script now uses identical behavior for local and CI execution
- Push logic moved from build script to CD workflow
- Server now initializes telemetry and registers observability middleware
- Configuration validation includes log level validation (fail-fast on invalid level)
- Moved MetricsRegistry from server package to telemetry package for proper dependency direction
- Refactored cmd/version to delegate to internal/version, fixing internal->cmd dependency violation
- `NormalizePrompt` now trims only (no longer lowercases); matchers own case-folding for their specific needs
- Routing engine design doc updated: Pattern Matcher section corrected from in-Go cosine similarity to pgvector-delegated approach via `<=>` operator
- `data-storage.md` updated: `SimilarityOptions` struct includes `PatternIDs` field

### Fixed

- Typo in Mnemonic E2E tests docker-compose.yaml (`menmonic_tests` → `mnemonic_tests`)
- `NormalizePrompt` changed from lowercase+trim to trim-only, fixing case-sensitive regex matching (regex flags now control case folding)
- Unknown regex flags now rejected with clear error message instead of being silently ignored
