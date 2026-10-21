#!/usr/bin/env bash

set -euo pipefail

THIS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIG_DIR="$(cd "${THIS_DIR}/.." && pwd)"
BATS_DIR="${THIS_DIR}/bats"
PG_MIG_DIR="$(cd "${MIG_DIR}/postgres" && pwd)"
DOCKER_COMPOSE="${MIG_DIR}/docker-compose.yaml"

MAX_RETRIES="${MAX_RETRIES:-30}"
RETRY_INTERVAL="${RETRY_INTERVAL:-1}"

printf "=== Database Migrations Test Runner ===\n"

validate(){
    if [ ! -d "${THIS_DIR}" ]; then
        printf "ERROR: '%s' could not be found" "${THIS_DIR}"
        return 1
    fi

        if [ ! -d "${BATS_DIR}" ]; then
        printf "ERROR: '%s' could not be found" "${BATS_DIR}"
        return 1
    fi

    if [ ! -d "${MIG_DIR}" ]; then
        printf "ERROR: '%s' could not be found" "${MIG_DIR}"
        return 1
    fi

    if [ ! -d "${PG_MIG_DIR}" ]; then
        printf "ERROR: '%s' could not be found" "${MIG_DIR}"
        return 1
    fi

    if [ ! -f "${DOCKER_COMPOSE}" ]; then
        printf "ERROR: '%s' could not be found" "${DOCKER_COMPOSE}"
        return 1
    fi
    return 0
}

start_infra(){
    if ! docker compose -f "${DOCKER_COMPOSE}" up -d; then
        printf "ERROR: Failed to start database infrastructure\n" >&2
        return 1
    fi
    return 0
}

stop_infra(){
    docker compose -f "${DOCKER_COMPOSE}"  down -v --remove-orphans
}

wait_for_postgres(){
    local retries=0
    local max_retries="${MAX_RETRIES}"
    local interval="${RETRY_INTERVAL}"

    printf "Waiting for PostgreSQL to be ready...\n"

    while [ "$retries" -lt "$max_retries" ]; do
        if PGPASSWORD=mnemonic_dev psql -h localhost -p 15435 -U mnemonic -d mnemonic -c '\q' 2>/dev/null; then
            printf "PostgreSQL is ready\n"
            return 0
        fi

        retries=$((retries + 1))
        printf "Attempt %d/%d: PostgreSQL not ready yet, retrying in %d second(s)...\n" \
            "$retries" "$max_retries" "$interval"
        sleep "$interval"
    done

    printf "ERROR: PostgreSQL did not become ready after %d attempts\n" "$max_retries" >&2
    return 1
}

run_tests(){
    if ! wait_for_postgres; then
        return 1
    fi

    printf "\nRunning BATS tests...\n"
    if ! bats "${BATS_DIR}/migrations.bats"; then
        printf "ERROR: BATS tests failed\n" >&2
        return 1
    fi

    printf "\nAll tests passed successfully\n"
    return 0
}

main(){
    if ! validate; then
        return 1
    fi

    trap stop_infra EXIT

    if ! start_infra; then
        return 1
    fi

    if ! run_tests; then
        return 1
    fi
}

main "$@"
