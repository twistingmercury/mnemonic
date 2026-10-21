#!/usr/bin/env bash
# schema-entrypoint.sh — Neo4j entrypoint that applies the Mnemonic schema on every start.
#
# Why this exists: the official neo4j image declares /data as a VOLUME, so
# anything written to /data at build time is never part of the image. The
# schema is therefore applied at container start, against whatever /data holds
# (fresh volume, seeded volume, or no volume). Every Cypher file is idempotent.
#
# Design: the schema job is forked into the background and the shell then
# `exec`s the base image entrypoint, so Neo4j stays the foreground process
# under tini (PID 1) and receives SIGTERM from `docker stop` directly.
#
# Readiness: MNEMONIC_SCHEMA_READY_FILE is removed at start and created only after every
# Cypher file applied cleanly. Health checks should test for that file.
#
# Usage: ENTRYPOINT ["tini", "-g", "--", "/startup/schema-entrypoint.sh"] with CMD ["neo4j"]

set -euo pipefail

MNEMONIC_BASE_ENTRYPOINT="${MNEMONIC_BASE_ENTRYPOINT:-/startup/docker-entrypoint.sh}"
MNEMONIC_SCHEMA_DIR="${MNEMONIC_SCHEMA_DIR:-/startup/schema}"
MNEMONIC_SCHEMA_READY_FILE="${MNEMONIC_SCHEMA_READY_FILE:-/tmp/schema_ready}"
MNEMONIC_SCHEMA_MAX_RETRIES="${MNEMONIC_SCHEMA_MAX_RETRIES:-60}"
MNEMONIC_SCHEMA_RETRY_SLEEP="${MNEMONIC_SCHEMA_RETRY_SLEEP:-2}"
ENTERPRISE_ONLY_FILE="002_create_existence_constraints.cypher"

log_info() {
    printf 'INFO: [schema] %s\n' "$*"
    return 0
}

log_error() {
    printf 'ERROR: [schema] %s\n' "$*" >&2
    return 0
}

# cypher-shell reads NEO4J_USERNAME and NEO4J_PASSWORD, which keeps the
# password off the process list and reuses the NEO4J_AUTH the image already has.
export_credentials() {
    local auth="${NEO4J_AUTH:-}"

    if [ -z "${auth}" ]; then
        return 0
    fi

    if [ "${auth}" = "none" ]; then
        return 0
    fi

    export NEO4J_USERNAME="${auth%%/*}"
    export NEO4J_PASSWORD="${auth#*/}"

    return 0
}

wait_for_neo4j() {
    local attempt=1

    log_info "Waiting for Neo4j to accept bolt connections..."

    while [ "${attempt}" -le "${MNEMONIC_SCHEMA_MAX_RETRIES}" ]; do
        if cypher-shell "RETURN 1" >/dev/null 2>&1; then
            log_info "Neo4j ready after ${attempt} attempt(s)"
            return 0
        fi

        log_info "Attempt ${attempt}/${MNEMONIC_SCHEMA_MAX_RETRIES} - not ready yet, retrying in ${MNEMONIC_SCHEMA_RETRY_SLEEP}s..."
        sleep "${MNEMONIC_SCHEMA_RETRY_SLEEP}"
        attempt=$((attempt + 1))
    done

    log_error "Neo4j did not accept connections after $((MNEMONIC_SCHEMA_MAX_RETRIES * MNEMONIC_SCHEMA_RETRY_SLEEP))s"
    return 1
}

# Existence constraints need Enterprise Edition; the base image sets NEO4J_EDITION.
is_skipped_file() {
    local file_name="${1}"

    if [ "${file_name}" != "${ENTERPRISE_ONLY_FILE}" ]; then
        return 1
    fi

    [ "${NEO4J_EDITION:-community}" != "enterprise" ]
}

apply_file() {
    local cypher_file="${1}"
    local file_name

    file_name="$(basename "${cypher_file}")"

    if is_skipped_file "${file_name}"; then
        log_info "Skipping ${file_name} (Enterprise Edition only)"
        return 0
    fi

    log_info "Applying ${file_name}..."
    if ! cypher-shell -f "${cypher_file}" >/dev/null; then
        log_error "Failed to apply ${file_name}"
        return 1
    fi

    return 0
}

apply_schema() {
    local cypher_file

    # The glob expands in sorted order, which is the migration order.
    for cypher_file in "${MNEMONIC_SCHEMA_DIR}"/*.cypher; do
        if ! apply_file "${cypher_file}"; then
            return 1
        fi
    done

    return 0
}

schema_job() {
    export_credentials

    if ! wait_for_neo4j; then
        return 1
    fi

    if ! apply_schema; then
        return 1
    fi

    touch "${MNEMONIC_SCHEMA_READY_FILE}"
    log_info "Schema applied; wrote ${MNEMONIC_SCHEMA_READY_FILE}"

    return 0
}

# A failed schema must not leave a running database that looks healthy to
# checks that only run a query, so stop Neo4j and let the orchestrator see it.
run_schema_job() {
    local neo4j_pid="${1}"

    if schema_job; then
        return 0
    fi

    log_error "Schema was not applied; stopping Neo4j (pid ${neo4j_pid})"
    kill -TERM "${neo4j_pid}"

    return 1
}

main() {
    rm -f "${MNEMONIC_SCHEMA_READY_FILE}"

    # Only the database server needs a schema, not `neo4j-admin` or a shell.
    if [ "${1:-}" = "neo4j" ]; then
        run_schema_job "$$" &
    fi

    exec "${MNEMONIC_BASE_ENTRYPOINT}" "$@"
}

main "$@"
