# Build Pipeline Diagram

> Back to [Project README](../../README.md)

This diagram shows how `src/build/build.sh` drives both local builds and CI builds in `mnemonic-api`, `mnemonic-mcp` and `mnemonic-enricher`. The steps below are from `mnemonic-api` (`src/build/build.sh`, `src/build/Dockerfile`, `.github/workflows/mnemonic-api-ci.yaml`); the other two services differ only in image name (`ghcr.io/twistingmercury/librarian-mcp` for mcp, `ghcr.io/twistingmercury/librarian-enricher` for the enricher), workflow file name and E2E service names.
<!-- Reference diagram remains in mnemonic/_draft-skills/docker-first-ci-cd-implementation/references/docker-first-ci-cd-diagram.md -->

```mermaid
flowchart TD
    A["Build requested"] --> K{"Context?"}
    K -->|Local run| L["Run ./build/build.sh locally<br/>same build, quality gates, unit tests, E2E"]
    L --> L1["Done"]
    K -->|"CI run: push or PR to develop"| V

    subgraph CI0[CI workflow: one job]
    V["Validate root docker-compose.yaml<br/>(api only)"] --> B["Run ./build/build.sh"]
    B --> B1["Resolve build metadata<br/>BUILD_VER = latest git tag<br/>BUILD_DATE / BUILD_COMMIT"]
    subgraph C0[Docker build stage]
    C["docker build, target final<br/>tags :BUILD_VER and :latest"] --> C1["Quality gates in build stage<br/>goimports / golangci-lint / govulncheck / gosec"]
    C1 --> C2["Unit tests in build stage<br/>go test ./cmd/main/... ./internal/..."]
    C2 --> C3["Compile binary<br/>embed BUILD_VER / BUILD_DATE / BUILD_COMMIT"]
    end
    B1 --> C

    subgraph D0[E2E test stage: src/tests/docker-compose.yaml]
    E["docker compose up -d<br/>postgres neo4j rabbitmq"]
    G["docker compose up --build<br/>--abort-on-container-exit<br/>--exit-code-from librarian_tests<br/>librarian_api librarian_tests"]
    E --> G
    end

    C3 --> E
    G --> H{"Tests pass?"}
    H -->|No| I["build.sh exits non-zero<br/>CI job fails"]
    H -->|Yes| N{"Event is a<br/>pull request?"}
    N -->|Yes| Q["Stop: PR builds are never pushed"]
    N -->|"No: push to develop"| O["Log in to GHCR"]
    O --> R["Tag and push<br/>:latest-dev and :BUILD_VER-dev<br/>e.g. v0.4.0-dev"]
    end
```

The workflow also contains a `main` branch path that pushes `:latest` and `:BUILD_VER`, but no `main` branch exists yet, so only the `develop` path runs.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
