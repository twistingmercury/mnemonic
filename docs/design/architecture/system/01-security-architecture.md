---
version: 3
date: 2026-10-22
notes: "Current state now describes the built gateway (Envoy jwt_authn, OPA ext_authz, Keycloak, egress proxy); planned sections note where the build differs or what is not implemented."
---

# Security Architecture

> Back to [System Architecture](README.md) | [Design Index](../../README.md)

**Stance:** The Mnemonic services carry no authentication code ([SE-1](../../requirements.md#se-1)). The MCP interface is read-only ([SE-4](../../requirements.md#se-4)) and database credentials are injected from the runtime environment ([SE-6](../../requirements.md#se-6)). The repository ships a gateway that adds authentication and authorization in front of both services: Envoy verifies a Keycloak-issued JWT and asks OPA whether to forward the request. The compose stack (`deploy/docker-compose.yaml`) and the Helm chart (`deploy/helm/mnemonic`) both run it. [Current State](#current-state-local-phase) describes what is built. The rest of this document is the original Post-MVP design; parts of it are built in a different shape, and each section says where. API keys, identity headers, team-scoped roles, policy bundles, the device code flow and the token store are not implemented.

## Table of Contents

- [Current State (Local Phase)](#current-state-local-phase)
- [Introduction](#introduction)
- [Security Model Overview](#security-model-overview)
- [Authentication](#authentication)
  - [JWT Tokens](#jwt-tokens-primary)
  - [API Keys](#api-keys-secondary)
  - [Supported Identity Providers](#supported-identity-providers)
- [Authorization](#authorization)
  - [RBAC Model](#rbac-model)
  - [OPA Policy Structure](#opa-policy-structure)
- [Component Architecture](#component-architecture)
- [Admin Authentication Flow (Post-MVP Only)](#admin-authentication-flow-post-mvp-only)
  - [Device Code Flow](#device-code-flow)
  - [Token Storage](#token-storage)
- [Identity Headers](#identity-headers)
- [Architectural Decisions](#architectural-decisions)
- [Deployment Considerations](#deployment-considerations)
- [Migration Path](#migration-path)
- [Trade-offs Summary](#trade-offs-summary)

## Current State (Local Phase)

This is what the code and the shipped configuration do today (the api service v0.5.0, the enricher service v0.5.0, the mcp service v0.6.0). The gateway, its access rules and the dev realm are described in [gateway/README.md](../../../../gateway/README.md); this table summarizes them.

| Surface                                  | Current behavior                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Services                                 | No authentication or authorization in process. Started on their own (as in the e2e test stack `tests/integration/docker-compose.yaml`), any caller that reaches them can call every endpoint                                                                                                                                                                                                                                                                                                              |
| Gateway                                  | Envoy (`gateway/envoy/envoy.yaml`) publishes the API edge on :8080 and the MCP edge on :8081; Keycloak publishes :8180. `jwt_authn` verifies the token signature against Keycloak's JWKS, the issuer, the expiry and the audience (`mnemonic-api` or `mnemonic-mcp`, one per edge), then removes the token before forwarding. `ext_authz` asks OPA (`gateway/opa/policy/`), which decides from the `scope` claim, the method and the path. OPA runs as its own container or Deployment, not as a sidecar  |
| REST `/v1/api` and `/swagger` (api edge) | Reads need `patterns:read`; create and update need `patterns:write`; delete and Swagger need `patterns:admin`. Scopes nest (admin includes write, write includes read). Anything else is denied                                                                                                                                                                                                                                                                                                           |
| MCP `/mcp` (mcp edge)                    | Streamable HTTP (MCP 2026-07-28), stateless, read-only tools only (`search_patterns`, `find_related_patterns`, `get_pattern`). `tools/call` needs `mcp:search` for `search_patterns` and `mcp:read` for the other two; every `POST` must send `MCP-Protocol-Version: 2026-07-28`. An unauthenticated request gets 401 with an RFC 9728 `WWW-Authenticate` challenge. The mcp service itself answers cross-origin browser requests with 403                                                                |
| `/health`, `/version` and `/metrics`     | Unauthenticated on the services (`/health` and `/version` on :8080, `/metrics` on :9090). Through the gateway, `GET /version` on the api edge is public, `GET /health` needs any valid `mnemonic-api` token, and nothing else of the operations ports is exposed                                                                                                                                                                                                                                          |
| Network isolation                        | Compose: the services, OPA and the data stores sit on an internal network with no route out. Helm: NetworkPolicies deny all traffic by default and allow only the listed flows; they need a CNI that enforces policy, such as Calico                                                                                                                                                                                                                                                                      |
| TLS                                      | Plain HTTP throughout the dev gateway. The api service has no in-process TLS; the mcp and enricher services can serve their operations port (:8080) over TLS when `server.tls.enabled` is true (off by default); the MCP listener (:8081) is plain HTTP                                                                                                                                                                                                                                                   |
| Secrets                                  | The real OpenAI key is held only by the egress proxy (`MNEMONIC_OPENAI_API_EMBEDDINGS_KEY`, `gateway/envoy/envoy-egress.yaml`), which adds it to each forwarded request. In the shipped stacks the services get the placeholder `MNEMONIC_OPENAI_API_KEY=proxy-managed` and `MNEMONIC_OPENAI_BASE_URL=http://egress:8443/v1`. PostgreSQL, Neo4j and RabbitMQ passwords come from config or `MNEMONIC_*` environment variables; none are hard-coded. Keycloak uses fixed dev client secrets and a dev user |
| PostgreSQL                               | `database.postgres.ssl_mode`, default `prefer` (the shipped stacks set `disable`)                                                                                                                                                                                                                                                                                                                                                                                                                         |
| RabbitMQ                                 | SASL PLAIN credentials passed separately from the connection URL                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Error responses                          | Public 500 and 503 bodies use fixed messages; private logs redact upstream bodies, URLs and credential fields                                                                                                                                                                                                                                                                                                                                                                                             |
| Data sent off-host                       | Pattern content and search queries go to the OpenAI API for embeddings and concept extraction. In the shipped stacks they go through the egress proxy, which forwards only `POST /v1/embeddings` and `POST /v1/chat/completions` to `api.openai.com` and answers everything else with 403. The services' `/health` OpenAI check (`GET /models/<model>`) is one of the refused requests, so behind the proxy it always reports a Warning                                                                   |

The sections below are the original Post-MVP design.

## Introduction

Post-MVP (Production Deployment) adds enterprise-grade security to Mnemonic using infrastructure-layer components. Authentication and authorization are handled outside the application code, keeping the services lightweight and focused on team knowledge synchronization.

This approach follows the principle of separation of concerns: security infrastructure handles identity verification and access control, while Mnemonic remains focused on its core responsibilities.

**Security Scope:**

- **Admin REST API** (`:8080`): Protected by Envoy + OPA for write operations (patterns); the built gateway guards reads as well
- **MCP Server** (`:8081`): Read-only access. The built gateway puts Envoy + OPA in front of MCP as well (its own edge and audience), as the MVP 4 and MVP 5 concept diagrams do

## Security Model Overview

Mnemonic security operates at the infrastructure layer rather than the application layer. This design choice provides several benefits:

- **Separation of concerns**: Security policies managed independently from application code
- **Policy updates without deployment**: Security rules can change without redeploying Mnemonic
- **Consistent enforcement**: All endpoints protected by the same security infrastructure
- **Fail-closed design**: Requests are denied by default unless explicitly allowed

The security stack consists of:

- **Envoy Proxy**: Handles authentication (JWT validation, API keys, TLS termination)
- **OPA Sidecar**: Handles authorization (fine-grained RBAC with Rego policies)
- **Mnemonic**: Receives pre-validated identity via trusted headers

> **Built (2026-10-22):** Envoy handles JWT validation only (no API keys, no TLS in the dev gateway). OPA runs as its own container or Deployment, not as a sidecar, and loads its policies from mounted files, not bundles. The services receive no identity: Envoy removes the token and adds no identity headers.

```mermaid
graph TB
    subgraph "Admin Workstation (Post-MVP)"
        ADMIN[Admin curl/scripts]
        STORE[(Token Store)]
    end

    subgraph "Server Infrastructure"
        subgraph "Edge Layer"
            ENV[Envoy Proxy]
        end

        subgraph "Policy Layer"
            OPA[OPA Sidecar]
            BUNDLE[(Policy Bundles)]
        end

        subgraph "Application Layer"
            MCP[MCP Server]
            API[Admin API]
            ENR[Enricher]
        end

        subgraph "External"
            IDP[Identity Provider]
        end
    end

    ADMIN -->|"1. Login"| IDP
    IDP -->|"Tokens"| ADMIN
    ADMIN -->|"Store"| STORE
    ADMIN -->|"2. REST + JWT"| ENV
    ENV -->|"3. ext_authz"| OPA
    OPA -->|"Load"| BUNDLE
    ENV -->|"4. Headers"| API
```

## Authentication

Authentication verifies user identity before requests reach Mnemonic. Envoy handles all authentication at the edge layer.

### JWT Tokens (Primary)

JWT tokens are the primary authentication mechanism, providing rich claims for authorization decisions.

**Flow:**

1. User authenticates with identity provider via OAuth2/OIDC
2. CLI receives and stores access token and refresh token
3. CLI includes JWT in Authorization header for all requests
4. Envoy validates JWT signature using JWKS from identity provider
5. Envoy extracts claims: `user_id`, `team_id`, `roles`

**Token Characteristics:**

| Aspect     | Detail                                       |
| ---------- | -------------------------------------------- |
| Format     | JWT (JSON Web Token)                         |
| Signing    | RS256 or ES256                               |
| Validation | JWKS endpoint from identity provider         |
| Expiry     | Access token: 1 hour, Refresh token: 30 days |
| Claims     | user_id, team_id, roles, exp, iat            |

> **Built (2026-10-22):** The identity provider is Keycloak with the dev realm `gateway/keycloak/realm-mnemonic.json` (access tokens live 300 seconds). Policies read only `sub`, `aud` and `scope`; there are no `user_id`, `team_id` or `roles` claims. Service clients get tokens with the client credentials grant; the public client `mnemonic-mcp-client` uses authorization code with PKCE. There is no CLI.

### API Keys (Secondary)

> **Not implemented.** Envoy accepts only Keycloak JWTs; automation uses a Keycloak client with the client credentials grant instead.

API keys provide authentication for service-to-service communication and automation scenarios where interactive login is not possible.

**Characteristics:**

- Hashed storage (never stored in plaintext)
- Rotation support with grace period
- Rate limited per key
- Scoped to specific operations

**Use Cases:**

| Scenario              | Authentication Method    |
| --------------------- | ------------------------ |
| Interactive CLI usage | JWT via Device Code Flow |
| CI/CD pipelines       | API Key                  |
| Service-to-service    | API Key                  |
| Automated scripts     | API Key                  |

### Supported Identity Providers

Envoy supports standard OAuth2/OIDC identity providers for JWT validation. The choice depends on organizational requirements.

| Provider | Use Case                | Complexity | Notes                                   |
| -------- | ----------------------- | ---------- | --------------------------------------- |
| Auth0    | SaaS, quick setup       | Low        | Managed service, minimal configuration  |
| Keycloak | Self-hosted, enterprise | Medium     | Full control, requires infrastructure   |
| Azure AD | Microsoft ecosystem     | Medium     | Native integration with Microsoft tools |
| Okta     | Enterprise SSO          | Low        | Managed service, enterprise features    |

The built gateway uses Keycloak.

## Authorization

Authorization determines what authenticated users can do. OPA (Open Policy Agent) evaluates policies for every request.

### RBAC Model

> **Not implemented as designed.** There are no teams or roles. The built gateway authorizes by OAuth scope per edge: `patterns:read`, `patterns:write` and `patterns:admin` on the API edge, `mcp:read` and `mcp:search` on the MCP edge. The rules are listed in [gateway/README.md](../../../../gateway/README.md#access-rules).

Access control uses Role-Based Access Control with team-scoped resources.

**Scope Hierarchy:**

```mermaid
graph TB
    ORG[Organization]
    ORG --> TEAM1[Team A]
    ORG --> TEAM2[Team B]
    TEAM1 --> R1[Patterns]
    TEAM2 --> R2[Patterns]
```

**Roles:**

| Role      | Permissions                                        |
| --------- | -------------------------------------------------- |
| admin     | Full access to team resources, manage team members |
| developer | Create, update, delete patterns                    |
| viewer    | Read-only access to patterns                       |

**Resource Types:**

| Resource | Description                              |
| -------- | ---------------------------------------- |
| patterns | Engineering patterns for semantic search |

### OPA Policy Structure

OPA policies are written in Rego and evaluated for each request. Policies are loaded from bundles that can be updated independently of deployments.

> **Built (2026-10-22):** The policies in `gateway/opa/policy/` differ from the sketch below. The entry point is `envoy.authz` (`main.rego`), which delegates to `mnemonic.api` or `mnemonic.mcp` by the `edge` context extension that each Envoy route sets; `mnemonic.token` reads the verified JWT payload that Envoy passes as metadata. The policies return only allow or deny and inject no headers. OPA loads them from mounted files. A denied request gets 403; if OPA errors or does not answer within 0.5 s, Envoy returns 503 (fail-closed).

```rego
package mnemonic.authz

import rego.v1

default allow := false

# Allow authenticated users to read patterns via MCP
allow if {
    input.method == "GET"
    startswith(input.path[0], "mcp")
    input.user.team_id != ""
}

# Pattern write operations require developer or admin role
allow if {
    input.method in ["PUT", "DELETE", "POST"]
    startswith(input.path[0], "v1")
    input.user.roles[_] in {"admin", "developer"}
}

# Response headers to inject
headers["X-User-ID"] := input.user.user_id
headers["X-Team-ID"] := input.user.team_id
headers["X-User-Roles"] := concat(",", input.user.roles)
```

**Policy Evaluation Flow:**

```mermaid
sequenceDiagram
    participant ENV as Envoy
    participant OPA as OPA Sidecar
    participant BUNDLE as Policy Bundle

    ENV->>OPA: ext_authz request
    Note over OPA: Load policy from cache
    OPA->>OPA: Evaluate policy
    alt Allowed
        OPA-->>ENV: allow: true, headers
    else Denied
        OPA-->>ENV: allow: false, status: 403
    end
```

## Component Architecture

The security components integrate with the existing Mnemonic architecture.

> **Built (2026-10-22):** Unlike this diagram, the MCP server is inside the perimeter: Claude Code reaches it through the Envoy MCP edge. An egress proxy (Envoy) is the only component with a route to the internet and holds the OpenAI key. There is no token store and no policy bundle service.

```mermaid
graph TB
    subgraph "Admin Workstation (Post-MVP)"
        ADMIN[Admin curl/scripts]
        TS[(Token Store)]
    end

    subgraph "Server Infrastructure"
        subgraph "Security Layer"
            ENV[Envoy Proxy<br/>Authentication]
            OPA[OPA Sidecar<br/>Authorization]
        end

        subgraph "Application Layer"
            MCP[MCP Server]
            API[Admin API]
            ENR[Enricher]
        end

        subgraph "Data Layer"
            PG[(Postgres + PGVector)]
            NEO[(Neo4j)]
            RMQ[(RabbitMQ)]
        end

        subgraph "External Services"
            IDP[Identity Provider]
            BUNDLES[(Policy Bundles)]
        end
    end

    ADMIN <--> TS
    ADMIN -->|"JWT/API Key"| ENV
    ENV <-->|"ext_authz"| OPA
    OPA <-->|"Load"| BUNDLES
    ENV <-->|"Validate JWT"| IDP
    ENV -->|"Headers"| API
    MCP --> PG
    MCP --> NEO
    API <--> PG
    API <--> NEO
    API --> RMQ
    ENR <--> RMQ
    ENR <--> PG
    ENR --> NEO
```

**Component Responsibilities:**

| Component         | Responsibility                                                        |
| ----------------- | --------------------------------------------------------------------- |
| Envoy Proxy       | TLS termination, JWT validation, API key validation, header injection |
| OPA Sidecar       | Policy evaluation, RBAC enforcement, header generation                |
| Admin API         | Trust identity headers, apply business logic for pattern management   |
| MCP Server        | Read-only pattern serving; operates outside the security perimeter    |
| Enricher          | Internal consumer; not exposed through Envoy                          |
| Identity Provider | User authentication, token issuance, JWKS hosting                     |
| Policy Bundles    | Store and distribute Rego policies                                    |

## Admin Authentication Flow (Post-MVP Only)

> **Note:** This section describes authentication for the Admin REST API in Post-MVP (Production Deployment). The admin UI is not part of this repository for now.
>
> **Not implemented.** There is no admin CLI, the dev realm does not enable the device authorization grant, and nothing stores tokens. Admin callers get tokens from Keycloak directly (see [gateway/README.md](../../../../gateway/README.md#getting-a-token)).

### Device Code Flow

The Device Code Flow is recommended for admin authentication when a CLI is introduced. It allows users to authenticate via a web browser without exposing credentials to the terminal.

```mermaid
sequenceDiagram
    participant User
    participant ADMIN as Admin Tool/CLI
    participant Browser
    participant IDP as Identity Provider

    User->>ADMIN: mnemonic login
    ADMIN->>IDP: POST /oauth/device/code
    IDP-->>ADMIN: device_code, user_code, verification_uri
    ADMIN-->>User: Visit {verification_uri}, Enter code: {user_code}

    User->>Browser: Open verification_uri
    Browser->>IDP: Enter user_code
    IDP->>Browser: Login prompt
    User->>Browser: Authenticate
    IDP-->>Browser: Authorization granted

    loop Poll for token
        ADMIN->>IDP: POST /oauth/token (grant_type=device_code)
        IDP-->>ADMIN: access_token, refresh_token
    end

    ADMIN->>ADMIN: Store tokens securely
    ADMIN-->>User: Login successful
```

**Flow Steps:**

1. User runs admin login command
2. Admin tool requests device code from identity provider
3. Admin tool displays verification URL and user code
4. User opens browser, enters code, authenticates
5. Admin tool polls for token completion
6. Admin tool stores tokens in secure storage
7. User can now make authenticated requests

### Token Storage

Tokens are stored using platform-native secure storage mechanisms.

| Platform | Storage Mechanism              | Security                   |
| -------- | ------------------------------ | -------------------------- |
| macOS    | Keychain                       | Hardware-backed encryption |
| Linux    | Secret Service API (libsecret) | User session encryption    |
| Windows  | Credential Manager             | DPAPI encryption           |

**Token Lifecycle:**

```mermaid
stateDiagram-v2
    [*] --> NoToken: Initial state
    NoToken --> ValidToken: admin login
    ValidToken --> ExpiredToken: Token expires
    ExpiredToken --> ValidToken: Auto-refresh
    ExpiredToken --> NoToken: Refresh fails
    ValidToken --> NoToken: admin logout
```

## Identity Headers

> **Not implemented.** Envoy removes the token (`forward: false`) and injects no identity headers; the services do not read any. Network isolation (compose internal network, Helm NetworkPolicies) keeps callers from bypassing Envoy.

After successful authentication and authorization, Envoy injects identity headers that the Admin API trusts.

| Header       | Description                   | Example            |
| ------------ | ----------------------------- | ------------------ |
| X-User-ID    | Authenticated user identifier | `user_abc123`      |
| X-Team-ID    | User's team identifier        | `team_xyz789`      |
| X-User-Roles | Comma-separated roles         | `developer,viewer` |

**Trust Model:**

- The Admin API only accepts traffic from Envoy (network isolation)
- Headers are set by Envoy, not forwarded from client
- Client-provided identity headers are stripped by Envoy

```mermaid
graph LR
    ADMIN[Admin Tool] -->|"Authorization: Bearer ..."| ENV[Envoy]
    ENV -->|"X-User-ID, X-Team-ID, X-User-Roles"| API[Admin API]

    style ADMIN fill:#f9f
    style ENV fill:#9f9
    style API fill:#99f
```

## Architectural Decisions

### SEC-ADR-001: Infrastructure-Layer Security

**Context:** Mnemonic needs authentication and authorization for multi-tenant operation.

**Decision:** Handle authentication and authorization at infrastructure layer using Envoy and OPA. The Admin API receives only pre-validated identity headers.

**Rationale:**

- Services stay lightweight and focused on pattern knowledge management
- Security policies update without application deployment
- Consistent security across all endpoints
- Clear separation of concerns
- Standard, well-tested security components

**Trade-offs:**

- Additional infrastructure components to operate
- Network hop for authorization decisions
- Requires Rego expertise for policy management

### SEC-ADR-002: Authentication Strategy

**Context:** Mnemonic needs to support both interactive admin users and automated systems.

**Decision:** Support both JWT tokens and API keys with JWT as primary authentication method.

**Rationale:**

- JWT provides rich claims for authorization decisions
- API keys enable CI/CD integration and automation
- Supports enterprise identity providers
- Industry-standard protocols

**Trade-offs:**

- Two authentication paths to maintain
- API key management adds operational complexity

### SEC-ADR-003: Authorization Model

**Context:** Mnemonic needs fine-grained access control for team resources.

**Decision:** Use RBAC with team-scoped resources, extensible to ABAC if needed.

**Rationale:**

- Simple model covers most use cases
- OPA policies are straightforward to write and audit
- Clear audit trail for compliance
- Extensible to attribute-based access control

**Trade-offs:**

- Role explosion possible with complex permission requirements
- May need ABAC for advanced scenarios

## Deployment Considerations

### Minimal Deployment

For small teams or development environments.

```mermaid
graph TB
    subgraph "Single Host"
        ENV[Envoy]
        OPA[OPA]
        MCP[MCP Server]
        API[Admin API]
        ENR[Enricher]
    end

    subgraph "External"
        IDP[Identity Provider]
        PG[(Postgres)]
        NEO[(Neo4j)]
        RMQ[(RabbitMQ)]
    end

    ENV --> OPA
    ENV --> API
    MCP --> PG
    MCP --> NEO
    API --> PG
    API --> NEO
    API --> RMQ
    ENR --> RMQ
    ENR --> PG
    ENR --> NEO
    ENV <--> IDP
```

**Characteristics:**

- Single container host with all application components
- External managed databases
- External identity provider (Auth0, Okta)
- Suitable for small teams (less than 20 users)

### AWS Fargate Deployment

For production environments with high availability requirements (MVP 5 target).

```mermaid
graph TB
    subgraph "AWS Fargate"
        subgraph "Edge"
            ENV[Envoy Proxy]
        end

        subgraph "Tasks"
            OPA[OPA Sidecar]
            MCP[MCP Server]
            API[Admin API]
            ENR[Enricher]
        end
    end

    subgraph "AWS Managed Services"
        IDP[Identity Provider]
        SQS[(SQS<br/>Enrichment Queue)]
        S3[(S3<br/>Policy Bundles)]
    end

    subgraph "External"
        PG[(Postgres)]
        NEO[(Neptune)]
    end

    ENV --> OPA
    ENV --> API
    OPA --> S3
    MCP --> PG
    MCP --> NEO
    API --> PG
    API --> NEO
    API --> SQS
    ENR --> SQS
    ENR --> PG
    ENR --> NEO
    ENV <--> IDP
```

**Characteristics:**

- Envoy proxy at the edge for authentication
- OPA sidecar for authorization policy evaluation
- Policy bundles loaded from S3
- SQS replaces RabbitMQ for managed queue delivery
- Neptune replaces self-hosted Neo4j for graph storage
- Horizontal scaling via Fargate task definitions

### Failure Modes

| Failure                   | Impact                                                           | Mitigation                                         |
| ------------------------- | ---------------------------------------------------------------- | -------------------------------------------------- |
| Envoy unavailable         | All requests fail                                                | Expected behavior, load balancer health checks     |
| OPA unavailable           | All requests denied (fail-closed; the built gateway returns 503) | OPA sidecar per task, policy bundle caching        |
| IdP unavailable           | New logins fail, existing tokens work until expiry               | Token refresh, offline validation with cached JWKS |
| Policy bundle unavailable | OPA uses cached policies                                         | Bundle caching in OPA, durable S3 storage          |

## Migration Path

Security can be added incrementally to an existing deployment.

> **Status (2026-10-22):** The built gateway went straight to enforcement: Envoy, JWT authentication and OPA authorization are all on, with no pass-through or permissive stage, and the services are reachable only through Envoy. TLS termination is not set up.

```mermaid
graph LR
    S1[Step 1<br/>Add Envoy] --> S2[Step 2<br/>Add JWT Auth]
    S2 --> S3[Step 3<br/>Add OPA]
    S3 --> S4[Step 4<br/>Enforce]
```

### Step 1: Add Envoy as Reverse Proxy

- Deploy Envoy in front of the Admin API
- No authentication, pass-through mode
- Verify traffic flows correctly
- Establish TLS termination

### Step 2: Add JWT Authentication

- Configure identity provider
- Provide admin authentication mechanism
- Configure Envoy JWT validation
- Allow both authenticated and unauthenticated requests

### Step 3: Add OPA Authorization

- Deploy OPA sidecar
- Start with permissive policies (allow all authenticated)
- Add logging for authorization decisions
- Refine policies based on access patterns

### Step 4: Enforce Security

- Require authentication for all Admin API requests
- Remove direct Admin API access
- Enable fail-closed mode
- Monitor and alert on authorization failures

## Trade-offs Summary

| Aspect        | Trade-off                                                          |
| ------------- | ------------------------------------------------------------------ |
| Complexity    | +2 components (Envoy, OPA), but security isolated from application |
| Latency       | +1-5ms per request for authorization, mitigated by caching         |
| Operational   | Policy updates without deployment, but requires Rego expertise     |
| Failure modes | Fail-closed provides security, but requires high availability      |
| Flexibility   | Standard components enable customization, but more configuration   |
| Auditability  | Clear separation enables detailed audit trails                     |

**Next:** [System Architecture](02-system-architecture.md)

See also:

- [Architecture Overview](README.md) for the full document index
- [Deployment Architecture](06-deployment-architecture.md) for deployment patterns

---

Copyright (c) 2025-2026 Jeremy K. Johnson. Licensed under FSL-1.1-ALv2; see [LICENSE](../../../../LICENSE).
