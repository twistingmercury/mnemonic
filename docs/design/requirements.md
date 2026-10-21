# Mnemonic Requirements

> Back to [Project README](../README.md)

## Table of Contents

- [Overview](#overview)
- [Knowledge Management](#knowledge-management)
- [Administration](#administration)
- [Observability](#observability)
- [Security](#security)
- [Deployment](#deployment)
- [Scope Boundaries](#scope-boundaries)
- [Assumptions and Constraints](#assumptions-and-constraints)

## Overview

Mnemonic solves the problem of inconsistent tooling and isolated knowledge in teams using Claude Code.
See [concept.md](concept.md) for the full problem statement and rationale.

Mnemonic must provide a searchable team knowledge base. An admin REST API handles all writes; a read-only MCP interface serves Claude Code.

Requirements marked **Not implemented** below are not met by the current code (`mnemonic-api` v0.5.0, `mnemonic-enricher` v0.5.0, `mnemonic-mcp` v0.6.0). See the [Requirements Traceability Matrix](requirements-traceability.md) for where each requirement is implemented and tested.

## Knowledge Management

<a id="km-1"></a>**KM-1.** Mnemonic must store team knowledge as named, searchable patterns. Each pattern has a name,
description, content body (up to 100 KB, enforced as 100,000 bytes), and optional tags.

<a id="km-2"></a>**KM-2.** Mnemonic must support semantic search over patterns so that a natural-language query
returns the most relevant patterns by meaning, not just keyword match.

<a id="km-3"></a>**KM-3.** Mnemonic must expose relationships between patterns so that a caller can discover patterns
related to a given pattern by shared concepts or associations.

<a id="km-4"></a>**KM-4.** Mnemonic must track the enrichment state of each pattern (pending, enriched, failed) and
only include enriched content in semantic search results. (Implemented per chunk: search returns
chunks whose own status is `enriched`, so a pattern can be partly searchable while its other chunks are pending.)

<a id="km-5"></a>**KM-5.** Mnemonic must retry failed pattern enrichment and surface enrichment errors to
administrators.

<a id="km-6"></a>**KM-6.** Mnemonic must allow patterns to be associated with specific agents, with a relevance score
per association. **Not implemented:** no service stores or reads agent associations, and the pattern API has no agent field.

**Note:** Tooling management (agents, skills) and tooling synchronization requirements have been removed. Mnemonic is focused on pattern knowledge management. Agent and skill definitions are best managed via version-controlled repositories.

## Administration

<a id="ad-1"></a>**AD-1.** Mnemonic must expose an administrative API for all write operations: creating, updating,
and deleting patterns.

<a id="ad-2"></a>**AD-2.** The admin API must return structured error responses so that scripts and CI pipelines can
detect and handle failures programmatically.

<a id="ad-3"></a>**AD-3.** The admin API must support listing patterns with filtering by tag and full-text search on
name and description.

<a id="ad-4"></a>**AD-4.** The admin API must accept pattern uploads and immediately acknowledge them, completing
semantic enrichment asynchronously in the background.

<a id="ad-5"></a>**AD-5.** The admin API must expose an endpoint to manually trigger retry of failed enrichment jobs.
**Not implemented:** there is no retry endpoint. Failed jobs are retried automatically by the enricher; re-submitting
a pattern with `PUT` creates fresh jobs.

## Observability

<a id="ob-1"></a>**OB-1.** Mnemonic must emit structured logs for all incoming requests on both the admin and MCP
interfaces, including method, path, status code, and latency. (Partly implemented: the API logs every request;
the MCP server logs each tool call, not each HTTP request to `/mcp`.)

<a id="ob-2"></a>**OB-2.** Mnemonic must expose a health check endpoint that indicates whether the service and its
storage dependencies are ready to serve traffic.

<a id="ob-3"></a>**OB-3.** Mnemonic must expose metrics sufficient to monitor request rates, error rates, and latency
for both the admin API and MCP server.

<a id="ob-4"></a>**OB-4.** Mnemonic must log enrichment job activity, including successful completions, failures, and
retry attempts.

## Security

<a id="se-1"></a>**SE-1.** In MVP (local deployment), Mnemonic may operate without authentication, as it runs in
a trusted network environment.

<a id="se-2"></a>**SE-2.** In production (Post-MVP), the admin API must be protected by
infrastructure-layer authentication and authorization. The MCP interface may remain unauthenticated
within a trusted internal network.

<a id="se-3"></a>**SE-3.** Mnemonic must never store user credentials or session tokens.

<a id="se-4"></a>**SE-4.** The MCP interface must be read-only. No write operations may be performed through it.

<a id="se-5"></a>**SE-5.** In production, all external traffic must be carried over TLS.

<a id="se-6"></a>**SE-6.** Database credentials must not be embedded in the deployed service. They must be injected
from the runtime environment.

## Deployment

<a id="dp-1"></a>**DP-1.** Mnemonic must be deployable to both local development and production environments.

<a id="dp-2"></a>**DP-2.** Mnemonic must be stateless. All persistent state must reside in external storage so that
multiple instances can run without coordination.

<a id="dp-3"></a>**DP-3.** Mnemonic must verify schema compatibility at startup (e.g., by checking that the database
schema version is compatible with the running version). Mnemonic does not manage schema versioning or
run migrations. Migrations are pre-applied at Docker image build time via entrypoint scripts,
independent of the application lifecycle. **Not implemented:** the services do not check the schema version at startup;
only the "no migrations in the services" half holds.

## Scope Boundaries

Mnemonic's scope is knowledge storage. The following boundaries define
what the system is responsible for:

<a id="sb-1"></a>- Mnemonic's only uses of external AI services are embedding generation (chunks and search queries) and concept extraction during pattern enrichment.
<a id="sb-2"></a>- All workflow and orchestration decisions belong to the user.
<a id="sb-3"></a>- All tool execution and file operations happen on the client side.
<a id="sb-4"></a>- Identity management and access policy enforcement belong to the infrastructure layer.
<a id="sb-5"></a>- All interaction with Mnemonic is programmatic (API and protocol).
<a id="sb-6"></a>- The primary data store is the source of truth. Secondary stores are eventually consistent.

## Assumptions and Constraints

- Workstations running Claude Code can reach the Mnemonic MCP endpoint over the local or team
  network.
- A team member or CI process takes responsibility for loading and maintaining patterns via the admin API.
- Pattern content is curator-supplied. Mnemonic does not generate or validate content quality.
- The external embedding API (used during pattern enrichment) is available. If it is unavailable,
  enrichment will be retried; patterns will not be searchable until enrichment succeeds.
- Expected pattern counts are in the range of hundreds to tens of thousands, not millions.
- Mnemonic does not enforce multi-tenancy. A single deployment serves one team.

**Next:** [Architectural Decisions](architecture/system/00-architectural-decisions.md)

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
