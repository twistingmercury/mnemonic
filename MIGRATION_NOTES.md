# Librarian monorepo: Phase 0 inventory

Written 2026-10-21. Read-only inventory of the eight fork repos. Nothing created, copied or changed except this file.

## Decisions made (2026-10-21)

- **License: Functional Source License, FSL-1.1-ALv2** (converts to Apache 2.0 after two years), one `LICENSE` at the repo root for everything. Permits any use except a competing product or service (so no hosting it for profit). The text must be copied unmodified from the official source (fsl.software) in Phase 2, with the copyright line filled in by the owner. Nothing authored by me. The five existing Apache copies, dbs (proprietary), gateway (MIT) and docs (CC-BY) are not carried over. Source-available, not OSI open source: the README must not call it "open source".
- **`librarian-docs` is not part of the monorepo.** No `docs/` tree is created. The repo stays untouched where it is.

- **Admin is removed from the monorepo for now.** No `web/admin/`, no npm/admin Docker/Dependabot entries, no admin gate, no `admin/v*` tag, no admin CI. `librarian-admin` stays untouched where it is. Admin-dependent items (admin typecheck/lint/tests gate, `compose` admin service if any, README mentions) are dropped; A grep of the gateway shows only Envoy/Keycloak/OPA "admin" (Envoy admin interface, Keycloak bootstrap admin, an admin role in `api.rego`), not the UI; I'll still check CORS origins and port 3000 in Phase 2.
- **Design docs: `docs/design/` at the monorepo root** (written with `/design-docs-writer`), and nothing else under `docs/`. The "any knowledge" design doc goes there, copied, with a Librarian-flavored name.
- **Wiki (local sibling `librarian.wiki/`): how to use, quick starts, installation, troubleshooting.** Architecture/reference content stays in `librarian-docs` only.

## Answers received (2026-10-21)

- Wiki page set: the proposed one (introduction, quickstart, authoring, managing, team setup as installation, plus new troubleshooting).
- Docs: there is no separate docs repo going forward; the docs content goes under `docs/design/` in the monorepo. READMEs link there. (My reading: the `librarian-docs` architecture/design/reference pages are copied into `docs/design/`, the usage pages go to the wiki. Confirm.)
- Leave the `mnemonic` names. Registry `ghcr.io/twistingmercury/librarian-*` confirmed.
- Copyright holder: Jeremy K. Johnson (FSL header: `Copyright 2025-2026 Jeremy K. Johnson` or year to be confirmed).
- Module path: no objection raised, treated as `github.com/twistingmercury/librarian`.
- Gateway release tags: not answered; plan default stands (no gateway image, no tag).

## Still open

1. **Repo location.** Owner wants the repo at `~/dev/librarian` itself (no stutter). That directory currently holds the eight forks, the two plans, `CLAUDE.md` and this file. Needs a layout decision before `git init`.
2. Confirm the docs reading above.

## Repo state

All eight repos are clean (no uncommitted changes), no remotes. Heads match the handoff: admin 5605feb, api 72179b2, mcp 09716d3, enricher 6a8e807, dbs 49f8baf, integration-tests 7adfef5, docs c11a36f, gateway cc9924c.

Gitignored local files in the forks (not tracked, so they won't be copied by a git-aware copy; a plain `cp -r` would pick them up, so copy via `git ls-files` or `git archive`):
- admin: `dist/`, `node_modules/`, `test-results/`
- api: `.claude/settings.local.json`, `.idea/`, `local/`, `src/coverage.out`, and `docs/code_reviews/`, `docs/plans/remove_mcp_listener/` (PRD.md, PROMPT.md)
- mcp: `src/.bin/`; enricher: `.bin/` and the whole `docs/` directory is ignored
- `mnemonic-patterns` is out of scope.

## Differences from the plan's assumptions

- **Entry points are `src/cmd/main/`** in all three services, not `cmd/<name>/`. Phase 1 renames to `src/cmd/api|mcp|enricher/`. Each has a `main_test.go`.
- **Five OpenAI stubs, not three** (all different): api (71 lines), mcp (71), enricher e2e (132), integration-tests (202), gateway `test/stub` (44). Merging into one `tests/openai-stub/` is a real consolidation, not a copy. Gateway stub is on Go 1.24 and has its own Dockerfile and `compose.stub.yaml`.
- **Go versions:** all modules 1.27.2 except enricher e2e `openai-stub` (1.26.6, as expected) and both gateway test modules (**1.24**, not mentioned in the plan).
- **`librarian-helm-chart` is not empty.** It's the stock `helm create` scaffold (`Chart.yaml` named `mnemonic-helm-chart`, ingress/hpa/httproute templates). Still junk; dropping it as planned.
- **Existing `build.sh` scripts** (api/mcp/enricher) use `--no-cache`, `git describe --tags --abbrev=0` with no `--match` (so one repo-wide version), tag `${IMAGE_TAG}` and `:latest`, and also run an e2e step through `src/tests/docker-compose.yaml`. Phase 2 has to split image build from e2e and add `version.sh`. The `PULL_POLICY` env handling the plan mentions wasn't in `api/build.sh`; I haven't checked mcp/enricher/dbs/admin, will confirm in Phase 2.
- `api` has generated Swagger code at `src/docs/swagger/docs.go` (the only generated code found); `make docs-swagger` regenerates it and its paths mention `src/mnemonic/docs/swagger/`.
- No `go.work` and no `replace` directives anywhere.
- **Per-service `internal/` packages** have the same names in all three (`config database handlers health metrics queue repository server service telemetry version`; mcp has `mcpserver`, enricher has `enricher`, api has `middleware`). Moving to `src/internal/<name>/` avoids collisions as the plan says.

## Per-repo inventory

| Repo | Dockerfiles / build | Makefile targets | CI workflow |
|---|---|---|---|
| api | `src/build/Dockerfile`, `src/build/build.sh`; e2e and stub Dockerfiles | default build analyze tests-unit tests-integration tests-bench docs-swagger start stop help | `mnemonic-api-ci.yaml` (90 lines), dependabot |
| mcp | `src/build/{Dockerfile,build.sh}`; e2e, stub | help local build start stop analyze tests-unit tests-coverage tests-bench | `mnemonic-ci.yaml` (70), dependabot |
| enricher | `src/build/{Dockerfile,build.sh}`; e2e, e2e/openai-stub | default local build analyze tests-unit tests-bench tests-db-graph help | `mnemonic-enrichment-ci.yaml` (73), dependabot |
| dbs | `src/postgres/{Dockerfile,build.sh}`, `src/neo4j/{Dockerfile,build.sh}` | root: default help build build-postgres build-neo4j test test-postgres test-neo4j; `src/Makefile`: tests-postgres tests-neo4j tests | `mnemonic-ci.yaml` (102) |
| admin | `build/{Dockerfile,build.sh}` | default help dev build build-local test test-coverage e2e typecheck lint lint-fix format format-check | `frontend-ci.yaml` (70) |
| gateway | `test/stub/Dockerfile` only (no image of its own) | help test-policy validate-envoy up down e2e e2e-stub k8s-cluster k8s-images k8s-config k8s-secret k8s-up k8s-forward k8s-forward-stop k8s-test k8s-down k8s-delete | `gateway-ci.yaml` (42) |
| integration-tests | none (stub Dockerfile only) | test start stop analyze help | `integration-tests.yaml` (46), dependabot |
| docs | none | help analyze | none |

The CI workflows are 493 lines total and still carry `mnemonic-*` filenames. They'll be replaced by the new workflows in Phase 3, not ported.

Go modules (module path, all `go 1.27.2` unless noted):
- `librarian-api`, `librarian-mcp`, `librarian-enricher` (at `src/`)
- nested test modules: `<svc>/tests/e2e` ×3, `<svc>/tests/openai-stub` ×2, enricher `tests/e2e/openai-stub` (1.26.6)
- `librarian-integration-tests` (at `src/`) and its `openai-stub`
- gateway `test/e2e`, `test/stub` (1.24)

`heartbeat/v2 v2.0.0` and `otelx/v2 v2.0.0` are at the same version in all three services; `otelx/middleware/gin/v2` in api and enricher. So merging requirements should be conflict-free for those; the rest I'll compare in Phase 2.

## Lint and ignore configs

- `.markdownlint.json`: two variants. `c82b13d1…` in admin, api, dbs, enricher, mcp; `ad734ce5…` in docs and integration-tests. I'll diff them before picking.
- `.markdownlintignore` (5 copies) and `.shellcheckrc` (5 copies): identical across repos, so just copy.
- `.gitignore`: eight different files. `.dockerignore`: api root, plus `src/.dockerignore` in api (differs) and mcp/enricher (identical to each other). All need merging; none exist for gateway, dbs, docs, admin as `.dockerignore`.
- No `.dockerignore` at repo root of mcp/enricher; the plan's single root `.dockerignore` must be written fresh from these.

## Integration tests coupling

`librarian-integration-tests` has no import of any `librarian-*` package and no path reference into service source. Its only references are the compose image names (`ghcr.io/twistingmercury/librarian-{api,mcp,enricher}:latest-dev`). So Phase 2 step 5 is trivial. Its `go.mod` is `librarian-integration-tests` with no requirements on the services.

## Leftover `mnemonic` names (left alone, per plan)

Roughly 270 files across the forks still contain `mnemonic`. Main groups:
- **Auth:** Keycloak realm `mnemonic` (`keycloak/realm-mnemonic.json`, `http://localhost:18180/realms/mnemonic`), audiences `aud-mnemonic-api`, `aud-mnemonic-mcp`
- **Data:** DB user/db `mnemonic` / `mnemonic_dev`, `neo4j/mnemonic_dev`, `postgres://mnemonic:mnemonic_dev@…`, `localhost:15433/mnemonic`, test db `mnemonic_tests`
- **Runtime paths:** `/etc/mnemonic/config.yaml` (17 hits), `/mnemonic`, `/mnemonic-api`, `/mnemonic-enricher` (in-image binary names)
- **Metrics/trace attributes:** `mnemonic.db.query_latency`, `.query_errors`, `mnemonic.mcp.tool_calls`, `.tool_duration`, `mnemonic.patterns.*`, `mnemonic.queue.publish_failures_total`, `mnemonic.enrichment.*`, `mnemonic.http.request.*`, `mnemonic.job_id`, `.pattern_id`, `.chunk_id`
- **MCP tool prefix in docs:** `mcp__mnemonic__search_patterns`, `get_pattern`, `find_related_patterns`
- **Other:** problem-type URL `https://mnemonic.example.com/problems/not-found`, `mnemonic-patterns` references (27), the stock helm-chart helper names (only in the dropped skeleton), `mnemonicApiUrl` in admin, doc paths like `docs/api/openapi/mnemonic-v1.yaml`, `../architecture/mnemonic/08-mcp-tools.md`

Note `docs/architecture/mnemonic/` is itself a directory name, so the monorepo `docs/` tree will keep a `mnemonic` folder unless you say to rename it.

## Docs sorting (`librarian-docs`)

Superseded in part: docs is out of the monorepo, so the `docs/` column below no longer applies. Only the wiki split (and what happens to the design doc) is still open. Kept for reference.

README says "Mnemonic Docs" and is itself a multi-repo index (rewrite). Files: concept, requirements, requirements-traceability, diagrams (7), guides (6), design (8), reference (2), architecture (admin 5, dbs 5, mnemonic 2, system 9), `code_reviews/cross_repository_sot_audit_v01.md`, plus a stray `docker-compose.yaml` and `Makefile`.

Proposed:
- **`docs/` (architecture/design/reference):** concept, requirements, requirements-traceability, diagrams/, architecture/ (all), design/ (all 8, existing, so `docs/design/` already has content and the "any knowledge" doc goes beside it), reference/ (glossary, pattern-schema), code_reviews/ audit (historical; probably keep, flag).
- **Wiki (usage):** `guides/01-introduction`, `03-quickstart`, `05-authoring-patterns`, `06-managing-patterns`.
- **Ambiguous, your call:**
  - `guides/02-concepts` (356 lines): explains concepts to users but overlaps `docs/concept.md` and the glossary.
  - `guides/04-setting-up-for-your-team` (489 lines): usage, but 7 multi-repo hits, will need a rewrite either way.
  - `reference/pattern-schema.md`: user-facing format spec, could be either.
  - `code_reviews/cross_repository_sot_audit_v01.md`: about the multi-repo setup, likely stale.
- Rewrite for the monorepo regardless: `README.md`, `03-quickstart` (4 hits), `04-setting-up-for-your-team` (7), `02-concepts` (1).
- The design-doc source `~/dev/mnemonic/Mnemonic From Coding Patterns to Any Knowledge.md` exists (confirmed in the directory listing; not opened).

## Gateway / Helm specifics for re-pointing

- Chart: `librarian-gateway/deploy/helm/librarian/` (`Chart.yaml`, `templates/`, `values.yaml`); e2e script `deploy/test/k8s-e2e.sh`. Values image refs: `ghcr.io/twistingmercury/librarian-{api,mcp,enricher}:latest-dev`, `librarian-postgres:v1.0.0-dev`, `librarian-neo4j:v1.0.0-dev`, plus stock `envoyproxy/envoy:v1.34.1`, `openpolicyagent/opa:1.4.2-envoy-static`, `keycloak/keycloak:26.2.5`, `rabbitmq:4-management-alpine`. These all match the plan's tag expectations.
- Gateway Makefile's `k8s-images` hardcodes the three service image names; `docker-compose.yaml` takes `${LIBRARIAN_*_IMAGE}` overrides. Gateway e2e does `cd test/e2e`, which becomes `gateway/test/e2e`.
- Gateway compose service names are `librarian-api` / `librarian-mcp` / `librarian-enricher`.

## Known and unverified (carried forward)

From plan sections 12-13, unchanged: Neo4j healthcheck in api/mcp/enricher compose files is the old one; `TestRepository_InstrumentsOperations` fails at `-count=3`; `hadolint` not installed; Neptune behavior unverified; RabbitMQ read-only rootfs and the "config edit then `make k8s-up` restarts pods" claim unverified; `TestForwardedHeaders` skipped against the real stack; search threshold 0.5; host `inotify` limit 128.

Unproven until a remote exists (Phase 3-5): GitHub Actions runs, cache hit rate, warm-rebuild speed, Dependabot (`dependabot.yml` does nothing without a remote), real tag-triggered releases.

## Not checked in Phase 0

I didn't read file contents beyond what's quoted above: the CI workflows, the Dockerfiles, `PULL_POLICY` handling in the other build scripts, the two `.markdownlint.json` differences, and the dependency version spread outside heartbeat/otelx. Those are Phase 2 inputs.
