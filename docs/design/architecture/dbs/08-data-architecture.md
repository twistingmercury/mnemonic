# mnemonic-dbs — Data Architecture

> Back to [Database Architecture](README.md) | [Project README](../../../README.md)

## Table of Contents

- [Database Technology Stack](#database-technology-stack)
- [PostgreSQL Schema](#postgresql-schema)
- [Neo4j Schema](#neo4j-schema)
- [Which Service Uses What](#which-service-uses-what)
- [Migration Strategy](#migration-strategy)

## Database Technology Stack

### PostgreSQL (`pgvector/pgvector:pg16`)

**Purpose:** Relational store for patterns, their semantic search chunks, and enrichment jobs. PGVector provides the `vector` extension for embedding storage and HNSW similarity search. There are no agent or skill tables: `agents`, `pattern_agent_associations`, `skills` and `skill_files` are planned (see [Data Storage Implementation](../../design/data-storage.md#jsonb-document-model)) but have no migrations.

| Criterion | Requirement | How It Is Met |
| --- | --- | --- |
| Semantic search | Vector similarity over pattern chunk embeddings | `vector` extension with HNSW index on `pattern_chunks.embedding` |
| Relational integrity | FK constraints between entities | Native Postgres foreign keys |
| Enrichment tracking | Job state and retry tracking | `enrichment_jobs` table with status column |
| Flexible metadata | Tags and related-pattern names as JSON arrays | `jsonb` columns; tag filters use `?&` (list) and `@>` (search) |

### Neo4j (`neo4j:5` Community Edition)

**Purpose:** Knowledge graph projection of Postgres data. The services use `Pattern` and `Concept` nodes joined by `MENTIONED_IN` (concept to pattern) and `RELATED_TO` (pattern to pattern) relationships, for graph traversal queries that are impractical in SQL (e.g. "find patterns related to X via concept graph"). The Cypher files define constraints and indexes for `Pattern` and `Concept` only; the planned `Agent` node and `RELEVANT_FOR` relationship have no schema.

| Criterion | Requirement | How It Is Met |
| --- | --- | --- |
| Graph traversal | Multi-hop relationship queries | Native Neo4j Cypher traversal |
| Full-text search | Natural language pattern/concept lookup | Fulltext indexes on name and description |
| Uniqueness | One-to-one correspondence with Postgres | Uniqueness constraints on `id` / `name` |

## PostgreSQL Schema

### Entity Relationship Diagram

```mermaid
erDiagram
    patterns {
        uuid id PK
        varchar name UK
        varchar description "nullable"
        text content
        jsonb tags "JSON array"
        varchar entity_type
        varchar language
        varchar domain
        varchar version "nullable"
        jsonb related_patterns "JSON array of names"
        varchar enrichment_status "pending, enriched, failed"
        text enrichment_error "nullable"
        timestamptz enriched_at "nullable"
        timestamptz created_at
        timestamptz updated_at
    }
    pattern_chunks {
        uuid id PK
        uuid pattern_id FK
        varchar section_title
        int chunk_index
        text content
        vector embedding "vector(2000), nullable"
        varchar enrichment_status "pending, enriched, failed"
        text enrichment_error "nullable"
        timestamptz enriched_at "nullable"
        timestamptz created_at
        timestamptz updated_at
    }
    enrichment_jobs {
        uuid id PK
        uuid pattern_id FK "nullable, legacy jobs"
        uuid chunk_id FK "nullable, chunk jobs"
        varchar status "pending, processing, completed, failed"
        int attempts
        int max_attempts
        text last_error "nullable"
        timestamptz scheduled_for
        timestamptz started_at "nullable"
        timestamptz completed_at "nullable"
        timestamptz created_at
        timestamptz updated_at
    }

    patterns ||--o{ pattern_chunks : "split into"
    patterns ||--o{ enrichment_jobs : "legacy job for"
    pattern_chunks ||--o{ enrichment_jobs : "queued for"
```

`patterns`, `pattern_chunks` and `enrichment_jobs` are the only tables in the schema; the columns above are all of their columns (see [Data Storage Implementation](../../design/data-storage.md#postgresql-migrations) for the DDL).

Notes from the migrations and the services' queries:

- Deleting a pattern cascades to its `pattern_chunks` and `enrichment_jobs` rows (FK `ON DELETE CASCADE`); deleting a chunk cascades to its jobs.
- `enrichment_jobs_target_exclusive` requires each job to target exactly one of `pattern_id` or `chunk_id`. `mnemonic-api` only creates chunk jobs (`chunk_id` set, `pattern_id` NULL); the enricher still processes legacy pattern jobs.
- `pattern_chunks.embedding` stays NULL until the enricher embeds the chunk; search only considers rows where it is set and `enrichment_status = 'enriched'`.

### Tables

| Table | Migration | Purpose |
| --- | --- | --- |
| `patterns` | 000002 | Reusable context patterns (embedding column and 10KB content CHECK dropped in 000005; metadata columns added in 000005) |
| `enrichment_jobs` | 000003 | Queue and state tracking for pattern/chunk enrichment (`chunk_id` added and `pattern_id` made nullable in 000005) |
| `pattern_chunks` | 000005 | Semantic chunks of patterns with per-chunk embeddings (`vector(2000)` since 000006) |

### Indexes

| Index | Table | Type | Purpose |
| --- | --- | --- | --- |
| `idx_patterns_enriched` | `patterns` | B-tree, partial | `id` where `enrichment_status = 'enriched'` (000004) |
| `idx_patterns_tags` | `patterns` | GIN | `tags` containment (000004) |
| `idx_patterns_search` | `patterns` | GIN | Full-text on `name` and `description` (000004) |
| `idx_patterns_language`, `idx_patterns_domain`, `idx_patterns_entity_type` | `patterns` | B-tree | Metadata filters (000005) |
| `idx_patterns_embedding` | `patterns` | IVFFlat | Historical: created in 000004, dropped with `patterns.embedding` in 000005; superseded by `idx_pattern_chunks_embedding` |
| `idx_pattern_chunks_pattern_id` | `pattern_chunks` | B-tree | Chunks of a pattern (000005) |
| `idx_pattern_chunks_embedding` | `pattern_chunks` | HNSW | Vector similarity search on chunk embeddings (000005; recreated for `vector(2000)` in 000006) |
| `idx_enrichment_jobs_pattern` | `enrichment_jobs` | B-tree | Jobs of a pattern (000003) |
| `idx_enrichment_jobs_unique_pending` | `enrichment_jobs` | Unique, partial | One pending/processing job per `pattern_id` (000003; legacy pattern jobs only) |
| `idx_enrichment_jobs_unique_pending_chunk` | `enrichment_jobs` | Unique, partial | One pending/processing job per `chunk_id` (000005) |
| `idx_enrichment_jobs_pending`, `idx_enrichment_jobs_processing` | `enrichment_jobs` | B-tree, partial | Pending jobs by `scheduled_for`, processing jobs by `started_at` (000004) |

### Embedding Dimensions

Pattern chunk embeddings use `vector(2000)` (OpenAI text-embedding-3-large with Matryoshka truncation). All three services default `openai.embedding_dimensions` to 2000 and `openai.embedding_model` to `text-embedding-3-large`; changing either requires a matching schema change. Migration 000006 updated `pattern_chunks.embedding` to this dimension. The `patterns` table embedding column was removed in migration 000005 — embeddings now exist only in `pattern_chunks`.

## Neo4j Schema

### Node Types

| Label | Unique Constraint | Description |
| --- | --- | --- |
| `Pattern` | `pattern_id_unique` on `id` | Graph projection of a Postgres pattern row; properties `id` (UUID string), `name`, `description`, `updatedAt` |
| `Concept` | `concept_name_unique` on `name` | Concept extracted during enrichment; `name` (lowercase, trimmed), `type` (`technology`, `practice` or `domain`), `createdAt` |
| `SchemaVersion` | — | Tracks applied migration version (`v.version`, `v.migratedAt`) |

### Relationships

| Relationship | From → To | Properties | Purpose |
| --- | --- | --- | --- |
| `MENTIONED_IN` | `Concept → Pattern` | — | Concepts extracted from a pattern's content |
| `RELATED_TO` | `Pattern → Pattern` | `similarity` (float), `updatedAt` | Pre-computed edges between patterns that share concepts; stored in one direction, queried undirected |

`RELATED_TO.similarity` is `sharedConcepts / max(conceptsA, conceptsB)`; the enricher only creates edges at or above `enrichment.related_to_min_similarity` (default 0.3).

```mermaid
graph LR
    C1(["Concept<br/>name, type"]) -->|MENTIONED_IN| P1["Pattern<br/>id, name, description"]
    C1 -->|MENTIONED_IN| P2["Pattern"]
    C2(["Concept"]) -->|MENTIONED_IN| P1
    P1 -->|"RELATED_TO {similarity}"| P2
```

### Indexes

| Index | Type | On | Purpose |
| --- | --- | --- | --- |
| `pattern_name_index` | B-tree | `Pattern.name` | Name-based pattern lookup |
| `concept_type_index` | B-tree | `Concept.type` | Filter concepts by category |
| `pattern_content_fulltext` | Fulltext | `Pattern.name`, `Pattern.description` | Natural language pattern search |
| `concept_name_fulltext` | Fulltext | `Concept.name` | Natural language concept lookup |

Auto-created indexes from uniqueness constraints: `Pattern.id`, `Concept.name`.

Existence constraints (`pattern_name_exists`, `concept_name_exists`) are in `002_create_existence_constraints.cypher`, which needs Enterprise Edition; the `neo4j:5` Community image skips it.

### Schema Version

The Neo4j schema is versioned via a `SchemaVersion` node:

```cypher
MATCH (v:SchemaVersion {name: 'mnemonic'}) RETURN v.version, v.migratedAt
```

Current version: **3** (set by `003_create_indexes.cypher`; the image skips 002, which would set 2).

## Which Service Uses What

The schema is shared by three services with different access. This is what their repository code (`src/internal/repository/**`) actually does:

| Service | PostgreSQL | Neo4j |
| --- | --- | --- |
| `mnemonic-api` | Reads and writes `patterns`; inserts and deletes `pattern_chunks`; inserts `enrichment_jobs` (one pending job per chunk, in the same transaction as the pattern and chunks); chunk similarity search | Reads `RELATED_TO` and `MENTIONED_IN` for pattern detail; on pattern delete, removes the `Pattern` node and any of its concepts no other pattern mentions. Creates nothing. |
| `mnemonic-enricher` | Reads patterns and chunks; writes chunk embeddings and status, pattern enrichment status; claims, updates and deletes `enrichment_jobs` | Writes: merges the `Pattern` node, its `Concept` nodes and `MENTIONED_IN` edges, and recomputes `RELATED_TO`, all in one write transaction |
| `mnemonic-mcp` | Read-only: chunk similarity search, pattern lookups | Read-only: concepts and `RELATED_TO` lookups |

```mermaid
flowchart LR
    API["mnemonic-api"]
    ENR["mnemonic-enricher"]
    MCP["mnemonic-mcp<br/>(read-only)"]
    MQ[("RabbitMQ<br/>enrichment-jobs")]
    subgraph PG["PostgreSQL + pgvector"]
        T1["patterns"]
        T2["pattern_chunks"]
        T3["enrichment_jobs"]
    end
    subgraph NEO["Neo4j"]
        G1["Pattern / Concept<br/>MENTIONED_IN / RELATED_TO"]
    end
    API -->|"insert / update / delete"| T1
    API -->|"insert / delete"| T2
    API -->|insert| T3
    API -->|"publish job id"| MQ
    API -.->|"read; delete on pattern delete"| G1
    MQ -->|consume| ENR
    ENR -->|"claim / update / delete"| T3
    ENR -->|"embedding + status"| T2
    ENR -->|status| T1
    ENR -->|"sync (one write tx)"| G1
    MCP -.->|read| T1
    MCP -.->|read| T2
    MCP -.->|read| G1
```

## Migration Strategy

- **Tooling:** None at runtime. The schema ships in the images.
- **Postgres:** SQL files named `000001_description.up.sql` … `000006_description.up.sql`, copied into the image and executed in order by `docker-entrypoint-initdb.d` on first container start.
- **Neo4j:** Cypher files named `001_description.cypher` … `003_description.cypher`, applied via `cypher-shell` during image build (002 is skipped: Enterprise only).
- **Rollback:** Pull a prior image tag. No down migration files are maintained.
- **Testing:** BATS test suites in `src/tests/bats/` verify schema state after initialization. CI gates image pushes on passing tests.
- **Adding migrations:** Add new SQL/Cypher files with the next sequence number, add corresponding BATS assertions, open a PR.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
