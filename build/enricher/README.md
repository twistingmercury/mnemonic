# mnemonic-enricher build guide

This directory contains the Docker image build and end-to-end test workflow for
`mnemonic-enricher`. It does not contain a production deployment definition. Image
publication is handled by GitHub Actions, and deployment is owned downstream.

Run the commands in this guide from the repository root.

## Build and test workflow

The canonical local workflow is:

```bash
make build
```

`make build` sets `LOCAL=1` and invokes `src/build/build.sh`. The script performs
these stages in order:

1. Builds the final image with no Docker layer cache. The Dockerfile runs
   `goimports`, `golangci-lint`, `govulncheck`, `gosec`, and Go unit tests before
   compiling the binary.
2. Tags the image as both
   `ghcr.io/twistingmercury/mnemonic-enricher:<version>` and
   `ghcr.io/twistingmercury/mnemonic-enricher:latest`. The version defaults to
   the most recent Git tag, or `dev` when no tag is available.
3. Starts the isolated stack in `src/tests/docker-compose.yaml` and runs the
   end-to-end tests against the newly built `latest` image.

The end-to-end stack requires Docker to pull these dependency images:

- `ghcr.io/twistingmercury/librarian-postgres:v1.0.0-dev`
- `ghcr.io/twistingmercury/librarian-neo4j:v1.0.0-dev`
- `rabbitmq:4.0-management`

It builds and uses a local OpenAI stub; a live OpenAI key is not required for these
tests.

> **Local cleanup:** Because `make build` sets `LOCAL=1`, the script removes the
> end-to-end containers and volumes, deletes the test-runner image, and runs
> `docker system prune -f` when it exits. Docker system prune removes stopped
> containers, unused networks, dangling images, and unused build cache across the
> local Docker installation. Run `LOCAL=0 src/build/build.sh` if that broader local
> cleanup is not wanted.

To build only the image from the repository root, without running the end-to-end
stack or cleanup, use:

```bash
docker build --rm --no-cache \
  --file src/build/Dockerfile \
  --build-arg BUILD_VER=dev \
  --build-arg BUILD_COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --target final \
  --tag ghcr.io/twistingmercury/mnemonic-enricher:latest \
  src
```

No standalone `docker run` example is provided because the worker requires a
compatible Mnemonic PostgreSQL schema, Neo4j, RabbitMQ, and network-accessible
dependency configuration.

## Image characteristics

The Dockerfile produces a statically compiled Linux/amd64 binary with CGO disabled.
The final image is based on `scratch` and contains only the binary and CA
certificates. It has no shell, package manager, or in-container debugging tools.
Use Docker metadata, logs, health checks, or a separate diagnostic container when
investigating runtime issues.

## CI publication

The GitHub Actions workflow runs `src/build/build.sh`, so publication occurs only
after the image build gates and end-to-end tests pass. It publishes these tags:

| Git ref | Published tags |
| --- | --- |
| `main` | `latest`, `<version>` |
| Other configured branches and pull-request refs | `latest-dev`, `<version>-dev` |

The workflow currently runs for pushes and pull requests targeting `main` or
`develop`, plus manual dispatches. Registry publication is distinct from deployment;
this repository does not define a production rollout.

## Broader Mnemonic Compose stack

The root `docker-compose.yaml` is an integration contract for development with the
broader Mnemonic stack. It is not a standalone quick start from this repository.
Before using it, provide all of the following:

- PostgreSQL migration files at `src/migrations/postgres`, supplied by the
  companion Mnemonic API project.
- Registry access to pull `ghcr.io/twistingmercury/mnemonic-mcp:latest-dev` for
  `dev_api`.
- A local
  `ghcr.io/twistingmercury/mnemonic-enricher:latest-dev` image for
  `dev_enricher`. The local build script creates `latest`, not `latest-dev`, so tag
  it explicitly when appropriate:

  ```bash
  docker tag \
    ghcr.io/twistingmercury/mnemonic-enricher:latest \
    ghcr.io/twistingmercury/mnemonic-enricher:latest-dev
  ```

- A repository-root `.env` file containing a valid `MNEMONIC_OPENAI_API_KEY`.
  Start with `cp src/.env.example .env`, then edit the copy. Do not commit `.env`.

After supplying those external assets, validate the service names and start the
stack:

```bash
docker compose config --services
docker compose up -d
docker compose ps
```

The Compose services are `dev_postgres`, `migrate`, `dev_neo4j`, `dev_rabbitmq`,
`dev_api`, and `dev_enricher`. The API and enricher wait for their declared healthy
database dependencies; the enricher also waits for `dev_rabbitmq` to become healthy.

Stop the stack with `docker compose down`. Adding `--volumes` also deletes the local
PostgreSQL and Neo4j development data:

```bash
docker compose down --volumes
```

## Services and endpoints

The host-published endpoints in the root Compose stack are:

| Service | Host endpoint | Purpose |
| --- | --- | --- |
| `dev_api` | <http://localhost:18080> | Mnemonic REST API |
| `dev_api` | <http://localhost:18081> | Mnemonic MCP endpoint |
| `dev_api` | <http://localhost:19090> | Mnemonic API metrics |
| `dev_postgres` | `postgresql://localhost:15433` | PostgreSQL |
| `dev_neo4j` | <http://localhost:17475> | Neo4j Browser |
| `dev_neo4j` | `bolt://localhost:17688` | Neo4j Bolt protocol |
| `dev_rabbitmq` | `amqp://localhost:15672` | RabbitMQ AMQP |
| `dev_rabbitmq` | <http://localhost:25672> | RabbitMQ management UI |

`dev_enricher` is a worker, not the Mnemonic API. Its health and version server on
port 8080 and metrics server on port 9090 are available only on the Compose network;
the root Compose file does not publish them to the host. Probe the container with:

```bash
docker compose exec dev_enricher /mnemonic-enricher --health
docker compose exec dev_enricher /mnemonic-enricher --version
```

## Local dependency credentials

These credentials are for the root development Compose stack only. Never use them
in production.

| Dependency | Username | Password | Additional configuration |
| --- | --- | --- | --- |
| PostgreSQL | `mnemonic` | `mnemonic_dev` | Database `mnemonic` |
| Neo4j | `neo4j` | `mnemonic_dev` | Default database |
| RabbitMQ | `guest` | `guest` | Queue `enrichment-jobs` |

RabbitMQ is required by `dev_enricher`: the worker connects to the
`dev_rabbitmq:5672` Compose address, consumes from `enrichment-jobs`, and does not
start until the RabbitMQ health check passes. From the host, use
`localhost:15672` for AMQP clients and <http://localhost:25672> for the management
UI.

## Troubleshooting

Inspect service and dependency state without rendering secrets:

```bash
docker compose config --services
docker compose ps
docker compose logs dev_enricher
docker compose logs dev_postgres dev_neo4j dev_rabbitmq
docker compose exec dev_postgres pg_isready -U mnemonic
docker compose exec dev_neo4j cypher-shell \
  -u neo4j -p mnemonic_dev "RETURN 1"
docker compose exec dev_rabbitmq rabbitmq-diagnostics ping
docker compose exec dev_rabbitmq rabbitmqctl list_queues name messages consumers
```

For queue-specific startup or delivery problems, inspect
`docker compose logs dev_rabbitmq dev_enricher` and confirm that `dev_rabbitmq` is
healthy in `docker compose ps`. The management UI can also confirm whether the
`enrichment-jobs` queue exists and has an active consumer.

Avoid sharing unrestricted `docker compose config` output: interpolation can expose
the OpenAI key and plaintext development credentials. If rendered configuration is
needed for diagnosis, limit it to the relevant service and redact secrets before
sharing it.
