# Mnemonic Service Architecture

> Back to [Project README](../../../README.md)

This subdirectory documents the MCP service component of the mnemonic system (`mnemonic-mcp`). The MCP service exposes 3 read-only pattern search tools to Claude Code over Streamable HTTP (stateless, unauthenticated) at `http://localhost:18081/mcp`.

## Contents

- [08-mcp-tools.md](08-mcp-tools.md) — MCP tool definitions, discovery, parameters, response formats, and error handling

For implementation details, see the [MCP Server design](../../design/mcp-server.md) and the [Service Layer design](../../design/service-layer.md).

For system-level architecture spanning all components, see the [Architecture System Overview](../system/README.md).

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
