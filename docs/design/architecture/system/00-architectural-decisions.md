---
version: 1
date: 2026-10-21
notes: "Imported from librarian-docs; service references updated for the monorepo, ADR history kept; back-link repointed."
---

# Architectural Decisions

> Back to [System Architecture](README.md) | [Design Index](../../README.md)

## Table of Contents

- [Decision Record Format](#decision-record-format)
- [ADR-001: Team Knowledge Graph](#adr-001-team-knowledge-graph) (ACTIVE)
- [ADR-002: MCP Protocol for Claude Code Integration](#adr-002-mcp-protocol-for-claude-code-integration) (ACTIVE)
- [ADR-003: Entity Storage with JSONB Document Model](#adr-003-entity-storage-with-jsonb-document-model) (ACTIVE)
- [ADR-004: Pattern Storage and Enrichment Pipeline](#adr-004-pattern-storage-and-enrichment-pipeline) (ACTIVE)
- [ADR-005: Open Vocabulary for language and domain Fields](#adr-005-open-vocabulary-for-language-and-domain-fields) (SUPERSEDED by ADR-007)
- [ADR-006: 204 No Content on Full-Replacement PUT](#adr-006-204-no-content-on-full-replacement-put) (ACTIVE)
- [ADR-007: Config-Driven Vocabulary Enforcement at the Handler Layer](#adr-007-config-driven-vocabulary-enforcement-at-the-handler-layer) (ACTIVE)
- [ADR-008: RabbitMQ for Enrichment Job Dispatch](#adr-008-rabbitmq-for-enrichment-job-dispatch) (ACTIVE)
- [ADR-009: Best-Effort Publish with PostgreSQL as Recovery Source](#adr-009-best-effort-publish-with-postgresql-as-recovery-source) (ACTIVE)
- [Decision Summary](#decision-summary)

## Decision Record Format

Each architectural decision follows this structure:

- **Context**: The situation and forces at play
- **Decision**: What we decided to do
- **Consequences**: The results of the decision, both positive and negative

## <a id="adr-001"></a>ADR-001: Team Knowledge Graph

**Date:** 2026-02-14
**Status:** Accepted

### Context

Teams using Claude Code develop inconsistent tooling over time. Each developer accumulates their own set of agents and skills at different versions, with no shared baseline. Alongside this, knowledge silos form — patterns and approaches discovered by one team member don't reliably reach others. The result is duplicated effort, divergent practices, and no persistent team memory.

Claude Code already handles orchestration well. The real problem is knowledge isolation.

### Decision

**Mnemonic is a team knowledge graph service.**

Core capabilities:

1. **Team knowledge graph**: Curated patterns with semantic search (PGVector) and knowledge relationships (Neo4j)
2. **User is the orchestrator**: Mnemonic provides memory; the user decides workflow

### Consequences

**Positive:**

- Architecture is focused on the actual team pain point: knowledge silos
- MCP protocol provides natural Claude Code integration without a separate CLI
- Manual orchestration is intentional and supported by team knowledge
- Single server with two listeners simplifies deployment

**Negative:**

- Pattern_agent_associations table retained to support "which agents use this pattern" queries
- Users must understand that Mnemonic provides knowledge, not routing decisions

> **Amendment (2026-10-15):** "Single server with two listeners" was superseded by the service decomposition in [ADR-008](#adr-008): the mcp, api and enricher services are separate services. None of the current services (v0.4.0) reads or writes agent associations; the services manage patterns only. The `pattern_agent_associations` table was never created: `db/` has no agent or skill tables.

## <a id="adr-002"></a>ADR-002: MCP Protocol for Claude Code Integration

**Date:** 2026-02-15
**Status:** Accepted

### Context

Mnemonic needs a way to expose its knowledge graph to Claude Code sessions. The integration must support read-only access — Claude Code searches for patterns, but does not write to Mnemonic directly. Admin operations (data loading, updates) use a separate REST API.

Options considered:

1. **MCP protocol**: Leverage Claude Code's native Model Context Protocol support
2. **Claude Code skills + shell scripts**: Wrapper scripts calling a REST API

Key considerations:

- Seamless Claude Code integration without a separate CLI repository
- Native protocol support vs. custom integration
- Read-only access pattern for Claude Code
- Admin operations still need a programmatic interface

### Decision

> **Amended by ADR-008:** The single-server topology described below was superseded in MVP 2. The MCP server and Admin API are now separate services (the mcp and api services), and the enricher is a standalone service (the enricher service). They started in separate repositories and now build from one monorepo. The core MCP protocol decisions (Streamable HTTP, read-only tools, three tool registrations) remain in effect.

**Use MCP (Model Context Protocol) over Streamable HTTP for Claude Code integration.**

Architecture:

- **MCP Server** (`:8081`): Read-only pattern search for Claude Code (3 tools)
- **Admin REST API** (`:8080`): Write operations for data loading
- **Single server**: Two HTTP listeners on one Go server

MCP tools (3 pattern search tools):

- `search_patterns`: Semantic search over team knowledge graph
- `find_related_patterns`: Find patterns related to a given pattern
- `get_pattern`: Retrieve specific pattern by ID

### Consequences

> **Note:** Consequences marked with ~~strikethrough~~ are superseded by the service decomposition in ADR-008.

**Positive:**

- No separate CLI repository needed
- Native Claude Code integration via MCP
- Read-only MCP pattern keeps Claude Code safe
- Admin REST API for data loading via curl/scripts
- ~~Single server simplifies deployment~~

**Negative:**

- Must implement MCP server in Go (new protocol support)
- Two protocol surfaces to maintain (REST admin + MCP read-only)
- MCP is less mature than REST for debugging and tooling

## <a id="adr-003"></a>ADR-003: Entity Storage with JSONB Document Model

**Date:** 2026-02-15
**Status:** Accepted

### Context

Agents, skills, and skill files are first-class entities in Mnemonic (see [ADR-001](#adr-001)). Each entity type has a different internal schema: agents have system prompts and allowed tools; skills have content and child files. Traditional column-per-field modeling creates tight coupling between Go structs and database schema, requiring migrations for every field change.

### Decision

> **Amendment (2026-10-15):** Not implemented in the current services. As of v0.4.0, the api, mcp and enricher services have no agent, skill or skill-file entities or endpoints; patterns are the only resource, and the database schema in `db/` has no `agents`, `skills` or `skill_files` tables. This record is kept for history and as the design to follow if entity storage returns.

**Use a JSONB document model for entity definitions.** Each entity table follows a common pattern:

| Column       | Type            | Purpose                                                                |
| ------------ | --------------- | ---------------------------------------------------------------------- |
| `id`         | UUID PK         | Stable reference (entities may be renamed)                             |
| `name`       | VARCHAR UNIQUE  | Human-readable identifier, URL-safe (`^[a-z]([a-z0-9](-[a-z0-9])*)*$`) |
| `definition` | JSONB NOT NULL  | Complete entity document (all fields)                                  |
| `crc64`      | BIGINT NOT NULL | CRC-64/ISO checksum for change detection                               |
| `created_at` | TIMESTAMPTZ     | Row creation time                                                      |
| `updated_at` | TIMESTAMPTZ     | Last modification time                                                 |

**Name conventions:**

- Agents: max 64 chars, `^[a-z]([a-z0-9](-[a-z0-9])*)*$`
- Skills: max 64 chars, `^[a-z]([a-z0-9](-[a-z0-9])*)*$`

This pattern disallows consecutive hyphens and trailing hyphens, ensuring clean filesystem-compatible names.

**CRC-64 change detection:** Computed in Go via `hash/crc64` with ISO polynomial. Stored as BIGINT. Enables efficient diff without comparing full JSONB documents.

**Skill files** use a variation: `skill_id` FK with CASCADE delete, plus `path` (VARCHAR(1024)) for file identification within the skill directory. The file body is stored in a `content` TEXT column with a `crc64` VARCHAR(20) checksum for change detection.

### Consequences

**Positive:**

- Schema-agnostic: new fields in definitions require no migration
- Single source of truth: Go struct marshals directly to JSONB
- Efficient sync: CRC-64 comparison avoids deep document diffs

**Negative:**

- No column-level database constraints on definition content (application validates)
- Queries into definition fields require JSONB operators (acceptable; list queries use `name` column)
- Future option: pg_jsonschema extension for database-level JSONB validation if needed

## <a id="adr-004"></a>ADR-004: Pattern Storage and Enrichment Pipeline

**Date:** 2026-02-15
**Status:** Accepted

### Context

Patterns are the core knowledge artifacts in Mnemonic. Unlike entities (agents, skills) which use JSONB documents, patterns require relational columns for vector search, enrichment tracking, and graph relationships. The enrichment pipeline must generate embeddings via an external API (OpenAI text-embedding-3-large) and extract entities for the Neo4j knowledge graph.

### Decision

> **Amended by ADR-008 and ADR-009:** Enrichment dispatch now uses RabbitMQ as the primary queue. The Postgres-backed `enrichment_jobs` table with `FOR UPDATE SKIP LOCKED` remains as a recovery mechanism for jobs that fail to publish to RabbitMQ. The pattern storage design (relational columns, two-phase enrichment) remains in effect.
>
> **Amendment (2026-10-15), chunked enrichment:** Embeddings are now stored per chunk in `pattern_chunks`, not in a single pattern-level `embedding` column. The api service splits content into chunks (one per `[//]: pattern` section) and creates one enrichment job per chunk in the same transaction as the pattern. The enricher embeds each chunk; only when the pattern's last chunk is embedded does it extract concepts and sync Neo4j, then mark the pattern `enriched`. Retries use a fixed delay (`enrichment.retry_delay`, default 30s) up to the job's `max_attempts` (default 3), not exponential backoff, and are run by the enricher's 15-second retry sweep. The API handler accepts content up to 100000 bytes; the database has no content-length CHECK (`db/` migration 000005 dropped the 10KB one).

**Patterns use relational columns (not JSONB) with async enrichment via a Postgres-backed job queue.**

**Pattern table design:**

- UUID primary key (patterns may be renamed; need stable reference)
- Separate `embedding` column (vector(2000)) for PGVector similarity search
- Enrichment status tracking (`pending` / `enriched` / `failed`) with error capture
- JSONB `tags` column for flexible categorization

**Enrichment pipeline:**

- Postgres-backed queue using `FOR UPDATE SKIP LOCKED` for safe concurrent processing
- Retry with exponential backoff for transient API failures (max 3 attempts)
- CASCADE delete: enrichment jobs auto-cleaned when pattern deleted
- Two-phase enrichment: (1) generate embedding via OpenAI API, (2) extract entities and create Neo4j relationships

**Why relational columns for patterns (not JSONB):**

- Vector similarity search requires a dedicated `embedding` column for PGVector indexing
- Enrichment status must be queryable for job processing (`WHERE enrichment_status = 'pending'`)
- ~~Content size constraint (10KB max) enforced at database level via CHECK~~ (dropped by migration 000005 in `db/postgres/`; the API handler caps content at 100000 bytes)

### Consequences

**Positive:**

- PGVector index operates directly on the embedding column (no JSONB extraction)
- ~~Enrichment queue requires no external message broker~~ (superseded by ADR-008: RabbitMQ is now the primary dispatch)
- Concurrent enrichment workers are safe via `SKIP LOCKED` (and, since the RabbitMQ change, a conditional job start)
- Pattern deletion cascades to jobs (no orphans)

**Negative:**

- Mnemonic calls an external embedding API (OpenAI) — adds an external dependency
- Async enrichment means patterns are not immediately searchable after creation
- Two databases to keep in sync (Postgres source of truth, Neo4j projection)

## Decision Summary

| Decision | Choice                               | Rationale                                                                                      | Status     |
| -------- | ------------------------------------ | ---------------------------------------------------------------------------------------------- | ---------- |
| ADR-001  | Team knowledge graph                 | Solve knowledge isolation silos                                                                | ACTIVE     |
| ADR-002  | MCP protocol integration             | Native Claude Code integration, dual protocol architecture                                     | ACTIVE     |
| ADR-003  | JSONB document model                 | Schema-agnostic entity storage, CRC-64 change detection (no entities implemented as of v0.4.0) | ACTIVE     |
| ADR-004  | Pattern storage + enrichment         | Relational columns for vectors, Postgres-backed async queue                                    | ACTIVE     |
| ADR-005  | Open vocabulary for language/domain  | Kebab-case format only; vocabulary governed externally                                         | SUPERSEDED |
| ADR-006  | 204 No Content on PUT                | Full-replacement PUT with no body; client issues GET if needed                                 | ACTIVE     |
| ADR-007  | Config-driven vocabulary enforcement | Allow-lists in config; handler enforces; empty list = open                                     | ACTIVE     |
| ADR-008  | RabbitMQ for enrichment dispatch     | Decouple enricher from API server; best-effort publish with PG safety net                      | ACTIVE     |
| ADR-009  | Best-effort publish                  | Failed publish logs warning; PG `pending` status is recovery source                            | ACTIVE     |

## <a id="adr-005"></a>ADR-005: Open Vocabulary for language and domain Fields

**Date:** 2026-03-09
**Status:** Superseded by [ADR-007](#adr-007)

### Context

Pattern records carry `language` and `domain` fields that classify content by programming language (e.g., `go`, `typescript`) and problem domain (e.g., `backend`, `data-engineering`). The original design enforced a closed enum at both the OpenAPI spec level and in the Go handler. This created friction: adding a new language required an API release and handler change.

Two alternatives were considered:

1. Keep the closed enum in the API; extend it via migration and handler change on each new value.
2. Open the API to any structurally valid identifier; govern the approved vocabulary externally.

The approved vocabulary is already managed in the `mnemonic-patterns` repository (`config/validate.yaml`), which is the authoritative source for all pattern content pushed to Mnemonic. That repo's validation tooling runs before patterns reach the Admin REST API.

### Decision

**Remove enum enforcement from the API. Validate format only (kebab-case, max 64 characters). Vocabulary governance belongs to `mnemonic-patterns/config/validate.yaml`.**

The boundary is:

- **API layer** (`patterns.go`): structural validation — `^[a-z][a-z0-9-]*$`, length limit. Rejects malformed strings; accepts any well-formed identifier.
- **Sync tooling layer** (`mnemonic-patterns`): semantic validation — checks submitted values against the approved vocabulary before calling the Admin API.

This design treats the Admin REST API as an internal write interface used primarily by the sync tooling. Direct REST callers are out of scope for vocabulary enforcement; they are expected to respect the vocabulary by convention.

### Consequences

**Positive:**

- Adding a new approved language or domain requires only a change to `mnemonic-patterns/config/validate.yaml`, not an API release.
- API is stable; format validation is unchanged across vocabulary expansions.
- Handler logic is simpler (one regex, no enum list to maintain).

**Negative:**

- A direct REST caller that bypasses sync tooling can store out-of-vocabulary values without a 400 response. Discovery occurs at query time (filter returns no results) or at audit time.
- The governance boundary is implicit. Team members must know to consult `mnemonic-patterns` for the approved vocabulary; the API spec does not surface it.
- Single-character identifiers (e.g., `"a"`) pass format validation. This is harmless if sync tooling is the gatekeeper, but worth noting.

## <a id="adr-006"></a>ADR-006: 204 No Content on Full-Replacement PUT

**Date:** 2026-03-09
**Status:** Accepted

### Context

All PUT handlers in Mnemonic perform full replacement: the client submits a complete representation, the server overwrites the record, and the client is responsible for maintaining its own local copy. The question is whether a successful PUT should return 200 with the persisted representation or 204 with no body.

Returning 200 with a body is defensible when the server transforms the input — generating fields, normalising values, or applying side effects the client cannot predict. In Mnemonic, PUT handlers do not transform input beyond what the service layer stores verbatim. The only server-generated field that changes on update is `updated_at`, which the client can retrieve via a subsequent GET if needed.

RFC 9110 section 9.3.4 permits 204 when the server has no representation to return.

### Decision

**All PUT handlers return 204 No Content with no body.**

Affected endpoints:

- `PUT /v1/api/patterns/:id`
- `PUT /v1/api/agents/:name`
- `PUT /v1/api/skills/:name`
- `PUT /v1/api/skills/:name/scripts/:filename`
- `PUT /v1/api/skills/:name/references/:filename`
- `PUT /v1/api/skills/:name/assets/:filename`
- `PUT /v1/api/patterns/:id/agents`

> **Amendment (2026-10-15):** Only `PUT /v1/api/patterns/:id` exists in the api service v0.4.0, and it returns 204 No Content. The agent and skill endpoints listed above are not implemented.

Clients that need the updated representation after a PUT must issue a subsequent GET. This is consistent with the sync use case: the sync tooling already holds the canonical payload and does not need the server to echo it back.

### Consequences

**Positive:**

- Correct per RFC 9110; avoids misleading clients that no server-side transformation occurred.
- Reduces response payload size for batch sync operations.
- Consistent across all PUT endpoints; no per-endpoint special cases.

**Negative:**

- Clients that need `updated_at` or any other server-written field after a PUT must issue a second request. This is an acceptable trade-off given the sync-tooling primary use case.

## <a id="adr-007"></a>ADR-007: Config-Driven Vocabulary Enforcement at the Handler Layer

**Date:** 2026-03-10
**Status:** Accepted
**Supersedes:** [ADR-005](#adr-005)

### Context

ADR-005 moved vocabulary governance out of the API layer and into the `mnemonic-patterns` sync tooling. Under that model, the Admin REST API validated only format (kebab-case, max 64 chars) and accepted any well-formed identifier.

In practice, the sync tooling is not the only write path. Direct REST callers — operators loading patterns via curl, custom scripts, or alternative tooling — bypass the `mnemonic-patterns` validation layer entirely. Out-of-vocabulary values written this way are silent: they produce a 202 response, persist in Postgres, and are discovered only when filter queries return unexpected results.

Additionally, operators want to constrain deployments to their team's supported language and domain set without forking the sync tooling. A config-file mechanism is the natural operator affordance for this.

### Decision

**Enforce vocabulary allow-lists at the handler layer, loaded from `VocabularyConfig` in the server config.**

- `VocabularyConfig{Languages []string, Domains []string}` is defined in the `config` package, loaded by Viper from `config.yaml` or environment variables (`LIBRARIAN_VOCABULARY_LANGUAGES`, `LIBRARIAN_VOCABULARY_DOMAINS`).
- At startup, `VocabularyConfig.validate()` rejects empty lists. The server will not start without a non-empty vocabulary.
- `Handler` receives the vocabulary at construction time via `New(patternSvc, searchSvc, vocab)`. No global state.
- `validatePatternFields` checks `language` and `domain` against `allowedLanguages` and `allowedDomains` after format validation. A non-matching value returns `INVALID_VALUE` (400).
- If `allowedLanguages` or `allowedDomains` is empty at runtime (not possible via normal config loading, but possible programmatically in tests), the check is skipped — any well-formed value passes.
- Defaults ship with 38 languages and 10 domains in `defaults.go`.

> **Amendment (2026-10-15):** The defaults now ship 42 languages and 14 domains (`DefaultVocabularyLanguages` and `DefaultVocabularyDomains` in the api service (`src/internal/api/config/defaults.go`)). The runtime bypass was removed: `validatePatternFields` always checks membership, so an empty list rejects every value, and startup still requires both lists to be non-empty. This resolves the F-04 tension noted below in favor of keeping the startup check.

### Consequences

**Positive:**

- Out-of-vocabulary values are rejected at the API boundary regardless of write path. No silent persistence of invalid data.
- Vocabulary is operator-configurable without a code change or redeploy (env var override).
- Adding or removing values requires only a config change and server restart, not an API release.
- The `INVALID_VALUE` error code gives callers an actionable signal distinct from `INVALID_FORMAT`.

**Negative:**

- Vocabulary is static for the lifetime of the server process. Operators must restart to activate changes.
- `config.yaml` and `defaults.go` must be kept in sync; divergence creates a confusing operator experience (see review finding F-03 in `review-cycles-7-12-vocabulary.md`).
- The startup non-empty validation and the runtime `len > 0` bypass are in tension: open vocabulary is not a reachable mode via normal config loading. This should be resolved by either removing the bypass or removing the startup check (see F-04 in the review).
- Vocabulary is only enforced on pattern `language` and `domain`. Other resource types do not have configurable vocabulary, creating an asymmetry that must be extended if other types gain these fields.

## <a id="adr-008"></a>ADR-008: RabbitMQ for Enrichment Job Dispatch

**Date:** 2026-03-23
**Status:** Accepted

### Context

Prior to Phase 2, pattern enrichment was driven by an inline polling worker: a goroutine started at server boot that polled the `enrichment_jobs` table with `FOR UPDATE SKIP LOCKED` and processed jobs synchronously. This kept the stack simple (no broker) but coupled enrichment throughput to the API server's goroutine pool and complicated horizontal scaling — multiple API replicas would compete for the same poll lock.

The goal for Phase 2 is to decouple the enrichment worker into a standalone enricher service that can scale independently. A message broker is the natural coordination mechanism.

### Decision

**Replace the inline polling worker with RabbitMQ-backed job dispatch.**

After a pattern create or update, the service layer writes an `enrichment_jobs` row to PostgreSQL (durable, transactional) and then publishes `{"job_id": "<uuid>"}` to the `enrichment-jobs` queue (best-effort). The enricher service consumes the queue and reads job details from PostgreSQL by ID.

Two properties were non-negotiable:

1. A broker failure must not fail a pattern write — publishing is best-effort; the DB write is the commit point.
2. Jobs written to PostgreSQL must never be silently lost — the DB row is always the recovery source.

RabbitMQ was chosen over alternatives (Redis Streams, Postgres LISTEN/NOTIFY, SQS) because the existing infrastructure already targets RabbitMQ and the durability semantics (persistent delivery mode, durable queue) match the requirement.

### Consequences

**Positive:**

- The enricher service scales independently of the API server.
- Pattern writes are never blocked on enrichment throughput.
- PostgreSQL remains the authoritative record; the queue is a delivery hint, not a source of truth.
- The `pending` status on an unpublished job is a natural trigger for future recovery tooling.

> **Amendment (2026-10-15):** The sweep now exists. The enricher service runs a retry sweep every 15 seconds that claims, with `FOR UPDATE SKIP LOCKED`, failed jobs whose retry is due and never-attempted jobs pending for more than 2 minutes (the grace period for an unpublished or undelivered message). Job start is conditional (`pending` and due), so a message for a job the sweep already took is acked without work. Publishes run in a producer span and carry W3C `traceparent` in the AMQP headers; the enricher continues that trace. Concurrent publishes share one channel guarded by a mutex, and a failed publish triggers a single shared reconnect.

**Negative:**

- A failed publish leaves the job in `pending` state with no automatic retry. Recovery requires operator intervention or a separate sweep process (not implemented in Phase 2). *(Superseded: see the amendment above.)*
- The broker is a new runtime dependency; API startup fails if RabbitMQ is unavailable at `wireDependencies` time (see ADR-009 for implications).
- A single AMQP channel is used for all publishes. This is safe under the current sequential, single-service model but will need revisiting if publish concurrency increases.

## <a id="adr-009"></a>ADR-009: Best-Effort Publish with PostgreSQL as Recovery Source

**Date:** 2026-03-23
**Status:** Accepted

### Context

The dual-write sequence in Phase 2 is: (1) write `enrichment_jobs` row to PostgreSQL, (2) publish job ID to RabbitMQ. These two operations cannot be made atomic with standard tooling — PostgreSQL transactions and AMQP channels do not share a two-phase commit coordinator.

Three strategies were considered:

1. **Fail the HTTP request on publish failure.** Consistent from the API's perspective, but undesirable: a transient broker outage would degrade the write API, and the pattern row is already committed.
2. **Transactional outbox pattern.** A background process reads an outbox table and publishes, guaranteeing at-least-once delivery. Correct, but adds a polling worker — the same complexity Phase 2 is trying to eliminate.
3. **Best-effort publish with DB as safety net.** Publish after commit; log and ignore errors. Jobs that miss their publish remain `pending` in PostgreSQL and can be swept later.

### Decision

> **Amendment (2026-10-15):** The "future sweep process" below is implemented in the enricher service rather than as a republisher: every 15 seconds it claims pending jobs directly from PostgreSQL (`FOR UPDATE SKIP LOCKED`), taking never-attempted jobs once they are 2 minutes old, and runs them without going through RabbitMQ. Publishing now happens only after the pattern, chunk and job rows commit in one transaction. The reconnect wait is a context-aware `select` held without the publisher lock, and a publish that still fails increments `mnemonic.queue.publish_failures_total`. API `/health` reports a disconnected publisher as a Warning.

**Publish is best-effort. A failed publish logs a warning and is not propagated to the caller.**

The `patternService.publishJob` method calls `publisher.Publish` and discards errors, logging them as warnings with the job ID. The pattern response is returned to the client regardless of publish outcome.

Recovery relies on the `pending` status in the `enrichment_jobs` table. A future sweep process (not in Phase 2 scope) can query `WHERE status = 'pending' AND created_at < NOW() - INTERVAL '5 minutes'` and republish.

### Consequences

**Positive:**

- Pattern write reliability is independent of broker availability.
- Recovery path is well-defined: `pending` rows are self-describing and can be swept at any time.
- No new polling goroutine or outbox table required in Phase 2.

**Negative:**

- Without an active sweep process, stuck `pending` jobs accumulate silently. Operators need alerting on pending job age (not implemented in Phase 2).
- The reconnect-on-publish path introduces a synchronous `time.Sleep(ReconnectDelay)` (default 5 s) inside the HTTP handler goroutine. Under broker degradation, API write latency spikes by up to 5 s per request. This is bounded by the single-retry design but is a known latency risk.
- No publisher confirms are used. The broker can accept the message and lose it before routing (e.g., if the exchange has no binding). This is acceptable given the DB safety net, but must be understood by operators.

<!-- design docs live in docs/design/ at the monorepo root -->

**Next:** [System Architecture](02-system-architecture.md)

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
