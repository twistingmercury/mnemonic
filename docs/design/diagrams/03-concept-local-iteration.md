# MVP 3

> Back to [Project README](../../README.md)

Iteration 3 moves stateful infrastructure to managed AWS services while keeping application runtime local:

1. Replace local PostgreSQL + PGVector with AWS PostgreSQL + PGVector.
2. Replace local Neo4j with AWS Neptune.
3. Replace local RabbitMQ with AWS SQS, while MCP server, Admin API, and enricher remain local.

```mermaid
---
config:
  theme: redux
  layout: dagre
---
flowchart TB
  subgraph local["Local Environment"]
    claude["Claude Code"] -->|MCP| mcp_server["mnemonic MCP server"]
    admin["admin tools"] -->|REST| admin_api["mnemonic admin API"]
    enricher["enricher"]
  end

  subgraph aws["AWS Services"]
    aws_pg[("AWS PostgreSQL + PGVector")]
    aws_sqs[("AWS SQS")]
    aws_neptune[("AWS Neptune")]
  end

  admin_api --> aws_pg
  admin_api --> aws_sqs
  aws_sqs --> enricher
  enricher --> aws_pg
  enricher --> aws_neptune
  mcp_server -->|read only| aws_pg
  mcp_server -->|read only| aws_neptune
```

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
