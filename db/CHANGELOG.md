# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- 2026-10-22: The `build.sh` scripts now default the image tag to `v` plus `build/version.sh db` instead of the newest git tag of any component, which contained a slash and broke the build (#26).
- 2026-10-21: Renamed the project back to Mnemonic in the monorepo, dropping the placeholder name "Librarian" from the documentation, images, environment variables and host ports; see the two entries below for the image and variable details.
- 2026-10-21: Renamed the database images to `mnemonic-postgres` and `mnemonic-neo4j`, dropping the placeholder fork name (tags unchanged); the test harness host ports return to Postgres `5435` and Neo4j `7477`/`7690`.
- 2026-10-21: Prefixed the environment variables defined by this repository's scripts with `MNEMONIC_` (`MNEMONIC_VERSION`, `MNEMONIC_NEO4J_USER`, `MNEMONIC_SCHEMA_DIR`, ...); the old names are no longer read. Variables owned by Neo4j, PostgreSQL and libpq are unchanged.

### Added

- 2026-10-22: Tags `api/v0.5.0`, `mcp/v0.6.0`, `enricher/v0.5.0` and `db/v1.0.0` give each component its own version; `build/version.sh <component>` reads them. The database build scripts still read the newest tag of any component.

### Fixed

- 2026-10-22: README corrected: the `make db-*` targets no longer exist, so it shows the `bash` commands for the build scripts and test runners, and it states that a prefixed git tag is not a valid image tag.
- 2026-10-22: Removed Unreleased entries copied from the service fork (configuration, telemetry, routing and repository packages); they did not describe this component.
