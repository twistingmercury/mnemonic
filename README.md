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

Run `make help` to list the targets. Each service image is built by its own script, `build/<name>/<name>.build.sh` (`api`, `mcp`, `enricher`), which builds the image and runs the end-to-end tests in containers and never pushes. Publishing is planned for CI, whose workflows are not written yet ([design](docs/design/ci_workflows.md)). Details are in the component READMEs below.

### Testing

Go unit tests run with `make tests-unit` (or `go -C src test ./...`). The BATS tests for the build tooling run with `bats tests/bats`. The database images and the gateway have their own test instructions in their READMEs.

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
