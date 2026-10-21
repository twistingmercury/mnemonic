# Mnemonic

[![Mnemonic MCP CI](https://github.com/twistingmercury/librarian-mcp/actions/workflows/mnemonic-ci.yaml/badge.svg)](https://github.com/twistingmercury/librarian-mcp/actions/workflows/mnemonic-ci.yaml)

> **Maturity Level**: Emerging — MCP search server functional, Admin REST API in a separate service (`mnemonic-api`)
> **Version**: v0.6.0
>
> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

---

## Table of Contents

- [Mnemonic](#mnemonic)
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

Mnemonic exposes Streamable HTTP MCP at <http://localhost:18081/mcp>, speaking MCP protocol version 2026-07-28 (and 2025-11-25 and earlier for older clients).

Example MCP tool invocation:

```json
{
  "tool": "search_patterns",
  "arguments": { "query": "Go error handling patterns" }
}
```

MCP tools available: `search_patterns`, `find_related_patterns`, `get_pattern`.

Pattern data is managed via the companion [mnemonic-api](https://github.com/twistingmercury/librarian-api) service.

## How it works

Mnemonic stores curated engineering patterns in Postgres (with PGVector for
embeddings) and Neo4j (for concept relationships). This service exposes MCP
search tools over that data. The companion Admin API publishes enrichment jobs
to RabbitMQ, and the separate `mnemonic-enricher` service consumes them.

**Local dev stack (Docker Compose):**

| Service        | Image                                       | Host access                                     |
| -------------- | ------------------------------------------- | ----------------------------------------------- |
| `dev_mcp`      | `ghcr.io/twistingmercury/librarian-mcp`          | MCP `:8081/mcp`; metrics `:9090/metrics`        |
| `dev_api`      | `ghcr.io/twistingmercury/librarian-api`      | Admin REST API on `:8080`                       |
| `dev_postgres` | `ghcr.io/twistingmercury/librarian-postgres` | Postgres + PGVector on `:15433`                  |
| `dev_neo4j`    | `ghcr.io/twistingmercury/librarian-neo4j`    | Neo4j HTTP on `:17475`; Bolt on `:17688`          |
| `dev_rabbitmq` | `rabbitmq:4-management-alpine`              | AMQP on `:15673`; management console on `:25673` |

Both database images are pre-configured with the required schema, so no
migration step is needed. From the host, MCP is available at
<http://localhost:18081/mcp> and metrics at <http://localhost:19090/metrics>.
Within the Compose network, the MCP service exposes operations at
`http://dev_mcp:8080/health` and `http://dev_mcp:8080/version`. Its operations
port is not published to the host; host port 8080 routes to `dev_api`. The stack
does not include `mnemonic-enricher`, so queued content will not become
searchable unless that service is run separately.

To run the add the mcp server to Claude: `claude mcp add --transport http --scope user mnemonic http://localhost:18081/mcp`

## Key Considerations

- **This repo**: Read-only MCP search and operational endpoints; administration
  lives in `mnemonic-api`
- **Current enrichment**: The Admin API publishes jobs to RabbitMQ for the
  separate `mnemonic-enricher` worker; this service reads the resulting data
- **MVP security**: Local, trusted, single-user deployment without authentication
- **Post-MVP**: Authentication, authorization, and production deployment

## Development Considerations

### Quick Start

Requires Go 1.27.2+, Docker 27+, and Docker Compose 2.32+.

```bash
git clone https://github.com/twistingmercury/librarian-mcp.git
cd mnemonic-mcp
make tests-unit
```

### Building & running

`make build` builds `ghcr.io/twistingmercury/librarian-mcp` with the current version
and `latest` tags, then runs the end-to-end suite. The suite also requires the
`ghcr.io/twistingmercury/librarian-api:latest-dev` image to be available to
Docker. It points `openai.base_url` (`LIBRARIAN_OPENAI_BASE_URL`, default
`https://api.openai.com/v1`) at a local stub in `src/tests/openai-stub`, so no
E2E test calls OpenAI.

The local Compose stack is a separate workflow. Before `make start`, provide
`LIBRARIAN_OPENAI_API_KEY` and ensure these development images already exist
locally because their services use `pull_policy: never`:

- `ghcr.io/twistingmercury/librarian-mcp:latest-dev`
- `ghcr.io/twistingmercury/librarian-api:latest-dev`

```bash
export LIBRARIAN_OPENAI_API_KEY="your-api-key"
make start
```

`make start` does not build or retag images.

### Testing

golangci-lint excludes test files; the unit-test suite still compiles and runs
them. Keep `pgx/v5` at v5.10.0 while using `pgxmock/v4` v4.9.0: the mock does
not implement the `Rows.TypeMap()` method required by pgx v5.11.0.

```bash
# Unit tests
make tests-unit

# Image build and end-to-end tests
make build
```

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Version is determined from git tags:

```bash
git describe --tags --abbrev=0
```

See [CHANGELOG.md](CHANGELOG.md) for development progress.

## Documentation

### Architecture

Architecture and related decisions can be found here: [Mnemonic Docs](https://github.com/twistingmercury/librarian-docs/blob/develop/docs/architecture/system/README.md)

### Design

Design documents are here: [Design Documents](https://github.com/twistingmercury/librarian-docs/blob/develop/docs/design/README.md)
