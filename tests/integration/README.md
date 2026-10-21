# Mnemonic integration tests

> **Maturity Level**: Emerging - black-box end-to-end tests for the complete
> Mnemonic stack.
> **Version**: v0.0.3

---

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Continuous Integration](#continuous-integration)
- [Development Considerations](#development-considerations)
  - [Quick Start](#quick-start)
  - [Building & running](#building--running)
  - [Testing](#testing)
  - [Versioning](#versioning)

## Usage

Run the suite through the build script of the component you changed. Each
script builds that component's image, then runs this whole suite against it in
containers, and never pushes:

```bash
make build-api        # build/api/api.build.sh
make build-mcp        # build/mcp/mcp.build.sh
make build-enricher   # build/enricher/enricher.build.sh
```

The suite always covers all three services. The component just built runs from
its fresh local image; the other two come from `ghcr.io` (`latest-dev`) unless
you point them elsewhere (see [Key Considerations](#key-considerations)). No
OpenAI key is needed.

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

[docker-compose.yaml](docker-compose.yaml) defines the stack: Postgres, Neo4j,
RabbitMQ, the OpenAI stub, the three services and a `mnemonic-tests` runner.
The runner is the [Dockerfile](Dockerfile) in this directory (Python with
`uv` and `pytest`). Its exit code is the exit code of the run. The build
scripts start the stack under their own compose project name and remove only
what that project created, also when a test fails or the run is interrupted.

The suite is black-box. It reaches the services only over HTTP, MCP, AMQP and
the database wire protocols, by service name on the compose network, and never
imports or reads service source. `build/check-e2e-boundary.sh` and
`tests/test_blackbox.py` both enforce that.

| Test file | What it covers |
| --- | --- |
| `test_stack_flows.py` | Each service's `/health` lists every dependency as `OK`; a pattern flows from API to enricher to MCP search and `get_pattern`; an update is re-enriched; related patterns and delete; the enricher's retry sweep recovers a failed job |
| `test_smoke_images.py` | Things only the built images can show: stamped `version`, `build_date` and `commit`, the `/metrics` listener, swagger, and the api serving no `/mcp` |
| `test_mcp_edge.py` | MCP over the real listener: a pending pattern, a pattern with no graph node, unknown ids, cross-site `POST` returns 403, `GET` returns 405, a client sending no MCP headers |
| `test_enricher_failures.py` | Jobs seeded in Postgres and published straight to RabbitMQ: a terminal OpenAI failure, a malformed message, an unknown job id |
| `test_api_behavior.py` | API behavior that needs a real database and a running enricher: search excludes a pattern until it is enriched, `enrichment_error`, list filters, pagination, PUT replacement, a failed update keeps the prior state |

Single-service behavior that needs no other service belongs in that service's
Go tests, not here. The `src/mnemonic_e2e` package holds the helpers (HTTP,
MCP, AMQP, Postgres and Neo4j access, the stub client and polling).

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

- **Prerequisites:** Docker with Compose, and pull access to ghcr.io for the
  images you do not build locally.
- **Images under test:** `MNEMONIC_API_IMAGE`, `MNEMONIC_MCP_IMAGE` and
  `MNEMONIC_ENRICHER_IMAGE` choose the image of each service (default
  `ghcr.io/twistingmercury/mnemonic-<n>:latest-dev`).
  `MNEMONIC_API_PULL_POLICY`, `MNEMONIC_MCP_PULL_POLICY` and
  `MNEMONIC_ENRICHER_PULL_POLICY` set the compose `pull_policy` of each
  (default `always`, so a clean CI runner and a laptop behave the same). A
  build script sets the image and `never` for the component it just built and
  leaves the other two as you set them.
- **Runner settings:** the runner reads the variables below; the defaults are
  the service names, ports and development credentials of the compose file, so
  you only set them to point the suite at a stack you run yourself.

  | Variable | Default |
  | --- | --- |
  | `MNEMONIC_API_URL` | `http://mnemonic-api:8080` |
  | `MNEMONIC_MCP_URL` | `http://mnemonic-mcp:8081` |
  | `MNEMONIC_MCP_OPS_URL` | `http://mnemonic-mcp:8080` |
  | `MNEMONIC_ENRICHER_URL` | `http://mnemonic-enricher:8080` |
  | `MNEMONIC_OPENAI_STUB_URL` | `http://openai_stub:8090` |
  | `MNEMONIC_METRICS_PORT` | `9090` |
  | `MNEMONIC_DATABASE_URL` | `postgresql://mnemonic:mnemonic_dev@postgres:5432/mnemonic` |
  | `MNEMONIC_NEO4J_URI` | `bolt://neo4j:7687` |
  | `MNEMONIC_NEO4J_USER` | `neo4j` |
  | `MNEMONIC_NEO4J_PASSWORD` | `mnemonic_dev` |
  | `MNEMONIC_NEO4J_DATABASE` | `neo4j` |
  | `MNEMONIC_RABBITMQ_URL` | `amqp://mnemonic:mnemonic_dev@rabbitmq:5672/` |

- **Serial runs only:** failure injection is global to the stub (`stub.reset()`
  clears every injected failure), so tests that inject one must not run in
  parallel with each other. Run the suite without a parallelizing plugin.
- **Isolation:** the stack publishes no host ports, so it can run alongside
  other Mnemonic stacks. Every test makes its own patterns with unique names
  and search tokens, so the tests share one stack in any order.

## Continuous Integration

The CI workflows are not written yet. When they are, they should run the build
script of each changed component, the same as a local run.

## Development Considerations

### Quick Start

The project uses `uv` and Python 3.14. From this directory:

```bash
uv sync --frozen
uv run --frozen pytest -q --ignore=tests/test_stack_flows.py --ignore=tests/test_smoke_images.py --ignore=tests/test_mcp_edge.py --ignore=tests/test_enricher_failures.py --ignore=tests/test_api_behavior.py
```

### Building & running

To run the stack tests without a build script, start the stack from the
repository root, then run the runner image on the compose network:

```bash
docker compose -p mnemonic-it -f tests/integration/docker-compose.yaml up --build --abort-on-container-exit --exit-code-from mnemonic-tests
docker compose -p mnemonic-it -f tests/integration/docker-compose.yaml down -v --remove-orphans --rmi local
```

Set the `MNEMONIC_<N>_IMAGE` and `MNEMONIC_<N>_PULL_POLICY` variables on both
commands to test a local image. To run one test against a standing stack, start
the services without the runner, then run the runner image with `--network`
set to the compose network and the test path as its argument.

### Testing

The files above are of two kinds:

- **Unit tests** (`test_config.py`, `test_db.py`, `test_api.py`, `test_mcp.py`,
  `test_stub.py`, `test_tokens.py`, `test_wait.py`, `test_smoke_scaffold.py`,
  `test_blackbox.py`) test the helper package and need no stack. Run them with
  the `--ignore` flags shown under Quick Start.
- **Stack tests** (the five files in the table above) need the running compose
  stack and run in the runner container.

Before a change, run the gates: `uv run --frozen ruff format --check .`,
`uv run --frozen ruff check .`, `uv run --frozen mypy .` and
`uv run --frozen bandit -c pyproject.toml -r src -q`, then
`build/check-e2e-boundary.sh` from the repository root.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/). The last
release of this suite, made before it joined the monorepo, was `v0.0.3`.

---

Part of the Mnemonic monorepo. See the [design documents](../../docs/design/README.md)
for architecture and design documentation.
