# mnemonic-api

> **Maturity Level**: Emerging - interfaces and local workflows are still evolving
> **Version**: v0.5.0

---

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
  - [Quick Start](#quick-start)
  - [Building & running](#building--running)
  - [Configuration](#configuration)
  - [Testing](#testing)
  - [API documentation](#api-documentation)
  - [Versioning](#versioning)

## Usage

Mnemonic-api manages reusable knowledge-graph patterns through an Admin REST API. It does not expose an MCP endpoint or listener; the separate `mnemonic-mcp` service exclusively serves the read-only Model Context Protocol (MCP) interface.

With the service running directly on its default ports, create and search patterns through the REST API:

```bash
curl --request POST http://localhost:18080/v1/api/patterns \
  --header "Content-Type: application/json" \
  --data '{
    "name": "go-error-wrapping",
    "description": "Pattern for wrapping errors with context",
    "content": "Use fmt.Errorf with %w to preserve the error chain.",
    "tags": ["go", "error-handling"],
    "entity_type": "go-pattern",
    "language": "go",
    "domain": "backend"
  }'

curl --get http://localhost:18080/v1/api/patterns/search \
  --data-urlencode "q=error handling" \
  --data-urlencode "limit=5"
```

The generated [Swagger 2.0 specification](src/docs/swagger/swagger.yaml) documents the complete REST API. Swagger UI is available at `http://localhost:18080/swagger/index.html`.

## How it works

The Go process runs only the REST API on port 8080. It connects to PostgreSQL with PGVector for pattern and chunk storage, Neo4j for graph relationships, RabbitMQ for enrichment jobs, and OpenAI for query embeddings.

- Pattern mutations are handled by the REST API under `/v1/api/patterns`.
- Semantic searches embed the query and rank enriched pattern chunks by vector similarity.
- Health is exposed at `/health`; Prometheus metrics use a separate listener on port 9090 by default.

Unexpected HTTP failures return generic 500/503 details. A single private request-completion log records the status, route template, duration, request ID, active trace/span IDs, and a bounded diagnostic cause; tracing records an exception and HTTP metrics include recovered panics. Diagnostics remove upstream response bodies, URLs, quoted values, and credential/content fields. Request bodies and query strings are not included in completion logs. New dependency error formats must preserve this privacy boundary.

Error response `traceId` identifies the active OpenTelemetry trace and is omitted when no valid trace context exists. `X-Request-ID` remains separate: the API echoes identifiers of up to 128 ASCII letters, digits, dots, underscores, or hyphens, and generates a UUID for missing or invalid values. Successful `/health` and `/metrics` requests skip completion logging, tracing, and HTTP metrics.

## Key Considerations

- The current API deployment model assumes a trusted environment and does not authenticate REST requests.
- TLS terminates at Envoy or an equivalent reverse proxy. The API serves plain HTTP behind it and has no in-process TLS settings, so the network path between the proxy and the API must be trusted. Encrypting that hop, for example with mesh mTLS, is proxy or sidecar configuration, not application code.
- Pattern creation is asynchronous and returns `202 Accepted`; semantic results become available after enrichment completes.
- A create or update writes the pattern, its chunks and one enrichment job per chunk in one transaction. A failure persists nothing, so a rejected create leaves its name free to retry. Publishing those jobs to RabbitMQ happens after the commit and is best-effort: if it fails the rows remain pending rather than being lost.
- PostgreSQL, Neo4j, RabbitMQ, and an OpenAI API key are required for a working runtime.
- Direct execution uses ports 8080 and 9090. The root Docker Compose stack publishes the Admin API on port 3000; the separate `dev_mcp` service owns its MCP listener.
- Configuration and API contracts may change while the project remains at the Emerging maturity level.

## Development Considerations

### Quick Start

Prerequisites are Go 1.27.2, Docker, Docker Compose v2, and Git.

```bash
git clone https://github.com/twistingmercury/librarian-api.git
cd mnemonic-api
export LIBRARIAN_OPENAI_API_KEY="your-api-key"
make build
```

`make build` builds `ghcr.io/twistingmercury/librarian-api`, runs its unit tests in the Docker build, and executes the Docker Compose E2E suite.

### Building & running

Run all commands from the repository root:

```bash
make help
make build
make start
```

`make start` launches the root [Docker Compose stack](docker-compose.yaml) and publishes mnemonic-api at `http://localhost:13000`. The `dev_api` service requires the locally built mnemonic-api image (`pull_policy: never`); `dev_mcp` and `dev_enricher` use normal registry behavior. `make stop` removes the Compose volumes, deletes the local `migrate/migrate:latest` image, and prunes unused Docker data.

### Configuration

Configuration precedence is built-in defaults, an optional YAML file, then `LIBRARIAN_` environment variables. Only one file is read, chosen in this order:

1. The `--config <path>` flag.
2. The `LIBRARIAN_CONFIG_FILE` environment variable.
3. `/etc/mnemonic/config.yaml`, if it exists.
4. `./config.yaml`, if it exists.

Finding no file is fine: the service starts on defaults plus environment variables. Once a file is selected, by any of these routes, it must be readable and valid YAML. A missing explicit path, a permission error or a parse error stops startup with `failed to read config file <path>` instead of falling back to defaults.

`--health` probes `http://localhost:<port>/health` and exits non-zero unless it gets `200`. It resolves `server.port` from the same flag, file and environment sources as the server, but does not validate the rest of the configuration, so it runs without database, OpenAI or RabbitMQ credentials. An unreadable config file or a port outside 1-65535 fails the probe without sending a request.

Nested keys use underscores in environment variables. For example, `server.port` becomes `LIBRARIAN_SERVER_PORT`, and the OpenAI credential is `LIBRARIAN_OPENAI_API_KEY`. `openai.base_url` (`LIBRARIAN_OPENAI_BASE_URL`, default `https://api.openai.com/v1`) points the embedding client and the health check at an OpenAI-compatible endpoint; the E2E suite uses it to reach a local stub. This API has no `mcp.*` configuration; MCP settings belong to `mnemonic-mcp`.

### Testing

The root Makefile provides the supported test entry points:

```bash
make tests-unit          # Unit tests with coverage
make tests-integration   # Tests tagged `integration` against a throwaway PostgreSQL
make tests-bench         # Internal package benchmarks
make build               # Image build plus the full E2E suite
```

The E2E suite uses the pre-migrated PostgreSQL and Neo4j images and a local OpenAI stub (`src/tests/openai-stub`), and requires Docker. It never calls api.openai.com.

`make tests-integration` starts PostgreSQL on port 15435 in its own Compose project, declaring no volume so every run begins with an empty database, and tears it down afterwards. Scope it with `PKGS=./internal/service/pattern/...`. These tests carry the `integration` build tag and are skipped by `make tests-unit`; without `TEST_DATABASE_URL` they skip and still report `ok`, so use `GOFLAGS=-v` to confirm they actually ran.

`make build` is the verification bar. A stack left running by `make start` keeps whatever image it began with, so testing against it says nothing about the current working tree.

The [golangci-lint configuration](src/.golangci.yml) sets `run.tests: false` to exclude `*_test.go` files from lint analysis. `go test` still compiles and runs those tests. Keep `pgx` at 5.10.0 for compatibility with `pgxmock` 4.9.0; `pgx` 5.11.0 requires a `TypeMap` method that this mock version does not implement.

### API documentation

Regenerate the tracked Swagger files after changing routes or schemas:

```bash
make docs-swagger
```

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/). Build metadata derives the version from the latest reachable Git tag:

```bash
git describe --tags --abbrev=0
```

See [CHANGELOG.md](CHANGELOG.md) for development history.
