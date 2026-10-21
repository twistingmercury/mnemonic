# Pattern UI Use Case Diagram

> Back to [Admin UI Architecture](README.md) | [Project README](../../../README.md)

This diagram captures the agreed phase-1 scope for the Mnemonic Admin web UI.
The primary user is a technical curator or engineer who explores patterns and,
when needed, imports a single pattern file into the system.

## Diagram

```mermaid
flowchart LR
    User[Technical Curator / Engineer]

    subgraph UI[Mnemonic Admin UI]
        direction TB
        subgraph Discovery[Discovery]
            direction TB
            UC_Browse([Browse pattern library])
            UC_Search([Search patterns semantically])
            UC_Filter([Apply filters])
            UC_Detail([Inspect pattern detail])
            UC_Chunks([View chunk summaries])
            UC_Related([Pivot to related pattern])
            UC_Agents(["Inspect agent associations (planned)"])
        end

        subgraph Import[Import]
            direction TB
            UC_Import([Import single pattern file])
            UC_Validate([Validate pattern file locally])
            UC_Translate([Translate pattern file to API payload])
            UC_Result([Review import success or errors])
        end
    end

    User --> UC_Browse
    User --> UC_Search
    User --> UC_Filter
    User --> UC_Detail
    User --> UC_Import
    User --> UC_Result

    UC_Search -. extends .-> UC_Filter
    UC_Detail -. includes .-> UC_Chunks
    UC_Detail -. includes .-> UC_Related
    UC_Detail -. includes .-> UC_Agents

    UC_Import -. includes .-> UC_Validate
    UC_Import -. includes .-> UC_Translate
    UC_Import -. includes .-> UC_Result

    Discovery --> Import
```

## Targeted Phase-1 Use Cases

- Browse the current pattern catalog without entering a search query.
- Search patterns with semantic query support.
- Narrow results with lightweight filters: tag, language, or domain. (An agent filter is planned; the API does not support it.)
- Open a pattern and inspect its full metadata and content.
- Review chunk summaries and use result context to understand why a pattern matched.
- Move from one pattern to another through related-pattern links.
- Inspect agent associations as supporting context. **Planned:** the API returns no agent data yet ([KM-6](../../requirements.md#km-6)).
- Import one Markdown pattern file by validating it locally, translating it to the pattern create payload, and submitting it to the API.
- See clear import outcomes, including validation failures, conflict responses, and successful creation.

## Out of Scope for Phase 1

- Batch imports.
- Pattern editing.
- Pattern deletion.
- Separate agent management screens.
- Separate skill management screens.
- Repository-wide or folder-based ingestion jobs.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
