---
version: 5
date: 2026-10-22
notes: "CI workflow implemented; diagram and text updated."
---

# Build Pipeline Diagram

> Back to [Design Index](../README.md)

This diagram shows how each Go service's build script (`build/<service>/<service>.build.sh`) builds the image and runs the service's end-to-end suite. The steps below are from the api service (`build/api/api.build.sh`, `build/api/api.Dockerfile`, and the shared steps in `build/build-lib.sh`). The mcp and enricher services differ only in image name (`ghcr.io/twistingmercury/mnemonic-mcp` and `ghcr.io/twistingmercury/mnemonic-enricher`), Dockerfile and end-to-end service names.

The build scripts never push. Pushing is the job of the CI `publish` job (see [CI Workflows](../ci_workflows.md)), which pushes the image the build job tested; the last step in the diagram is shown dashed because it is not part of the script. The end-to-end suite is the Python suite in `tests/integration/`; each build script runs all of it against the image it just built.

```mermaid
---
title: Go service build script flow (api service)
---
flowchart TD
    A["Run build/api/api.build.sh<br/>(local run, or the CI service job)"] --> B["Resolve build metadata<br/>MNEMONIC_BUILD_VER from build/version.sh api<br/>(latest api/vX.Y.Z tag, else 0.0.0-dev)<br/>MNEMONIC_BUILD_DATE, MNEMONIC_BUILD_COMMIT"]

    subgraph C0["Image build: docker build --no-cache --pull, target final"]
        C["Generate the swagger docs<br/>(swag init)"] --> C1["Quality gates<br/>goimports, golangci-lint,<br/>govulncheck, gosec"]
        C1 --> C2["Unit tests<br/>go test ./cmd/api/... ./internal/..."]
        C2 --> C3["Compile the binary<br/>embed version, date and commit"]
        C3 --> C4["Final image FROM scratch<br/>tags :vX.Y.Z and :latest"]
    end

    B --> C

    subgraph D0["E2E stage: one compose project per run"]
        E["Start the infrastructure<br/>postgres, neo4j, rabbitmq"] --> G["Start the three services, the OpenAI stub and the test container<br/>stop when any exits<br/>exit code from the test container"]
    end

    C4 --> E
    G --> H{"Tests pass?"}
    H -->|No| I["Script exits non-zero"]
    H -->|Yes| J["Script exits zero"]
    G --> K["Tear down the run's containers,<br/>networks and volumes"]
    J -.->|"CI only, not the script"| L["CI logs in to GHCR<br/>and pushes the image"]
```

Whether the script runs locally or in CI, it does the same work: a fresh image build with the quality gates and unit tests inside it, then the end-to-end suite against that image, then cleanup of everything that run created. The CI triggers, tag rules and push conditions are in [CI Workflows](../ci_workflows.md).

---

Copyright (c) 2025-2026 Jeremy K. Johnson. Licensed under FSL-1.1-ALv2; see [LICENSE](../../../LICENSE).
