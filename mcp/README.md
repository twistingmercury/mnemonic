# mnemonic-mcp

> **Maturity Level**: Emerging — MCP search server functional, Admin REST API in a separate service (`mnemonic-api`)
> **Version**: v0.6.0
>
> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

---

## Table of Contents

- [mnemonic-mcp](#mnemonic-mcp)
  - [Table of Contents](#table-of-contents)
  - [Usage](#usage)
  - [How it works](#how-it-works)
  - [Key Considerations](#key-considerations)
  - [Development Considerations](#development-considerations)
    - [Quick Start](#quick-start)
    - [Building & running](#building--running)
    - [Testing](#testing)
    - [Versioning](#versioning)
  - [Documentation](#documentation)
    - [Architecture](#architecture)
    - [Design](#design)

## Usage

Mnemonic exposes Streamable HTTP MCP at <http://localhost:8081/mcp> through the gateway, speaking MCP protocol version 2026-07-28 (and 2025-11-25 and earlier for older clients).

Example MCP tool invocation:

```json
{
  "tool": "search_patterns",
  "arguments": { "query": "Go error handling patterns" }
}
```

MCP tools available: `search_patterns`, `find_related_patterns`, `get_pattern`.

Pattern data is managed via the companion [mnemonic-api](../api/README.md) service.

## How it works

Mnemonic stores curated engineering patterns in Postgres (with PGVector for
embeddings) and Neo4j (for concept relationships). This service exposes MCP
search tools over that data. The companion Admin API publishes enrichment jobs
to RabbitMQ, and the separate `mnemonic-enricher` service consumes them.

**Local stack (Docker Compose):** [deploy/docker-compose.yaml](../deploy/docker-compose.yaml)
runs this service as `mnemonic-mcp` (image `ghcr.io/twistingmercury/mnemonic-mcp`)
next to `mnemonic-api`, `mnemonic-enricher`, `mnemonic-postgres`,
`mnemonic-neo4j` and RabbitMQ, behind an Envoy gateway. Only Envoy and
Keycloak publish host ports: the MCP endpoint is `:8081/mcp`, the API is
`:8080` and Keycloak is `:8180`. The service itself listens on port 8081
inside the Compose network and is not published directly. Both database images
are pre-configured with the required schema, so no migration step is needed.
The gateway checks a Keycloak token on each request; see the
[gateway README](../gateway/README.md).

To add the mcp server to Claude Code: `claude mcp add --transport http --scope user mnemonic http://localhost:8081/mcp`

## Key Considerations

- **This repo**: Read-only MCP search and operational endpoints; administration
  lives in `mnemonic-api`
- **Current enrichment**: The Admin API publishes jobs to RabbitMQ for the
  separate `mnemonic-enricher` worker; this service reads the resulting data
- **Security**: The service has no authentication code; the gateway checks a
  Keycloak token before a request reaches it. Run it only behind the gateway or
  an equivalent proxy
- **Ports**: MCP listens on 8081 (`MNEMONIC_MCP_PORT`), health and version on
  8080 and Prometheus metrics on 9090

## Development Considerations

### Quick Start

Requires Go 1.27.3+, Docker 27+, and Docker Compose 2.32+.

```bash
git clone https://github.com/twistingmercury/mnemonic.git
cd mnemonic
make tests-unit
```

### Building & running

`make build-mcp` runs `build/mcp/mcp.build.sh`: it builds
`ghcr.io/twistingmercury/mnemonic-mcp` with the current version and `latest`
tags, then runs the whole end-to-end suite in [`tests/integration`](../tests/integration/README.md)
in containers. It never pushes; publishing is planned for CI, which is not written yet. The suite points `openai.base_url` (`MNEMONIC_OPENAI_BASE_URL`, default
`https://api.openai.com/v1`) at the local stub in `tests/openai-stub`, so no
E2E test calls OpenAI. See the [build guide](../build/mcp/README.md).

To run the whole local stack, export `MNEMONIC_OPENAI_API_EMBEDDINGS_KEY` in
your shell (only the egress proxy reads it) and run `docker compose -f deploy/docker-compose.yaml up -d`;
`docker compose -f deploy/docker-compose.yaml down` stops it. The stack uses the images named by `MNEMONIC_API_IMAGE`,
`MNEMONIC_MCP_IMAGE` and `MNEMONIC_ENRICHER_IMAGE`, defaulting to the
`latest-dev` images on ghcr.io, and `docker compose up` does not build or retag images.

### Testing

golangci-lint excludes test files; the unit-test suite still compiles and runs
them. Keep `pgx/v5` at v5.10.0 while using `pgxmock/v4` v4.9.0: the mock does
not implement the `Rows.TypeMap()` method required by pgx v5.11.0.

```bash
# Unit tests
make tests-unit

# Image build and end-to-end tests
make build-mcp
```

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Each component has its own tag (`mcp/vX.Y.Z`); the version comes from the
latest one:

```bash
build/version.sh mcp
```

See [CHANGELOG.md](CHANGELOG.md) for development progress.

## Documentation

### Architecture

Architecture and related decisions can be found here: [Architecture](../docs/design/architecture/system/README.md)

### Design

Design documents are here: [Design Documents](../docs/design/README.md)
