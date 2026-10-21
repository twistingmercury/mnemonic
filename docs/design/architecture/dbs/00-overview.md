---
version: 1
date: 2026-10-21
notes: "Imported from librarian-docs; repo references rewritten to the db/ component and monorepo service names; back-link repointed."
---

# Database Images — Architecture Overview

> Back to [Database Architecture](README.md) | [Design Index](../../README.md)

## Table of Contents

- [Introduction](#introduction)
- [Core Concept](#core-concept)
- [System Model](#system-model)
- [Key Principles](#key-principles)
- [Document Navigation](#document-navigation)

## Introduction

The `db/` component owns the database schema and pre-seeded Docker images for the Mnemonic project. It produces two images — `librarian-postgres` and `librarian-neo4j` — that application services pull directly instead of running migrations at startup.

The component is intentionally decoupled from the application code, even though it sits in the same monorepo. Schema changes are made in `db/`, tested, built into new image tags, and consumed by the mcp, api and enricher services by updating their image references. Today the compose files and the Helm chart pin `ghcr.io/twistingmercury/librarian-postgres:v1.0.0-dev` and `ghcr.io/twistingmercury/librarian-neo4j:v1.0.0-dev`. This gives schema changes their own lifecycle: reviewed, tested, and versioned independently.

## Core Concept

```mermaid
graph LR
    subgraph db["db/ (this component)"]
        PG_SQL["db/postgres/*.up.sql"]
        NEO_CYP["db/neo4j/*.cypher"]
        PG_BUILD["db/postgres/build.sh"]
        NEO_BUILD["db/neo4j/build.sh"]
        PG_SQL --> PG_BUILD
        NEO_CYP --> NEO_BUILD
    end

    subgraph GHCR["GitHub Container Registry"]
        PG_IMG["librarian-postgres:v1.0.0-dev"]
        NEO_IMG["librarian-neo4j:v1.0.0-dev"]
        PG_BUILD --> PG_IMG
        NEO_BUILD --> NEO_IMG
    end

    subgraph Apps["Application Services"]
        API["api service"]
        MCP["mcp service"]
        ENR["enricher service"]
        PG_IMG --> API
        PG_IMG --> MCP
        PG_IMG --> ENR
        NEO_IMG --> API
        NEO_IMG --> MCP
        NEO_IMG --> ENR
    end
```

## System Model

| Component                 | Description                                                                        |
| ------------------------- | ---------------------------------------------------------------------------------- |
| `db/postgres/`            | SQL schema files (`*.up.sql`) and Dockerfile for the Postgres image                |
| `db/neo4j/`               | Cypher schema files and Dockerfile/build script for the Neo4j image                |
| `db/tests/`               | BATS test suites and test runner scripts that verify schema correctness            |
| `src/docker-compose.yaml` | Local test infrastructure (isolated ports, not used by application services)       |
| CI workflow               | Triggers on schema file changes, runs tests, builds and pushes both images to GHCR |

## Key Principles

1. **Schema owns its own lifecycle** — changes to the database schema ship as new image tags, reviewed and tested independently of application code.
2. **Self-initializing images** — containers apply the schema on first start with no external tooling required at runtime.
3. **No runtime migration tooling** — application services do not run migrations at startup; they pull images that are already initialized.
4. **Tested before shipped** — BATS tests verify the schema state after initialization runs; CI gates image pushes on passing tests.
5. **Community Edition only** — both images target freely available database editions (pgvector community, Neo4j Community Edition) to avoid licensing constraints.

## Document Navigation

| # | Document | Description | Status |
| --- | --- | --- | --- |
| 00 | [Overview](00-overview.md) | This document | Current |
| 02 | [Architectural Decisions](02-architectural-decisions.md) | ADR log | Current |
| 05 | [Deployment Architecture](05-deployment-architecture.md) | Image build and CI/CD pipeline | Current |
| 08 | [Data Architecture](08-data-architecture.md) | PostgreSQL and Neo4j schema reference | Current |

**Next:** [Architectural Decisions](02-architectural-decisions.md)

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
