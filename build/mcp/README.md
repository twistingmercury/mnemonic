# Build internals

This directory contains the Docker image definition and the script that builds
and validates the Mnemonic MCP service:

- `mcp.Dockerfile` runs quality gates and tests, builds the Go binary, and creates
  the minimal runtime image.
- `mcp.build.sh` supplies build metadata, tags the image, runs the end-to-end suite,
  and cleans up its test environment.

For local stack setup and service topology, use the [root README](../../README.md) and the [deploy Compose file](../../deploy/docker-compose.yaml).

## Prerequisites

Run builds from a Git checkout with:

- Docker and Docker Compose available to the current user
- GNU Make
- The `latest-dev` api and enricher images
  (`ghcr.io/twistingmercury/mnemonic-api` and `mnemonic-enricher`) pullable
  from GHCR for the end-to-end suite, or your own through
  `MNEMONIC_API_IMAGE` and `MNEMONIC_ENRICHER_IMAGE`

## Run the build

The canonical local entry point is the root Makefile:

```bash
make build-mcp
```

Run this command from the repository root. It invokes
`build/mcp/mcp.build.sh`.

## Pipeline

The build runs in this order:

1. Docker builds the `mcp.Dockerfile` `build` stage with the project source.
2. The build stage runs `goimports`, `golangci-lint`, `govulncheck`, `gosec`,
   and the Go tests for `cmd/mcp` and `internal` (run with `-race`).
3. The build stage compiles the statically linked `/mnemonic` binary with
   version, build date, and commit metadata.
4. The `final` scratch stage copies the binary and CA certificates into the
   runtime image.
5. `mcp.build.sh` applies the configured version tag and the `latest` tag.
6. Docker Compose starts PostgreSQL, Neo4j, RabbitMQ and the OpenAI stub, then
   the `mnemonic-api`, `mnemonic-mcp`, `mnemonic-enricher` and `mnemonic-tests`
   services of `tests/integration/docker-compose.yaml`. The MCP service uses the
   image just built and is never pulled; the other two are pulled. See the
   [suite README](../../tests/integration/README.md).
7. The test runner's exit code determines success. The project-scoped teardown
   (`mnemonic-e2e-mcp-<pid>`) then removes the E2E stack and its volumes, also
   on failure or interrupt.

The Docker build always uses `--no-cache` and `--pull`, so every run executes all
quality gates and tests and starts from a fresh base image.

## Build inputs

`mcp.build.sh` accepts these environment variables:

| Variable                    | Default                                                          | Purpose                          |
| --------------------------- | ---------------------------------------------------------------- | -------------------------------- |
| `MNEMONIC_BUILD_VER`        | Latest `mcp/vX.Y.Z` tag (`build/version.sh mcp`), or `0.0.0-dev` | Binary and image version         |
| `MNEMONIC_BUILD_DATE`       | Current UTC time in RFC 3339 format                              | Image and binary build timestamp |
| `MNEMONIC_BUILD_COMMIT`     | Current short Git commit, or `unknown`                           | Image and binary source revision |
| `MNEMONIC_IMAGE_NAME`       | `ghcr.io/twistingmercury/mnemonic-mcp`                           | Image repository                 |
| `MNEMONIC_IMAGE_TAG`        | `v` followed by `MNEMONIC_BUILD_VER`                             | Primary local image tag          |
| `MNEMONIC_E2E_COMPOSE_FILE` | `tests/integration/docker-compose.yaml`                          | Compose file for the E2E suite   |

For example, to build with explicit metadata while retaining the canonical
Make target:

```bash
MNEMONIC_BUILD_VER=0.3.1 MNEMONIC_BUILD_COMMIT=abc12345 make build-mcp
```

## Output and publishing

Each successful script run produces both:

- `${MNEMONIC_IMAGE_NAME}:${MNEMONIC_IMAGE_TAG}`
- `${MNEMONIC_IMAGE_NAME}:latest`

The script never pushes images. Local builds stop after image creation and E2E
validation. The CI workflow runs `build/mcp/mcp.build.sh` directly and its
`publish` job pushes the tested image; see
[Continuous integration](../../README.md#continuous-integration).

The script removes only its own `mnemonic-e2e-mcp-<pid>` compose project
(containers, networks, volumes, orphans and compose-built images without a custom
tag), runs no prune, and does so also when the tests fail or the run is
interrupted.

## Validation and troubleshooting

Validate the entry point and supporting files without running the build:

```bash
make -n build-mcp
bash -n build/mcp/mcp.build.sh
docker compose -f tests/integration/docker-compose.yaml config --quiet
```

The companion api and enricher images are pulled on every run (`pull_policy`
defaults to `always`). To use local copies instead, set `MNEMONIC_API_IMAGE` and
`MNEMONIC_API_PULL_POLICY=never` (and the same for `ENRICHER`).

After a successful run, inspect the generated tags with:

```bash
docker image ls ghcr.io/twistingmercury/mnemonic-mcp
```

For failures:

- A Docker build failure identifies the failed quality gate, test, or compile
  instruction in its layer output.
- An infrastructure startup failure is reported before application containers
  run; verify that Docker can obtain the PostgreSQL, Neo4j, and RabbitMQ images.
- An E2E failure is the exit status from the `mnemonic-tests` service; review the streamed
  Compose output because the cleanup trap removes the test stack on exit.

See the [deployment architecture](../../docs/design/architecture/system/06-deployment-architecture.md)
for system-level deployment decisions.
