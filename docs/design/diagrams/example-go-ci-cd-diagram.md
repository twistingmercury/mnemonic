---
version: 1
date: 2026-10-21
notes: "Imported from librarian-docs; build script paths changed to build/<service>/; CI description marked as the former per-repo workflow, not yet in the monorepo."
---

# Build Pipeline Diagram

> Back to [Design Index](../README.md)

This diagram shows how each Go service's build script (`build/<service>/<service>.build.sh`) drives both local builds and CI builds for the api service, the mcp service and the enricher service. The steps below are from the api service (`build/api/api.build.sh`, `build/api/api.Dockerfile`, and the former per-repo workflow `mnemonic-api-ci.yaml`, which is not in the monorepo yet). The other two services differ only in image name (`ghcr.io/twistingmercury/librarian-mcp` for mcp, `ghcr.io/twistingmercury/librarian-enricher` for the enricher), Dockerfile and E2E service names.
<!-- Reference diagram remains in mnemonic/_draft-skills/docker-first-ci-cd-implementation/references/docker-first-ci-cd-diagram.md -->

```mermaid
flowchart TD
    A["Build requested"] --> K{"Context?"}
    K -->|Local run| L["Run build/api/api.build.sh locally<br/>same build, quality gates, unit tests, E2E"]
    L --> L1["Done"]
    K -->|"CI run: push or PR to develop"| V

    subgraph CI0[CI workflow: one job]
    V["Validate docker-compose.yaml<br/>(api only)"] --> B["Run build/api/api.build.sh"]
    B --> B1["Resolve build metadata<br/>BUILD_VER = latest git tag<br/>BUILD_DATE / BUILD_COMMIT"]
    subgraph C0[Docker build stage]
    C["docker build, target final<br/>tags :BUILD_VER and :latest"] --> C1["Quality gates in build stage<br/>goimports / golangci-lint / govulncheck / gosec"]
    C1 --> C2["Unit tests in build stage<br/>go test ./cmd/main/... ./internal/..."]
    C2 --> C3["Compile binary<br/>embed BUILD_VER / BUILD_DATE / BUILD_COMMIT"]
    end
    B1 --> C

    subgraph D0[E2E test stage: tests/legacy/api/docker-compose.yaml]
    E["docker compose up -d<br/>postgres neo4j rabbitmq"]
    G["docker compose up --build<br/>--abort-on-container-exit<br/>--exit-code-from librarian_tests<br/>librarian_api librarian_tests"]
    E --> G
    end

    C3 --> E
    G --> H{"Tests pass?"}
    H -->|No| I["build script exits non-zero<br/>CI job fails"]
    H -->|Yes| N{"Event is a<br/>pull request?"}
    N -->|Yes| Q["Stop: PR builds are never pushed"]
    N -->|"No: push to develop"| O["Log in to GHCR"]
    O --> R["Tag and push<br/>:latest-dev and :BUILD_VER-dev<br/>e.g. v0.4.0-dev"]
    end
```

The former workflow also contains a `main` branch path that pushes `:latest` and `:BUILD_VER`, but no `main` branch exists yet, so only the `develop` path runs.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
