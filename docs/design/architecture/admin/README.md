# Admin UI Architecture

> Back to [Project README](../../../README.md)

This subdirectory documents the admin web UI component of the mnemonic system — a pattern-centric internal application for pattern management, search, and import.

## Contents

- [0001-frontend-stack.md](0001-frontend-stack.md) — ADR for the frontend stack (React 19, TypeScript, Vite, TanStack Query, Tailwind CSS)
- [screen-map.md](screen-map.md) — Wireframes, screen layout, interaction flows, and state maps
- [use-cases.md](use-cases.md) — UML use case diagrams, persona definitions, scope boundaries
- [tech-stack-justification.md](tech-stack-justification.md) — Executive justification for frontend technology choices

## Backend API Used by the UI

The admin UI repository (mnemonic-admin) is not described in detail here; this section lists the mnemonic-api endpoints the phase-1 screens depend on, as implemented today (`src/internal/handlers/patterns/patterns.go`, `src/docs/swagger/swagger.yaml`). All are under `/v1/api` on the API's port 8080, with no authentication.

| UI need | Endpoint | Notes |
| --- | --- | --- |
| Browse list | `GET /v1/api/patterns` | Query: `limit` (1-100, default 20), `cursor`, `tags` (comma-separated, AND), `search` (full-text on name and description), `language`, `domain`, `entity_type`. Returns `data[]` summaries (`id`, `name`, `description`, `tags`, `enrichment_status`, `created_at`, `updated_at`) and `pagination` (`limit`, `cursor`, `next_cursor`, `has_more`). |
| Semantic search | `GET /v1/api/patterns/search` | Query: `q` (required, max 1000 chars), `limit` (1-50, default 10), `threshold` (0-1, default 0.7), `tags`, `language`, `domain`. Returns one result per matching chunk (`pattern_id`, `pattern_name`, `section_title`, `chunk_index`, `content`, `similarity`, ...) plus `metadata`. No graph expansion. |
| Pattern detail | `GET /v1/api/patterns/{id}` | Full pattern, `chunks[]` (`chunk_index`, `section_title`, `enrichment_status`), and `graph` (`related_patterns[]` with `id`, `name`, `relationship`, `strength`; `concepts[]`) once enriched, else `graph: null`. |
| Chunk summaries | `GET /v1/api/patterns/{id}/chunks` | `{chunks, count}`; same data as `chunks[]` in the detail response. |
| Import | `POST /v1/api/patterns` | Body: `name`, `entity_type`, `language`, `domain`, `content` (required); `description`, `tags`, `version`, `related_patterns`. `202` on success, `400` with field errors, `409` on a duplicate name. |

Not available from the API: agent associations and agent filters (planned, see [KM-6](../../requirements.md#km-6)), a list of allowed `language`/`domain` values (the UI must carry its own copy of the API's `vocabulary` defaults), and CORS headers (the API sets none, so a browser UI on another origin needs a same-origin reverse proxy to reach it).

## System Context

For system-level architecture spanning all components, see the [Architecture System Overview](../system/README.md).

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
