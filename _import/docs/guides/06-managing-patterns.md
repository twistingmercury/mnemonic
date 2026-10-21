# Managing Patterns

This guide covers loading, updating, deleting, and monitoring patterns in Mnemonic using the REST API and shell scripts. It assumes you have Mnemonic running (see [Setting Up for Your Team](04-setting-up-for-your-team.md)) and want to manage your knowledge base.

## Overview

The best way to load and maintain patterns is directly through the REST API and the shell scripts in the mnemonic-patterns repository. There is also a web UI (mnemonic-admin at port 3000) that is under active development but not yet fully fleshed out.

## Loading Patterns via the REST API

### Create a pattern

```bash
curl -X POST http://localhost:18080/v1/api/patterns \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "my-pattern",
    "description": "What this pattern is about",
    "entity_type": "best-practice",
    "language": "go",
    "domain": "backend",
    "tags": ["go", "errors"],
    "version": "Go 1.21+",
    "related_patterns": ["go-testing-strategies"],
    "content": "# My Pattern\n\n## Overview\n\n...\n\n[//]: pattern\n\n## Implementation\n\n..."
  }'
```

Request fields: `name`, `entity_type`, `language`, `domain`, `content` (required); `description`, `tags`, `version`, `related_patterns` (optional). Unknown fields are ignored. See [Pattern File Schema](../reference/pattern-schema.md) for the validation rules.

Returns: 202 Accepted with a `Location: /v1/api/patterns/{id}` header and the full pattern as JSON, with `enrichment_status: "pending"`. One enrichment job per decorated section is queued.

### Get a pattern by ID

```bash
curl -s http://localhost:18080/v1/api/patterns/{id} | jq .
```

Returns every stored field plus `enrichment_status`, `enrichment_error`, `enriched_at`, `created_at`, `updated_at`, a `chunks` array (`chunk_index`, `section_title`, `enrichment_status`), and, once the pattern is `enriched`, a `graph` object with `related_patterns` (`id`, `name`, `relationship`, `strength`) and `concepts`. `graph` is `null` while enrichment is pending or if Neo4j is unavailable.

### List a pattern's chunks

```bash
curl -s http://localhost:18080/v1/api/patterns/{id}/chunks | jq .
```

Returns `{"chunks": [...], "count": N}` with the same chunk summaries.

### List patterns

```bash
curl -s 'http://localhost:18080/v1/api/patterns?limit=20' | jq .
```

Supports query parameters:

- `limit` (1-100, default 20)
- `cursor` (opaque pagination cursor)
- `tags` (comma-separated, e.g. `go,best-practices`)
- `search` (PostgreSQL full-text search on name and description)
- `language` (filter by language)
- `domain` (filter by domain)
- `entity_type` (filter by entity type)

The response is `{"data": [...], "pagination": {"limit", "cursor", "next_cursor", "has_more"}}`. Each item is a summary (`id`, `name`, `description`, `tags`, `enrichment_status`, `created_at`, `updated_at`) without `content`. Pass `next_cursor` as `cursor` to get the next page.

### Update a pattern

```bash
curl -X PUT http://localhost:18080/v1/api/patterns/{id} \
  -H 'Content-Type: application/json' \
  -d '{ ... the complete pattern ... }'
```

`PUT` is a full replacement: send every field, with the same rules as create. Omitted optional fields are cleared. Returns `204 No Content`. The update replaces all of the pattern's chunks and enrichment jobs, resets `enrichment_status` to `pending`, and re-enriches the pattern. Renaming to a name another pattern uses returns `409`.

### Delete a pattern

```bash
curl -X DELETE http://localhost:18080/v1/api/patterns/{id}
```

Returns `204 No Content`. Deletes the pattern and its chunks from PostgreSQL, then removes its Neo4j node (best effort) and any concepts left orphaned.

### Semantic search via API

```bash
curl -s 'http://localhost:18080/v1/api/patterns/search?q=error+handling+in+Go&limit=5&threshold=0.7' | jq .
```

Query parameters: `q` (required, max 1000 characters), `limit` (1-50, default 10), `threshold` (0-1), `tags` (comma-separated, AND), `language`, `domain`.

The `threshold` parameter controls the minimum cosine similarity for results. The default is `0.7` for the REST API. Note that the MCP `search_patterns` tool uses a different default threshold of `0.5`, configurable via `LIBRARIAN_MCP_DEFAULT_SEARCH_THRESHOLD` on the MCP server.

The response is `{"results": [...], "metadata": {"query", "total_candidates", "search_duration_ms"}}`. Each result is one matching chunk: `pattern_id`, `pattern_name`, `entity_type`, `language`, `domain`, `tags`, `section_title`, `chunk_index`, `content`, `similarity`. Unlike the MCP tool, the REST search does not add graph-expanded results. It returns `503` if the embedding service is unavailable.

## Bulk Loading with Shell Scripts

The mnemonic-patterns repository includes scripts for bulk validation and loading.

### Validate all patterns

```bash
cd mnemonic-patterns
./install/validate.sh --dir ./patterns
```

Checks all `.md` files against the pattern schema. Dependencies: yq, jq.

### Load all patterns

```bash
cd mnemonic-patterns
./install/load.sh
```

This script:

1. Runs validation first (aborts if any file fails)
2. Extracts frontmatter and body from each Markdown file
3. Builds a JSON payload from the frontmatter fields and the body
4. POSTs to `http://localhost:18080/v1/api/patterns`
5. Reports: loaded (202), skipped/already exists (409), or failed

### Custom server target

```bash
./install/load.sh --server http://your-server:8080
```

### Custom patterns directory

```bash
./install/load.sh --dir /path/to/your/patterns
```

Dependencies: curl, yq (mikefarah/yq), jq.

Environment variables:

- `LIBRARIAN_BASE_URL` — API base URL (default: `http://localhost:18080`)
- `PATTERNS_DIR` — directory containing pattern Markdown files

## Monitoring Enrichment

After loading patterns, the enrichment worker processes them asynchronously.

### Check enrichment status for all patterns

```bash
curl -s http://localhost:18080/v1/api/patterns?limit=100 | jq '.data[] | {name, enrichment_status}'
```

Enrichment states:

- `pending` — queued, not yet processed (also the state of a pattern with no `[//]: pattern` sections, which never leaves it)
- `enriched` — every chunk embedded, concepts extracted, graph relationships created, searchable via MCP
- `failed` — a chunk failed to embed; `enrichment_error` says why. The enricher retries the job automatically (default every 30s up to the job's attempt limit), and the pattern becomes `enriched` if a retry succeeds. To force a fresh attempt, `PUT` the pattern again; there is no retry endpoint.

### Check enricher logs

```bash
docker compose logs -f dev_enricher
```

### Search availability

Semantic search (MCP and REST) only considers chunks whose status is `enriched`. If a pattern isn't showing up in search, check its enrichment status and its chunks (`GET /v1/api/patterns/{id}/chunks`) first.

## Pattern Lifecycle

1. **Author** — write a Markdown pattern file following the schema (see [Authoring Patterns](05-authoring-patterns.md))
2. **Validate** — run `validate.sh` to check schema compliance
3. **Load** — POST via API or `load.sh` script
4. **Enrich** — automatic; generates embeddings and graph relationships
5. **Search** — pattern becomes available via MCP tools
6. **Update** — PUT via API to modify; re-enrichment is triggered automatically
7. **Delete** — DELETE via API to remove

## Error Handling

The API returns RFC 7807 Problem Details for errors:

- **400 Bad Request** — validation failures, invalid UUIDs, bad query parameters; `errors` lists `{field, code, message}` with `code` one of `REQUIRED`, `MAX_LENGTH`, `MAX_ITEMS`, `TOO_LARGE`, `INVALID_FORMAT`, `INVALID_VALUE`
- **404 Not Found** — pattern doesn't exist
- **409 Conflict** — another pattern already has that name (create or update)
- **503 Service Unavailable** — a dependency needed for search is unavailable
- **500 Internal Error** — server error

Each body has `type`, `title`, `status`, `detail`, `instance`, and `traceId` (when tracing is active).

```json
{
  "type": "https://mnemonic.example.com/problems/validation-error",
  "title": "Validation Error",
  "status": 400,
  "detail": "The request body contains invalid fields",
  "instance": "/v1/api/patterns",
  "errors": [
    {"field": "language", "code": "INVALID_VALUE", "message": "language is not an allowed value"}
  ]
}
```

Every response includes an `X-Request-ID` header (echoed from the request, or generated) for correlation with logs.

## API Reference

For the complete API specification, see the Swagger 2.0 document generated from the handlers, `src/docs/swagger/swagger.yaml` in mnemonic-api, or browse it at <http://localhost:18080/swagger/index.html> on a running API.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
