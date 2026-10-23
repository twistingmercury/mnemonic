# mnemonic-dbs

> **Maturity Level**: Emerging - Pre-seeded database images and schema migrations for the Mnemonic project
> **Version**: v1.0.0
>
> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

---

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)

## Usage

Pull the pre-seeded images for use in local development or testing:

```bash
docker pull ghcr.io/twistingmercury/mnemonic-postgres:latest-dev
docker pull ghcr.io/twistingmercury/mnemonic-neo4j:latest-dev
```

Both images are self-initializing — the schema is applied by the container itself on every start (idempotent). No migration tool required.

## How it works

**PostgreSQL image** (`ghcr.io/twistingmercury/mnemonic-postgres`): built `FROM pgvector/pgvector:pg16`. All `.up.sql` migration files are copied into `/docker-entrypoint-initdb.d/` and executed automatically in order on first start.

**Neo4j image** (`ghcr.io/twistingmercury/mnemonic-neo4j`): built `FROM neo4j:5` with APOC enabled. The Cypher constraint and index files ship inside the image, and a wrapper entrypoint applies them with `cypher-shell` every time the container starts, then writes `/tmp/schema_ready`. The schema cannot be baked into the image because `/data` is a Docker VOLUME. Health checks should test for `/tmp/schema_ready` (the image's built-in `HEALTHCHECK` does).

**CI**: on a change under `db/`, the `db` job runs the BATS suites for both databases and builds both images. A push to `develop` then publishes each image as `latest-dev` and `sha-<short>` (the first 7 characters of the commit SHA). On `main` (dormant until the branch exists) it would publish `latest` and `v<version>` from `build/version.sh db`. Nothing is pushed from a local build. See [Continuous integration](../README.md#continuous-integration).

## Key Considerations

- `002_create_existence_constraints.cypher` is skipped at startup on Community Edition — existence constraints require Neo4j Enterprise Edition
- `.down.sql` files are not included; rollback is handled by rebuilding from a prior image tag
- The build scripts tag each image with the version and `latest`; the CI tags are described under **CI** above

## Development Considerations

### Quick Start

Requires Docker Engine 28+ (the test runners use `--mount type=image`) and Docker Compose 2.32+. The test runners run `bats` and `psql` from pinned images (`db/tests/run-bats.sh`), so neither needs installing.

Build the images locally from the repository root (each script tags the image with the version and `latest`):

```bash
MNEMONIC_VERSION=v1.0.0 bash db/postgres/build.sh
MNEMONIC_VERSION=v1.0.0 bash db/neo4j/build.sh
```

Without `MNEMONIC_VERSION` the scripts tag the image `v<version>`, where `<version>` is what `build/version.sh db` prints (`v1.0.0` with the tag `db/v1.0.0`, `v0.0.0-dev` when no `db/v*` tag exists). These scripts do not push, and like the service build scripts they pass `--no-cache --pull`.

### Testing

BATS tests verify the schema state after all migrations have been applied.

```bash
bash db/tests/postgres-test-runner.sh   # PostgreSQL migrations
bash db/tests/neo4j-test-runner.sh      # Neo4j migrations and the Neo4j image
bats db/tests/bats/schema-entrypoint.bats db/tests/bats/build-scripts.bats   # scripts, no Docker stack needed
```

The runners start `db/docker-compose.yaml` on isolated host ports (Postgres `5435`, Neo4j http `7477` and bolt `7690`) so they do not conflict with other running stacks. Container ports are unchanged.

Environment variables (all optional):

| Variable | Default | Used by |
| --- | --- | --- |
| `MNEMONIC_VERSION` | `v` plus the output of `build/version.sh db` | `build.sh` scripts: image tag |
| `MNEMONIC_MAX_RETRIES` | `30` | test runners: readiness attempts |
| `MNEMONIC_RETRY_INTERVAL` | `1` | test runners: seconds between attempts |
| `MNEMONIC_NEO4J_USER` / `MNEMONIC_NEO4J_PASSWORD` | `neo4j` / `mnemonic_dev` | Neo4j test runner and BATS |
| `MNEMONIC_NEO4J_IMAGE` | built by the runner | Neo4j test runner: test an existing image instead |
| `MNEMONIC_READY_TIMEOUT` / `MNEMONIC_STOP_TIMEOUT` | `150` / `30` | `neo4j-image.bats`: seconds |
| `MNEMONIC_SCHEMA_DIR` | `/startup/schema` | in-image entrypoint |
| `MNEMONIC_SCHEMA_READY_FILE` | `/tmp/schema_ready` | in-image entrypoint |
| `MNEMONIC_SCHEMA_MAX_RETRIES` / `MNEMONIC_SCHEMA_RETRY_SLEEP` | `60` / `2` | in-image entrypoint |
| `MNEMONIC_BASE_ENTRYPOINT` | `/startup/docker-entrypoint.sh` | in-image entrypoint |

Variables defined by the third-party tools keep their names: `NEO4J_AUTH`, `POSTGRES_*`, and the libpq `PG*` variables.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

The `build.sh` scripts take the image version from `MNEMONIC_VERSION`, or else from `build/version.sh db`, which reads the newest `db/v*` tag and the scripts prefix with `v` (`db/v1.0.0` gives the image tag `v1.0.0`). Tags of other components never affect it.
