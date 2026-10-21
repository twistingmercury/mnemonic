# MVP 4

> Back to [Project README](../../README.md)

MVP 4 introduces local Kubernetes deployment and explicit authentication/authorization controls.

1. Deploy Mnemonic services locally on a single-node Kubernetes cluster (for example, minikube).
2. Add Envoy configuration as the ingress/authN layer for MCP and Admin API traffic.
3. Add OPA policies as the authZ checkpoint before requests reach service handlers.
4. Begin Helm charts for repeatable local cluster deployment.
5. Keep data and queue infrastructure on managed AWS services.

```mermaid
---
config:
  theme: redux
  layout: dagre
---
flowchart TB
  subgraph local["Local Environment"]
    claude["Claude Code"]
    admin["admin tools"]

    subgraph k8s["Local Kubernetes Cluster (minikube/single-node)"]
      subgraph mcp_pod["Mnemonic MCP Pod"]
        mcp_envoy["envoy"]
        mcp_opa["OPA"]
        mcp_server["mnemonic MCP server"]
      end

      subgraph admin_pod["Mnemonic Admin Pod"]
        admin_envoy["envoy"]
        admin_opa["OPA"]
        admin_api["mnemonic admin API"]
      end

      subgraph enrich_pod["Mnemonic Enrichment Pod"]
        enricher["enricher"]
      end
    end
  end

  subgraph aws["AWS Services"]
    aws_pg[("AWS PostgreSQL + PGVector")]
    aws_sqs[("AWS SQS")]
    aws_neptune[("AWS Neptune")]
  end

  claude -->|MCP| mcp_envoy
  mcp_envoy -->|authN check| mcp_opa
  mcp_opa -->|authZ allow/deny| mcp_server

  admin -->|REST| admin_envoy
  admin_envoy -->|authN check| admin_opa
  admin_opa -->|authZ allow/deny| admin_api

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
