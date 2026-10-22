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
curl --request POST http://localhost:8080/v1/api/patterns \
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

curl --get http://localhost:8080/v1/api/patterns/search \
  --data-urlencode "q=error handling" \
  --data-urlencode "limit=5"
```

The generated [Swagger 2.0 specification](../src/docs/api/swagger/swagger.yaml) documents the complete REST API. Swagger UI is available at `http://localhost:8080/swagger/index.html`.

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
- Direct execution uses ports 8080 and 9090. The full stack in [deploy/docker-compose.yaml](../deploy/docker-compose.yaml) does not publish the API directly: Envoy publishes it on port 8080 behind token authentication, and the separate `mnemonic-mcp` service owns the MCP listener.
- Configuration and API contracts may change while the project remains at the Emerging maturity level.

## Development Considerations

### Quick Start

Prerequisites are Go 1.27.3, Docker, Docker Compose v2, and Git.

```bash
git clone https://github.com/twistingmercury/mnemonic.git
cd mnemonic
make build-api
```

`make build-api` runs `build/api/api.build.sh`. It builds `ghcr.io/twistingmercury/mnemonic-api` (runs the unit tests in the Docker build), then runs the whole Docker Compose E2E suite in [`tests/integration`](../tests/integration/README.md) against it. It never pushes; publishing is planned for CI, which is not written yet.

### Building & running

Run all commands from the repository root:

```bash
make help
make build-api
make local
```

`make local` builds the `api`, `mcp` and `enricher` binaries into `src/.bin` without tests or Docker. To run the whole stack behind the gateway, export `MNEMONIC_OPENAI_API_EMBEDDINGS_KEY` in your shell and run `docker compose -f deploy/docker-compose.yaml up -d` (`docker compose -f deploy/docker-compose.yaml down` stops it). That file is [deploy/docker-compose.yaml](../deploy/docker-compose.yaml), which uses the images named by `MNEMONIC_API_IMAGE`, `MNEMONIC_MCP_IMAGE` and `MNEMONIC_ENRICHER_IMAGE`, defaulting to the `latest-dev` images on ghcr.io, and publishes the API on `http://localhost:8080`. To run the service directly, copy [.env.example](../.env.example) to `.env` and use the databases from `db/docker-compose.yaml`. That file has no RabbitMQ, so run your own broker on `localhost:5672` (the `guest`/`guest` defaults in `.env.example`); the RabbitMQ in `deploy/docker-compose.yaml` publishes no host port and uses `mnemonic` / `mnemonic_dev`. The egress proxy is reachable only inside that compose network, so a direct run cannot use it.

### Configuration

Configuration precedence is built-in defaults, an optional YAML file, then `MNEMONIC_` environment variables. Only one file is read, chosen in this order:

1. The `--config <path>` flag.
2. The `MNEMONIC_CONFIG_FILE` environment variable.
3. `/etc/mnemonic/config.yaml`, if it exists.
4. `./config.yaml`, if it exists.

Finding no file is fine: the service starts on defaults plus environment variables. Once a file is selected, by any of these routes, it must be readable and valid YAML. A missing explicit path, a permission error or a parse error stops startup with `failed to read config file <path>` instead of falling back to defaults.

`--health` probes `http://localhost:<port>/health` and exits non-zero unless it gets `200`. It resolves `server.port` from the same flag, file and environment sources as the server, but does not validate the rest of the configuration, so it runs without database, OpenAI or RabbitMQ credentials. An unreadable config file or a port outside 1-65535 fails the probe without sending a request.

Nested keys use underscores in environment variables. For example, `server.port` becomes `MNEMONIC_SERVER_PORT`, and the OpenAI credential is `MNEMONIC_OPENAI_API_KEY`. `openai.base_url` (`MNEMONIC_OPENAI_BASE_URL`, default `https://api.openai.com/v1`) points the embedding client and the health check at an OpenAI-compatible endpoint; the E2E suite uses it to reach a local stub. This API has no `mcp.*` configuration; MCP settings belong to `mnemonic-mcp`.

### Testing

The root Makefile provides the supported test entry points:

```bash
make tests-unit          # Unit tests with coverage
make analyze             # Linters, formatters and security scanners
make build-api           # Image build plus the full E2E suite
```

The E2E suite uses the pre-migrated PostgreSQL and Neo4j images and a local OpenAI stub (`tests/openai-stub`), and requires Docker. It never calls api.openai.com.

`make build-api` is the verification bar. A stack left running by `docker compose up` keeps whatever image it began with, so testing against it says nothing about the current working tree.

The [golangci-lint configuration](../src/.golangci.yml) sets `run.tests: false` to exclude `*_test.go` files from lint analysis. `go test` still compiles and runs those tests. Keep `pgx` at 5.10.0 for compatibility with `pgxmock` 4.9.0; `pgx` 5.11.0 requires a `TypeMap` method that this mock version does not implement.

### API documentation

Regenerate the tracked Swagger files after changing routes or schemas:

```bash
make openapi
```

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/). The monorepo versions each component by its own tag (`api/vX.Y.Z`); the build script derives the api version from the latest one:

```bash
build/version.sh api
```

See [CHANGELOG.md](CHANGELOG.md) for development history.
