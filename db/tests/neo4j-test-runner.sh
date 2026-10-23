#!/usr/bin/env bash
#
# Neo4j Migration Test Runner
# Starts Neo4j via Docker Compose, applies migrations, runs BATS tests, and tears down.

set -euo pipefail

THIS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIG_DIR="$(cd "${THIS_DIR}/.." && pwd)"
BATS_DIR="${THIS_DIR}/bats"
NEO4J_MIG_DIR="$(cd "${MIG_DIR}/neo4j" && pwd)"
DOCKER_COMPOSE="${MIG_DIR}/docker-compose.yaml"

MNEMONIC_MAX_RETRIES="${MNEMONIC_MAX_RETRIES:-30}"
MNEMONIC_RETRY_INTERVAL="${MNEMONIC_RETRY_INTERVAL:-1}"
MNEMONIC_NEO4J_USER="${MNEMONIC_NEO4J_USER:-neo4j}"
MNEMONIC_NEO4J_PASSWORD="${MNEMONIC_NEO4J_PASSWORD:-mnemonic_dev}"
# The image under test; set MNEMONIC_NEO4J_IMAGE to test an existing image instead of building one.
MNEMONIC_NEO4J_IMAGE="${MNEMONIC_NEO4J_IMAGE:-}"
BUILT_IMAGE=0

printf "=== Neo4j Migrations Test Runner ===\n"

validate(){
    if [ ! -d "${THIS_DIR}" ]; then
        printf "ERROR: '%s' could not be found\n" "${THIS_DIR}" >&2
        return 1
    fi

    if [ ! -d "${BATS_DIR}" ]; then
        printf "ERROR: '%s' could not be found\n" "${BATS_DIR}" >&2
        return 1
    fi

    if [ ! -d "${MIG_DIR}" ]; then
        printf "ERROR: '%s' could not be found\n" "${MIG_DIR}" >&2
        return 1
    fi

    if [ ! -d "${NEO4J_MIG_DIR}" ]; then
        printf "ERROR: '%s' could not be found\n" "${NEO4J_MIG_DIR}" >&2
        return 1
    fi

    if [ ! -f "${DOCKER_COMPOSE}" ]; then
        printf "ERROR: '%s' could not be found\n" "${DOCKER_COMPOSE}" >&2
        return 1
    fi

    if [ ! -f "${NEO4J_MIG_DIR}/001_create_constraints.cypher" ]; then
        printf "ERROR: Migration file '001_create_constraints.cypher' not found\n" >&2
        return 1
    fi

    if [ ! -f "${NEO4J_MIG_DIR}/003_create_indexes.cypher" ]; then
        printf "ERROR: Migration file '003_create_indexes.cypher' not found\n" >&2
        return 1
    fi

    return 0
}

start_infra(){
    printf "Starting Neo4j...\n"
    if ! docker compose -f "${DOCKER_COMPOSE}" up -d neo4j; then
        printf "ERROR: Failed to start Neo4j infrastructure\n" >&2
        return 1
    fi
    return 0
}

stop_infra(){
    printf "Stopping infrastructure...\n"
    docker compose -f "${DOCKER_COMPOSE}" down -v --remove-orphans
}

get_neo4j_container(){
    local container_id
    container_id=$(docker compose -f "${DOCKER_COMPOSE}" ps -q neo4j)
    if [ -z "${container_id}" ]; then
        printf "ERROR: Could not find Neo4j container\n" >&2
        return 1
    fi
    printf "%s" "${container_id}"
    return 0
}

wait_for_neo4j(){
    local retries=0
    local max_retries="${MNEMONIC_MAX_RETRIES}"
    local interval="${MNEMONIC_RETRY_INTERVAL}"
    local container_id

    if ! container_id=$(get_neo4j_container); then
        return 1
    fi

    printf "Waiting for Neo4j to be ready...\n"

    while [ "${retries}" -lt "${max_retries}" ]; do
        if docker exec "${container_id}" cypher-shell -u "${MNEMONIC_NEO4J_USER}" -p "${MNEMONIC_NEO4J_PASSWORD}" "RETURN 1" 2>/dev/null 1>/dev/null; then
            printf "Neo4j is ready\n"
            return 0
        fi

        retries=$((retries + 1))
        printf "Attempt %d/%d: Neo4j not ready yet, retrying in %d second(s)...\n" \
            "${retries}" "${max_retries}" "${interval}"
        sleep "${interval}"
    done

    printf "ERROR: Neo4j did not become ready after %d attempts\n" "${max_retries}" >&2
    return 1
}

apply_migrations(){
    local container_id

    if ! container_id=$(get_neo4j_container); then
        return 1
    fi

    printf "\nApplying Neo4j migrations...\n"

    # Apply 001_create_constraints.cypher
    printf "Applying 001_create_constraints.cypher...\n"
    if ! docker cp "${NEO4J_MIG_DIR}/001_create_constraints.cypher" "${container_id}:/tmp/001_create_constraints.cypher"; then
        printf "ERROR: Failed to copy 001_create_constraints.cypher to container\n" >&2
        return 1
    fi

    if ! docker exec "${container_id}" cypher-shell -u "${MNEMONIC_NEO4J_USER}" -p "${MNEMONIC_NEO4J_PASSWORD}" -f /tmp/001_create_constraints.cypher; then
        printf "ERROR: Failed to apply 001_create_constraints.cypher\n" >&2
        return 1
    fi

    # Skip 002_create_existence_constraints.cypher (Enterprise Edition only)
    printf "Skipping 002_create_existence_constraints.cypher (Enterprise Edition only)\n"

    # Apply 003_create_indexes.cypher
    printf "Applying 003_create_indexes.cypher...\n"
    if ! docker cp "${NEO4J_MIG_DIR}/003_create_indexes.cypher" "${container_id}:/tmp/003_create_indexes.cypher"; then
        printf "ERROR: Failed to copy 003_create_indexes.cypher to container\n" >&2
        return 1
    fi

    if ! docker exec "${container_id}" cypher-shell -u "${MNEMONIC_NEO4J_USER}" -p "${MNEMONIC_NEO4J_PASSWORD}" -f /tmp/003_create_indexes.cypher; then
        printf "ERROR: Failed to apply 003_create_indexes.cypher\n" >&2
        return 1
    fi

    printf "Migrations applied successfully\n"
    return 0
}

run_tests(){
    local container_id

    if ! wait_for_neo4j; then
        return 1
    fi

    if ! apply_migrations; then
        return 1
    fi

    if ! container_id=$(get_neo4j_container); then
        return 1
    fi

    # Export container ID for BATS tests
    export MNEMONIC_NEO4J_CONTAINER="${container_id}"
    export MNEMONIC_NEO4J_USER
    export MNEMONIC_NEO4J_PASSWORD

    printf "\nRunning BATS tests...\n"
    if ! "${THIS_DIR}/run-bats.sh" "${BATS_DIR}/neo4j-migrations.bats"; then
        printf "ERROR: BATS tests failed\n" >&2
        return 1
    fi

    if ! run_image_tests; then
        return 1
    fi

    printf "\nAll tests passed successfully\n"
    return 0
}

build_image(){
    if [ -n "${MNEMONIC_NEO4J_IMAGE}" ]; then
        return 0
    fi

    MNEMONIC_NEO4J_IMAGE="mnemonic-neo4j:test-$$"
    printf "\nBuilding image %s...\n" "${MNEMONIC_NEO4J_IMAGE}"
    if ! docker build --no-cache --pull -q -t "${MNEMONIC_NEO4J_IMAGE}" "${NEO4J_MIG_DIR}" >/dev/null; then
        printf "ERROR: Failed to build %s\n" "${MNEMONIC_NEO4J_IMAGE}" >&2
        return 1
    fi

    BUILT_IMAGE=1
    return 0
}

# Starts containers from the image itself: proves the image applies its own schema.
run_image_tests(){
    if ! build_image; then
        return 1
    fi

    export MNEMONIC_NEO4J_IMAGE
    export MNEMONIC_NEO4J_USER
    export MNEMONIC_NEO4J_PASSWORD

    printf "\nRunning image BATS tests...\n"
    if ! "${THIS_DIR}/run-bats.sh" "${BATS_DIR}/neo4j-image.bats"; then
        printf "ERROR: image BATS tests failed\n" >&2
        return 1
    fi

    return 0
}

cleanup(){
    stop_infra
    if [ "${BUILT_IMAGE}" -eq 1 ]; then
        docker rmi "${MNEMONIC_NEO4J_IMAGE}" 2>/dev/null || true
    fi
}

main(){
    if ! validate; then
        return 1
    fi

    trap cleanup EXIT

    if ! start_infra; then
        return 1
    fi

    if ! run_tests; then
        return 1
    fi
}

main "$@"
