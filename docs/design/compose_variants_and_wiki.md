---
version: 1
date: 2026-10-23
notes: Initial version.
---

# Compose Variants and the Wiki

[Design documents](README.md) | [Project README](../../README.md)

## Table of Contents

- [Why](#why)
- [Goals and non-goals](#goals-and-non-goals)
- [Part 1: the two compose stacks](#part-1-the-two-compose-stacks)
- [Part 2: the wiki](#part-2-the-wiki)
- [Order of work](#order-of-work)
- [Decided and left to the builder](#decided-and-left-to-the-builder)

## Why

The wiki is for people who set up Mnemonic and use it. The six guides in the `mnemonic-docs` repo describe a stack with no authentication: clone, export an OpenAI key, `docker compose up`, `curl` the API. The monorepo's only compose file, `deploy/docker-compose.yaml`, now runs everything behind Envoy, OPA and Keycloak, with an egress proxy as the only holder of the OpenAI key. A newcomer who follows the old guides gets a stack they cannot reach, and `mnemonic-patterns`' `install/load.sh` sends no `Authorization` header, so it gets a 401 for every pattern.

Trying Mnemonic should not require understanding a gateway. So the repo ships two stacks: a plain one for trying and small trusted teams, and the gateway one as the worked example of adding authentication. The wiki is written against both.

## Goals and non-goals

**Goals**

- A newcomer reaches a working search in Claude Code with one compose file and one environment variable.
- `mnemonic-patterns` and plain `curl` work against the plain stack without changes.
- The gateway stack stays exactly as it is, documented as the example of adding authentication.
- The wiki replaces the `mnemonic-docs` guides, so that repo can be retired.

**Non-goals**

- Helm and Kubernetes setup. The wiki says only that a Kubernetes deployment on AWS services (SQS, Neptune and others) is in the works.
- Teaching readers to build their own Keycloak realm (users, clients, scopes). The gateway example keeps the shipped dev realm.
- Bulk-loading tooling. `mnemonic-patterns` is presented as an optional example to follow.
- Developer documentation. The README and this directory remain that.

**Anti-goals**

- The plain stack must not be mistaken for something safe to expose. Every page that uses it says it has no authentication and belongs on a local machine or a trusted network.

## Part 1: the two compose stacks

Both live in `deploy/`.

| Stack | File | Contains | Published ports |
| --- | --- | --- | --- |
| Plain | new file next to the current one (name is the builder's call) | mnemonic-api, mnemonic-mcp, mnemonic-enricher, Postgres, Neo4j, RabbitMQ | API 8080, MCP 8081 |
| Gateway (example) | `deploy/docker-compose.yaml`, unchanged | the plain stack's services plus Envoy, OPA, Keycloak and the egress proxy | Envoy 8080 and 8081, Keycloak 8180 |

The plain stack:

- Uses the same GHCR images and the same `MNEMONIC_*` settings as the gateway stack, minus everything about the egress proxy.
- Gives the three services the OpenAI key directly. The host variable is `MNEMONIC_OPENAI_API_KEY`, passed through to `mnemonic-api`, `mnemonic-mcp` and `mnemonic-enricher`, and compose fails fast when it is unset. The services' `MNEMONIC_OPENAI_BASE_URL` points at the OpenAI default, not at an egress proxy.
- Publishes the API and MCP ports on the host, so the services listen where the old guides said they did.
- Keeps the data stores off the host.

The gateway stack's rule that the services never see the key remains true there and only there. The docs for the plain stack say that the services hold the key.

## Part 2: the wiki

Hosted as the GitHub wiki of `twistingmercury/mnemonic`. Pages, built on the six existing guides:

| Page | Work |
| --- | --- |
| Home | New. Short, links to everything. |
| Introduction | Update the maturity and limitations text: no authentication in the plain stack, the gateway stack as the example. |
| Concepts | Check against the current code, then update. |
| Quick start | Rewrite for the plain stack: clone this repo, set the key, `docker compose up`, load a test pattern, wait for enrichment, add the MCP server to Claude Code. |
| Team setup | Rewrite: health checks, logs, stop and restart, ports, credentials, troubleshooting, for the plain stack. |
| Authoring patterns | Check the frontmatter schema against the current code, then update. |
| Managing patterns | Update paths and service names. Mention `mnemonic-patterns` as an optional bulk-load example. |
| Adding authentication | New. Walks through the gateway stack with its dev realm: how tokens are obtained, what each scope allows, how an MCP client signs in, how to call the API with a bearer token. |
| What's next | New, short: the Kubernetes deployment on AWS services is in the works. |

The documents are written by the `technical writer`. Every command and path in them is run against the real stack before the page is accepted.

## Order of work

1. The plain compose file, with its checks (see below).
2. The wiki pages that depend on it, then the rest.
3. Once the wiki is live, the owner retires `mnemonic-docs`.

## Decided and left to the builder

**Decided with the owner:** two stacks (plain and gateway); no Helm in the wiki; the dev realm as shipped; services take the OpenAI key directly in the plain stack; `mnemonic-patterns` stays an optional example.

**My calls (not yet confirmed):**

- The plain file sits beside `deploy/docker-compose.yaml`, which is left alone.
- The host variable for the plain stack is `MNEMONIC_OPENAI_API_KEY`, the name the services read.
- The wiki lives in the monorepo's GitHub wiki rather than under `docs/`. The repo is public and the wiki is already enabled. Pages are drafted as markdown in a local clone of `mnemonic.wiki.git`, committed there, and the owner pushes.
- The old guides are read from `twistingmercury/mnemonic-docs` with `gh api`.
- Image tags: the plain stack uses the same tags as the gateway file (`latest-dev` for the services, `v1.0.0-dev` for Postgres and Neo4j), and the Quick start says these are development builds.
- The plain stack has its own compose project name and its own volumes, so it never adopts the gateway stack's data. Both publish 8080 and 8081, so they cannot run at once; the docs say to stop one before starting the other.
- Verification: the builder checks the plain stack and every wiki command against `tests/openai-stub` by setting `MNEMONIC_OPENAI_BASE_URL`, with a fake key. Agents never use the owner's key. The owner runs one real-key pass at the end.
- `mnemonic-patterns` is cloned into scratch space to confirm `install/load.sh` works unchanged against the plain stack. If it does not, that is a separate issue in that repo, not a fix here.
- The root README and any `deploy/` docs that describe `deploy/docker-compose.yaml` as the way to run the stack are updated to describe both stacks.

**Left to the builder:**

- The plain file's name, and whether CI should validate it (at minimum `docker compose config`).
- Whether `mnemonic-patterns` needs a token option for the gateway stack. It is out of scope here and, if wanted, is its own issue in that repo.
- Page order and wording in the wiki.
