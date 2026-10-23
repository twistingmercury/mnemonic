---
version: 7
date: 2026-10-23
notes: Change detection ignores *.md, *.build.sh and db/tests/ paths, so a change confined to them builds nothing. The db build scripts must be committed executable. The first GitHub runs confirmed the image mount, the jq mount, the artifact download and the GHCR push.
---

# CI Workflows

[Design documents](README.md) | [Project README](../../README.md)

## Table of Contents

- [Intent](#intent)
- [Goals, non-goals and anti-goals](#goals-non-goals-and-anti-goals)
- [Constraints](#constraints)
- [Decisions](#decisions)
- [Triggers](#triggers)
- [Finding what changed](#finding-what-changed)
- [Jobs](#jobs)
- [Publishing images](#publishing-images)
- [Registry state and bootstrap](#registry-state-and-bootstrap)
- [Forks and going public](#forks-and-going-public)
- [Launch checklist (owner)](#launch-checklist-owner)
- [The required check](#the-required-check)
- [Calls made by the author](#calls-made-by-the-author)
- [Left to the builder](#left-to-the-builder)

## Intent

The monorepo needs continuous integration that behaves exactly like a local run, blocks bad merges without exceptions, and publishes images to GHCR only after a merge. The old per-component repos had solid CI; the one recurring pain was Go version drift between CI and local machines. The build scripts under `build/` already run every gate inside pinned Docker images, so the workflows stay thin: they decide what to run, call the scripts, and push.

## Goals, non-goals and anti-goals

Goals:

- A change to one service builds and tests that service, with its fresh image, against the registry images of the other two.
- A change to a Go package that more than one service imports rebuilds and tests every service that imports it.
- Pull requests never push an image. Only a merge to `develop` or `main` does.
- A failing run blocks merging into `develop` and `main`.

Non-goals (this phase):

- Release tagging and the version scheme beyond what `build/version.sh` already does (Phase 4).
- Dependabot and the wiki (Phase 5).
- The Kubernetes end-to-end run; it needs the real OpenAI key and stays a manual run by the owner.
- Changing the `-dev` suffix. The owner is considering dropping it and decided to leave it as it is for now.

Anti-goals:

- Any workflow step that can push from a pull request or a manual run.
- Layer caching anywhere (the owner rule: builds always run `--no-cache --pull`).
- A path filter that can leave a required check pending forever.

## Constraints

- Local and CI runs behave the same: an ephemeral clean runner, the same scripts, the same pinned images.
- The `build/<n>/<n>.build.sh` scripts build the image and run the e2e suite, and never push. Pushing is a separate workflow step.
- Nothing is installed on the runner. Every tool (shellcheck, hadolint, markdownlint, bats, helm, OPA, Envoy) runs from a Docker image pinned to a version tag and digest.
- Pinned Go toolchain image: `ghcr.io/twistingmercury/golang-tooling:go1.27.3`, changed in one place.
- No inline suppressions anywhere, workflows included.
- hadolint runs with `--failure-threshold warning`.
- Every environment variable the project defines starts with `MNEMONIC_`.

## Decisions

Decided by the owner:

- Triggers are pushes to `develop` and `main`, pull requests, and a manual run.
- The service under test uses the image just built; the other two services come from the registry as their latest images (`latest-dev` today).
- Affected services are those whose own folder changed plus every service importing a changed shared package.
- A failing CI run blocks merging into `develop` and `main`, with no exceptions.
- A local run of a `*.build.sh` never pushes.
- Versions stay per component, as they were in the old repos. The monorepo carries one prefixed git tag per component (`api/vX.Y.Z`, `mcp/vX.Y.Z`, `enricher/vX.Y.Z`, `db/vX.Y.Z`, later `helm/vX.Y.Z`); a commit can carry several tags. The current versions are seeded once as tags on the current `develop` commit: `api/v0.5.0`, `mcp/v0.6.0`, `enricher/v0.5.0`, `db/v1.0.0`. Pushing those tags needs the owner's explicit go-ahead.
- `main` does not exist yet and will be branched from `develop`. Until then only the `develop` publishing is live. The `main` publishing is written but dormant: on a merge to `main`, only affected components publish, each with `latest` and its own component version. This is revisited in Phase 4 when release tags exist.
- A push to `develop` publishes, for each affected component, `latest-dev` and `sha-<short>` (the short commit SHA, an immutable tag for tracing and rollback). It never publishes a version tag, because the build scripts tag images `v<version>` and a develop build would otherwise collide with a release.
- `latest-dev` stays as the floating develop tag. `latest` is reserved for the newest release from `main`. A pull request to `develop` pulls `latest-dev` for the peer services; a pull request to `main` will pull `latest`.
- Fork PRs are in scope: the repo is private now and becomes public once builds, tests, CI and the GHCR push all work.
- The `db` images (`mnemonic-postgres`, `mnemonic-neo4j`) publish on `develop` like the services (`latest-dev` and `sha-<short>`), only when `db/` changed and its tests passed. On `main` (later) they follow the same rule as the services, using the `db/vX.Y.Z` version. `gateway/` publishes nothing: its only Dockerfile is a test stub. `deploy/helm` publishes nothing in this phase; publishing the chart is Phase 4.
- Registry access uses `GITHUB_TOKEN` with `packages: write`; no personal access token.

Proposed by the author and accepted as a starting point ("if it needs changing later, fine"): everything under [Jobs](#jobs), [Publishing images](#publishing-images) and [The required check](#the-required-check) that is not listed above.

## Triggers

- `push` to `develop` and `main`.
- `pull_request` targeting `develop` and `main`.
- `workflow_dispatch`, with an input to pick one component or all. The manual run builds and tests; it never reaches the push step.

## Finding what changed

A new script, `build/changed-components.sh`, prints the components to build for a given base and head commit.

- A service (`api`, `mcp`, `enricher`) is affected when a file under its own directories changed, or when `go list -deps` for its `cmd/<service>` package includes a package whose directory changed.
- Changes to the shared Go module files (`go.mod`, `go.sum`), to `build/` or to `.github/` mark all three services affected.
- Changes under `db/`, `gateway/`, `deploy/helm/` and `tests/` map to their own jobs below. A change under `tests/integration/` or `tests/openai-stub/` also marks all three services affected, because those tests run in every service's e2e step.
- On a manual run, the input overrides detection. `build/detect-components.sh` wraps this script for the workflow: it applies the override and prints the `components` and `services` JSON arrays for `$GITHUB_OUTPUT`.
- The script has BATS tests, like the other build tooling.
- Before any rule runs, the script drops paths ending in `.md` or `.build.sh` and paths under `db/tests/`. A change confined to those builds nothing, and a doc or db-test file inside a service's package directory no longer marks the services that import that package. Dockerfiles, Go source, `build-lib.sh`, `version.sh`, `.github/` and the `db/*/build.sh` scripts still trigger rebuilds.
- The static checks still run on every trigger, so docs and Dockerfiles stay linted even when nothing is built.

`db/postgres/build.sh` and `db/neo4j/build.sh` must be committed executable, because the db job runs them directly; at mode `100644` the first pull request run failed with exit code 126.

The first runs on GitHub confirmed the `--mount type=image` trick on Docker 28, the pinned `jq` mount, the artifact download with `merge-multiple`, and the GHCR push from `develop` (`latest-dev` and `sha-<short>` for api, mcp, enricher, postgres and neo4j). The gateway and helm jobs have not run on GitHub yet.

## Jobs

Always run, on every trigger, without path filtering (they are cheap):

`build/static-checks.sh` runs all of these in order and stops at the first failure; the `static` job and a local run are the same command (a local run needs git and Docker Engine 28 or newer, for `--mount type=image`):

- Static checks: shellcheck, hadolint, markdownlint and `helm lint --strict`, each run directly.
- Build tooling tests: BATS, run from pinned images, with a pinned `jq` image mounted in because the Go image lacks it.
- Boundary check: `build/check-e2e-boundary.sh`.

Run when their inputs changed:

- One job per affected service: run `build/<n>/<n>.build.sh`. It builds the image from scratch, runs the quality gates and the full e2e suite inside containers.
- `db/`: the two database test runners (`db/tests/postgres-test-runner.sh`, `db/tests/neo4j-test-runner.sh`) and build of the two database images.
- `gateway/`: the OPA policy checks, the Envoy config validations and the stub-gateway e2e run.
- `deploy/helm`: lint and template only.

Not in CI: the Kubernetes end-to-end run.

## Publishing images

A separate job runs only on a push to `develop` or `main`, only after all jobs for that run passed, and only for the components whose build passed.

- The job logs in to GHCR with the built-in `GITHUB_TOKEN` and pushes the images. Only this job has `packages: write`; every other job has the least permissions it needs.
- From `develop`, each affected component is tagged `latest-dev` and `sha-<short>`, never a version tag. From `main` (dormant until the branch exists), each affected component is tagged `latest` and its version from `build/version.sh <component>`.
- The `db/postgres/build.sh` and `db/neo4j/build.sh` scripts take their version from `build/version.sh db` and build with `--no-cache --pull`. The postgres image no longer bakes in a default `POSTGRES_PASSWORD`.
- The pushed image must be the one that was tested: the build job saves the image as a workflow artifact (or the push job rebuilds with the identical command). Choosing between the two is left to the builder, with the preference stated under [Left to the builder](#left-to-the-builder).
- Pull requests and manual runs never run this job.

## Registry state and bootstrap

Verified on 2026-10-22 with read-only calls:

| Package | Visibility | Linked to | `latest-dev` |
| --- | --- | --- | --- |
| `mnemonic-api` | public | old repo `mnemonic-api` | yes (pre-monorepo build) |
| `mnemonic-mcp` | public | unlinked | yes (seeded 2026-10-22 from a current local build) |
| `mnemonic-enricher` | public | unlinked | yes (seeded 2026-10-22 from a current local build) |
| `mnemonic-postgres` | public | old repo `mnemonic-dbs` | yes |
| `mnemonic-neo4j` | public | old repo `mnemonic-dbs` | yes |

No package has a `latest` tag. The old packages `mnemonic` (the former mcp) and `mnemonic-enrichment` (the former enricher) still exist and nothing in the repo references them; retiring them is the owner's call.

The owner did the bootstrap by hand: the two new packages were seeded once from local builds, and all five packages were made public. The seed is outside the workflows, so the rule that workflow runs never push from pull requests or manual runs still holds. Once the first publish from `develop` succeeds, the seeded images are replaced by CI builds.

For `GITHUB_TOKEN` to write, each of the five packages needs `twistingmercury/mnemonic` under Manage Actions access with the Write role. The owner reports doing this. The API does not expose that setting, so it is verified by the first publish: if the push step fails with `permission_denied`, the grant is missing on that package.

## Forks and going public

The repo becomes public after everything works. The design is safe for fork PRs from the start, though it cannot be exercised while the repo is private:

- Pull requests use `pull_request`, never `pull_request_target`, so fork runs get a read-only token and no secrets.
- Pull request jobs need no secrets: the e2e suite uses the OpenAI stub, and the peers and the tooling image are public.
- Only the publish job writes, and it runs only on a push to `develop` or `main`.

## Launch checklist (owner)

- Turn on "include administrators" in branch protection and mark `ci-gate` required.
- Before making the repo public, require approval for workflow runs from first-time contributors (an Actions setting).
- Push the seed tags (`api/v0.5.0`, `mcp/v0.6.0`, `enricher/v0.5.0`, `db/v1.0.0`) before the first CI run on `develop`.

## The required check

A job that is skipped by a path filter reports no status, so a required check could hang forever. A final job, `ci-gate`, always runs after all other jobs and passes only if every job that ran succeeded; a skipped job does not count as a failure. It is the only check marked required in branch protection.

- No job uses `continue-on-error`.
- "No exceptions" for administrators is a repository setting ("include administrators" in branch protection) that the owner turns on; a workflow cannot enforce it.

## Calls made by the author

These are the author's calls, not agreed with the owner:

- GitHub-hosted Ubuntu runners.
- Static checks run unfiltered on every run.
- The manual run picks a component but never pushes.
- `ci-gate` fails when the detection job fails or is cancelled, not only when a build fails.
- The exact tested image reaches the publish job by save and load. `build/build-lib.sh` already leaves the tagged image in place after the run (teardown removes only unnamed images), so the build job runs `docker save` on it and uploads the file as a workflow artifact.
- Concurrency: a newer run on the same pull request cancels the older one. Pushes do not queue: GitHub keeps one pending run per concurrency group and cancels older pending ones even without `cancel-in-progress`, which would drop a commit that is never built or published (the next run's base is `event.before`). So each push gets its own group, keyed on the commit SHA, and nothing is dropped or cancelled. The cost: runs of quick successive pushes can finish out of order, so `latest-dev` can briefly point at an older commit; `sha-<short>` is always right.

## Left to the builder

- Workflow file layout (one file with jobs, or a few files).
- Passing the tested image to the push job. Preference: save and load the exact image, so the pushed bytes are the tested bytes.
- Whether the `golang-tooling` image is public or needs a login step on the runner.
- Concurrency settings and timeouts.
- Job naming, so branch protection can reference `ci-gate`.
