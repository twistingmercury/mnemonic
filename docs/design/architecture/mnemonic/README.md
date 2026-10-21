---
version: 2
date: 2026-10-21
notes: "Renamed to Mnemonic (environment variables, image names, ports, links); license footer corrected to FSL-1.1-ALv2."
---

# Mnemonic Service Architecture

> Back to [Design Index](../../README.md)

This subdirectory documents the MCP service component of the mnemonic system (the mcp service). The MCP service exposes 3 read-only pattern search tools to Claude Code over Streamable HTTP (stateless, unauthenticated) at `http://localhost:18081/mcp`.

## Contents

- [08-mcp-tools.md](08-mcp-tools.md) — MCP tool definitions, discovery, parameters, response formats, and error handling

For implementation details, see the [MCP Server design](../../mcp-server.md) and the [Service Layer design](../../service-layer.md).

For system-level architecture spanning all components, see the [Architecture System Overview](../system/README.md).

---

Copyright (c) 2025-2026 Jeremy K. Johnson. Licensed under FSL-1.1-ALv2; see [LICENSE](../../../../LICENSE).
