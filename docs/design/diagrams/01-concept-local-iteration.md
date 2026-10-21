---
version: 2
date: 2026-10-21
notes: "Renamed to Mnemonic (environment variables, image names, ports, links); license footer corrected to FSL-1.1-ALv2."
---

# MVP 1

> Back to [Design Index](../README.md)

Iteration 1 establishes the local baseline:

1. Run a single local Mnemonic service with MCP search and Admin API endpoints.
2. Use local PostgreSQL + PGVector and Neo4j as the initial data stores.
3. Validate end-to-end read/write behavior with manual API and MCP calls.

```mermaid
---
config:
  theme: redux
  layout: dagre
---
flowchart TB
    n3["Admin<br>bash - cURL|HTTPie"] -- REST --> n2["Mnemonic"]
    n2 --> n7["PostgreSQL +<br>PGVector"] & n8["Neo4j"]
    n9(["Claude Code | Codex"]) -- MCP --> n2

    n3@{ shape: subproc}
    n7@{ shape: cyl}
    n8@{ shape: cyl}
```

---

Copyright (c) 2025-2026 Jeremy K. Johnson. Licensed under FSL-1.1-ALv2; see [LICENSE](../../../LICENSE).
