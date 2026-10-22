---
version: 3
date: 2026-10-22
notes: "Security Architecture row now names the built gateway (Envoy, OPA, Keycloak) instead of \"no auth\"."
---

# Architecture

> Back to [Design Index](../../README.md)

Start here:

1. [What is Mnemonic?](../../concept.md) — the problem, the vision, how it fits together
2. [Requirements](../../requirements.md) — what Mnemonic must do
3. The architecture docs below — how those requirements are realized

## Architecture Documents

| Document                                                       | Description                                                          |
| -------------------------------------------------------------- | -------------------------------------------------------------------- |
| [Architectural Decisions](00-architectural-decisions.md)       | Key decision records (ADR-001 through ADR-009)                       |
| [Security Architecture](01-security-architecture.md)           | Built gateway (Envoy, OPA, Keycloak, egress) and planned authN/authZ |
| [System Architecture](02-system-architecture.md)               | Topology, ports, components and data flow                            |
| [Communication Patterns](03-communication-patterns.md)         | MCP, REST and AMQP patterns, retries and fallbacks                   |
| [Data Architecture](04-data-architecture.md)                   | Database schemas and data management                                 |
| [Database Integration Flow](05-database-integration-flow.md)   | End-to-end database interaction patterns                             |
| [Deployment Architecture](06-deployment-architecture.md)       | Deployment topology and operations                                   |
| [Observability Architecture](07-observability-architecture.md) | Monitoring, logging, and tracing                                     |
| [MCP Tools](../mnemonic/08-mcp-tools.md)                       | Tool discovery, definitions, and response patterns                   |

## Design Documents

Architecture docs describe **what** and **why**. Design docs describe **how** and live in `docs/design/`.

<!-- design docs live in docs/design/ -->

---

Copyright (c) 2025-2026 Jeremy K. Johnson. Licensed under FSL-1.1-ALv2; see [LICENSE](../../../../LICENSE).
