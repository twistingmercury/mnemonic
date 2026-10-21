# Build internals

This directory contains the Docker image definition and the script that builds
and validates the Mnemonic MCP service:

- `Dockerfile` runs quality gates and tests, builds the Go binary, and creates
  the minimal runtime image.
- `build.sh` supplies build metadata, tags the image, runs the end-to-end suite,
  and cleans up its test environment.

For local stack setup and service topology, use the [root README](../../README.md).

## Prerequisites

Run builds from a Git checkout with:

- Docker and Docker Compose available to the current user
- GNU Make
- `ghcr.io/twistingmercury/librarian-api:latest-dev` available locally or
  pullable from GHCR for the end-to-end suite

## Run the build

The canonical local entry point is the root Makefile:

```bash
make build
```

Run this command from the repository root. It sets `LOCAL=1` and invokes
`src/build/build.sh`.

## Pipeline

The build runs in this order:

1. Docker builds the `Dockerfile` `build` stage with the project source.
2. The build stage runs `goimports`, `golangci-lint`, `govulncheck`, `gosec`,
   and Go tests for `cmd/main` and `internal`.
3. The build stage compiles the statically linked `/mnemonic` binary with
   version, build date, and commit metadata.
4. The `final` scratch stage copies the binary and CA certificates into the
   runtime image.
5. `build.sh` applies the configured version tag and the `latest` tag.
6. Docker Compose starts PostgreSQL, Neo4j, and RabbitMQ, then runs
   `librarian_mcp`, `librarian_api`, and the `librarian_tests` container.
7. The test runner's exit code determines success, after which the E2E stack
   and its volumes are removed.

The Docker build uses `--no-cache`, so every run executes all quality gates and
tests.

## Build inputs

`build.sh` accepts these environment variables:

| Variable       | Default                                      | Purpose                              |
| -------------- | -------------------------------------------- | ------------------------------------ |
| `BUILD_VER`    | Latest Git tag, or `dev`                     | Binary and image version             |
| `BUILD_DATE`   | Current UTC time in RFC 3339 format          | Image and binary build timestamp     |
| `BUILD_COMMIT` | Current short Git commit, or `unknown`       | Image and binary source revision     |
| `IMAGE_NAME`   | `ghcr.io/twistingmercury/librarian-mcp`           | Image repository                     |
| `IMAGE_TAG`    | Value of `BUILD_VER`                         | Primary local image tag              |
| `LOCAL`        | `0`; root `make build` overrides this to `1` | Enables additional local cleanup     |

For example, to build with explicit metadata while retaining the canonical
Make target:

```bash
BUILD_VER=v0.3.1 BUILD_COMMIT=abc12345 make build
```

## Output and publishing

Each successful script run produces both:

- `${IMAGE_NAME}:${IMAGE_TAG}`
- `${IMAGE_NAME}:latest`

The script never pushes images. Local builds stop after image creation and E2E
validation. CI invokes `src/build/build.sh` directly with the default
`LOCAL=0`, authenticates to GHCR after validation, and pushes tags in the
workflow:

- `main`: `latest` and the Git-derived version
- Other refs: `latest-dev` and the Git-derived version with a `-dev` suffix

> **Warning:** The root `make build` sets `LOCAL=1`. Its cleanup currently
> removes the E2E test-runner image and runs `docker system prune -f`, which can
> remove other unused Docker objects on the host.

## Validation and troubleshooting

Validate the entry point and supporting files without running the build:

```bash
make -n build
bash -n src/build/build.sh
docker compose -f src/tests/docker-compose.yaml config --quiet
```

To check for a local copy of the companion API image, run:

```bash
docker image inspect ghcr.io/twistingmercury/librarian-api:latest-dev
```

If it is absent locally, Docker Compose pulls it from GHCR when accessible.

After a successful run, inspect the generated tags with:

```bash
docker image ls ghcr.io/twistingmercury/librarian-mcp
```

For failures:

- A Docker build failure identifies the failed quality gate, test, or compile
  instruction in its layer output.
- An infrastructure startup failure is reported before application containers
  run; verify that Docker can obtain the PostgreSQL, Neo4j, and RabbitMQ images.
- An E2E failure is the exit status from `librarian_tests`; review the streamed
  Compose output because the cleanup trap removes the test stack on exit.

See the [deployment architecture](https://github.com/twistingmercury/librarian-docs/blob/develop/docs/architecture/system/06-deployment-architecture.md)
for system-level deployment decisions.
