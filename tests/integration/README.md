# Mnemonic integration tests

> **Maturity Level**: Emerging — black-box smoke tests for the complete
> Mnemonic stack.
> **Version**: v0.0.3

---

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Continuous Integration](#continuous-integration)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)

## Usage

The suite runs in a self-contained Docker Compose stack, defined by
[docker-compose.yaml](docker-compose.yaml) in this directory. No OpenAI key is
needed.

The monorepo has no Make targets for this suite yet, and the Compose file is
not re-homed: it still uses the fork's service and image names, mounts a
`./src` directory that no longer exists (the tests now sit next to the Compose
file) and builds a stub from `./openai-stub`, which now lives in
[tests/openai-stub](../openai-stub). Until that is fixed the stack does not
start as written. The tests themselves read the service URLs from the
environment, so they can also be pointed at a running stack with
`go test -count=1 ./...` from this directory.

## How it works

```text
            ┌──────────┐  publish  ┌──────────┐  consume  ┌──────────┐
  tests ──► │   API    │ ────────► │ RabbitMQ │ ────────► │ Enricher │
            └────┬─────┘           └──────────┘           └────┬─────┘
                 │ write                                       │ embed, extract
                 ▼                                             ▼
            ┌──────────┐                                  ┌──────────┐
            │ Postgres │ ◄──── chunks, embeddings ─────── │  Neo4j   │
            └────┬─────┘                                  └────┬─────┘
                 └──────────────► ┌──────────┐ ◄───────────────┘
                                  │   MCP    │ ◄──── tests
                                  └──────────┘
   All three services call the OpenAI stub instead of api.openai.com.
```

Each service's own E2E suite already covers its endpoints in detail. This
suite covers only what none of them can: a pattern written through the API,
enriched by the enricher, and read back through the MCP server.

| Test | What it proves |
|---|---|
| `TestServicesHealthy` | Each service's `/health` lists every dependency it is wired to, all `OK` |
| `TestPatternFlowsFromAPIToMCP` | Create → enriched → found by API and MCP search → MCP `get_pattern` shows extracted concepts |
| `TestUpdateReenrichesPattern` | An update is re-enriched; search finds the new text, not the old |
| `TestRelatedPatternsAndDelete` | Shared concepts link patterns in Neo4j; a delete removes the pattern from every MCP read path |
| `TestEnricherRetriesFailedJob` | A failed OpenAI call fails the job and the enricher's retry sweep recovers it |

### OpenAI stub

The stub in [tests/openai-stub](../openai-stub) replaces OpenAI for all three
services through `MNEMONIC_OPENAI_BASE_URL`, so runs are deterministic and free:

- **Embeddings** hash each distinct word of the input into the vector, so
  cosine similarity tracks shared words. Tests search for a random word only
  one pattern contains, with a low threshold (0.1).
- **Concept extraction** returns the concepts on a `Concepts: a, b` line of
  the pattern content.
- **`POST /control/fail?match=X&n=N`** fails the next N embedding requests
  whose input contains X.

The enricher runs with `MNEMONIC_OPENAI_RETRY_ATTEMPTS=0` and
`MNEMONIC_ENRICHMENT_RETRY_DELAY=1s` so one forced failure reaches its retry
sweep quickly.

## Key Considerations

- **Prerequisites:** Docker Compose and pull access to the service images on
  ghcr.io. The tests run in the stock `golang:1.27.2-alpine` image with the suite
  directory mounted read-only; only the small stub image is built.
- **Images under test:** the `latest-dev` tags of `mnemonic-api`,
  `mnemonic-mcp` and `mnemonic-enricher`, which CI is meant to publish.
- **Isolation:** the stack publishes no host ports, so it can run alongside
  other Mnemonic stacks.

## Continuous Integration

The CI workflows are not written yet. The fork ran this suite on pull
requests, on pushes, daily and on demand, and printed Compose service status and
logs on failure; the workflow for the monorepo has to be written.

## Development Considerations

Once the Compose file is re-homed, run a standing stack, inspect it, then stop
it with `docker compose -f tests/integration/docker-compose.yaml up -d --wait`,
`docker compose -f tests/integration/docker-compose.yaml run --rm <tests service> go test -count=1 -run TestUpdate ./...`
and `docker compose -f tests/integration/docker-compose.yaml down --volumes`.

All tests live in one package in this directory; `client_test.go` holds the
HTTP, MCP and polling helpers. Add a test here only when it needs more than one
service; single-service behaviour belongs in that service's E2E suite.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/). The last
release of this suite, made before it joined the monorepo, was `v0.0.3`.

---

Part of the Mnemonic monorepo. See the [design documents](../../docs/design/README.md)
for architecture and design documentation.
