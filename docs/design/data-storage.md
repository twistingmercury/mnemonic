---
version: 1
date: 2026-10-21
notes: "Imported from librarian-docs; repository, service and db/ references updated for the monorepo."
---

# Data Storage Implementation

> Back to [Design Documents](README.md)

**Code paths:** a path written as `internal/<dir>/...` is relative to the service's source directory, `src/internal/<service>/`. For example, `internal/metrics/tools.go` in the mcp service is `src/internal/mcp/metrics/tools.go`.

## Table of Contents

- [Overview](#overview)
- [JSONB Document Model](#jsonb-document-model)
  - [Design Rationale](#design-rationale)
  - [Common Table Shape](#common-table-shape)
  - [CRC64 Change Detection](#crc64-change-detection)
  - [JSONB Contents by Entity](#jsonb-contents-by-entity)
  - [JSONB Indexing Strategy](#jsonb-indexing-strategy)
  - [Application-Layer Validation](#application-layer-validation)
- [PostgreSQL Migrations](#postgresql-migrations)
  - [Migration File Structure](#migration-file-structure)
  - [Migration 000001: Extensions](#migration-000001-extensions)
  - [Migration 000002: Patterns Table](#migration-000002-patterns-table)
  - [Migration 000003: Enrichment Jobs Table](#migration-000003-enrichment-jobs-table)
  - [Migration 000004: Performance Indexes](#migration-000004-performance-indexes)
  - [Migration 000005: Pattern Chunks Table](#migration-000005-pattern-chunks-table)
  - [Migration 000006: Update Chunk Embedding Dimensions](#migration-000006-update-chunk-embedding-dimensions)
- [PGVector Configuration](#pgvector-configuration)
  - [Index Selection Guidelines](#index-selection-guidelines)
  - [Similarity Search Queries](#similarity-search-queries)
  - [Index Maintenance](#index-maintenance)
- [Neo4j Setup](#neo4j-setup)
  - [Schema Constraints](#schema-constraints)
  - [Index Configuration](#index-configuration)
  - [Node Creation Patterns](#node-creation-patterns)
  - [Relationship Definitions](#relationship-definitions)
  - [Graph Synchronization Queries](#graph-synchronization-queries)
- [Repository Interfaces](#repository-interfaces)
  - [AgentRepository](#agentrepository)
  - [PatternRepository](#patternrepository)
  - [ChunkRepository](#chunkrepository)
  - [SkillRepository](#skillrepository)
  - [SkillFileRepository](#skillfilerepository)
  - [EnrichmentJob Repository](#enrichmentjob-repository)
  - [GraphRepository](#graphrepository)
- [Connection Configuration](#connection-configuration)
  - [PostgreSQL Connection](#postgresql-connection)
  - [Neo4j Connection](#neo4j-connection)
  - [Connection String Formats](#connection-string-formats)
- [References](#references)

## Overview

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Data Architecture](architecture/system/04-data-architecture.md) | [System Architecture - Mnemonic](architecture/system/02-system-architecture.md#mnemonic-server)

This document provides implementation details for the Mnemonic data storage layer. It translates the architectural specifications from [Data Architecture](architecture/system/04-data-architecture.md) into concrete SQL migrations, Cypher queries, and Go interface definitions.

**Implementation Scope:**

| Component | Technology | Purpose |
| ----------- | ------------ | --------- |
| Relational Storage | PostgreSQL 16 (`pgvector/pgvector:pg16` base image) | Patterns, pattern chunks, enrichment jobs |
| Vector Storage | PGVector extension | Pattern chunk embeddings (`vector(2000)`) for semantic search |
| Graph Storage | Neo4j 5.x | Knowledge graph relationships |
| Migrations | Docker entrypoint scripts | Schema baked into the images: `docker-entrypoint-initdb.d` on first container start (Postgres) and `cypher-shell` at image build (Neo4j); no runtime migration tool |

**Implementation Status:**

| Store | Object | Status | Read by | Written by |
| ------- | -------- | -------- | --------- | ------------ |
| PostgreSQL | `patterns` | Implemented | api, enricher, mcp | api (CRUD), enricher (enrichment status) |
| PostgreSQL | `pattern_chunks` | Implemented | api, enricher, mcp | api (insert/delete), enricher (embedding, status) |
| PostgreSQL | `enrichment_jobs` | Implemented | enricher | api (insert), enricher (claim, update, delete) |
| PostgreSQL | `agents`, `skills`, `skill_files`, `pattern_agent_associations` | Planned (no schema yet) | none | none |
| Neo4j | `Pattern`, `Concept`, `MENTIONED_IN`, `RELATED_TO` | Implemented | api, mcp | enricher (`SyncPattern`); api deletes on pattern delete |
| Neo4j | `Agent`, `RELEVANT_FOR` | Planned (no schema yet) | none | none |

The mcp service is read-only against both databases. The sections below on the JSONB document model, agents and skills describe the planned design for tables that do not exist yet; the pattern, chunk, job and graph sections describe what the services do today.

```mermaid
flowchart LR
    API["api service"] -->|"patterns, pattern_chunks,<br/>enrichment_jobs (one tx)"| PG[("PostgreSQL<br/>+ pgvector")]
    API -->|"job id"| MQ[("RabbitMQ")]
    MQ --> ENR["enricher service"]
    ENR -->|"embeddings, statuses,<br/>job lifecycle"| PG
    ENR -->|"SyncPattern<br/>(one write tx)"| NEO[("Neo4j")]
    API -.->|"read; DeletePattern"| NEO
    MCP["mcp service"] -.->|read| PG
    MCP -.->|read| NEO
```

**Data Model:**

- `agents`, `skills` would use a JSONB document model with `crc64` change detection (planned; no tables yet)
- `skill_files` would store skill child files with path and content columns (planned; no table yet)
- `patterns` use relational columns (metadata, enrichment status); their embeddings live in `pattern_chunks`
- `pattern_chunks` holds one row per `[//]: pattern`-marked section, each with its own embedding
- `pattern_agent_associations` would be a join table for agent-scoped pattern filtering (planned; no table yet)
- `enrichment_jobs` is the durable state for the background enrichment queue
- Neo4j is required for the knowledge graph

**Deployment Independence:**

Database migrations and application code are versioned and deployed independently. Migrations live in `db/`, whose build scripts produce the `librarian-postgres` and `librarian-neo4j` images; the services pin an image tag (currently `v1.0.0-dev`). This enables:

- Logic bug fixes in Go without database deployment
- Schema changes without rebuilding application containers
- Forward-compatible migrations for zero-downtime deployments

## JSONB Document Model

[Table of Contents](#table-of-contents)

### Design Rationale

> **Status:** Planned. There are no `agents`, `skills` or `skill_files` tables in `db/`, no service code for them, and no agent/skill API or sync protocol yet. This section is the design for that work; the table shapes and indexes below have no migrations.

Agents, skills, and skill files are markdown documents synced to disk. Their field sets evolve as the Claude Code Agent Skills spec evolves. Storing each field as an individual relational column creates migration churn every time a field is added, renamed, or removed.

The JSONB document model stores the full document as a single JSONB column. Only the fields required for database-level operations (lookup key, change detection, audit timestamps) are promoted to top-level columns. Everything else lives inside the JSONB document.

**Benefits:**

- **No migration churn.** Adding a field to the agent spec requires only an application change, not a database migration.
- **Spec alignment.** The JSONB column mirrors what the API returns and what the sync protocol transmits. No impedance mismatch between storage and wire format.
- **Simpler repository code.** One `definition JSONB` column replaces five or more individual columns. Reads and writes are straightforward marshal/unmarshal operations.

**Trade-offs:**

- **No column-level constraints on JSONB contents.** Field validation (max lengths, required fields, allowed values) is enforced by the application, not by CHECK constraints on individual columns.
- **GIN indexes required for JSONB queries.** Querying inside the document (tag filtering, field searches) requires GIN indexes instead of simple btree indexes.

### Common Table Shape

All document tables (agents, skills) share the same column structure:

| Column | Type | Constraints | Purpose |
| -------- | ------ | ------------- | --------- |
| `id` | UUID | PK, DEFAULT gen_random_uuid() | Internal identifier |
| `name` | VARCHAR(255) | UNIQUE NOT NULL | Lookup key; the one field always queried by |
| `definition` | JSONB | NOT NULL | Complete document (all entity fields) |
| `crc64` | VARCHAR(20) | NOT NULL | CRC-64 checksum of serialized JSONB, for change detection |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT now() | DB sets on INSERT |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT now() | App updates on UPDATE |

The `skill_files` table stores child files for skills with `skill_id` and `path` as the composite lookup key, plus `content` as TEXT for the file body.

### CRC64 Change Detection

Each document table includes a `crc64` column storing a CRC-64 checksum of the JSONB content.

**Computation:**

- Computed server-side (in the Go application) on every INSERT and UPDATE.
- Input: the serialized JSONB content in canonical form (deterministic key ordering, no extra whitespace). Go's `encoding/json` produces deterministic output for the same struct, but the application should use a canonical serialization function to guarantee consistency.
- Algorithm: CRC-64 with ISO polynomial (matching Go's `hash/crc64` package with `crc64.MakeTable(crc64.ISO)`).
- Output: a 64-bit unsigned integer, stored as PostgreSQL VARCHAR(20) (decimal string representation). Go converts the uint64 to its decimal string form for storage and parses it back on read.

**Usage in the sync protocol:**

- The Admin REST API returns per-entity CRC64 values in list responses.
- The sync client compares local CRC64 values against the manifest to determine which entities have changed.
- A collection-level version hash can be derived from individual CRC64 values (e.g., XOR of all entity CRC64s for a given collection).

**Why CRC-64 and not SHA-256:**

CRC-64 is fast, fits in a single VARCHAR(20) column, and provides sufficient collision resistance for change detection (not security). The sync protocol uses it to answer "has this document changed?" not "is this document authentic?"

### JSONB Contents by Entity

#### Agents definition JSONB

The `definition` column stores the complete agent specification:

```json
{
  "description": "Implements Go code: functions, packages, tests",
  "system_prompt": "You are an expert Go engineer...",
  "model": "sonnet",
  "allowed_tools": ["Read", "Write", "Edit", "Bash"],
  "version": "1.2.0"
}
```

| Field | Type | Required | Description |
| ------- | ------ | ---------- | ------------- |
| `description` | string | yes | Short summary of the agent's purpose |
| `system_prompt` | string | yes | Full system prompt content |
| `model` | string | yes | Claude model: sonnet, opus, haiku |
| `allowed_tools` | string[] | yes | MCP tool names this agent can use |
| `version` | string | yes | Semantic version of the definition |

#### Skills definition JSONB

Aligned with the [Claude Code Agent Skills spec](https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/skills) frontmatter fields:

```json
{
  "description": "Synchronize agents and skills from Mnemonic",
  "content": "---\nname: mnemonic-sync\n---\n\nYou are synchronizing...",
  "tags": ["sync", "infrastructure"],
  "license": "MIT",
  "compatibility": "Claude Code 1.0+",
  "metadata": {"author": "team-platform"},
  "allowed_tools": ["Read", "Write", "Bash"],
  "version": "1.0.0"
}
```

| Field | Type | Required | Description |
| ------- | ------ | ---------- | ------------- |
| `description` | string | yes | Short summary of the skill |
| `content` | string | yes | Full skill markdown content |
| `tags` | string[] | no | Categorization tags |
| `license` | string | no | License identifier (e.g., MIT) |
| `compatibility` | string | no | Compatible Claude Code versions |
| `metadata` | object | no | Arbitrary key-value metadata |
| `allowed_tools` | string[] | no | MCP tool names this skill can use |
| `version` | string | yes | Semantic version of the definition |

#### Skill files

Skill files do not use the JSONB document model. They store file content directly in a TEXT column with a `path` column for identification within the skill directory. The `crc64` column provides change detection on the file content.

| Column | Type | Purpose |
| -------- | ------ | --------- |
| `id` | UUID | PK, DEFAULT gen_random_uuid() |
| `skill_id` | UUID | FK to parent skill (CASCADE DELETE) |
| `path` | VARCHAR(1024) | File path within the skill directory |
| `content` | TEXT | File content |
| `crc64` | VARCHAR(20) | CRC-64 checksum for change detection |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT now() |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT now(); app updates on UPDATE |

### JSONB Indexing Strategy

Document tables use GIN indexes on the `definition` column to support queries that filter by JSONB contents.

**Primary use case: tag filtering.**

Skills support tag-based filtering. The GIN index enables efficient `@>` (contains) queries:

```sql
-- Find skills tagged with "sync"
SELECT id, name, definition
FROM skills
WHERE definition @> '{"tags": ["sync"]}'::jsonb;
```

**Index definitions** (planned; no migration creates them yet):

```sql
-- GIN index on definition column for tag filtering
CREATE INDEX idx_skills_definition ON skills USING GIN (definition);
```

**Path-specific GIN indexes** (alternative, more selective):

```sql
-- Index only the tags path within definition
CREATE INDEX idx_skills_definition_tags ON skills USING GIN ((definition -> 'tags'));
```

The path-specific indexes are smaller and faster for tag-only queries. Use full-column GIN indexes if queries need to filter on other JSONB fields.

### Application-Layer Validation

Because JSONB contents are not constrained at the database level, the application enforces all field validation:

| Entity | Field | Constraint | Enforced By |
| -------- | ------- | ----------- | ------------- |
| Agent | name | max 255 chars, unique | DB (VARCHAR + UNIQUE) |
| Agent | name | `^[a-z][a-z0-9-]*$` format | Application |
| Agent | definition.description | max 500 chars | Application |
| Agent | definition.system_prompt | max 50KB | Application |
| Agent | definition.model | one of: sonnet, opus, haiku | Application |
| Agent | definition.allowed_tools | must be string array | Application |
| Agent | definition.version | required, semver format | Application |
| Skill | name | max 255 chars, unique | DB (VARCHAR + UNIQUE) |
| Skill | name | `^[a-z][a-z0-9-]*$` format | Application |
| Skill | definition.description | max 1024 chars | Application |
| Skill | definition.content | max 512KB | Application |
| Skill | definition.version | required, semver format | Application |
| Skill File | path | max 1024 chars | DB (VARCHAR) |
| Skill File | content | max 1MB | Application |

The database enforces only structural constraints (primary keys, uniqueness, foreign keys). Name format validation and content-level validation belong to the application.

**Future Consideration:**

For database-level JSONB schema validation, consider the [pg_jsonschema](https://github.com/supabase/pg_jsonschema) PostgreSQL extension. This extension enables CHECK constraints that validate JSONB documents against JSON Schema specifications, providing an alternative to application-only validation.

## PostgreSQL Migrations

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Data Architecture - Migration Strategy](architecture/system/04-data-architecture.md#migration-strategy)

### Migration File Structure

All PostgreSQL migrations are `.up.sql` files copied into the `librarian-postgres` image and applied by `docker-entrypoint-initdb.d` when the container first starts with an empty data directory. Mnemonic does not run or manage migrations at runtime. There are no `.down.sql` files — rollback is accomplished by pulling a prior image tag (see [dbs ADR-001](architecture/dbs/02-architectural-decisions.md)).

```text
db/
├── postgres/
│   ├── 000001_extensions.up.sql
│   ├── 000002_create_patterns.up.sql
│   ├── 000003_create_enrichment_jobs.up.sql
│   ├── 000004_create_performance_indexes.up.sql
│   ├── 000005_pattern_schema_chunks.up.sql
│   └── 000006_update_embedding_dimensions.up.sql
└── neo4j/
    ├── 001_create_constraints.cypher
    ├── 002_create_existence_constraints.cypher
    └── 003_create_indexes.cypher
```

PostgreSQL migration files use a 6-digit zero-padded prefix (e.g., `000001`) to guarantee execution order. The Docker entrypoint runs all `.up.sql` files from `/docker-entrypoint-initdb.d/` in filename-sorted order on first container start.

Neo4j migrations are numbered `.cypher` files applied via `cypher-shell` during Docker image build (see [Manual Constraint Creation](#startup-constraint-validation)).

The SQL below is copied from `db/` (`develop`). The header comments still name the files' old `src/migrations/...` location; the files live in `db/postgres/`.

> **No agent or skill schema:** There are no `agents`, `skills`, `skill_files` or `pattern_agent_associations` tables. The [JSONB Document Model](#jsonb-document-model) section is the planned design for them; it will need new migrations when that work starts.

### Migration 000001: Extensions

**Purpose:** Enable required PostgreSQL extensions.

```sql
-- src/migrations/postgres/000001_extensions.up.sql
-- Enables required PostgreSQL extensions.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- This migration must be applied first as it provides:
-- - Vector operations extension for pattern embeddings

-- Enable vector operations for embeddings (pgvector extension)
create extension if not exists vector;
```

**Note:** Per the storage-only database philosophy, no trigger functions are created. `updated_at` management is the application's responsibility. The application sets `updated_at = now()` on every UPDATE.

### Migration 000002: Patterns Table

**Purpose:** Create the patterns table. Its `embedding` column and 10KB content CHECK are dropped again by [000005](#migration-000005-pattern-chunks-table), and the 1536-dimension comment is historical.

```sql
-- src/migrations/postgres/000002_create_patterns.up.sql
-- Creates the patterns table with PGVector embedding support.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies: 000001_extensions (for vector extension)
--
-- Patterns use relational columns (not the JSONB document model) because they
-- have enrichment status, graph context, and pgvector embeddings that require
-- individual columns.
--
-- Note: updated_at is managed by the application layer, not database triggers.

create table if not exists patterns (
    -- UUID primary key for stable references (patterns may be renamed)
    id uuid primary key default gen_random_uuid(),

    -- Pattern metadata
    name varchar(255) not null,
    description varchar(500),

    -- Pattern content (up to 10KB)
    content text not null,

    -- Categorization tags (JSON array)
    tags jsonb not null default '[]'::jsonb,

    -- Vector embedding for semantic search (1536 dimensions for text-embedding-3-small)
    embedding vector(1536),

    -- Enrichment processing state
    enrichment_status varchar(20) not null default 'pending',
    enrichment_error text,
    enriched_at timestamptz,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Constraints
    constraint patterns_name_unique unique (name),
    constraint patterns_content_length
        check (length(content) <= 10240),
    constraint patterns_tags_array
        check (jsonb_typeof(tags) = 'array'),
    constraint patterns_enrichment_status_valid
        check (enrichment_status in ('pending', 'enriched', 'failed'))
);

comment on table patterns is 'Reusable context patterns for prompt enrichment';
comment on column patterns.embedding is 'Vector embedding (1536d) for semantic similarity search';
comment on column patterns.enrichment_status is 'Processing state: pending, enriched, or failed';
```

> **Note:** The 10KB `patterns_content_length` CHECK no longer exists: [migration 000005](#migration-000005-pattern-chunks-table) drops it. The database does not limit content length; the API handler caps `content` at 100,000 bytes.

### Migration 000003: Enrichment Jobs Table

**Purpose:** Create the enrichment jobs queue table for background pattern processing.

```sql
-- src/migrations/postgres/000003_create_enrichment_jobs.up.sql
-- Creates the enrichment jobs queue table for background pattern processing.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies: 000002_create_patterns (for patterns table)
--
-- Enrichment jobs track background processing of patterns:
-- - Embedding generation via OpenAI text-embedding-3-large
-- - Entity extraction for Neo4j knowledge graph
--
-- This table serves as a simple Postgres-backed job queue using
-- FOR UPDATE SKIP LOCKED for safe concurrent processing.
--
-- Job lifecycle: pending -> processing -> completed | failed
--
-- Note: updated_at is managed by the application layer, not database triggers.

create table if not exists enrichment_jobs (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Reference to pattern being enriched
    pattern_id uuid not null,

    -- Job processing state
    status varchar(20) not null default 'pending',

    -- Retry tracking
    attempts integer not null default 0,
    max_attempts integer not null default 3,

    -- Error information
    last_error text,

    -- Scheduling and timing
    scheduled_for timestamptz not null default now(),
    started_at timestamptz,
    completed_at timestamptz,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Foreign key with cascade delete
    constraint fk_enrichment_jobs_pattern
        foreign key (pattern_id) references patterns(id) on delete cascade,

    -- Constraints
    constraint enrichment_jobs_status_valid
        check (status in ('pending', 'processing', 'completed', 'failed')),
    constraint enrichment_jobs_attempts_valid
        check (attempts >= 0),
    constraint enrichment_jobs_max_attempts_valid
        check (max_attempts >= 1)
);

-- Index for pattern lookups
create index idx_enrichment_jobs_pattern
    on enrichment_jobs(pattern_id);

-- Prevent duplicate pending or processing jobs for the same pattern
create unique index idx_enrichment_jobs_unique_pending
    on enrichment_jobs(pattern_id)
    where status in ('pending', 'processing');

comment on table enrichment_jobs is 'Background processing queue for pattern enrichment';
comment on column enrichment_jobs.status is 'Job state: pending, processing, completed, or failed';
comment on column enrichment_jobs.scheduled_for is 'When the job should be processed (supports delayed retry)';
```

[Migration 000005](#migration-000005-pattern-chunks-table) makes `pattern_id` nullable and adds `chunk_id`. The services create only chunk jobs (`pattern_id` null), so `idx_enrichment_jobs_unique_pending` (on `pattern_id`) no longer constrains them; `idx_enrichment_jobs_unique_pending_chunk` does.

### Migration 000004: Performance Indexes

**Purpose:** Create performance-optimized indexes for common query patterns.

```sql
-- src/migrations/postgres/000004_create_performance_indexes.up.sql
-- Creates performance-optimized indexes for common query patterns.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies:
--   - 000002_create_patterns (for patterns table)
--   - 000003_create_enrichment_jobs (for enrichment_jobs table)

-- =============================================================================
-- PATTERNS INDEXES
-- =============================================================================

-- Partial index for filtering to only enriched patterns
-- Used when selecting patterns eligible for similarity search
create index idx_patterns_enriched
    on patterns(id)
    where enrichment_status = 'enriched';

-- Vector similarity search (IVFFlat for MVP scale)
-- lists = 100 suitable for 1,000-10,000 patterns
-- NOTE: IVFFlat requires at least one row with a non-null embedding to build
-- centroids. If applied to an empty database, create this index after seeding
-- initial data, or use HNSW as an alternative that builds on empty tables.
create index idx_patterns_embedding
    on patterns using ivfflat (embedding vector_cosine_ops)
    with (lists = 100);

-- GIN index for tag filtering using JSONB containment operator (@>)
create index idx_patterns_tags
    on patterns using gin (tags);

-- Full-text search on name and description
create index idx_patterns_search
    on patterns using gin (
        to_tsvector('english', name || ' ' || coalesce(description, ''))
    );

-- =============================================================================
-- ENRICHMENT JOBS INDEXES
-- =============================================================================

-- Pending jobs by scheduled time (worker polling)
create index idx_enrichment_jobs_pending
    on enrichment_jobs(scheduled_for)
    where status = 'pending';

-- Processing jobs for timeout detection
create index idx_enrichment_jobs_processing
    on enrichment_jobs(started_at)
    where status = 'processing';

-- Index documentation
comment on index idx_patterns_embedding is
    'IVFFlat index for vector similarity search (100 lists for MVP scale)';
comment on index idx_enrichment_jobs_pending is
    'Optimizes worker polling for pending jobs';
```

`idx_patterns_embedding` no longer exists: [migration 000005](#migration-000005-pattern-chunks-table) drops `patterns.embedding`, which drops the index with it. The other indexes remain.

### Migration 000005: Pattern Chunks Table

**Purpose:** Introduce chunk-based semantic search by creating the `pattern_chunks` table, moving embeddings from patterns to per-chunk rows, adding structured metadata columns to patterns, and updating enrichment jobs to support chunk-level processing.

> **Architecture Reference:** [Data Architecture - Pattern Chunks](architecture/dbs/08-data-architecture.md#tables) | [Pattern Enrichment - Generate Embeddings](pattern-processing.md#step-2-generate-embeddings)

```sql
-- src/migrations/postgres/000005_pattern_schema_chunks.up.sql
-- Adds structured metadata columns to patterns, introduces chunk-based
-- semantic search via pattern_chunks, and updates enrichment_jobs to
-- support chunk-level enrichment jobs.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies:
--   - 000002_create_patterns (for patterns table)
--   - 000003_create_enrichment_jobs (for enrichment_jobs table)
--   - 000004_create_performance_indexes (for idx_patterns_embedding, which is dropped)

-- =============================================================================
-- PATTERNS TABLE — drop embedding, remove content limit, add metadata columns
-- =============================================================================

-- Drop the 10KB content constraint (pattern files can exceed this)
alter table patterns drop constraint if exists patterns_content_length;

-- Drop embedding from patterns (embeddings move to per-chunk rows)
alter table patterns drop column if exists embedding;

-- Add structured metadata columns populated from pattern file YAML frontmatter
alter table patterns
    add column entity_type      varchar(100) not null default '',
    add column language         varchar(50)  not null default '',
    add column domain           varchar(50)  not null default '',
    add column version          varchar(50),
    add column related_patterns jsonb        not null default '[]'::jsonb;

-- Enforce that related_patterns is always a JSON array
alter table patterns
    add constraint patterns_related_patterns_array
        check (jsonb_typeof(related_patterns) = 'array');

-- Indexes for common filter query params (language, domain, entity_type)
create index idx_patterns_language    on patterns (language);
create index idx_patterns_domain      on patterns (domain);
create index idx_patterns_entity_type on patterns (entity_type);

-- =============================================================================
-- PATTERN_CHUNKS TABLE — one row per H2 section of a parent pattern
-- =============================================================================

create table if not exists pattern_chunks (
    id                uuid         primary key default gen_random_uuid(),
    pattern_id        uuid         not null references patterns(id) on delete cascade,
    section_title     varchar(255) not null,
    chunk_index       int          not null,
    content           text         not null,
    embedding         vector(1536),
    enrichment_status varchar(20)  not null default 'pending',
    enrichment_error  text,
    enriched_at       timestamptz,
    created_at        timestamptz  not null default now(),
    updated_at        timestamptz  not null default now(),
    constraint pattern_chunks_enrichment_status_valid
        check (enrichment_status in ('pending', 'enriched', 'failed'))
);

create index idx_pattern_chunks_pattern_id on pattern_chunks (pattern_id);

-- Vector similarity search on chunks.
-- HNSW is used instead of IVFFlat because IVFFlat requires existing rows to
-- build centroids; on an empty table it produces zero centroids and degrades
-- all vector searches to sequential scans until the index is rebuilt manually
-- after seeding. HNSW builds correctly on empty tables and requires no
-- centroid seeding, making it safe to create at migration time.
create index idx_pattern_chunks_embedding
    on pattern_chunks using hnsw (embedding vector_cosine_ops);

comment on table pattern_chunks is 'H2-bounded chunks of patterns with per-chunk embeddings for semantic search';
comment on column pattern_chunks.chunk_index is 'Zero-based order within parent pattern';
comment on index idx_pattern_chunks_embedding is
    'HNSW index for chunk vector similarity search; chosen over IVFFlat because it builds correctly on empty tables without centroid seeding';

-- =============================================================================
-- ENRICHMENT_JOBS — add chunk_id, make pattern_id nullable
-- =============================================================================

-- chunk_id is set for chunk-based enrichment jobs; pattern_id is set for legacy jobs
alter table enrichment_jobs
    add column chunk_id uuid references pattern_chunks(id) on delete cascade;
alter table enrichment_jobs
    alter column pattern_id drop not null;

-- Prevent duplicate pending/processing jobs for the same chunk
CREATE UNIQUE INDEX idx_enrichment_jobs_unique_pending_chunk
    ON enrichment_jobs (chunk_id)
    WHERE status IN ('pending', 'processing');

-- Ensure each enrichment job targets exactly one of: a pattern or a chunk
ALTER TABLE enrichment_jobs
    ADD CONSTRAINT enrichment_jobs_target_exclusive
        CHECK (
            (pattern_id IS NOT NULL AND chunk_id IS NULL) OR
            (chunk_id IS NOT NULL AND pattern_id IS NULL)
        );
```

Key changes in this migration:

- **Patterns table**: Drops the `embedding` column (embeddings move to `pattern_chunks`), removes the 10KB content length constraint, and adds structured metadata columns (`entity_type`, `language`, `domain`, `version`, `related_patterns`) populated from pattern file YAML frontmatter.
- **Pattern chunks table**: Creates `pattern_chunks` with one row per section of a parent pattern (the comments say "H2", but the API chunks at `[//]: pattern` decorators, any heading level). Each chunk has its own `embedding`, `enrichment_status`, `enrichment_error`, and `enriched_at` fields. The HNSW index is used instead of IVFFlat because IVFFlat requires existing rows to build centroids and degrades to sequential scans on empty tables.
- **Enrichment jobs table**: Adds a `chunk_id` column (FK to `pattern_chunks`), makes `pattern_id` nullable, adds a unique index to prevent duplicate pending/processing jobs per chunk, and adds an exclusive constraint ensuring each job targets exactly one of a pattern or a chunk.

### Migration 000006: Update Chunk Embedding Dimensions

**Purpose:** Update `pattern_chunks.embedding` from `vector(1536)` to `vector(2000)` to support `text-embedding-3-large` with Matryoshka truncation at 2000 dimensions.

> **Architecture Reference:** [Data Architecture - Embedding Dimensions](architecture/dbs/08-data-architecture.md#embedding-dimensions) | [Pattern Enrichment - OpenAI Embeddings](pattern-processing.md#openai-api-embeddings)

```sql
-- src/migrations/postgres/000006_update_embedding_dimensions.up.sql
-- Updates pattern_chunks.embedding from vector(1536) to vector(2000)
-- to support text-embedding-3-large with Matryoshka truncation at 2000 dimensions.
-- 2000 is the maximum supported by pgvector's HNSW and IVFFlat index types.
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies:
--   - 000005_pattern_schema_chunks (creates pattern_chunks table with vector(1536))

-- Vector dimensions cannot be altered in place; must drop and recreate the HNSW index.
drop index if exists idx_pattern_chunks_embedding;

-- Update the embedding column to 2000 dimensions.
alter table pattern_chunks
    alter column embedding type vector(2000);

-- Recreate the HNSW index for the new dimension.
create index idx_pattern_chunks_embedding
    on pattern_chunks using hnsw (embedding vector_cosine_ops);

comment on index idx_pattern_chunks_embedding is
    'HNSW index for chunk vector similarity search at 2000 dimensions (text-embedding-3-large with Matryoshka truncation)';
```

This migration is separated from 000005 because the embedding dimension is a deployment-time decision. Migration 000005 creates the table with a default 1536-dimension column (the original `text-embedding-3-small` size), and migration 000006 updates it to 2000 dimensions to match the `text-embedding-3-large` model configuration. The HNSW index must be dropped and recreated because PGVector does not support in-place dimension changes on indexed columns.

## PGVector Configuration

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Data Architecture - PGVector Configuration](architecture/system/04-data-architecture.md#pgvector-configuration)

### Index Selection Guidelines

Embeddings live only in `pattern_chunks.embedding` (`vector(2000)`); `patterns.embedding` and its IVFFlat index were dropped in migration 000005. The current index is:

| Index | Column | Type | Notes |
| ------- | -------- | ------ | ------- |
| `idx_pattern_chunks_embedding` | `pattern_chunks.embedding` | HNSW, `vector_cosine_ops`, pgvector default build parameters | Created in 000005, recreated at 2000 dimensions in 000006 |

HNSW was chosen over IVFFlat because IVFFlat builds its centroids from existing rows and degrades to sequential scans when created on an empty table, which is exactly how the image-baked schema starts. 2000 dimensions is the most pgvector can index for the `vector` type, which is why the services default `openai.embedding_dimensions` to 2000.

**Tuning HNSW** (when recall or build time becomes a problem):

```sql
-- Rebuild with explicit parameters (higher m / ef_construction: better recall, more memory, slower build)
drop index if exists idx_pattern_chunks_embedding;
create index idx_pattern_chunks_embedding
    on pattern_chunks using hnsw (embedding vector_cosine_ops)
    with (m = 16, ef_construction = 64);
```

### Similarity Search Queries

Both the Admin API (`GET /v1/api/patterns/search`) and the MCP server (`search_patterns`) search chunks, not patterns, with one query joined to `patterns` for metadata. This is the Admin API's version (`internal/repository/chunk/repository.go`); the MCP version is the same `WHERE`/`ORDER BY` but selects only `pc.pattern_id, p.name, p.tags, pc.section_title, pc.content, similarity` and has no pattern-ID filter.

```sql
-- $1: query embedding (vector(2000))
-- $2: minimum similarity (0.0-1.0; MCP default 0.5)
-- $3: language filter ('' disables)
-- $4: domain filter ('' disables)
select pc.id, pc.pattern_id, p.name, p.entity_type, p.language, p.domain, p.tags,
       pc.section_title, pc.chunk_index, pc.content,
       1 - (pc.embedding <=> $1) as similarity
from pattern_chunks pc
join patterns p on p.id = pc.pattern_id
where pc.embedding is not null
  and pc.enrichment_status = 'enriched'
  and 1 - (pc.embedding <=> $1) >= $2
  and ($3::text = '' or p.language = $3)
  and ($4::text = '' or p.domain = $4)
  -- appended only when set:
  --   and pc.pattern_id = any($5)     -- API only
  --   and p.tags @> $6::jsonb         -- pattern must have all requested tags
order by pc.embedding <=> $1
limit $7;  -- default 10
```

Other pattern queries the services run:

```sql
-- Admin API list (content omitted; no total count)
select id, name, description, tags, entity_type, language, domain, version,
       related_patterns, enrichment_status, enrichment_error, enriched_at,
       created_at, updated_at
from patterns
where tags ?& $1                                         -- has all tags (optional)
  and to_tsvector('english', name || ' ' || coalesce(description, ''))
      @@ plainto_tsquery('english', $2)                  -- search (optional)
  and language = $3 and domain = $4 and entity_type = $5 -- each optional
order by name asc
limit $6 offset $7;

-- MCP graph-expansion metadata
select id, name, tags, language, domain from patterns where id = any($1);
```

### Index Maintenance

**Adjusting HNSW search breadth:**

```sql
-- Default ef_search is 40; higher values improve recall at the cost of latency.
-- The services do not set it, so the server default applies.
set hnsw.ef_search = 100;

select pc.id, 1 - (pc.embedding <=> $1) as similarity
from pattern_chunks pc
where pc.enrichment_status = 'enriched'
order by pc.embedding <=> $1
limit 5;

reset hnsw.ef_search;
```

Because the similarity threshold and the language/domain/tag filters are applied to the rows the index returns, a heavily filtered search can return fewer than `limit` rows.

**Reindexing After Bulk Updates:**

```sql
-- Pattern updates delete and re-insert chunks, so heavy churn can bloat the index.
reindex index concurrently idx_pattern_chunks_embedding;
```

**Analyzing for Query Planner:**

```sql
analyze pattern_chunks;
analyze patterns;
```

## Neo4j Setup

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Data Architecture - Neo4j Graph Model](architecture/system/04-data-architecture.md#neo4j-graph-model)

What the services use: `Pattern` and `Concept` nodes, `MENTIONED_IN` (Concept to Pattern) and `RELATED_TO` (Pattern to Pattern). The Cypher files define constraints and indexes only for `Pattern` and `Concept`; the planned `Agent` node and `RELEVANT_FOR` relationship have no schema. The image applies 001 and 003 at build time and skips 002 (Enterprise only); each file also records its number on a `SchemaVersion {name: 'mnemonic'}` node.

```mermaid
graph LR
    C(["Concept<br/>name UNIQUE, type, createdAt"]) -->|MENTIONED_IN| P["Pattern<br/>id UNIQUE, name, description, updatedAt"]
    P -->|"RELATED_TO<br/>similarity, updatedAt"| P2["Pattern"]
```

### Schema Constraints

Create these constraints during initial Neo4j setup.

**Uniqueness Constraints (Community Edition + Enterprise Edition):**

These constraints are compatible with all Neo4j editions and are created by `001_create_constraints.cypher` (copied from `db/`):

```cypher
// src/migrations/neo4j/001_create_constraints.cypher
// Creates uniqueness constraints for Neo4j node labels.
// Part of Mnemonic MVP
//
// Dependencies: None (first Neo4j migration)
//
// This migration establishes uniqueness constraints for the two
// node types in the knowledge graph: Pattern and Concept.
// All constraints use IF NOT EXISTS for idempotent execution.
//
// These constraints are compatible with Neo4j Community Edition.
// For existence constraints (Enterprise Edition only), see
// 002_create_existence_constraints.cypher.
//
// Neo4j is a best-effort graph projection of PostgreSQL data.
// Mnemonic validates these constraints at startup:
//   - All constraints exist  -> log info, continue
//   - Some missing           -> log warning with names, continue
//   - Connection failure     -> log warning, continue
//
// Constraint names checked at startup:
//   pattern_id_unique, concept_name_unique
//
// Manual application:
//   cypher-shell -u neo4j -p <password> -f src/migrations/neo4j/001_create_constraints.cypher

// =============================================================================
// UNIQUENESS CONSTRAINTS
// =============================================================================

// Pattern nodes use the UUID from PostgreSQL as their unique identifier.
// This ensures one-to-one correspondence between Postgres rows and graph nodes.
CREATE CONSTRAINT pattern_id_unique IF NOT EXISTS
FOR (p:Pattern) REQUIRE p.id IS UNIQUE;

// Concept nodes use a normalized lowercase name as their unique identifier.
// Concepts are extracted during enrichment and deduplicated by name.
CREATE CONSTRAINT concept_name_unique IF NOT EXISTS
FOR (c:Concept) REQUIRE c.name IS UNIQUE;

// =============================================================================
// SCHEMA VERSION
// =============================================================================

MERGE (v:SchemaVersion {name: 'mnemonic'})
SET v.version = 1, v.migratedAt = datetime(), v.migration = '001_create_constraints';

// =============================================================================
// ROLLBACK
// =============================================================================
// To reverse this migration, run:
//   DROP CONSTRAINT pattern_id_unique IF EXISTS;
//   DROP CONSTRAINT concept_name_unique IF EXISTS;
//   MATCH (v:SchemaVersion {name: 'mnemonic'}) DELETE v;
```

The file's header comment about validating constraints at startup describes a plan; the services do not check constraints (see [Startup Constraint Validation](#startup-constraint-validation)).

**Existence Constraints (Enterprise Edition Only):**

These constraints require Neo4j Enterprise Edition and are created by `002_create_existence_constraints.cypher`. The Community Edition image skips this migration (see [dbs ADR-004](architecture/dbs/02-architectural-decisions.md)). The enricher rejects a nil pattern ID or an empty pattern name before writing, regardless of whether these constraints are present.

```cypher
// src/migrations/neo4j/002_create_existence_constraints.cypher
// Creates existence constraints for Neo4j.
// Part of Mnemonic MVP
//
// Dependencies: 001_create_constraints (should be applied first)
//
// IMPORTANT: This migration requires Neo4j Enterprise Edition.
// Existence constraints (IS NOT NULL) are not available in Community Edition.
// Running this file against Community Edition will produce errors.
//
// For local development with Community Edition, skip this migration.
// The application layer enforces these properties regardless of whether
// these constraints are present in Neo4j.
//
// All statements use IF NOT EXISTS for idempotent execution.
//
// Manual application:
//   cypher-shell -u neo4j -p <password> -f src/migrations/neo4j/002_create_existence_constraints.cypher

// =============================================================================
// EXISTENCE CONSTRAINTS (Enterprise Edition only)
// =============================================================================

// Every Pattern node must have a name property.
// The name is synced from PostgreSQL and is required for display and search.
CREATE CONSTRAINT pattern_name_exists IF NOT EXISTS
FOR (p:Pattern) REQUIRE p.name IS NOT NULL;

// Every Concept node must have a name property.
// The name is the normalized identifier used for deduplication.
CREATE CONSTRAINT concept_name_exists IF NOT EXISTS
FOR (c:Concept) REQUIRE c.name IS NOT NULL;

// =============================================================================
// SCHEMA VERSION
// =============================================================================

MERGE (v:SchemaVersion {name: 'mnemonic'})
SET v.version = 2, v.migratedAt = datetime(), v.migration = '002_create_existence_constraints';

// =============================================================================
// ROLLBACK
// =============================================================================
// To reverse this migration, run:
//   DROP CONSTRAINT pattern_name_exists IF EXISTS;
//   DROP CONSTRAINT concept_name_exists IF EXISTS;
//   MERGE (v:SchemaVersion {name: 'mnemonic'}) SET v.version = 1, v.migratedAt = datetime();
```

### Startup Constraint Validation

Neo4j is required. Each service creates its driver at startup and calls `VerifyConnectivity`; if that fails, startup fails. The services do **not** check that constraints or indexes exist: they rely on the `librarian-neo4j` image having them baked in. A constraint check that logs missing constraints at startup is not implemented.

Neo4j reachability is also part of every service's `GET /health` check.

**Manual Constraint Creation:**

If you run against a Neo4j that was not started from the `librarian-neo4j` image, apply the migration scripts manually:

```bash
# Using cypher-shell (Community + Enterprise)
cypher-shell -u neo4j -p <password> -f src/neo4j/001_create_constraints.cypher

# Enterprise Edition only (optional)
cypher-shell -u neo4j -p <password> -f src/neo4j/002_create_existence_constraints.cypher

# Indexes
cypher-shell -u neo4j -p <password> -f src/neo4j/003_create_indexes.cypher
```

### Index Configuration

```cypher
// src/migrations/neo4j/003_create_indexes.cypher
// Creates indexes for common Neo4j query patterns.
// Part of Mnemonic MVP
//
// Dependencies: 001_create_constraints (uniqueness constraints also create indexes,
//   but these additional indexes cover non-unique lookup and full-text search)
//   Optionally: 002_create_existence_constraints (Enterprise Edition only)
//
// Note: Neo4j automatically creates indexes for uniqueness constraints,
// so Pattern.id and Concept.name already have indexes from migration 001.
// The indexes below cover additional query patterns.
//
// All indexes use IF NOT EXISTS for idempotent execution.
//
// Manual application:
//   cypher-shell -u neo4j -p <password> -f src/migrations/neo4j/003_create_indexes.cypher

// =============================================================================
// PROPERTY INDEXES
// =============================================================================

// Pattern lookup by name for display and search-by-name queries.
// Note: Pattern.id already has an index from the uniqueness constraint,
// but name lookups are also common when syncing from PostgreSQL.
CREATE INDEX pattern_name_index IF NOT EXISTS
FOR (p:Pattern) ON (p.name);

// Concept filtering by type (technology, practice, domain).
// Used when querying concepts of a specific category for pattern discovery.
CREATE INDEX concept_type_index IF NOT EXISTS
FOR (c:Concept) ON (c.type);

// =============================================================================
// FULL-TEXT SEARCH INDEXES
// =============================================================================

// Full-text search across pattern name and description fields.
// Enables natural language queries against the knowledge graph.
// Note: Pattern.description may be null; patterns without a description are
// excluded from fulltext search results for description-related queries.
// Usage: CALL db.index.fulltext.queryNodes('pattern_content_fulltext', 'search terms')
CREATE FULLTEXT INDEX pattern_content_fulltext IF NOT EXISTS
FOR (p:Pattern) ON EACH [p.name, p.description];

// Full-text search on concept names.
// Enables fuzzy matching and natural language lookup of concepts.
// Usage: CALL db.index.fulltext.queryNodes('concept_name_fulltext', 'search term')
CREATE FULLTEXT INDEX concept_name_fulltext IF NOT EXISTS
FOR (c:Concept) ON EACH [c.name];

// =============================================================================
// SCHEMA VERSION
// =============================================================================

MERGE (v:SchemaVersion {name: 'mnemonic'})
SET v.version = 3, v.migratedAt = datetime(), v.migration = '003_create_indexes';

// =============================================================================
// ROLLBACK
// =============================================================================
// To reverse this migration, run:
//   DROP INDEX pattern_name_index IF EXISTS;
//   DROP INDEX concept_type_index IF EXISTS;
//   DROP INDEX pattern_content_fulltext IF EXISTS;
//   DROP INDEX concept_name_fulltext IF EXISTS;
//   -- Community Edition (002 skipped): set version back to 1
//   MERGE (v:SchemaVersion {name: 'mnemonic'}) SET v.version = 1, v.migratedAt = datetime();
//   -- Enterprise Edition (002 applied): set version back to 2
//   MERGE (v:SchemaVersion {name: 'mnemonic'}) SET v.version = 2, v.migratedAt = datetime();
```

The services' Cypher looks nodes up by `Pattern.id` and `Concept.name` (the uniqueness constraints' indexes); none of their queries use the full-text indexes today.

### Node Creation Patterns

Only the enricher creates nodes, and only inside `SyncPattern` (see [Graph Synchronization Queries](#graph-synchronization-queries)).

**Pattern Node:**

```cypher
// Create or update pattern node (enricher, after all chunks are enriched)
MERGE (p:Pattern {id: $patternId})
SET p.name = $patternName,
    p.description = $patternDescription,
    p.updatedAt = datetime()
```

`id` is the Postgres pattern UUID as a string. There is no `createdAt` on Pattern nodes.

**Concept Node:**

```cypher
// Create concept if not exists (extracted during enrichment); type follows the latest extraction
MERGE (c:Concept {name: concept.name})
ON CREATE SET c.type = concept.type, c.createdAt = datetime()
ON MATCH SET c.type = concept.type
```

Concept names are lowercased and trimmed by the extraction service before they reach Neo4j.

**Agent Node:** Not implemented. No service creates `Agent` nodes.

### Relationship Definitions

**MENTIONED_IN (Concept to Pattern):** created with `MERGE (c)-[:MENTIONED_IN]->(p)` inside `SyncPattern`; no properties.

**RELATED_TO (Pattern to Pattern):** created with `CREATE` by the RELATED_TO computation inside `SyncPattern`, with properties `similarity` (float, 0.0-1.0) and `updatedAt`. Each sync deletes the pattern's existing `RELATED_TO` edges (both directions) first, so edges are never updated in place. Edges are stored one way and always queried undirected.

**RELEVANT_FOR (Pattern to Agent):** Planned, not implemented. There is no schema for it, no service creates it, and there is no `pattern_agent_associations` table to sync from.

### Graph Synchronization Queries

**Sync Pattern (enricher `graph.Repository.SyncPattern`):**

Called once per pattern, by the job that finds all of the pattern's chunks enriched, after concept extraction. All five statements run in one managed write transaction (`session.ExecuteWrite`), so a pattern's node, concepts and edges change together or not at all. Statement 3 is skipped when extraction returned no concepts.

```cypher
// 1. Create/update pattern node
MERGE (p:Pattern {id: $patternId})
SET p.name = $patternName,
    p.description = $patternDescription,
    p.updatedAt = datetime()

// 2. Remove MENTIONED_IN edges to concepts no longer extracted for this pattern
MATCH (c:Concept)-[r:MENTIONED_IN]->(p:Pattern {id: $patternId})
WHERE NOT c.name IN $conceptNames
DELETE r

// 3. Create concepts and relationships
// $concepts: array of {name: string, type: string}
UNWIND $concepts AS concept
MERGE (c:Concept {name: concept.name})
ON CREATE SET c.type = concept.type, c.createdAt = datetime()
ON MATCH SET c.type = concept.type
WITH c
MATCH (p:Pattern {id: $patternId})
MERGE (c)-[:MENTIONED_IN]->(p)

// 4. Delete existing RELATED_TO edges for this pattern
MATCH (p:Pattern {id: $patternId})-[r:RELATED_TO]-()
DELETE r

// 5. Compute RELATED_TO edges (below)
```

Statement 2 deletes edges, not nodes: a concept that loses its last `MENTIONED_IN` edge this way stays in the graph as an orphan.

**Compute RELATED_TO Edges (statement 5):**

Recomputes pattern-to-pattern similarity edges from shared concepts. Only edges meeting the minimum similarity threshold (`enrichment.related_to_min_similarity`, default 0.3) are created. See [Pattern Processing - RELATED_TO Edge Computation](pattern-processing.md#related_to-edge-computation) for the similarity formula.

```cypher
// Parameters: $patternId (UUID string), $minSimilarity (float, default 0.3)
MATCH (p1:Pattern {id: $patternId})<-[:MENTIONED_IN]-(c:Concept)-[:MENTIONED_IN]->(p2:Pattern)
WHERE p1 <> p2
WITH p1, p2, count(DISTINCT c) AS sharedCount

// Count total concepts for each pattern
OPTIONAL MATCH (c1:Concept)-[:MENTIONED_IN]->(p1)
WITH p1, p2, sharedCount, count(DISTINCT c1) AS totalA
OPTIONAL MATCH (c2:Concept)-[:MENTIONED_IN]->(p2)
WITH p1, p2, sharedCount, totalA, count(DISTINCT c2) AS totalB

// Compute similarity = sharedConcepts / max(totalConceptsA, totalConceptsB)
WITH p1, p2, sharedCount,
     CASE WHEN totalA > totalB THEN totalA ELSE totalB END AS maxTotal
WITH p1, p2, sharedCount,
     CASE WHEN maxTotal = 0 THEN 0.0
          ELSE toFloat(sharedCount) / toFloat(maxTotal)
     END AS similarity
WHERE similarity >= $minSimilarity

// Create RELATED_TO edge (one direction only; queries use undirected traversal)
CREATE (p1)-[:RELATED_TO {similarity: similarity, updatedAt: datetime()}]->(p2)
```

**Find Related Patterns (MCP server, read-only):**

Uses pre-computed RELATED_TO edges and collects shared concept names. The Admin API reads related patterns only inside `GetPatternGraph`, whose query also returns `count(c) AS sharedConcepts`. See [service-layer.md "Resolving the find_related_patterns Data Gap"](service-layer.md#resolving-the-find_related_patterns-data-gap) for the design decision to use pre-computed edges rather than recomputing similarity at query time.

```cypher
MATCH (p1:Pattern {id: $patternId})-[r:RELATED_TO]-(p2:Pattern)
WITH p1, p2, r.similarity AS similarity
OPTIONAL MATCH (p1)<-[:MENTIONED_IN]-(c:Concept)-[:MENTIONED_IN]->(p2)
WITH p2, similarity, collect(c.name) AS conceptNames
ORDER BY similarity DESC
LIMIT $limit
RETURN p2.id AS id, p2.name AS name, similarity, conceptNames
```

**Pattern Concepts (Admin API and MCP server, read-only; the Admin API returns only `c.name`):**

```cypher
MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $patternId})
RETURN c.name AS name, c.type AS type
ORDER BY c.name
```

The Admin API's `GetPatternGraph` runs this and the related-patterns query in one read transaction (related limit 10) for `GET /v1/api/patterns/:id`.

**Delete Pattern (Admin API, best-effort after the Postgres delete):**

```cypher
// Collect the pattern's concepts, detach-delete the pattern,
// then delete only those concepts no other pattern mentions.
MATCH (p:Pattern {id: $patternId})
OPTIONAL MATCH (c:Concept)-[:MENTIONED_IN]->(p)
WITH p, collect(c) AS concepts
DETACH DELETE p
WITH concepts
UNWIND concepts AS c
WITH c WHERE NOT (c)-[:MENTIONED_IN]->()
DELETE c
```

This is the Admin API's only graph write. A failure is logged as a warning and not retried.

**Cleanup Orphaned Concepts (not implemented):**

No service runs a graph-wide sweep. Concepts orphaned by statement 2 of `SyncPattern` remain until something like this is run manually:

```cypher
MATCH (c:Concept)
WHERE NOT (c)-[:MENTIONED_IN]->()
DELETE c
RETURN count(c) AS deletedCount;
```

**Find Patterns for Agent:** Not implemented (depends on `RELEVANT_FOR`).

## Repository Interfaces

[Table of Contents](#table-of-contents)

Each service has its own `internal/repository` packages containing only what it needs; there is no shared repository module. The interfaces below are copied from the code (doc comments trimmed).

| Service          | Packages                                     | PostgreSQL access                                                        | Neo4j access                     |
| ---------------- | -------------------------------------------- | ------------------------------------------------------------------------ | -------------------------------- |
| api service      | `pattern`, `chunk`, `enrichmentjob`, `graph` | Pattern CRUD, chunk insert/list/delete/search, job insert                | Read; `DeletePattern` only write |
| enricher service | `pattern`, `chunk`, `enrichmentjob`, `graph` | Pattern read + status, chunk read + embedding/status, full job lifecycle | `SyncPattern` only               |
| mcp service      | `pattern`, `chunk`, `graph`                  | Read-only                                                                | Read-only                        |

All Postgres repositories take a `repository.DBTX` (the `Exec`/`Query`/`QueryRow` subset of `pgxpool.Pool` and `pgx.Tx`). In the api service, `WithTx` enlists a repository in a caller-owned transaction; pattern create/update use it to write the pattern, chunks and jobs atomically.

```mermaid
flowchart LR
    subgraph API["api service"]
        AP["pattern.Repository"]
        AC["chunk.Repository"]
        AJ["enrichmentjob.Repository"]
        AG["graph.Repository"]
    end
    subgraph ENR["enricher service"]
        EP["pattern.Repository"]
        EC["chunk.Repository"]
        EJ["enrichmentjob.Repository"]
        EG["graph.Repository"]
    end
    subgraph MCP["mcp service"]
        MP["pattern.Repository"]
        MC["chunk.Repository"]
        MG["graph.Repository"]
    end
    T1[("patterns")]
    T2[("pattern_chunks")]
    T3[("enrichment_jobs")]
    G[("Neo4j")]
    AP -->|CRUD| T1
    AC -->|"insert / delete / search"| T2
    AJ -->|insert| T3
    AG -.->|"read + DeletePattern"| G
    EP -->|"read + status"| T1
    EC -->|"read + embedding/status"| T2
    EJ -->|"claim / update / delete"| T3
    EG -->|SyncPattern| G
    MP -.->|read| T1
    MC -.->|search| T2
    MG -.->|read| G
```

### AgentRepository

**Status: planned, not implemented.** No service contains an agent repository; the interface below is the design for the planned `agents` table.

```go
// AgentRepository defines data access operations for agents.
// Agents are stored as JSONB documents with name as the unique lookup key.
type AgentRepository interface {
    // Create stores a new agent. The application computes crc64 from the
    // serialized definition before calling this method.
    // Returns ErrExists if name already exists.
    Create(ctx context.Context, agent *Agent) error

    // Get retrieves an agent by name. Returns ErrNotFound if not found.
    Get(ctx context.Context, name string) (*Agent, error)

    // GetByID retrieves an agent by UUID. Returns ErrNotFound if not found.
    GetByID(ctx context.Context, id uuid.UUID) (*Agent, error)

    // Update modifies an existing agent. Returns ErrNotFound if not found.
    Update(ctx context.Context, agent *Agent) error

    // Delete removes an agent by name. Returns ErrNotFound if not found.
    Delete(ctx context.Context, name string) error

    // List retrieves all agents with optional pagination.
    List(ctx context.Context, opts ListOptions) ([]*Agent, int64, error)

    // GetManifest returns name and crc64 for all agents (used by sync protocol).
    GetManifest(ctx context.Context) ([]ManifestEntry, error)
}

// Agent represents an agent definition stored as a JSONB document.
type Agent struct {
    ID         uuid.UUID       `db:"id"`
    Name       string          `db:"name"`
    Definition json.RawMessage `db:"definition"` // JSONB document
    CRC64      string          `db:"crc64"`       // CRC-64 checksum (decimal string)
    CreatedAt  time.Time       `db:"created_at"`
    UpdatedAt  time.Time       `db:"updated_at"`
}

// ManifestEntry represents a single entity in the sync manifest.
type ManifestEntry struct {
    Name  string `db:"name"`
    CRC64 string `db:"crc64"`
}
```

### PatternRepository

**The api service** (`src/internal/api/repository/pattern`): full CRUD. `Create` inserts with `enrichment_status = 'pending'`; `Update` resets `enrichment_status` to `pending` and clears `enrichment_error` and `enriched_at`; a unique violation on `name` returns `ErrNameExists`.

```go
type Repository interface {
    Create(ctx context.Context, pattern *Pattern) error
    Get(ctx context.Context, id uuid.UUID) (*Pattern, error)
    Update(ctx context.Context, pattern *Pattern) error
    Delete(ctx context.Context, id uuid.UUID) error
    // List leaves Content empty and does not count the total.
    List(ctx context.Context, filter Filter, opts repository.ListOptions) ([]*Pattern, error)
    WithTx(db repository.DBTX) Repository
}

type Pattern struct {
    ID               uuid.UUID  `db:"id"`
    Name             string     `db:"name"`
    Description      *string    `db:"description"`
    Content          string     `db:"content"`
    Tags             []string   `db:"-"` // JSONB array
    EntityType       string     `db:"entity_type"`
    Language         string     `db:"language"`
    Domain           string     `db:"domain"`
    Version          *string    `db:"version"`
    RelatedPatterns  []string   `db:"-"` // JSONB array of pattern names
    EnrichmentStatus string     `db:"enrichment_status"` // pending | enriched | failed
    EnrichmentError  *string    `db:"enrichment_error"`
    EnrichedAt       *time.Time `db:"enriched_at"`
    CreatedAt        time.Time  `db:"created_at"`
    UpdatedAt        time.Time  `db:"updated_at"`
}

type Filter struct {
    Tags             []string // pattern must have ALL of these (tags ?& $n)
    EnrichmentStatus string   // supported by the repository; not exposed by the list endpoint
    SearchQuery      string   // full-text search on name + description
    Language         string
    Domain           string
    EntityType       string
}
```

**The enricher service** (`src/internal/enricher/repository/pattern`):

```go
type Repository interface {
    // Get returns id, name, description and content.
    Get(ctx context.Context, id uuid.UUID) (*Pattern, error)
    // UpdateEnrichmentStatus sets status, error and enriched_at (now() when status is "enriched").
    UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error
}
```

**The mcp service** (`src/internal/mcp/repository/pattern`, read-only):

```go
type Repository interface {
    Get(ctx context.Context, id uuid.UUID) (*Pattern, error)          // fields get_pattern shows
    GetName(ctx context.Context, id uuid.UUID) (string, error)
    GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*Pattern, error) // id, name, tags, language, domain
}
```

There is no pattern-level `FindSimilar`, `UpdateEmbedding` or agent-association method in any service: embeddings and similarity search are on chunks.

### ChunkRepository

**The api service** (`src/internal/api/repository/chunk`):

```go
type Repository interface {
    // CreateBatch inserts all chunks of one pattern with a single INSERT ... SELECT FROM unnest.
    CreateBatch(ctx context.Context, chunks []*Chunk) error
    // ListSummariesByPatternID returns chunks ordered by chunk_index, without content or embedding.
    ListSummariesByPatternID(ctx context.Context, patternID uuid.UUID) ([]*Chunk, error)
    DeleteByPatternID(ctx context.Context, patternID uuid.UUID) error
    FindSimilar(ctx context.Context, embedding []float32, opts SimilarityOptions) ([]*Match, error)
    WithTx(db repository.DBTX) Repository
}

type SimilarityOptions struct {
    MinSimilarity float64
    MaxResults    int         // defaults to 10 when <= 0
    Language      string      // "" disables
    Domain        string      // "" disables
    PatternIDs    []uuid.UUID // empty disables
    Tags          []string    // pattern must contain all; empty disables
}
```

**The enricher service** (`src/internal/enricher/repository/chunk`):

```go
type Repository interface {
    // Get returns the chunk joined with its pattern's name and tags (used to build the embedding input).
    Get(ctx context.Context, id uuid.UUID) (*Chunk, error)
    // MarkEnriched stores the embedding and sets status 'enriched', enriched_at = now().
    MarkEnriched(ctx context.Context, id uuid.UUID, embedding []float32) error
    MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error
    // PatternProgress reports whether any chunk failed and whether all are enriched.
    PatternProgress(ctx context.Context, patternID uuid.UUID) (Progress, error)
}
```

**The mcp service** (`src/internal/mcp/repository/chunk`, read-only): `FindSimilar(ctx, embedding, opts)` with `MinSimilarity`, `MaxResults`, `Language`, `Domain` and `Tags`.

### SkillRepository

**Status: planned, not implemented.** No service contains a skill repository.

```go
// SkillRepository defines data access operations for skills.
// Skills are stored as JSONB documents with name as the unique lookup key.
type SkillRepository interface {
    Create(ctx context.Context, skill *Skill) error
    Get(ctx context.Context, id uuid.UUID) (*Skill, error)
    GetByName(ctx context.Context, name string) (*Skill, error)
    Update(ctx context.Context, skill *Skill) error
    Delete(ctx context.Context, id uuid.UUID) error
    List(ctx context.Context, opts ListOptions) ([]*Skill, int64, error)
    GetManifest(ctx context.Context) ([]ManifestEntry, error)
}

// Skill represents a skill definition stored as a JSONB document.
type Skill struct {
    ID         uuid.UUID       `db:"id"`
    Name       string          `db:"name"`
    Definition json.RawMessage `db:"definition"` // JSONB document
    CRC64      string          `db:"crc64"`       // CRC-64 checksum (decimal string)
    CreatedAt  time.Time       `db:"created_at"`
    UpdatedAt  time.Time       `db:"updated_at"`
}
```

### SkillFileRepository

**Status: planned, not implemented.** No service contains a skill-file repository.

```go
// SkillFileRepository defines data access operations for skill child files.
// Skill files are keyed by (skill_id, path).
type SkillFileRepository interface {
    Create(ctx context.Context, file *SkillFile) error
    Get(ctx context.Context, id uuid.UUID) (*SkillFile, error)
    GetByPath(ctx context.Context, skillID uuid.UUID, path string) (*SkillFile, error)
    Update(ctx context.Context, file *SkillFile) error
    Delete(ctx context.Context, id uuid.UUID) error
    ListBySkill(ctx context.Context, skillID uuid.UUID) ([]*SkillFile, error)
    DeleteBySkill(ctx context.Context, skillID uuid.UUID) error
}

// SkillFile represents a child file for a skill.
type SkillFile struct {
    ID        uuid.UUID `db:"id"`
    SkillID   uuid.UUID `db:"skill_id"`
    Path      string    `db:"path"`
    Content   string    `db:"content"`
    CRC64     string    `db:"crc64"`
    CreatedAt time.Time `db:"created_at"`
    UpdatedAt time.Time `db:"updated_at"`
}
```

### EnrichmentJob Repository

**The api service** (`src/internal/api/repository/enrichmentjob`): creates jobs only.

```go
type Repository interface {
    // CreatePendingForChunks inserts one pending job per chunk (chunk_id set, pattern_id NULL,
    // max_attempts 3) in one INSERT ... SELECT FROM unnest and returns the job IDs in chunk order.
    // A unique violation returns ErrJobAlreadyPending; a check violation ErrInvalidJobTarget.
    CreatePendingForChunks(ctx context.Context, chunkIDs []uuid.UUID) ([]uuid.UUID, error)
    WithTx(db repository.DBTX) Repository
}
```

**The enricher service** (`src/internal/enricher/repository/enrichmentjob`): the whole lifecycle.

```go
type Repository interface {
    // Start moves one job pending -> processing if it is due (message path).
    // Returns ErrUnavailable if the job is not pending or not yet due.
    Start(ctx context.Context, id uuid.UUID) (*Job, error)

    // ClaimDue claims up to limit due pending jobs (retry sweep) using
    // UPDATE ... WHERE id IN (SELECT ... FOR UPDATE SKIP LOCKED).
    // Never-attempted jobs are only claimed once older than grace (2 minutes).
    ClaimDue(ctx context.Context, grace time.Duration, limit int) ([]*Job, error)

    MarkCompleted(ctx context.Context, id uuid.UUID) error

    // MarkFailed increments attempts; reschedules (status pending,
    // scheduled_for = now() + retryDelay) while attempts < max_attempts, else sets failed.
    MarkFailed(ctx context.Context, id uuid.UUID, err error, retryDelay time.Duration) error

    // ReclaimStale returns processing jobs older than timeout to pending
    // (or failed with "job timed out" when out of attempts).
    ReclaimStale(ctx context.Context, timeout time.Duration) (int64, error)

    // DeleteCompleted / DeleteFailed remove jobs past their retention
    // (completed_at for completed jobs, updated_at for failed jobs).
    DeleteCompleted(ctx context.Context, retention time.Duration) (int64, error)
    DeleteFailed(ctx context.Context, retention time.Duration) (int64, error)
}

// Job (enricher view; Start/ClaimDue return id, pattern_id, chunk_id).
type Job struct {
    ID        uuid.UUID
    PatternID *uuid.UUID // set on legacy pattern-level jobs only
    ChunkID   *uuid.UUID // set on chunk jobs (everything the API creates)
    // The table also has status, attempts, max_attempts, last_error,
    // scheduled_for, started_at, completed_at, created_at, updated_at.
}
```

The claim query:

```sql
update enrichment_jobs set
    status = 'processing', started_at = now(), updated_at = now()
where id in (
    select id from enrichment_jobs
    where status = 'pending'
      and ((attempts > 0 and scheduled_for <= now())
           or scheduled_for <= now() - $grace::interval)
    order by scheduled_for
    limit $limit
    for update skip locked
)
returning id, pattern_id, chunk_id;
```

### GraphRepository

**The api service** (`src/internal/api/repository/graph`):

```go
// Writing concepts and RELATED_TO edges is the enricher's job.
type Repository interface {
    // DeletePattern removes a Pattern node and all its relationships, along
    // with any of its concepts that no other pattern mentions.
    DeletePattern(ctx context.Context, patternID uuid.UUID) error
    // GetPatternGraph returns up to relatedLimit related patterns and all
    // concepts for a pattern, using a single session and read transaction.
    GetPatternGraph(ctx context.Context, patternID uuid.UUID, relatedLimit int) (*PatternGraph, error)
}
```

**The enricher service** (`src/internal/enricher/repository/graph`):

```go
type Repository interface {
    // SyncPattern merges the Pattern node, prunes stale MENTIONED_IN edges,
    // merges concepts and MENTIONED_IN edges, and recomputes RELATED_TO
    // (similarity >= minSimilarity), all in one write transaction.
    SyncPattern(ctx context.Context, pattern Pattern, concepts []Concept, minSimilarity float64) error
}

type Pattern struct {
    ID          uuid.UUID
    Name        string
    Description *string
}
```

**The mcp service** (`src/internal/mcp/repository/graph`, read-only; each query runs through `neo4j.ExecuteQuery` with reader routing):

```go
type Repository interface {
    GetPatternConcepts(ctx context.Context, patternID uuid.UUID) ([]Concept, error)
    FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]RelatedPattern, error)
}
```

Shared shapes:

```go
// Concept is a node linked to patterns via MENTIONED_IN.
type Concept struct {
    Name string // normalized lowercase
    Type string // technology, practice, or domain (mcp only)
}

// RelatedPattern is a pattern reached through a RELATED_TO edge.
type RelatedPattern struct {
    ID             uuid.UUID
    Name           string
    SharedConcepts int      // count of shared concepts (api only)
    Similarity     float64  // from the RELATED_TO edge (0.0-1.0)
    ConceptNames   []string // names of the shared concepts
}
```

Graph health is checked by the `/health` handler through the driver's `VerifyConnectivity`, not through a repository method.

### Common Types

`ListOptions` (in the api service's `src/internal/api/repository` package) is the pagination type for pattern listing.

`Filter` types are **not** shared -- each repository package defines its own `Filter` struct with fields specific to that entity. See the Filter definition in [PatternRepository](#patternrepository).

```go
// ListOptions defines pagination parameters.
type ListOptions struct {
    Offset int // Number of items to skip
    Limit  int // Maximum items to return (0 = no limit)
}
```

Repository errors are defined per package, for example `pattern.ErrNotFound` and `pattern.ErrNameExists` (api), `enrichmentjob.ErrJobAlreadyPending` and `enrichmentjob.ErrInvalidJobTarget` (api), and `enrichmentjob.ErrUnavailable` and `enrichmentjob.ErrNotFound` (enricher). Check each package's `errors.go` for the full set.

## Connection Configuration

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Configuration - Database Connections](configuration.md#configuration-file)

### PostgreSQL Connection

**Configuration Structure:**

```go
// PostgresConfig defines PostgreSQL connection parameters.
// Viper handles env var binding via viper.BindEnv() or viper.AutomaticEnv().
type PostgresConfig struct {
    Host            string        `mapstructure:"host"`
    Port            int           `mapstructure:"port"`
    Database        string        `mapstructure:"database"`
    Username        string        `mapstructure:"username"`
    Password        string        `mapstructure:"password"`
    SSLMode         string        `mapstructure:"ssl_mode"`
    MaxOpenConns    int32         `mapstructure:"max_open_conns"`
    MinConns        int32         `mapstructure:"min_conns"`
    ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}
```

Keys live under `database.postgres.*` (env `LIBRARIAN_DATABASE_POSTGRES_*`). Defaults in all three services: `max_open_conns` 25, `min_conns` 5, `conn_max_lifetime` 5m, `ssl_mode` `prefer`. The pool is a `pgxpool.Pool`: `max_open_conns` sets `MaxConns` and `min_conns` sets `MinConns`. Both are `int32`, the type pgxpool takes, so a value above 2147483647 fails config loading.

**Recommended Pool Settings:**

| Setting | Development | Production (Single Pod) | Production (Multi-Pod) |
| --------- | ------------- | ------------------------- | ------------------------ |
| MaxOpenConns | 5 | 25 | 15 per pod |
| MinConns | 2 | 5 | 3 per pod |
| ConnMaxLifetime | 5m | 1h | 1h |

### Neo4j Connection

**Configuration Structure:**

```go
// Neo4jConfig defines Neo4j connection parameters.
// Viper handles env var binding via viper.BindEnv() or viper.AutomaticEnv().
type Neo4jConfig struct {
    URI                          string        `mapstructure:"uri"`
    Username                     string        `mapstructure:"username"`
    Password                     string        `mapstructure:"password"`
    Database                     string        `mapstructure:"database"`
    MaxConnectionPoolSize        int           `mapstructure:"max_connection_pool_size"`
    ConnectionAcquisitionTimeout time.Duration `mapstructure:"connection_acquisition_timeout"`
}
```

Defaults in all three services: `max_connection_pool_size` 50, `connection_acquisition_timeout` 60s, `database` `neo4j`, `uri` `bolt://localhost:17687`.

**Recommended Pool Settings:**

| Setting | Development | Production |
| --------- | ------------- | ------------ |
| MaxConnectionPoolSize | 10 | 50 |
| ConnectionAcquisitionTimeout | 30s | 60s |

### Connection String Formats

**PostgreSQL DSN:**

```text
# Standard format
postgres://username:password@host:port/database?sslmode=prefer

# With all options
postgres://mnemonic:secret@localhost:15432/mnemonic?sslmode=require&connect_timeout=10

# pgx-style (recommended for Go)
host=localhost port=5432 dbname=mnemonic user=mnemonic password=secret sslmode=prefer pool_max_conns=25
```

**Neo4j URI:**

```text
# Bolt protocol (standard)
bolt://localhost:17687

# Bolt with encryption
bolt+s://neo4j.example.com:7687

# Neo4j AuraDB (cloud)
neo4j+s://xxxx.databases.neo4j.io
```

**Environment Variable Examples:**

```bash
# PostgreSQL
export LIBRARIAN_DATABASE_POSTGRES_HOST="localhost"
export LIBRARIAN_DATABASE_POSTGRES_PORT="5432"
export LIBRARIAN_DATABASE_POSTGRES_DATABASE="mnemonic"
export LIBRARIAN_DATABASE_POSTGRES_USERNAME="mnemonic"
export LIBRARIAN_DATABASE_POSTGRES_PASSWORD="secure-password"
export LIBRARIAN_DATABASE_POSTGRES_SSL_MODE="require"

# Neo4j
export LIBRARIAN_DATABASE_NEO4J_URI="bolt://localhost:17687"
export LIBRARIAN_DATABASE_NEO4J_USERNAME="neo4j"
export LIBRARIAN_DATABASE_NEO4J_PASSWORD="secure-password"
export LIBRARIAN_DATABASE_NEO4J_DATABASE="neo4j"
```

## References

[Table of Contents](#table-of-contents)

**Architecture Documents:**

- [Data Architecture](architecture/system/04-data-architecture.md) - Data model design and storage decisions
- [System Architecture](architecture/system/02-system-architecture.md) - Component overview
- [Deployment Architecture](architecture/system/06-deployment-architecture.md) - Deployment patterns

**Design Documents:**

- API Specification (2026-02-15-pivot-api-specification.md, in source repository) - REST and MCP API contracts
- [Configuration](configuration.md) - Server configuration including database settings
- [Pattern Processing](pattern-processing.md) - Enrichment pipeline using this data layer

**External References:**

- [pgx](https://github.com/jackc/pgx) - PostgreSQL driver for Go
- [pgvector](https://github.com/pgvector/pgvector) - Vector similarity search extension
- [neo4j-go-driver](https://github.com/neo4j/neo4j-go-driver) - Neo4j driver for Go
- [Claude Code Agent Skills Spec](https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/skills) - Skill definition format

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
