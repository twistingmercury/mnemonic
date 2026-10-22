# mnemonic-enricher build guide

This directory contains the Docker image build and end-to-end test workflow for
`mnemonic-enricher`. It does not contain a production deployment definition.
Image publication is not set up yet (see [CI publication](#ci-publication)), and
deployment is owned downstream.

Run the commands in this guide from the repository root.

## Build and test workflow

The canonical local workflow is:

```bash
make build-enricher
```

`make build-enricher` invokes `build/enricher/enricher.build.sh`. The script performs
these stages in order:

1. Builds the final image with no Docker layer cache and a freshly pulled base
   image (`--no-cache --pull`, always on). The Dockerfile runs
   `goimports`, `golangci-lint`, `govulncheck`, `gosec`, and the Go unit tests
   (with `-race`) before compiling the binary.
2. Tags the image as both
   `ghcr.io/twistingmercury/mnemonic-enricher:<version>` and
   `ghcr.io/twistingmercury/mnemonic-enricher:latest`. The version defaults to
   the most recent `enricher/vX.Y.Z` Git tag (`build/version.sh enricher`), or
   `0.0.0-dev` when there is none.
3. Starts the isolated stack in `tests/integration/docker-compose.yaml` and
   runs the whole end-to-end suite (see its
   [README](../../tests/integration/README.md)) with the enricher taken from the
   newly built image and never pulled.

The script never pushes an image; the CI `publish` job pushes. The stack needs Docker to pull the
pre-migrated PostgreSQL and Neo4j images, RabbitMQ
(`rabbitmq:3-management-alpine`) and the `latest-dev` api and mcp images from
GHCR, unless you set `MNEMONIC_API_IMAGE`, `MNEMONIC_MCP_IMAGE` or the matching
`MNEMONIC_<N>_PULL_POLICY` variables. It builds and uses a local OpenAI stub; a
live OpenAI key is not required for these tests.

> **Cleanup:** The script removes only its own `mnemonic-e2e-enricher-<pid>`
> compose project (containers, networks, volumes, orphans and compose-built
> images without a custom tag), runs no prune, and does so also when the tests
> fail or the run is interrupted.

To build only the image from the repository root, without running the end-to-end
stack or cleanup, use:

```bash
docker build --rm --no-cache --pull \
  --file build/enricher/enricher.Dockerfile \
  --build-arg BUILD_VER=dev \
  --build-arg BUILD_COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --target final \
  --tag ghcr.io/twistingmercury/mnemonic-enricher:latest \
  .
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

The CI workflow runs the build script as the gate, and its `publish` job
pushes the tested image only after the image build gates and end-to-end tests
pass. See [Continuous integration](../../README.md#continuous-integration).
Registry publication is distinct from deployment; this repository does not
define a production rollout.

## Full local stack

[`deploy/docker-compose.yaml`](../../deploy/docker-compose.yaml) runs the
enricher with the other services behind the gateway. Provide the following:

- Registry access to pull the `latest-dev` images on ghcr.io, or local images.
  The build script tags `latest`, not `latest-dev`, so point the stack at your
  build with `MNEMONIC_ENRICHER_IMAGE`:

  ```bash
  export MNEMONIC_ENRICHER_IMAGE=ghcr.io/twistingmercury/mnemonic-enricher:latest
  ```

- The OpenAI key in the host variable `MNEMONIC_OPENAI_API_EMBEDDINGS_KEY`,
  exported in your own shell. Only the `egress` service reads it; the enricher
  gets the placeholder `MNEMONIC_OPENAI_API_KEY=proxy-managed` and reaches
  OpenAI through that proxy. `.env.example` at the repository root leaves the
  key empty on purpose; do not put a real key in any file.

Then start and stop the stack from the repository root:

```bash
docker compose -f deploy/docker-compose.yaml up -d
docker compose -f deploy/docker-compose.yaml down
```

The enricher waits for healthy `postgres`, `neo4j` and `rabbitmq` services.
`down` keeps the named volumes; remove them with
`docker compose -f deploy/docker-compose.yaml down --volumes`, which also
deletes the local PostgreSQL and Neo4j development data.

## Services and endpoints

Only Envoy and Keycloak publish host ports:

| Service | Host endpoint | Purpose |
| --- | --- | --- |
| `envoy` | <http://localhost:8080> | Mnemonic REST API |
| `envoy` | <http://localhost:8081> | Mnemonic MCP endpoint |
| `keycloak` | <http://localhost:8180> | Keycloak (token issuer) |

`mnemonic-enricher` is a worker, not the Mnemonic API. Its health and version
server on port 8080 and metrics server on port 9090 are available only on the
Compose network. Probe the container with:

```bash
docker compose -f deploy/docker-compose.yaml exec mnemonic-enricher /mnemonic-enricher --health
docker compose -f deploy/docker-compose.yaml exec mnemonic-enricher /mnemonic-enricher --version
```

## Local dependency credentials

These credentials are for the local development Compose stack only. Never use
them in production.

| Dependency | Username | Password | Additional configuration |
| --- | --- | --- | --- |
| PostgreSQL | `mnemonic` | `mnemonic_dev` | Database `mnemonic` |
| Neo4j | `neo4j` | `mnemonic_dev` | Default database |
| RabbitMQ | `mnemonic` | `mnemonic_dev` | Queue `enrichment-jobs` |

RabbitMQ is required by `mnemonic-enricher`: the worker connects to the
`rabbitmq:5672` Compose address, consumes from `enrichment-jobs`, and does not
start until the RabbitMQ health check passes. RabbitMQ publishes no host port in
this stack.

## Troubleshooting

Inspect service and dependency state without rendering secrets:

```bash
docker compose -f deploy/docker-compose.yaml ps
docker compose -f deploy/docker-compose.yaml logs mnemonic-enricher
docker compose -f deploy/docker-compose.yaml logs postgres neo4j rabbitmq
docker compose -f deploy/docker-compose.yaml exec postgres pg_isready -U mnemonic
docker compose -f deploy/docker-compose.yaml exec neo4j cypher-shell \
  -u neo4j -p mnemonic_dev "RETURN 1"
docker compose -f deploy/docker-compose.yaml exec rabbitmq rabbitmq-diagnostics ping
docker compose -f deploy/docker-compose.yaml exec rabbitmq rabbitmqctl list_queues name messages consumers
```

For queue-specific startup or delivery problems, inspect
`docker compose -f deploy/docker-compose.yaml logs rabbitmq mnemonic-enricher`
and confirm that `rabbitmq` is healthy in `ps`. `rabbitmqctl list_queues`
shows whether the `enrichment-jobs` queue exists and has an active consumer.

Avoid sharing unrestricted `docker compose config` output: interpolation can
expose plaintext development credentials and, when it is set in your shell, the
OpenAI key. If rendered configuration is needed for diagnosis, limit it to the
relevant service and redact secrets before sharing it.
