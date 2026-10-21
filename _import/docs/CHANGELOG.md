# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.2] - 2026-10-16

Docs aligned with `mnemonic-api` v0.5.0, `mnemonic-enricher` v0.5.0 and `mnemonic-mcp` v0.6.0 (the second simplification pass).

### Changed

- Version references updated to the current releases (api v0.5.0, enricher v0.5.0, mcp v0.6.0) in the README, security, requirements, traceability and observability pages
- Pages updated for the second simplification pass: `database.postgres.min_conns` replaces `max_idle_conns`, HTTP metrics come only from otelgin, and the enricher takes a `--config` flag

### Fixed

- The deployment monitoring table listed the removed `mnemonic.http.request.*` metrics for api and mcp; it now lists otelgin's `http.server.request.duration`

## [0.5.1] - 2026-10-16

Docs aligned with `mnemonic-api` v0.4.1, `mnemonic-enricher` v0.4.2 and `mnemonic-mcp` v0.5.1.

### Changed

- Version references updated to the current releases (api v0.4.1, enricher v0.4.2, mcp v0.5.1) in the README, security, requirements, traceability and observability pages
- `database.postgres.max_open_conns` and `max_idle_conns` are documented as `int32` in all three services; out-of-range values fail config loading

## [0.5.0] - 2026-10-16

Docs aligned with `mnemonic-api` and `mnemonic-enricher` v0.4.1 and `mnemonic-mcp` v0.5.0.

### Changed

- Version references updated to the current releases (api and enricher v0.4.1, mcp v0.5.0) in the README, security, requirements, traceability and observability pages; a design changelog entry records the MCP protocol change
- The MCP server is documented as speaking MCP protocol 2026-07-28: no `initialize` or sessions, per-request `_meta`, the `MCP-Protocol-Version`/`Mcp-Method`/`Mcp-Name` headers, `server/discover`, the one-hour `tools/list` cache hint, the `-32020` and `-32022` errors, 403 for cross-origin browser requests, and `_meta.traceparent` on tool spans
- Content with no `[//]: pattern` section (or only empty ones) is documented as stored as one whole-content chunk, titled by its first heading or `Content`, so it is enriched and searchable; the "stays `pending` forever" limitation is gone
- Each service is documented as reporting its own name (`mnemonic-api`, `mnemonic-enricher`, `mnemonic-mcp`) as OpenTelemetry `service.name`, `/health` `name`, `/version` `service` and otelgin middleware name; the `OTEL_SERVICE_NAME` workaround and the matching known gap are removed
- Config reference: removed the unused API keys `logging.format`, `logging.include_caller`, `observability.health.*`, `openai.extraction_model`, `openai.max_requests_per_minute` and `queue.provider` (the enricher keeps `openai.extraction_model` and `queue.provider`)
- The 10KB database content CHECK is marked as dropped; the API handler's 100,000-byte cap is the only content limit
- Database schema docs checked against `mnemonic-dbs`: Postgres migrations renumbered to the real `000001`–`000006` with SQL copied from the files, Neo4j Cypher copied from `001`–`003`, column types, constraints and indexes corrected, and the `agents`, `skills`, `skill_files` and `pattern_agent_associations` tables and the `Agent` constraint and `RELEVANT_FOR` index, which do not exist, marked as planned with no schema (ADRs amended, history kept)

## [0.4.0] - 2026-10-15

Full review of the docs against the `develop` code of `mnemonic-api`, `mnemonic-enricher` and `mnemonic-mcp` v0.4.0 and `mnemonic-integration-tests`.

### Added

- README: current topology diagram, a "Current State at a Glance" table (versions, ports, auth, branching, Dependabot) and `mnemonic-integration-tests` in the repository table
- End-to-end trace propagation (API → RabbitMQ `traceparent` → enricher) and MCP request-path sequence diagrams
- Enrichment job lifecycle state diagram, retry sweep (15s, `FOR UPDATE SKIP LOCKED`, 2-minute grace) and maintenance documentation
- Per-service config reference with real keys, defaults and `MNEMONIC_*` variables, including `openai.base_url`
- Metric inventory (`mnemonic.http.*`, `mnemonic.db.*`, `mnemonic.patterns.*`, `mnemonic.enrichment.*`, `mnemonic.mcp.*`, `mnemonic.queue.publish_failures_total`) and the heartbeat v2 `/health` checks with severities
- "Which service uses what" tables and diagrams for PostgreSQL and Neo4j; corrected ER diagram
- Requirements traceability implementation-status table pointing at real code and tests
- REST API response shapes, the `/patterns/{id}/chunks` endpoint and error codes in the managing-patterns guide

### Changed

- MCP docs: three read-only tools with their real parameters (`language`, `domain`; no `agent`), Streamable HTTP at `/mcp` on 8081 (stateless, no auth), graph expansion in `search_patterns`, and Claude Code config using `"type": "http"`
- Chunking documented as API-side at `[//]: pattern` decorators, with per-chunk embedding jobs and graph sync after the last chunk
- Neo4j model corrected to `(Concept)-[:MENTIONED_IN]->(Pattern)` and `RELATED_TO {similarity}` (minimum 0.3)
- Observability docs rewritten for otelx v2 and heartbeat v2 (`utc_date_time`, OpenAI and API RabbitMQ checks at Warning severity); `observability-implementation.md` cut from 2,000 to about 770 lines of accurate content
- Deployment and CI docs: real image names (`mnemonic`, `mnemonic-api`, `mnemonic-enrichment`), `develop` as the trunk, `latest-dev` and `vX.Y.Z-dev` tags, PR builds that never push, Dependabot
- ADRs: amendment notes where decisions changed, history kept
- Versioning: versions come only from git tags (`git describe --tags --abbrev=0`); README version lines and changelogs are updated by hand
- Agents, skills, authentication, reconciliation and other unbuilt features are labelled planned

### Removed

- Hypothetical code listings, config keys, metrics, spans and repository methods that do not exist in the services

### Fixed

- Queue name (`enrichment-jobs`), search thresholds (MCP 0.5, REST 0.7), content limit (100,000 bytes), connection pool defaults, HNSW (not IVFFlat) index, quickstart health checks, and 40 broken links and anchors

## [0.3.0] - 2026-09-16

### Added

- `Makefile` with a self-documenting `help` target and an `analyze` target that runs markdownlint over all tracked Markdown files
- CC-BY-4.0 license

### Changed

- Documentation now records `mnemonic-mcp` as the sole owner of MCP tooling (README and system architecture)
- README: added `mnemonic-patterns` to the repository table and Quick Start directory layout; corrected "five source repositories" to "six"
- README: repointed the vision link from the deleted `PURPOSE.md` to `docs/concept.md`
- README: realigned Markdown tables (MD060) and tagged the directory-tree fence as `text` (MD040)
- Quickstart guide clarified with a clone step and a patterns-loading tip
- `.gitignore`: added `**/code-reviews/` and `**/code_reviews/` so local review artifacts are not committed

### Removed

- `PURPOSE.md` — its GOAL section was already stated in the README, and its PROBLEM section had gone stale, describing the repository split as still in progress
- Planning artifacts: `docs/plans/` and `plans/stale-docs-cleanup/`
- `docs/code_reviews/` taken out of version control; the file remains on disk

### Fixed

- Five broken relative links left by the reorganization into `system/`, `dbs/`, and `mnemonic/` subfolders: two in `docs/design/data-storage.md`, three in `docs/design/mcp-server.md`

## [0.2.0] - 2026-03-26

Large rewrite for MVP 2 decomposed three-service topology.

### Added

- Admin UI design documentation: screen map, use cases, and tech stack justification
- Guides: Introduction, Concepts, Quickstart, and Authoring Patterns
- Pattern file schema reference documentation
- Glossary of key terms used across solution
- Amendment notes to ADR-002 and ADR-004 reflecting MVP 2 decomposition

### Changed

**System Architecture (for three-service topology):**

- System architecture rewritten for decomposed MCP, Admin API, and Enricher services
- Deployment architecture rewritten for multi-service topology
- Observability architecture updated for three independent services
- Security architecture updated for three-service model
- Database integration flow redesigned for service-to-database communication
- Communication patterns documented for service interactions
- Dbs architecture updated to reference all three consuming services

**Service Design:**

- Enrichment worker redesigned as standalone RabbitMQ-based service
- Configuration design rewritten for per-service configs with Viper and environment variables
- Service layer design updated for three-service codebase split

**Data & Observability:**

- Data architecture updated for decomposed service diagram and enrichment flow
- Embedding model references updated from text-embedding-3-small/1536 to text-embedding-3-large/2000
- Default search threshold corrected from 0.7 to 0.5

**Documentation Structure:**

- Cross-references fixed for consolidated documentation structure
- Back-links standardized across all architecture documents
- Copyright footers standardized
- Table of contents added to design documents
- Reading order and context added to design docs index

### Fixed

- Factual errors: typos, stale embedding dimensions, deprecated index names, macOS paths
- Data-storage migration section updated to reflect Docker entrypoint approach (not migration tool)
- Pre-pivot routing engine references removed from observability design
- Duplicate concept.md diagram removed (02-concept-local-iteration.md is canonical)
- Markdown table alignment issues corrected
- Broken cross-references and anchors throughout documentation
- Stale imports and monolith language references

## [0.1.0] - 2026-03-23

### Added

- Initial consolidation of architecture, design, requirements, and reference documentation from source repositories
- Architecture documentation: system, databases, MCP server, and admin UI design
- Design documentation: configuration, data storage, enrichment flow, observability, pattern processing, and service layers
- Requirements and requirements traceability documentation
- Diagrams documenting MVP evolution (1-5) and system flow patterns
- .gitignore file

[Unreleased]: https://github.com/twistingmercury/librarian-docs/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/twistingmercury/librarian-docs/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/twistingmercury/librarian-docs/releases/tag/v0.4.0
[0.3.0]: https://github.com/twistingmercury/librarian-docs/releases/tag/v0.3.0
[0.2.0]: https://github.com/twistingmercury/librarian-docs/releases/tag/v0.2.0
[0.1.0]: https://github.com/twistingmercury/librarian-docs/releases/tag/v0.1.0
