# MVP 2

> Back to [Project README](../../README.md)

Iteration 2 decomposes the local runtime and introduces async enrichment:

1. Split MCP server, Admin API, and enricher into separate local services.
2. Add RabbitMQ for enrichment jobs published by the Admin API.
3. Keep MCP read-only and verify enrichment updates PostgreSQL + PGVector and Neo4j.

```mermaid
---
config:
  theme: redux
  layout: dagre
---
flowchart TB
  claude["Claude Code"] -->|MCP| mcp_server["mnemonic MCP server"]

  admin["admin tools"] -->|REST| admin_api["mnemonic admin API"]

  admin_api --> pg[("PostgreSQL + PGVector")]
  admin_api -->|publish job_id| queue[("RabbitMQ enrichment-jobs queue")]
  queue --> enricher["enricher"]
  enricher --> pg
  enricher --> neo4j[("Neo4j")]
  admin_api -->|read, delete cleanup| neo4j

  mcp_server -->|read only| pg
  mcp_server -->|read only| neo4j

  openai(["OpenAI API"])
  admin_api & mcp_server -->|query embeddings| openai
  enricher -->|embeddings, concept extraction| openai
```

MVP 2 is the current topology. For ports, protocols and the detailed flows, see [System Architecture](../architecture/system/02-system-architecture.md).

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
