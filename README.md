# Mnemonic

> **Maturity Level**: Emerging - the services work end to end, but the repository is still being consolidated into a monorepo and expects breaking changes.  
> **Version**: versioned per component (see [Versioning](#versioning))

Mnemonic is a shared knowledge base for a team that uses Claude Code. The team curates patterns (conventions, decisions, how things are done here), and Claude Code retrieves them over MCP when it needs context. It runs as a small set of services behind an authenticating gateway.

---

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
  - [Continuous integration](#continuous-integration)
- [Repository layout](#repository-layout)

## Usage

Installing, running, loading documents, connecting Claude Code and troubleshooting are covered in the [wiki](https://github.com/twistingmercury/mnemonic/wiki). The wiki is for people who run or use Mnemonic. This repository's `docs/` is for people who change it.

## How it works

Three services share one database layer: an admin REST **api** (the only write path), a read-only **mcp** server for Claude Code, and an **enricher** that adds embeddings and concepts in the background. Envoy, OPA and Keycloak in front of them handle authentication. Architecture, design decisions and diagrams are in the [design docs](docs/design/README.md).

## Key Considerations

- The code is under the [Functional Source License 1.1](LICENSE) (FSL-1.1-ALv2). It is source-available, not open source: you can use, modify and redistribute it for any purpose except a competing commercial product or service, and each version converts to Apache 2.0 two years after release.
- Authentication and authorization happen at the gateway; the services carry no auth code.
- Only the egress proxy holds the real OpenAI key; see the [gateway](gateway/README.md) for how that is enforced.
- Every environment variable the project defines starts with `MNEMONIC_`.

## Development Considerations

### Quick Start

Run `make help` to list the targets. Each service image is built by its own script, `build/<name>/<name>.build.sh` (`api`, `mcp`, `enricher`), which builds the image and runs the end-to-end tests in containers and never pushes. CI publishes the images; see [Continuous integration](#continuous-integration). Details are in the component READMEs below.

### Testing

Go unit tests run with `make tests-unit` (or `go -C src test ./...`). The BATS tests for the build tooling run with `bats tests/bats`. The database images and the gateway have their own test instructions in their READMEs.

### Continuous integration

One workflow, `.github/workflows/ci.yaml`, runs on pull requests to `develop` and `main`, on pushes to `develop` and `main`, and on a manual run. Its design is in [CI workflows](docs/design/ci_workflows.md).

| Job | What it runs |
| --- | --- |
| `detect` | `build/detect-components.sh` works out which components the change affects |
| `static` | `build/static-checks.sh`: shellcheck, hadolint, markdownlint, `helm lint --strict`, the BATS tests for the build tooling and the e2e boundary check. Always runs |
| `service` | One matrix entry per affected service: `build/<svc>/<svc>.build.sh` |
| `db` | The PostgreSQL and Neo4j test runners, then both image builds. Runs when `db/` changed |
| `gateway` | The OPA policy checks, the Envoy config validation and the stub-gateway e2e tests. Runs when `gateway/` changed |
| `helm` | `helm lint --strict` and `helm template` on the chart. Runs when `deploy/helm/` changed |
| `ci-gate` | Fails if any job above failed or was cancelled; skipped jobs are fine. This is the single required check |
| `publish` | Pushes the tested images to GHCR (push to `develop` or `main` only) |

A service counts as affected when its own code changed or a Go package it imports did. A change to `src/go.mod`, `src/go.sum`, `build/`, `.github/`, `tests/integration/` or `tests/openai-stub/` affects all three services.

Pull requests and manual runs build and test but never push. Only the `publish` job has `packages: write`; it uses `GITHUB_TOKEN`, and runs only when every job passed or was skipped and an image was built. It pushes the same image the build job tested, passed along as a workflow artifact that is kept for 1 day. Re-running only the `publish` job after more than a day fails because the artifact is gone; re-run the whole workflow instead.

| Branch | Tags pushed per affected image |
| --- | --- |
| `develop` | `latest-dev` and `sha-<short>`, never a version tag |
| `main` (dormant until the branch exists) | `latest` and `v<version>` from `build/version.sh` |

The `db` component publishes the `mnemonic-postgres` and `mnemonic-neo4j` images; `gateway` and `helm` publish nothing.

To run a component by hand, start the workflow from the Actions tab and pick the `component` input (`all`, `api`, `mcp`, `enricher`, `db`, `gateway` or `helm`). A specific component skips detection; `all` builds everything. It never publishes.

To run the same checks locally:

- `build/static-checks.sh` runs the static checks. It needs Docker Engine 28 or newer, because it uses `--mount type=image`.
- `build/<svc>/<svc>.build.sh` builds a service image and runs its e2e tests. It never pushes.
- `build/changed-components.sh <base-sha> <head-sha>` prints the components a change affects.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Components are released independently with prefixed tags (for example `api/v0.5.1`). A component with no tag yet reports `0.0.0-dev`. The version of a component is determined from its tags:

```bash
build/version.sh api
```

## Repository layout

| Path | What it is |
| --- | --- |
| [`src/`](src/) | One Go module with the `api`, `mcp` and `enricher` services (`cmd/` and `internal/`) |
| [`api/`](api/README.md), [`mcp/`](mcp/README.md), [`enricher/`](enricher/README.md) | Per-service README and changelog |
| [`build/`](build/) | Image build scripts and Dockerfiles, plus version tooling |
| [`db/`](db/README.md) | The PostgreSQL (pgvector) and Neo4j images and their tests |
| [`gateway/`](gateway/README.md) | Envoy, OPA and Keycloak configuration and the gateway tests |
| [`deploy/`](deploy/) | The local Docker Compose stack and the Helm chart |
| [`tests/`](tests/integration/README.md) | End-to-end tests, the OpenAI stub and the BATS tests |
| [`docs/design/`](docs/design/README.md) | Architecture and design documents |
