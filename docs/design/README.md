---
version: 2
date: 2026-10-21
notes: "Renamed to Mnemonic (environment variables, image names, ports, links); license footer corrected to FSL-1.1-ALv2."
---

# Design Documents

Implementation-level design documents covering cross-cutting concerns shared by the Go services (the mcp, api and enricher services under `src/`). These docs describe _how_ things are built. For _what_ and _why_, see the [architecture documents](architecture/system/README.md).

All design docs assume familiarity with the [System Architecture](architecture/system/README.md) and [Requirements](requirements.md).

## Recommended Reading Order

If you are new to the codebase, read the design docs in this order — each builds on concepts introduced by the previous:

1. **[Configuration](configuration.md)** — How services are configured (Viper layering, environment variables, config files)
2. **[Data Storage](data-storage.md)** — Database schema, migrations, JSONB model, PGVector indexes, Neo4j schema, repository interfaces
3. **[MCP Server](mcp-server.md)** — How the 3 read-only MCP tools are served (stateless Streamable HTTP at `:8081/mcp`), validated, formatted, and instrumented
4. **[Service Layer](service-layer.md)** — Per-service package layout and interfaces, transaction boundaries, cross-database coordination, error mapping
5. **[Pattern Processing](pattern-processing.md)** — Enrichment pipeline: chunking, embeddings, entity extraction, Neo4j relationships, job dispatch, retry sweep and job lifecycle
6. **[Observability Implementation](observability-implementation.md)** — OpenTelemetry integration, tracing, metrics, structured logging
7. **[Design Changelog](design-changelog.md)** — History of design decisions and how the design has evolved

## Document Reference

| Document                                                        | Covers                                                                                                                           |
| --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| [Configuration](configuration.md)                               | Layered configuration via Viper: defaults, config files, environment variables                                                   |
| [Data Storage](data-storage.md)                                 | JSONB document model, PostgreSQL migrations, PGVector indexes, Neo4j schema, repository interfaces                               |
| [MCP Server](mcp-server.md)                                     | MCP server setup, stateless Streamable HTTP handler, the 3 tools (inputs, limits, response formats), error handling, telemetry   |
| [Service Layer](service-layer.md)                               | Service and repository interfaces per service, dependency wiring, transactions, error mapping                                    |
| [Pattern Processing](pattern-processing.md)                     | Enrichment pipeline: chunking, embeddings, entity extraction, Neo4j relationships, RabbitMQ dispatch, retry sweep, job lifecycle |
| [Observability Implementation](observability-implementation.md) | OpenTelemetry integration, tracing, metrics, structured logging via zerolog                                                      |
| [Design Changelog](design-changelog.md)                         | History of design decisions and iterations                                                                                       |

## Other Documents

- [What Is Mnemonic?](concept.md) and [Requirements](requirements.md), with the [Requirements Traceability Matrix](requirements-traceability.md)
- [Architecture](architecture/system/README.md): system, communication, data, security, deployment and observability; database images under [architecture/dbs](architecture/dbs/README.md); MCP tools under [architecture/mnemonic](architecture/mnemonic/README.md)
- [Diagrams](diagrams/01-concept-local-iteration.md): concept diagrams for each iteration, plus the POST activity diagram
- [Glossary](reference/glossary.md) and [Pattern File Schema](reference/pattern-schema.md)
- [From Coding Patterns to Any Knowledge](mnemonic_from_coding_patterns_to_any_knowledge.md): design direction for generalizing beyond coding patterns

---

Copyright (c) 2025-2026 Jeremy K. Johnson. Licensed under FSL-1.1-ALv2; see [LICENSE](../../LICENSE).
