# mnemonic-enricher

> **Maturity Level**: Emerging - Prototype, not production-ready, expect breaking changes
> **Version**: v0.5.0
>
> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

[![Build Status](https://github.com/twistingmercury/librarian-enricher/actions/workflows/mnemonic-enrichment-ci.yaml/badge.svg)](https://github.com/twistingmercury/librarian-enricher/actions/workflows/mnemonic-enrichment-ci.yaml)

---

## Table of Contents

- [mnemonic-enricher](#mnemonic-enricher)
  - [Table of Contents](#table-of-contents)
  - [Usage](#usage)
  - [How it works](#how-it-works)
  - [Key Considerations](#key-considerations)
  - [Development Considerations](#development-considerations)
    - [Quick Start](#quick-start)
    - [Building & running](#building--running)
    - [Testing](#testing)
    - [Versioning](#versioning)

## Usage

`mnemonic-enricher` is a queue-driven Go worker. It consumes enrichment job IDs
from RabbitMQ, generates embeddings and concepts with OpenAI, persists enrichment
state in PostgreSQL, and synchronizes the knowledge graph to Neo4j.

Publish a job that already exists in PostgreSQL to the configured RabbitMQ queue
(`enrichment-jobs` by default):

```json
{
  "job_id": "00000000-0000-0000-0000-000000000000"
}
```

The service exposes operational endpoints on ports `8080` and `9090` by default:

```bash
curl http://localhost:18080/health
curl http://localhost:18080/version
curl http://localhost:19090/metrics
```

## How it works

1. The RabbitMQ subscriber receives an enrichment job ID.
2. The worker loads the job from PostgreSQL and marks it as processing.
3. Chunk jobs are embedded with context from their parent pattern; the worker waits
   for every chunk before finalizing the pattern. Legacy pattern-only jobs skip
   embedding.
4. OpenAI generates chunk embeddings and extracts concepts for the pattern.
5. The worker persists embeddings and job status to PostgreSQL, then synchronizes
   patterns, concepts, `MENTIONED_IN`, and similarity-based `RELATED_TO` data to
   Neo4j.
6. The worker marks the job completed or failed and acknowledges the queue delivery.

RabbitMQ is currently the only queue provider, behind the `queue.Subscriber`
interface.

## Key Considerations

- This worker is not the Mnemonic API. Its HTTP surface is limited to health,
  version, and metrics endpoints.
- PostgreSQL, Neo4j, RabbitMQ, and OpenAI are required runtime dependencies.
- The PostgreSQL schema and the producers that create enrichment jobs are maintained
  outside this repository and must be available before the worker starts.
- `LIBRARIAN_OPENAI_EMBEDDING_DIMENSIONS` must match the PostgreSQL vector column.
- OpenAI embedding and concept-extraction requests can incur usage and cost.
- Configuration precedence is compiled defaults, YAML, then `LIBRARIAN_` environment
  variables. See
  [`src/internal/config/defaults.go`](src/internal/config/defaults.go) for defaults.

## Development Considerations

### Quick Start

Requirements are Go 1.27.2 or newer, Docker with the Compose plugin for container
builds and end-to-end tests, accessible PostgreSQL, Neo4j, and RabbitMQ instances
with the Mnemonic schema, and an OpenAI API key for live enrichment.

### Building & running

Build and run a local binary:

```bash
git clone https://github.com/twistingmercury/librarian-enricher.git
cd mnemonic-enricher
mkdir -p .bin
make local
export LIBRARIAN_DATABASE_POSTGRES_PASSWORD=mnemonic_dev
export LIBRARIAN_DATABASE_NEO4J_PASSWORD=mnemonic_dev
export LIBRARIAN_QUEUE_RABBITMQ_PASSWORD=guest
export LIBRARIAN_OPENAI_API_KEY=your-api-key
./.bin/mnemonic-enricher
```

The [E2E Compose file](src/tests/docker-compose.yaml) provides test infrastructure
using published Mnemonic PostgreSQL and Neo4j images, RabbitMQ, and an OpenAI stub.
See the [build and development guide](src/build/README.md) for image builds,
Compose services, endpoints, and troubleshooting.

### Testing

The service retains `pgx/v5` v5.10.0 for compatibility with `pgxmock/v4` v4.9.0.
Upgrading to pgx v5.11.0 requires a mock implementation of `pgx.Rows.TypeMap`.

Run unit tests:

```bash
cd src
go test ./...
```

From the repository root, run benchmarks or the Docker-first build and end-to-end
pipeline used by CI:

```bash
make tests-bench
make build
```

Run formatting, linting, vulnerability analysis, and security scanning with
`make analyze`.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Version is determined from Git tags:

```bash
git describe --tags --abbrev=0
```

See [CHANGELOG.md](CHANGELOG.md) for release history.
