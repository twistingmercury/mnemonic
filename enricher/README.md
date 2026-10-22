# mnemonic-enricher

> **Maturity Level**: Emerging - Prototype, not production-ready, expect breaking changes
> **Version**: v0.5.0
>
> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

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
curl http://localhost:8080/health
curl http://localhost:8080/version
curl http://localhost:9090/metrics
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
  outside this service (the schema lives in [db](../db/README.md), the producer is the api) and must be available before the worker starts.
- `MNEMONIC_OPENAI_EMBEDDING_DIMENSIONS` must match the PostgreSQL vector column.
- OpenAI embedding and concept-extraction requests can incur usage and cost.
- Configuration precedence is compiled defaults, YAML, then `MNEMONIC_` environment
  variables. See
  [`src/internal/enricher/config/defaults.go`](../src/internal/enricher/config/defaults.go) for defaults.

## Development Considerations

### Quick Start

Requirements are Go 1.27.3 or newer, Docker with the Compose plugin for container
builds and end-to-end tests, accessible PostgreSQL, Neo4j, and RabbitMQ instances
with the Mnemonic schema, and an OpenAI API key for live enrichment.

### Building & running

Run the service directly from source:

```bash
git clone https://github.com/twistingmercury/mnemonic.git
cd mnemonic
export MNEMONIC_DATABASE_POSTGRES_PASSWORD=mnemonic_dev
export MNEMONIC_DATABASE_NEO4J_PASSWORD=mnemonic_dev
export MNEMONIC_QUEUE_RABBITMQ_PASSWORD=guest
export MNEMONIC_OPENAI_API_KEY=your-api-key
go -C src run ./cmd/enricher
```

`.env.example` at the repository root lists every setting for local runs. In the
full stack the services never hold the real OpenAI key; see the
[deploy Compose file](../deploy/docker-compose.yaml).

The end-to-end suite and its Compose file are in [`tests/integration`](../tests/integration/README.md).
See the [build and development guide](../build/enricher/README.md) for image
builds, Compose services, endpoints, and troubleshooting.

### Testing

The service retains `pgx/v5` v5.10.0 for compatibility with `pgxmock/v4` v4.9.0.
Upgrading to pgx v5.11.0 requires a mock implementation of `pgx.Rows.TypeMap`.

Run unit tests:

```bash
make tests-unit
```

From the repository root, run the Docker-first build and end-to-end pipeline
(CI will run the same script; the workflows are not written yet):

```bash
make build-enricher
```

Run formatting, linting, vulnerability analysis, and security scanning with
`make analyze`.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Each component has its own tag (`enricher/vX.Y.Z`); the version comes from the
latest one:

```bash
build/version.sh enricher
```

See [CHANGELOG.md](CHANGELOG.md) for release history.
