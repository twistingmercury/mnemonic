# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- An OpenAI stub (`openai-stub/`) that all three services use through
  `MNEMONIC_OPENAI_BASE_URL`. Runs are deterministic and need no OpenAI key.
- Cross-service tests for re-enrichment after an update, removal from every
  MCP read path after a delete, related patterns found through shared
  concepts, and the enricher's retry sweep recovering a failed job.
- A `pull_request` trigger, so changes to this suite are tested before merge.

### Changed

- 2026-10-21: Renamed the project back to Mnemonic in the monorepo, dropping the placeholder
  name "Librarian": images are `ghcr.io/twistingmercury/mnemonic-*`, services read
  `MNEMONIC_*` environment variables, and the stub moved to `tests/openai-stub`.
  The Compose file still has the fork's names until it is re-homed.
- MCP tool calls are sent as MCP protocol version 2026-07-28 requests: the
  protocol version, client info and capabilities in `_meta`, with the
  `MCP-Protocol-Version`, `Mcp-Method` and `Mcp-Name` headers.
- Test tokens are re-rolled when the OpenAI stub would hash them to the same
  dimension as a shared pattern word or another token. Such a collision made a
  search for a missing token match every test pattern, so
  `TestUpdateReenrichesPattern` failed now and then.
- The suite now covers only paths that span services. Tests that repeated a
  single service's own E2E coverage (API CRUD, MCP tool validation, per-service
  version checks) are removed; health is one table-driven test that checks
  every dependency is `OK`.
- Tests run in the stock `golang:1.27.2-alpine` image with `src/` mounted,
  replacing the 708 MB test-runner image and its readiness script.
- All tests are in one package with no third-party dependencies.
- `make build` is now `make test`.
- Go 1.27.2.

### Removed

- The `MNEMONIC_OPENAI_API_KEY` secret requirement.
- Unused request headers (`X-User-ID`, `X-Team-ID`, `X-User-Roles`) and
  helper types.

## [0.0.3] - 2026-09-09

### Added

- A `.gitignore` covering Go build and coverage artifacts, local environment
  files, logs, and editor/OS metadata.
- A `.markdownlint.json` and a `make analyze` target for linting Markdown.

### Changed

- Upgrade the Go module requirement from Go 1.26.6 to Go 1.27.1.
- Update the test-runner base image to
  `ghcr.io/twistingmercury/golang-tooling:go1.27.1`.
- Update project documentation to reflect the current release version.
- Correct the README's description of test coverage: the REST API suite
  exercises listing with pagination, not attribute filtering.
- Name the test-runner image `mnemonic_integration_tests:latest` in Compose so
  `make stop` actually removes it, matching the documented cleanup behavior.
- Make `help` the default `make` target.

### Fixed

- Add RabbitMQ to the README's list of prerequisite service images.

## [0.0.2] - 2026-08-23

### Added

- Black-box Docker Compose integration coverage for service health, REST API,
  MCP tools, enrichment, and the end-to-end create-to-search pipeline.
- A scheduled GitHub Actions workflow for the integration suite.

### Changed

- Use development service image tags published from the `develop` branch.
- Run integration tests for pushes and merged pull requests to `develop`.
- Improve CI diagnostics and ensure Compose resources are removed after every
  workflow run.

### Fixed

- Correct MCP health endpoint ports used by the test runner.
- Align pattern-update assertions with the API's `204 No Content` response.
- Increase Neo4j's CI startup allowance to reduce intermittent readiness
  failures.

## [0.0.1] - 2026-03-26

### Added

- Initial release of the Mnemonic black-box integration test project.
- Docker Compose orchestration for the Mnemonic service stack.
- Shared Go helpers for REST API and MCP integration tests.

[Unreleased]: https://github.com/twistingmercury/librarian-integration-tests/compare/v0.0.3...HEAD
[0.0.3]: https://github.com/twistingmercury/librarian-integration-tests/releases/tag/v0.0.3
[0.0.2]: https://github.com/twistingmercury/librarian-integration-tests/releases/tag/v0.0.2
[0.0.1]: https://github.com/twistingmercury/librarian-integration-tests/releases/tag/v0.0.1
