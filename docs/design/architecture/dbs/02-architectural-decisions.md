# mnemonic-dbs — Architectural Decisions

> Back to [Database Architecture](README.md) | [Project README](../../../README.md)

## Table of Contents

- [Decision Record Format](#decision-record-format)
- [Decision Summary](#decision-summary)
- [Decisions](#decisions)

## Decision Record Format

Each architectural decision is recorded as an ADR with the following structure:

- **Title**: Short descriptive name for the decision
- **Status**: Proposed | Accepted | Deprecated | Superseded by ADR-NNN
- **Context**: The situation, forces at play, and why a decision is needed
- **Decision**: What was decided and the rationale
- **Consequences**: Both positive outcomes and trade-offs accepted

## Decision Summary

| ADR #   | Title                                                            | Status   | Date       |
| ------- | ---------------------------------------------------------------- | -------- | ---------- |
| ADR-001 | Pre-seeded images over runtime migration tooling                 | Accepted | 2026-03-19 |
| ADR-002 | Separate repo from application code                              | Accepted | 2026-03-19 |
| ADR-003 | docker-entrypoint-initdb.d for Postgres; docker commit for Neo4j | Accepted | 2026-03-19 |
| ADR-004 | Neo4j Community Edition only                                     | Accepted | 2026-03-19 |

## Decisions

### ADR-001: Pre-seeded images over runtime migration tooling

**Status:** Accepted

**Context:**

Application services previously depended on a `migrate/migrate` sidecar container to apply SQL migrations at startup. This introduced a startup dependency chain (postgres healthy → migrate completes → app starts), added latency to E2E test runs, and occasionally caused CI failures when the migrate container failed on an empty directory or port conflict.

**Decision:**

Build database images with the schema already applied. The Postgres image uses `docker-entrypoint-initdb.d` to run SQL files on first container start. The Neo4j image is committed after Cypher migrations are applied in a temporary container. Application services simply reference these images — no migration step at runtime.

**Consequences:**

_Positive:_

- Application startup is simpler and faster — no migration sidecar needed
- E2E test stack comes up reliably without a migration dependency
- Schema changes are tested and versioned independently of application releases

_Negative:_

- Schema changes require rebuilding and pushing new image tags before applications can consume them
- A rollback means pulling a prior image tag rather than running a down migration

---

### ADR-002: Separate repo from application code

**Status:** Accepted

**Context:**

The database schema was originally embedded inside the `mnemonic` application repo under `src/migrations/`. With `mnemonic-api` splitting out as a separate service, both application repos would need to share or duplicate migration files. Keeping schema in an application repo also couples schema release cadence to application release cadence.

**Decision:**

Extract schema into its own repo (`mnemonic-dbs`). All three application repos (`mnemonic-mcp`, `mnemonic-api`, `mnemonic-enricher`) pull the pre-built images from GHCR. Schema changes have their own PR, review, and CI pipeline.

**Consequences:**

_Positive:_

- Schema changes are reviewed and deployed independently of application changes
- No duplication of SQL/Cypher files across application repos
- Clear ownership: `mnemonic-dbs` is the single source of truth for database schema

_Negative:_

- Schema changes require a two-step deploy: update `mnemonic-dbs` first, then update image references in application repos
- Adds a dependency between repos that must be coordinated

---

### ADR-003: docker-entrypoint-initdb.d for Postgres; docker commit for Neo4j

**Status:** Accepted

**Context:**

Both databases need their schema applied at image build time, but each supports different initialization mechanisms.

**Decision:**

- **Postgres**: Copy all `*.up.sql` files into `/docker-entrypoint-initdb.d/`. The `pgvector/pgvector:pg16` base image runs these scripts automatically on first container start. File naming (`000001_…` through `000006_…`) ensures correct execution order.
- **Neo4j**: No equivalent init-directory mechanism exists in the Community Edition image. The build script starts a temporary container, applies Cypher files via `cypher-shell`, stops the container, and commits it as the final image using `docker commit`.

**Consequences:**

_Positive:_

- Each approach uses the most idiomatic mechanism for its database
- Both produce self-initializing images with no external tooling at runtime

_Negative:_

- Neo4j build is slower and more complex — it requires a running container and network connectivity during the build
- `docker commit` captures the full container filesystem, including any neo4j runtime state, which increases image size slightly

---

### ADR-004: Neo4j Community Edition only

**Status:** Accepted

**Context:**

Neo4j offers property existence constraints (`IS NOT NULL`) only in Enterprise Edition. The schema file `002_create_existence_constraints.cypher` was written for Enterprise Edition and cannot run against Community Edition images (`neo4j:5`).

**Decision:**

Target Community Edition only. Skip `002_create_existence_constraints.cypher` in both the build script and CI. Existence constraints are enforced at the application layer instead. The file is kept in the repo for reference and future Enterprise Edition support.

**Consequences:**

_Positive:_

- No licensing cost or complexity; freely distributable images
- Consistent with the MVP stance of a trusted, single-team deployment

_Negative:_

- Property existence is not enforced at the database level for Neo4j nodes
- Switching to Enterprise Edition later requires re-running the build with migration 002 included

**Next:** [Deployment Architecture](05-deployment-architecture.md)

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
