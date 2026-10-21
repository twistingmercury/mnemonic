# mnemonic-integration-tests

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

Run the complete suite in a self-contained Docker Compose stack. No OpenAI key
is needed:

```bash
make test
```

Available targets:

- `make test` — start the stack, run the suite, then tear it down
- `make start` — start the stack without running tests
- `make stop` — remove this Compose stack and its volumes
- `make analyze` — lint all tracked Markdown files
- `make help` — list targets (default)

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

`openai-stub/` replaces OpenAI for all three services through
`LIBRARIAN_OPENAI_BASE_URL`, so runs are deterministic and free:

- **Embeddings** hash each distinct word of the input into the vector, so
  cosine similarity tracks shared words. Tests search for a random word only
  one pattern contains, with a low threshold (0.1).
- **Concept extraction** returns the concepts on a `Concepts: a, b` line of
  the pattern content.
- **`POST /control/fail?match=X&n=N`** fails the next N embedding requests
  whose input contains X.

The enricher runs with `LIBRARIAN_OPENAI_RETRY_ATTEMPTS=0` and
`LIBRARIAN_ENRICHMENT_RETRY_DELAY=1s` so one forced failure reaches its retry
sweep quickly.

## Key Considerations

- **Prerequisites:** Docker Compose and pull access to the service images on
  ghcr.io. The tests run in the stock `golang:1.27.2-alpine` image with `src/`
  mounted read-only; only the small stub image is built.
- **Images under test:** the `latest-dev` tags of `mnemonic-api`, `mnemonic`
  (MCP) and `mnemonic-enrichment`, published by each service's CI on merge to
  `develop`.
- **Isolation:** the stack publishes no host ports. Compose scopes its
  containers, network and volumes under the `mnemonic-integration-tests`
  project name, so it can run alongside other Mnemonic stacks.

## Continuous Integration

GitHub Actions runs the suite on pull requests to `develop` and `main`, on
every push to `develop`, daily at midnight UTC, and on demand. On failure it
prints Compose service status and logs before tearing the stack down.

## Development Considerations

Run a standing stack, inspect it, then stop it:

```bash
make start
docker compose -f docker-compose.yaml run --rm librarian_tests go test -count=1 -run TestUpdate ./...
docker compose -f docker-compose.yaml logs librarian_enricher
make stop
```

All tests live in one package under `src/`; `client_test.go` holds the HTTP,
MCP and polling helpers. Add a test here only when it needs more than one
service; single-service behaviour belongs in that service's E2E suite.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/). Version
is determined from Git tags:

```bash
git describe --tags --always
```

The current release tag is `v0.0.3`.

---

Part of the Mnemonic multi-repository workspace. See
[mnemonic-docs](https://github.com/twistingmercury/librarian-docs) for
architecture and design documentation.
