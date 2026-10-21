#!/usr/bin/env bash
#
# Runs Go tests tagged `integration` against a throwaway PostgreSQL container.
#
# Usage: run.sh [package ...]
#   Defaults to every package under ./internal/... when none are given.
#
# The container is torn down on exit, including on failure, so a crashed run
# cannot leave a listener on the port and make the next run look healthy.

set -euo pipefail

THIS_DIR="$(cd "$(dirname "$0")" && pwd)"
MODULE_ROOT="$(cd "${THIS_DIR}/../.." && pwd)"
COMPOSE_FILE="${THIS_DIR}/docker-compose.yaml"
PROJECT="librarian-inttest"

PG_PORT="${PG_PORT:-15435}"
PG_USER="${PG_USER:-mnemonic}"
PG_PASSWORD="${PG_PASSWORD:-mnemonic_test}"
PG_DB="${PG_DB:-mnemonic}"
READY_TIMEOUT="${READY_TIMEOUT:-60}"

compose() {
    docker compose --project-name "${PROJECT}" --file "${COMPOSE_FILE}" "$@"
}

cleanup() {
    printf '==> tearing down %s\n' "${PROJECT}" >&2
    compose down --volumes --remove-orphans >/dev/null 2>&1 || true
}

wait_for_ready() {
    # Poll the container's own healthcheck rather than the host, so no local
    # postgres client is required and a stale host listener cannot satisfy it.
    local deadline=$((SECONDS + READY_TIMEOUT))
    while ((SECONDS < deadline)); do
        if [ "$(compose ps --format '{{.Health}}' postgres 2>/dev/null)" = "healthy" ]; then
            return 0
        fi
        sleep 1
    done
    printf 'ERROR: postgres did not become healthy within %ss\n' "${READY_TIMEOUT}" >&2
    compose logs postgres >&2 || true
    return 1
}

main() {
    local packages=("$@")
    if [ ${#packages[@]} -eq 0 ]; then
        packages=("./internal/...")
    fi

    trap cleanup EXIT

    printf '==> starting postgres on port %s\n' "${PG_PORT}" >&2
    compose up --detach --wait --quiet-pull

    wait_for_ready

    export TEST_DATABASE_URL="postgres://${PG_USER}:${PG_PASSWORD}@localhost:${PG_PORT}/${PG_DB}?sslmode=disable"
    printf '==> running integration tests: %s\n' "${packages[*]}" >&2

    cd "${MODULE_ROOT}"
    go test -tags=integration -count=1 "${packages[@]}"
}

main "$@"
