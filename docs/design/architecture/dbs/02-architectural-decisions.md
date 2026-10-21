---
version: 2
date: 2026-10-21
notes: "ADR-003 corrected: Neo4j schema is applied by a startup entrypoint, not docker commit; ADR-004 updated. License footer corrected."
---

# Database Images — Architectural Decisions

> Back to [Database Architecture](README.md) | [Design Index](../../README.md)

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

| ADR #   | Title                                                                 | Status     | Date       |
| ------- | --------------------------------------------------------------------- | ---------- | ---------- |
| ADR-001 | Pre-seeded images over runtime migration tooling                      | Accepted   | 2026-03-19 |
| ADR-002 | Separate repo from application code                                   | Superseded | 2026-03-19 |
| ADR-003 | docker-entrypoint-initdb.d for Postgres; startup entrypoint for Neo4j | Accepted   | 2026-03-19 |
| ADR-004 | Neo4j Community Edition only                                          | Accepted   | 2026-03-19 |

## Decisions

### ADR-001: Pre-seeded images over runtime migration tooling

**Status:** Accepted

**Context:**

Application services previously depended on a `migrate/migrate` sidecar container to apply SQL migrations at startup. This introduced a startup dependency chain (postgres healthy → migrate completes → app starts), added latency to E2E test runs, and occasionally caused CI failures when the migrate container failed on an empty directory or port conflict.

**Decision:**

Build database images with the schema already applied. The Postgres image uses `docker-entrypoint-initdb.d` to run SQL files on first container start. The Neo4j image ships its Cypher files and a wrapper entrypoint that applies them at every container start (see ADR-003). Application services simply reference these images — no migration step at runtime.

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

**Status:** Superseded (monorepo, 2026-10-21): the component now lives in `db/` in the Mnemonic monorepo. The text below is the original decision, kept as history.

**Context:**

The database schema was originally embedded inside the `mnemonic` application repo under `src/migrations/`. With the api service splitting out as a separate service, both application repos would need to share or duplicate migration files. Keeping schema in an application repo also couples schema release cadence to application release cadence.

**Decision:**

Extract schema into its own repo (`db/`). All three application repos (the mcp service, the api service, the enricher service) pull the pre-built images from GHCR. Schema changes have their own PR and review, and are tested by the BATS suites in `db/tests/`.

**Consequences:**

_Positive:_

- Schema changes are reviewed and deployed independently of application changes
- No duplication of SQL/Cypher files across application repos
- Clear ownership: `db/` is the single source of truth for database schema

_Negative:_

- Schema changes require a two-step deploy: update `db/` first, then update image references in application repos
- Adds a dependency between repos that must be coordinated

---

### ADR-003: docker-entrypoint-initdb.d for Postgres; startup entrypoint for Neo4j

**Status:** Accepted

**Context:**

Both databases need their schema in place before the services use them, but each supports different initialization mechanisms.

**Decision:**

- **Postgres**: Copy all `*.up.sql` files into `/docker-entrypoint-initdb.d/`. The `pgvector/pgvector:pg16` base image runs these scripts automatically on first container start. File naming (`000001_…` through `000010_…`) ensures correct execution order.
- **Neo4j**: No equivalent init-directory mechanism exists in the Community Edition image, and the schema cannot be baked in at build time: `/data` is a `VOLUME` in the base image and `docker commit` drops volume contents, so the original plan (seed a temporary container, then `docker commit`) does not work. Instead the image copies the Cypher files to `/startup/schema/` and replaces the entrypoint with `schema-entrypoint.sh`. That script starts Neo4j in the foreground and, in the background, waits for it to accept connections, applies the idempotent Cypher files with `cypher-shell` and writes `/tmp/schema_ready`, which the image's `HEALTHCHECK` tests.

**Consequences:**

_Positive:_

- Each approach uses the most idiomatic mechanism for its database
- Both produce self-initializing images with no external tooling at runtime

_Negative:_

- The Neo4j schema is applied on every container start, so a Neo4j container is not ready until `/tmp/schema_ready` exists
- A failed schema job stops Neo4j, so a container never runs without its schema
- Every Cypher file must stay idempotent

---

### ADR-004: Neo4j Community Edition only

**Status:** Accepted

**Context:**

Neo4j offers property existence constraints (`IS NOT NULL`) only in Enterprise Edition. The schema file `002_create_existence_constraints.cypher` was written for Enterprise Edition and cannot run against Community Edition images (`neo4j:5`).

**Decision:**

Target Community Edition only. Skip `002_create_existence_constraints.cypher` at container start unless the edition is Enterprise (`schema-entrypoint.sh` checks `NEO4J_EDITION`). Existence constraints are enforced at the application layer instead. The file is kept in the repo for reference and future Enterprise Edition support.

**Consequences:**

_Positive:_

- No licensing cost or complexity; freely distributable images
- Consistent with the MVP stance of a trusted, single-team deployment

_Negative:_

- Property existence is not enforced at the database level for Neo4j nodes
- Enterprise Edition support needs no build change: the same entrypoint applies migration 002 when it runs on an Enterprise base image

**Next:** [Deployment Architecture](05-deployment-architecture.md)

---

Copyright (c) 2025-2026 Jeremy K. Johnson. Licensed under FSL-1.1-ALv2; see [LICENSE](../../../../LICENSE).
