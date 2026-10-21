# MVP 5

> Back to [Project README](../../README.md)

Iteration 5 is the cloud end state for runtime deployment and infrastructure automation:

1. Move Mnemonic runtime from local Kubernetes to AWS-hosted compute (as shown: AWS Fargate pods/services).
2. Keep Envoy and OPA in the request path for MCP and Admin API authorization controls.
3. Provision and manage AWS networking, compute, and managed services with Terraform.
4. Use AWS configuration management patterns (for example, SSM Parameter Store and Secrets Manager) for runtime config and secrets.

Converted from `_ideas/mnemonic-concept-cloud-deployment.drawio`.

```mermaid
---
config:
  theme: redux
  layout: dagre
---
flowchart TB
  subgraph local["User Local Machine"]
    claude["Claude Code"]
    admin["Admin Tools"]
  end

  subgraph fargate["AWS Fargate"]
    subgraph mcp_pod["Mnemonic POD"]
      mcp_envoy["envoy"]
      mcp_opa["OPA"]
      mcp_server["mnemonic MCP server"]
    end

    subgraph admin_pod["Mnemonic Admin POD"]
      admin_envoy["envoy"]
      opa["OPA"]
      admin_api["mnemonic Admin API"]
    end

    subgraph enrich_pod["Mnemonic Enrichment POD"]
      enricher["mnemonic enrichment process"]
    end
  end

  subgraph aws["AWS Cloud Services"]
    sqs[("AWS SQS")]
    pg[("AWS PostgreSQL + PGVector")]
    neptune[("AWS Neptune")]
  end

  claude --> mcp_envoy
  mcp_envoy -->|authz check| mcp_opa
  mcp_opa -->|allow/deny| mcp_server

  admin --> admin_envoy
  admin_envoy -->|authz check| opa
  opa -->|allow/deny| admin_api

  admin_api -.-> sqs
  sqs -.-> enricher

  admin_api --> pg
  enricher --> pg
  enricher --> neptune
  mcp_server -->|READ ONLY| pg
  mcp_server -->|READ ONLY| neptune
```

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
