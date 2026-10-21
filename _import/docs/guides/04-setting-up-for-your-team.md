# Setting Up Mnemonic for Your Team

This guide walks through setting up Mnemonic on a local or trusted internal network. It assumes you've read the Quick Start or want a thorough foundation from scratch.

> **Important:** Mnemonic is in early development. This setup is intended for local machines and trusted team networks only — not for public-facing or cloud deployments. There is no authentication, authorization, or encryption in transit. The infrastructure and deployment model will change significantly as Mnemonic evolves toward a cloud-native platform.

## Prerequisites

Before starting, ensure you have:

- **Docker and Docker Compose** installed and running
- **An OpenAI API key** with access to the `text-embedding-3-large` (embeddings) and `gpt-4o-mini` (concept extraction) models (<https://platform.openai.com/api-keys>)
- **curl, jq, and yq** (mikefarah/yq) for API interaction and pattern loading scripts
- **Git** to clone repositories
- The mnemonic-docs and mnemonic-patterns repositories (see next section)

Verify Docker is running:

```bash
docker --version
docker compose version
```

Verify you have required tools:

```bash
curl --version
jq --version
yq --version
```

If any are missing, install them using your system's package manager.

## Step 1: Clone the Repositories

You need two repositories to run Mnemonic: the orchestration repo (docker-compose.yaml) and the pattern library.

```bash
git clone https://github.com/twistingmercury/librarian-docs.git
git clone https://github.com/twistingmercury/mnemonic-patterns.git
```

The mnemonic-docs repo contains the docker-compose.yaml that starts all seven services. The mnemonic-patterns repo contains validation and loading scripts, plus a library of curated patterns your team can build on.

You don't need to clone the individual service repos (mnemonic-mcp, mnemonic-api, mnemonic-enricher, mnemonic-admin, mnemonic-dbs) — Docker Compose pulls pre-built images automatically.

## Step 2: Configure the Environment

Mnemonic services use layered configuration (Viper): built-in defaults, then a config file (`/etc/mnemonic/config.yaml`, `./config.yaml`, or the path in `LIBRARIAN_CONFIG_FILE`), then `LIBRARIAN_*` environment variables, where each `.` in a config key becomes `_` (for example `openai.base_url` is `LIBRARIAN_OPENAI_BASE_URL`). For team setup, you only need to set one environment variable.

Export your OpenAI API key:

```bash
export LIBRARIAN_OPENAI_API_KEY="sk-your-key-here"
```

Replace `sk-your-key-here` with your actual API key.

All other configuration uses development defaults in the docker-compose.yaml:

| Service | Default Port | Default Credentials |
| ------- | ------------ | ----------------- |
| PostgreSQL | 5432 | mnemonic / mnemonic_dev |
| Neo4j | 7474 (HTTP), 7687 (Bolt) | neo4j / mnemonic_dev |
| RabbitMQ | 5672 (AMQP), 15672 (Management) | mnemonic / mnemonic_dev |
| MCP Server | 8081 (`/mcp`) | — (read-only, no auth) |
| Admin API | 8080 (`/v1/api`, `/health`, `/version`, `/swagger/index.html`) | — (no auth in Phase 1) |
| Enricher | 8082 → container 8080 (`/health`, `/version` only) | — (internal use only) |
| Admin UI | 3000 | — (no auth in Phase 1) |

These defaults are appropriate for local machines and trusted team networks. The credentials are written literally into each service's `environment:` block in docker-compose.yaml; only `LIBRARIAN_OPENAI_API_KEY` is read from your shell. To change a credential on a shared server, edit docker-compose.yaml: change the database or broker's own variable (for example `POSTGRES_PASSWORD`, `NEO4J_AUTH`, `RABBITMQ_DEFAULT_PASS`) and the matching `LIBRARIAN_*` variable (for example `LIBRARIAN_DATABASE_POSTGRES_PASSWORD`, `LIBRARIAN_DATABASE_NEO4J_PASSWORD`, `LIBRARIAN_QUEUE_RABBITMQ_PASSWORD`) in every service that uses it. Do not expose these services to the public internet.

## Step 3: Start the Stack

Navigate to the mnemonic-docs directory and bring up all services:

```bash
cd mnemonic-docs
docker compose up -d
```

Docker Compose starts seven containers:

- PostgreSQL — pattern storage and vector embeddings
- Neo4j — pattern relationship graph
- RabbitMQ — asynchronous job queue
- MCP Server — MCP tools for Claude Code (port 8081)
- Admin API — REST API for pattern management (port 8080)
- Enricher — embedding and concept-extraction worker (health on port 8082)
- Admin UI — web interface for pattern browsing and management (port 3000)

Wait about 30 seconds for all services to become healthy. Check the status:

```bash
docker compose ps
```

All containers should show `Up` and (after a moment) `healthy` in the Status column (`dev_admin` has no health check).

Verify the services are responding. The MCP server's health endpoint is on its container port 8080, which docker-compose.yaml does not publish, so use its built-in probe:

```bash
curl -s http://localhost:18080/health | jq .   # Admin API
curl -s http://localhost:18082/health | jq .   # Enricher
docker compose exec dev_mcp /mnemonic --health
```

Each should report HTTP 200 / healthy with a status entry per dependency (PostgreSQL, Neo4j and the OpenAI embedding model; the API also checks RabbitMQ). If any returns an error, check logs:

```bash
docker compose logs dev_api
docker compose logs dev_mcp
```

## Step 4: Load the Pattern Library

The mnemonic-patterns repo includes a shell script that validates all patterns against the schema and loads them into Mnemonic via the REST API.

Navigate to the mnemonic-patterns directory:

```bash
cd ../mnemonic-patterns
```

Run the load script:

```bash
./install/load.sh
```

This script:

1. Validates all pattern files in the patterns/ directory (frontmatter, required sections, decorators)
2. POSTs each valid pattern to the Admin API at `http://localhost:18080/v1/api/patterns`
3. Reports results: loaded, skipped (409 conflict — pattern already exists), and failed
4. Exits with status 0 if all patterns loaded successfully

Example output:

```text
Loading patterns from ./patterns/
  ✓ go-error-handling
  ✓ go-testing-strategies
  ✓ database-migrations
  ✗ api-versioning (failed: 400 Bad Request)
  ⊘ git-workflow (skipped: 409 Conflict)

Loaded: 2, Skipped: 1, Failed: 1
```

If you want to point the script at a different Mnemonic server (e.g., a shared team instance), pass the server URL:

```bash
./install/load.sh --server http://your-server:8080
```

The script uses curl, yq, and jq to interact with the API. If loading fails, ensure:

- The Admin API is running and healthy (`curl -s http://localhost:18080/health`)
- Each pattern's frontmatter is valid YAML and contains the fields the API requires (`name`, `entity_type`, `language`, `domain`) with values the API accepts (see [Pattern File Schema](../reference/pattern-schema.md))

An invalid OpenAI key does not stop loading: the API accepts the pattern and the enricher later marks it `failed` with the OpenAI error in `enrichment_error`.

## Step 5: Verify Enrichment

After patterns are loaded, the enricher worker generates an embedding for each chunk and then extracts concepts into Neo4j, asynchronously. This process typically takes seconds to minutes, depending on the number of patterns and OpenAI API rate limits.

Check enrichment status:

```bash
curl -s http://localhost:18080/v1/api/patterns?limit=10 | jq '.data[] | {name, enrichment_status}'
```

Look for output like:

```json
{
  "name": "go-error-handling",
  "enrichment_status": "enriched"
}
```

Patterns should eventually show `"enriched"` (not `"pending"` or `"failed"`). If a pattern stays in `"pending"` for more than a few minutes, check the enricher logs:

```bash
docker compose logs dev_enricher -f
```

Common causes of enrichment failure:

- Invalid OpenAI API key (check the LIBRARIAN_OPENAI_API_KEY environment variable)
- OpenAI API rate limit reached (the enricher retries failed jobs automatically, 30 seconds apart by default)

For a failed pattern, `curl -s http://localhost:18080/v1/api/patterns/{id} | jq .enrichment_error` shows the error.

Once all patterns show `"enriched"`, your knowledge base is ready for search.

## Step 6: Configure Developers' AI Coding Assistants

Each team member configures their AI assistant to connect to Mnemonic as an MCP server. The MCP interface is read-only — developers can search patterns but cannot create or modify them through the assistant.

### Claude Code Configuration

Claude Code supports two levels of MCP configuration: project-level and user-level.

**Project-level (recommended for teams):**

In the root of each code repository your team works in (not the mnemonic-docs clone), run:

```bash
claude mcp add -s project --transport http mnemonic http://localhost:18081/mcp
```

This writes a `.mcp.json` file that you commit so the whole team shares it:

```json
{
  "mcpServers": {
    "mnemonic": {
      "type": "http",
      "url": "http://localhost:18081/mcp"
    }
  }
}
```

**User-level (personal setup):**

If you prefer not to commit .mcp.json to your repo, add the server for yourself across all projects:

```bash
claude mcp add -s user --transport http mnemonic http://localhost:18081/mcp
```

This stores the entry in `~/.claude.json`. Run `claude mcp list` (or `/mcp` inside Claude Code) to confirm "mnemonic" is connected; start a new Claude Code session if one was already open.

**For a remote team instance:**

If Mnemonic is running on a shared server instead of localhost, replace `http://localhost:18081` with the server's address and port:

```json
{
  "mcpServers": {
    "mnemonic": {
      "type": "http",
      "url": "http://mnemonic.team.example.com:8081/mcp"
    }
  }
}
```

### Other MCP-Compatible Assistants

If your team uses a different AI assistant that supports MCP over Streamable HTTP and can reach the server on your network, follow these steps:

1. Locate the assistant's MCP configuration file (check documentation for exact path)
2. Add a new MCP server entry with these settings:
   - **Name:** mnemonic
   - **Transport:** Streamable HTTP (no auth headers)
   - **URL:** <http://localhost:18081/mcp> (or your server's address)
3. Restart the assistant

## Step 7: Test the Integration

Verify that your developers can search Mnemonic from their AI assistant.

In Claude Code or your chosen assistant, ask a natural-language question that matches one of your loaded patterns:

> Search Mnemonic for our error handling pattern in Go.

or

> Find information about database migration strategies in Mnemonic.

The assistant should invoke the `search_patterns` MCP tool and return relevant patterns from your knowledge base. If the search works, your team is ready to use Mnemonic.

## Operational Basics

### Health Checks

Monitor the health of the stack:

```bash
curl -s http://localhost:18080/health | jq .   # Admin API
curl -s http://localhost:18082/health | jq .   # Enricher
docker compose exec dev_mcp /mnemonic --health
```

Each `/health` returns HTTP 200 when the service is healthy (a warning on OpenAI or RabbitMQ does not fail it). Each service also serves `GET /version`. Prometheus metrics are served on container port 9090 at `/metrics`, which docker-compose.yaml does not publish; add a port mapping if you want to scrape them from the host.

### Logs

All Mnemonic services emit structured JSON logs via zerolog (`LIBRARIAN_LOGGING_FORMAT`, default `json`; the compose file sets `LIBRARIAN_LOGGING_LEVEL=debug`). View logs for a specific service:

```bash
docker compose logs -f dev_api
docker compose logs -f dev_mcp
docker compose logs -f dev_enricher
```

View logs for all services:

```bash
docker compose logs -f
```

### Database Tools

**Neo4j Browser** (pattern relationship graph):

Open <http://localhost:17474> in a web browser. Log in with credentials: neo4j / mnemonic_dev.

From the Neo4j browser, you can explore the pattern relationship graph, run Cypher queries, and troubleshoot pattern connections.

**RabbitMQ Management UI** (job queue):

Open <http://localhost:25672> in a web browser. Log in with credentials: mnemonic / mnemonic_dev.

Use the management UI to inspect queues, monitor job throughput, and troubleshoot queue issues. The API publishes one message per chunk enrichment job to the `enrichment-jobs` queue, which the enricher consumes. Jobs also live in PostgreSQL, so a job whose message is lost is still picked up by the enricher's retry sweep.

**Admin UI** (web-based pattern browser):

Open <http://localhost:13000> in a web browser. No login required in Phase 1.

The Admin UI provides a web interface for searching patterns, viewing pattern details, and managing the pattern library. This is useful for admins and pattern authors.

### Stopping and Restarting

**Stop all services:**

```bash
docker compose down
```

**Stop all services and remove data:**

```bash
docker compose down -v
```

The `-v` flag removes Docker volumes, which deletes all data in PostgreSQL, Neo4j, and RabbitMQ. Use this to reset your local environment. Do not use `-v` in production unless you intend to delete all data.

**Restart the stack:**

```bash
docker compose up -d
```

Data is preserved between restarts unless you used `down -v`.

### Troubleshooting

**A service won't start:**

Check the logs for that service:

```bash
docker compose logs dev_api  # or dev_mcp, dev_enricher, etc.
```

Common causes:

- Port already in use (change the port in docker-compose.yaml)
- Insufficient disk space (free up space and retry)
- Missing or invalid OpenAI API key (check LIBRARIAN_OPENAI_API_KEY)

**Patterns load but enrichment fails:**

Check the enricher logs:

```bash
docker compose logs dev_enricher
```

Common causes:

- Invalid OpenAI API key
- OpenAI API rate limit (wait and retry)
- One chunk failed, which marks the whole pattern `failed` until its retry succeeds (check `enrichment_error`)

**MCP server is not responding:**

Verify the MCP server is running:

```bash
docker compose ps dev_mcp
docker compose exec dev_mcp /mnemonic --health
```

If the container is running but the health check fails, check logs:

```bash
docker compose logs dev_mcp
```

**Search results are empty:**

Verify patterns are enriched:

```bash
curl -s http://localhost:18080/v1/api/patterns?limit=5 | jq '.data[] | {name, enrichment_status}'
```

All patterns should show `"enriched"`. If they show `"pending"`, wait a moment and retry. If they show `"failed"`, check the enricher logs. Also note that the MCP `search_patterns` tool uses a 0.5 similarity threshold by default (`LIBRARIAN_MCP_DEFAULT_SEARCH_THRESHOLD`) and the REST search uses 0.7; a lower threshold returns more results.

## Security Considerations

**Mnemonic does not currently support public or cloud deployment.** The following limitations apply:

- **No authentication or authorization** — all API endpoints and the MCP endpoint are open. The MCP interface is read-only, but the REST API allows unrestricted pattern creation, modification, and deletion.
- **No encryption in transit** — all traffic is unencrypted HTTP.
- **No audit trail** — there is no record of who accessed or modified patterns.

**You must not expose Mnemonic to the public internet.** Run it only on local machines or trusted internal networks (e.g., a corporate LAN or VPN).

To reduce risk on shared internal networks:

- Restrict database and queue access to the Docker network only
- Use strong credentials for RabbitMQ, PostgreSQL, and Neo4j (edit them in docker-compose.yaml, as described in Step 2)
- Monitor logs regularly for unusual activity

Authentication, authorization, and secure transport are planned for future releases as Mnemonic moves toward a cloud-native deployment model.

## Customization

### Changing Ports

If a default port conflicts with another service, edit the docker-compose.yaml file and change the port mapping:

```yaml
dev_api:
  ports:
    - "8080:8080"  # Change first 8080 to your desired port
```

Then restart:

```bash
docker compose down
docker compose up -d
```

### Using a Different OpenAI Embedding Model

The default is `text-embedding-3-large` with 2000 dimensions (`openai.embedding_model`, `openai.embedding_dimensions`), which matches the `vector(2000)` column in PostgreSQL. The API and MCP server embed search queries and the enricher embeds chunks, so all three must use the same model. docker-compose.yaml does not pass these variables through from your shell, so add them to the `environment:` block of `dev_api`, `dev_mcp`, and `dev_enricher`:

```yaml
LIBRARIAN_OPENAI_EMBEDDING_MODEL: your-embedding-model
LIBRARIAN_OPENAI_EMBEDDING_DIMENSIONS: 2000
```

The model must be able to return 2000-dimensional vectors (for example, `text-embedding-3-small` cannot: its maximum is 1536), because the dimension count must match the database column. After switching models, existing embeddings are not comparable with new query embeddings: re-load or update every pattern so it is re-enriched. To point all three services at an OpenAI-compatible endpoint, set `LIBRARIAN_OPENAI_BASE_URL` the same way.

### Persisting Data Across Restarts

By default, data persists in Docker volumes. To verify volumes are not cleaned up on shutdown, always use `down` without `-v`:

```bash
docker compose down  # Keeps volumes and data
```

To permanently delete all data and start fresh:

```bash
docker compose down -v  # Removes volumes and data
docker compose up -d
```

### Scaling for a Larger Team

For larger teams sharing a single instance on an internal network:

- Deploy on a shared server accessible over the team's LAN or VPN
- Adjust resource limits in docker-compose.yaml (memory, CPU)
- Change the default credentials in docker-compose.yaml

Cloud-native deployment (managed databases, Kubernetes, authentication, SSL) is not yet supported but is on the roadmap. Expect the deployment model to change significantly in future releases.

## What's Next

**[Authoring Patterns](05-authoring-patterns.md)** — Learn how to write patterns that work well with semantic search. Covers frontmatter conventions, section structure, and best practices for clarity.

**[Managing Patterns](06-managing-patterns.md)** — Keep your knowledge base organized and up-to-date. Strategies for bulk loading, versioning, deprecation, and maintaining pattern quality.

**[Architecture Documentation](../architecture/)** — Deep dives into system design, data architecture, deployment architecture, and observability.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
