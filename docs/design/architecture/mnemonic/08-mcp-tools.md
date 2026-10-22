---
version: 3
date: 2026-10-22
notes: "Typo fixed; authentication note names the gateway; table alignment."
---

# MCP Tools

> Back to [MCP Service Architecture](README.md) | [Design Index](../../README.md)

## Table of Contents

- [Overview](#overview)
- [Tool Discovery](#tool-discovery)
- [Pattern Search Tools](#pattern-search-tools)
- [Response Formats](#response-formats)
- [Error Handling](#error-handling)
- [References](#references)

## Overview

Mnemonic exposes 3 read-only MCP tools for pattern search: `search_patterns`, `find_related_patterns`, and `get_pattern`. These tools are called during conversation when Claude Code needs team knowledge. They are served by the mcp service; the MCP service has no write tools (patterns are managed through the REST API).

## Tool Discovery

The MCP server speaks MCP protocol version **2026-07-28** over Streamable HTTP at `/mcp`. In the shipped stacks `http://localhost:8081/mcp` is the gateway's MCP edge (the Envoy `mcp_edge` listener); the service's own listener is set by `mcp.port` (default 8081) and is internal to the stack. The mcp service itself has **no authentication**; the gateway (`gateway/`, Envoy plus OPA) is what authenticates and authorizes callers in front of it, so do not expose the service port directly.

The 2026-07-28 protocol has no `initialize` handshake and no sessions. Every request is one HTTP POST that stands on its own:

1. **Discovery (optional)**: `server/discover` returns the supported protocol versions, the capabilities (`{"tools":{}}`) and `serverInfo` (`mnemonic`)
2. **Tool listing**: `tools/list` returns the 3 tools, sorted by name, with `ttlMs: 3600000` and `cacheScope: "public"`, so a client may cache the list for an hour
3. **Invocation**: `tools/call` with the tool name and arguments

Each request carries its protocol version, client info and client capabilities in `params._meta`, and mirrors them into headers:

| Header                 | Value                                 | Required for                                             |
| ---------------------- | ------------------------------------- | -------------------------------------------------------- |
| `MCP-Protocol-Version` | `2026-07-28`                          | every request                                            |
| `Mcp-Method`           | the JSON-RPC `method`                 | every request                                            |
| `Mcp-Name`             | the tool name                         | `tools/call`                                             |
| `Accept`               | `application/json, text/event-stream` | every request (the response may be an SSE `data:` event) |

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "search_patterns",
    "arguments": { "query": "Go error handling" },
    "_meta": {
      "io.modelcontextprotocol/protocolVersion": "2026-07-28",
      "io.modelcontextprotocol/clientInfo": { "name": "example", "version": "1.0.0" },
      "io.modelcontextprotocol/clientCapabilities": {}
    }
  }
}
```

Clients on protocol 2025-11-25 and earlier still work: they send `initialize` first, and the server answers with the newest version they share. `GET` and `DELETE` on `/mcp` return 405, and an `Mcp-Session-Id` header is ignored.

Browser requests from another origin (an `Origin` header that does not match the host) are rejected with 403, to block DNS-rebinding attacks. Clients that send no `Origin` header, such as Claude Code, are unaffected.

```mermaid
sequenceDiagram
    participant CC as Claude Code
    participant MCP as mcp service :8081/mcp
    participant PG as PostgreSQL + pgvector
    participant NEO as Neo4j
    participant OAI as OpenAI

    CC->>MCP: tools/list (cache for ttlMs)
    MCP-->>CC: find_related_patterns, get_pattern, search_patterns
    CC->>MCP: tools/call search_patterns {query}
    MCP->>OAI: embed query
    MCP->>PG: chunk similarity search
    MCP->>NEO: RELATED_TO from top 3 patterns
    MCP->>PG: metadata for graph candidates
    MCP-->>CC: markdown text content
```

Client configuration (Claude Code CLI):

```bash
claude mcp add --transport http --scope user mnemonic http://localhost:8081/mcp
```

Or in `.mcp.json`:

```json
{
  "mcpServers": {
    "mnemonic": {
      "type": "http",
      "url": "http://localhost:8081/mcp"
    }
  }
}
```

## Pattern Search Tools

These three tools are called during conversation when Claude Code determines that team knowledge is relevant to a user request.

### `search_patterns`

Semantic search over pattern chunks, with graph expansion of the best hits.

**Parameters:**

| Parameter   | Type         | Required | Default                              | Constraints | Description                               |
| ----------- | ------------ | -------- | ------------------------------------ | ----------- | ----------------------------------------- |
| `query`     | string       | yes      | —                                    | —           | Natural language search query             |
| `limit`     | integer      | no       | 10                                   | 1–50        | Maximum number of chunk results           |
| `threshold` | number       | no       | 0.5 (`mcp.default_search_threshold`) | 0.0–1.0     | Minimum cosine similarity score           |
| `tags`      | string array | no       | —                                    | —           | Conjunctive (AND) filter by tag           |
| `language`  | string       | no       | —                                    | —           | Filter by pattern language (e.g. `go`)    |
| `domain`    | string       | no       | —                                    | —           | Filter by pattern domain (e.g. `backend`) |

**Returns:** Markdown-formatted text. A header (`Found N sections across M patterns matching '<query>':`) is followed by one entry per matching chunk with pattern name, similarity percentage, section title, tags and the chunk content. If graph expansion finds anything, a `### Related Patterns (via graph)` section follows with up to 5 extra patterns, each with its concept-overlap similarity, the seed pattern it was found via, and shared concepts.

`limit` and `threshold` carry their `minimum`, `maximum` and `default` in the tool's input schema; the `threshold` default is `mcp.default_search_threshold`.

**Notes:** Only enriched chunks appear in results. Chunk results are ranked by pgvector cosine similarity. Graph expansion takes the top 3 distinct patterns from the vector results, follows their `RELATED_TO` edges in Neo4j (up to 5 per seed), drops patterns already in the vector results, and applies the same tag, domain and language filters (in the graph section only, patterns with language `agnostic` also pass a language filter). Expansion is best-effort: if Neo4j fails, the vector results are returned alone. There is no agent filter and no blended vector/graph score.

### `find_related_patterns`

Find patterns related to a given pattern via the knowledge graph.

**Parameters:**

| Parameter    | Type          | Required | Default | Constraints | Description                             |
| ------------ | ------------- | -------- | ------- | ----------- | --------------------------------------- |
| `pattern_id` | string (UUID) | yes      | —       | valid UUID  | ID of the pattern to find relations for |
| `limit`      | integer       | no       | 5       | 1–20        | Maximum number of related patterns      |

**Returns:** Markdown with the related patterns, each with relationship type (`RELATED_TO`), similarity score and shared concepts. Pattern content is not included.

**Notes:** Traverses `RELATED_TO` edges in Neo4j in either direction, ordered by similarity. The similarity (0.0–1.0) is concept overlap, `shared / max(conceptsA, conceptsB)`, computed during enrichment. `limit` carries its `minimum`, `maximum` and `default` in the input schema. An unknown `pattern_id` (including the all-zero UUID) returns a not-found error; a pattern with no edges (for example, one not enriched yet) returns `No related patterns found for '<name>'.`

### `get_pattern`

Retrieve a specific pattern by ID.

**Parameters:**

| Parameter | Type          | Required | Constraints | Description         |
| --------- | ------------- | -------- | ----------- | ------------------- |
| `id`      | string (UUID) | yes      | valid UUID  | Pattern ID to fetch |

**Returns:** Markdown with the pattern name, ID, tags, enrichment status, full content, and (when enriched) up to 10 related patterns and the extracted concept names.

**Notes:** Reads the pattern from PostgreSQL and, only if its `enrichment_status` is `enriched`, its graph context from Neo4j. The graph sections are omitted when enrichment is pending or failed, or when Neo4j is unavailable (the call still succeeds). A failed enrichment shows the stored error on the status line.

## Response Formats

MCP tools return markdown-formatted text. Structured data such as similarity scores and metadata is embedded in the prose because Claude Code consumes tool results as text content during conversation. Markdown makes results readable in context without additional parsing. Exact formats are in [MCP Server Design](../../mcp-server.md#tool-handler-signatures).

Results are delivered as MCP tool results with:

```json
{"content": [{"type": "text", "text": "..."}]}
```

## Error Handling

Mnemonic uses two error mechanisms depending on whether the failure is at the application or transport level.

### Application-Level Errors

For not-found conditions, validation failures, and backend unavailability, Mnemonic returns a normal JSON-RPC success response with `isError: true` in the tool result and the error message as text. The `isError` flag signals to Claude Code that the tool call did not succeed. Claude Code can retry or inform the user accordingly.

| Pattern             | Message prefix        | Applies To                                                                    |
| ------------------- | --------------------- | ----------------------------------------------------------------------------- |
| Not found           | `pattern not found`   | `get_pattern` and `find_related_patterns`                                     |
| Invalid input       | `invalid input`       | `get_pattern` and `find_related_patterns` — bad UUID                          |
| Service unavailable | `service unavailable` | All 3 tools — any other backend failure (PostgreSQL, Neo4j, OpenAI embedding) |

### Transport-Level Errors

For protocol problems, the MCP SDK rejects the request before any tool runs:

| HTTP | Body                                               | Cause                                                                                 |
| ---- | -------------------------------------------------- | ------------------------------------------------------------------------------------- |
| 400  | plain text `malformed payload: ...`                | Malformed JSON, or not a JSON-RPC 2.0 message                                         |
| 400  | JSON-RPC error `-32020` HeaderMismatch             | `MCP-Protocol-Version`, `Mcp-Method` or `Mcp-Name` missing or not matching the body   |
| 400  | JSON-RPC error `-32022` UnsupportedProtocolVersion | Unknown protocol version; `error.data.supported` lists the versions the server speaks |
| 403  | plain text                                         | Cross-origin browser request                                                          |
| 404  | JSON-RPC error `-32601` Method not found           | Unknown method, including `ping` and `logging/setLevel`, which 2026-07-28 removed     |
| 405  | plain text                                         | `GET` or `DELETE` on `/mcp`                                                           |

Arguments that do not match a tool's input schema (for example a number where `id` expects a string, a `limit` or `threshold` outside its `minimum`/`maximum`, or an explicit `null`) are not a transport error: the SDK returns a tool result with `isError: true` and a `validating "arguments": ...` message. The tool metrics count these, like any other argument-validation failure, under the `invalid_input` outcome.

## References

- [MCP Server Design](../../mcp-server.md) — implementation: server setup, handlers, formats, observability
- [Service Layer Design](../../service-layer.md) — the services behind the tools
- [Communication Patterns](../system/03-communication-patterns.md) — protocol overview and request flow

**Next:** Return to [Architecture Overview](README.md)

---

Copyright (c) 2025-2026 Jeremy K. Johnson. Licensed under FSL-1.1-ALv2; see [LICENSE](../../../../LICENSE).
