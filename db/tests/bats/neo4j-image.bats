#!/usr/bin/env bats
# BATS tests for the librarian-neo4j image: the schema must be applied by the
# image itself at container start, not baked into /data (a VOLUME).
#
# Required environment (exported by neo4j-test-runner.sh):
#   NEO4J_IMAGE — image under test
# Optional: NEO4J_USER (neo4j), NEO4J_PASSWORD (mnemonic_dev), READY_TIMEOUT (150)

NEO4J_USER="${NEO4J_USER:-neo4j}"
NEO4J_PASSWORD="${NEO4J_PASSWORD:-mnemonic_dev}"
READY_TIMEOUT="${READY_TIMEOUT:-150}"
STOP_TIMEOUT="${STOP_TIMEOUT:-30}"

setup() {
    export CONTAINER_NAME="test-neo4j-$$-${BATS_TEST_NUMBER}"
    export VOLUME_NAME="test-neo4j-vol-$$-${BATS_TEST_NUMBER}"
    docker volume create "${VOLUME_NAME}" >/dev/null
}

teardown() {
    docker rm -f -v "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm -f -v "${CONTAINER_NAME}-seed" 2>/dev/null || true
    docker volume rm "${VOLUME_NAME}" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

start_container() {
    local name="${1}"
    local image="${2}"

    docker run -d --name "${name}" \
        --env NEO4J_AUTH="${NEO4J_USER}/${NEO4J_PASSWORD}" \
        --volume "${VOLUME_NAME}:/data" \
        "${image}" >/dev/null
}

# Polls docker inspect until the container is healthy or the timeout expires.
wait_healthy() {
    local name="${1}"
    local deadline=$((SECONDS + READY_TIMEOUT))
    local health

    while [ "${SECONDS}" -lt "${deadline}" ]; do
        health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "${name}" 2>/dev/null || true)
        if [ "${health}" = "healthy" ]; then
            return 0
        fi
        sleep 1
    done

    printf 'container %s not healthy after %ss (last status: %s)\n' "${name}" "${READY_TIMEOUT}" "${health}" >&2
    docker logs "${name}" >&2 || true
    return 1
}

# Waits for bolt only; used for the stock image, which has no health check.
wait_bolt() {
    local name="${1}"
    local deadline=$((SECONDS + READY_TIMEOUT))

    while [ "${SECONDS}" -lt "${deadline}" ]; do
        if docker exec "${name}" cypher-shell -u "${NEO4J_USER}" -p "${NEO4J_PASSWORD}" "RETURN 1" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done

    printf 'container %s did not accept bolt after %ss\n' "${name}" "${READY_TIMEOUT}" >&2
    return 1
}

run_cypher_in() {
    local name="${1}"
    local query="${2}"

    docker exec "${name}" cypher-shell \
        -u "${NEO4J_USER}" -p "${NEO4J_PASSWORD}" \
        --format plain "${query}" 2>&1
}

run_cypher() {
    run_cypher_in "${CONTAINER_NAME}" "${1}"
}

# Prints a single integer returned by a count query.
cypher_count() {
    local query="${1}"
    local result

    result=$(run_cypher "${query}")
    printf '%s' "${result}" | grep -E '^[0-9]+$' | head -n 1
}

assert_contains() {
    local haystack="${1}"
    local needle="${2}"

    if printf '%s' "${haystack}" | grep -qF -- "${needle}"; then
        return 0
    fi

    printf 'expected output to contain "%s", got:\n%s\n' "${needle}" "${haystack}" >&2
    return 1
}

# ---------------------------------------------------------------------------
# Schema is applied at startup
# ---------------------------------------------------------------------------

@test "fresh container - creates all uniqueness constraints" {
    start_container "${CONTAINER_NAME}" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}"

    run run_cypher "SHOW CONSTRAINTS YIELD name RETURN name"

    [ "${status}" -eq 0 ]
    assert_contains "${output}" "pattern_id_unique"
    assert_contains "${output}" "agent_name_unique"
    assert_contains "${output}" "concept_name_unique"
}

@test "fresh container - creates the explicit indexes" {
    start_container "${CONTAINER_NAME}" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}"

    run run_cypher "SHOW INDEXES YIELD name RETURN name"

    [ "${status}" -eq 0 ]
    assert_contains "${output}" "pattern_name_index"
    assert_contains "${output}" "concept_type_index"
    assert_contains "${output}" "rel_relevant_for_relevance"
    assert_contains "${output}" "pattern_content_fulltext"
    assert_contains "${output}" "concept_name_fulltext"
}

@test "fresh container - records schema version 3 and skips the Enterprise-only migration" {
    start_container "${CONTAINER_NAME}" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}"

    run run_cypher "MATCH (v:SchemaVersion {name: 'mnemonic'}) RETURN v.version AS version"
    [ "${status}" -eq 0 ]
    assert_contains "${output}" "3"

    run run_cypher "SHOW CONSTRAINTS YIELD name WHERE name ENDS WITH '_exists' RETURN count(*) AS count"
    [ "${status}" -eq 0 ]
    assert_contains "${output}" "0"
}

# ---------------------------------------------------------------------------
# Restart and existing volumes
# ---------------------------------------------------------------------------

@test "container restart - reapplies schema without errors or duplicate objects" {
    local constraints_before
    local indexes_before
    local logs

    start_container "${CONTAINER_NAME}" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}"
    constraints_before=$(cypher_count "SHOW CONSTRAINTS YIELD name RETURN count(*) AS count")
    indexes_before=$(cypher_count "SHOW INDEXES YIELD name RETURN count(*) AS count")

    docker restart -t "${STOP_TIMEOUT}" "${CONTAINER_NAME}" >/dev/null
    wait_healthy "${CONTAINER_NAME}"

    [ "$(cypher_count "SHOW CONSTRAINTS YIELD name RETURN count(*) AS count")" -eq "${constraints_before}" ]
    [ "$(cypher_count "SHOW INDEXES YIELD name RETURN count(*) AS count")" -eq "${indexes_before}" ]
    [ "$(cypher_count "MATCH (v:SchemaVersion) RETURN count(v) AS count")" -eq 1 ]

    logs=$(docker logs "${CONTAINER_NAME}" 2>&1)
    [ "$(printf '%s\n' "${logs}" | grep -c 'Schema applied')" -eq 2 ]
    if printf '%s\n' "${logs}" | grep -q 'ERROR: \[schema\]'; then
        printf '%s\n' "${logs}" >&2
        return 1
    fi
}

@test "new container on a used volume - keeps existing data and schema" {
    start_container "${CONTAINER_NAME}-seed" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}-seed"
    run_cypher_in "${CONTAINER_NAME}-seed" "CREATE (:Concept {name: 'survivor'})" >/dev/null
    docker stop -t "${STOP_TIMEOUT}" "${CONTAINER_NAME}-seed" >/dev/null
    docker rm -f -v "${CONTAINER_NAME}-seed" >/dev/null

    start_container "${CONTAINER_NAME}" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}"

    run run_cypher "MATCH (c:Concept {name: 'survivor'}) RETURN count(c) AS count"
    [ "${status}" -eq 0 ]
    assert_contains "${output}" "1"
    [ "$(cypher_count "SHOW CONSTRAINTS YIELD name WHERE name = 'concept_name_unique' RETURN count(*) AS count")" -eq 1 ]
}

@test "volume created by the stock neo4j image - gains the schema and keeps its data" {
    start_container "${CONTAINER_NAME}-seed" "neo4j:5"
    wait_bolt "${CONTAINER_NAME}-seed"
    run_cypher_in "${CONTAINER_NAME}-seed" "CREATE (:Concept {name: 'legacy'})" >/dev/null
    docker stop -t "${STOP_TIMEOUT}" "${CONTAINER_NAME}-seed" >/dev/null
    docker rm -f -v "${CONTAINER_NAME}-seed" >/dev/null

    start_container "${CONTAINER_NAME}" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}"

    run run_cypher "MATCH (c:Concept {name: 'legacy'}) RETURN count(c) AS count"
    [ "${status}" -eq 0 ]
    assert_contains "${output}" "1"
    [ "$(cypher_count "SHOW CONSTRAINTS YIELD name WHERE name = 'concept_name_unique' RETURN count(*) AS count")" -eq 1 ]
}

# ---------------------------------------------------------------------------
# Constraint enforcement
# ---------------------------------------------------------------------------

# Kubernetes starts the container as the neo4j user (uid 7474), not as root, and
# gives it a writable /data through fsGroup. Root can read files that the neo4j
# user cannot, so the root-only tests above would not catch a permission bug.
@test "unprivileged start (uid 7474, as Kubernetes runs it) - applies the schema" {
    docker run --rm --user 0:0 --volume "${VOLUME_NAME}:/data" --entrypoint chown \
        "${NEO4J_IMAGE}" -R 7474:7474 /data

    docker run -d --name "${CONTAINER_NAME}" --user 7474:7474 \
        --env NEO4J_AUTH="${NEO4J_USER}/${NEO4J_PASSWORD}" \
        --volume "${VOLUME_NAME}:/data" \
        "${NEO4J_IMAGE}" >/dev/null
    wait_healthy "${CONTAINER_NAME}"

    run run_cypher "SHOW CONSTRAINTS YIELD name RETURN name"

    [ "${status}" -eq 0 ]
    assert_contains "${output}" "concept_name_unique"
}

@test "concept_name_unique - rejects a second Concept with the same name" {
    start_container "${CONTAINER_NAME}" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}"

    run run_cypher "CREATE (:Concept {name: 'dup'})"
    [ "${status}" -eq 0 ]

    run run_cypher "CREATE (:Concept {name: 'dup'})"
    [ "${status}" -ne 0 ]
    assert_contains "${output}" "already exists"
    [ "$(cypher_count "MATCH (c:Concept {name: 'dup'}) RETURN count(c) AS count")" -eq 1 ]
}

# ---------------------------------------------------------------------------
# Shutdown
# ---------------------------------------------------------------------------

@test "docker stop - Neo4j shuts down on SIGTERM and exits with code 0" {
    local started_at
    local elapsed
    local exit_code
    local logs

    start_container "${CONTAINER_NAME}" "${NEO4J_IMAGE}"
    wait_healthy "${CONTAINER_NAME}"

    started_at="${SECONDS}"
    docker stop -t "${STOP_TIMEOUT}" "${CONTAINER_NAME}" >/dev/null
    elapsed=$((SECONDS - started_at))

    exit_code=$(docker inspect -f '{{.State.ExitCode}}' "${CONTAINER_NAME}")
    logs=$(docker logs "${CONTAINER_NAME}" 2>&1)

    # The stock neo4j image itself takes about 10s to finish stopping, so the
    # bound is the grace period, not 10s; a SIGKILL would give exit code 137.
    [ "${exit_code}" -eq 0 ]
    [ "${elapsed}" -lt "${STOP_TIMEOUT}" ]
    assert_contains "${logs}" "Neo4j Server shutdown initiated by request"
    assert_contains "${logs}" "Stopped."
}

# ---------------------------------------------------------------------------
# Readiness
# ---------------------------------------------------------------------------

@test "failed schema job - stops the container instead of leaving a database without a schema" {
    docker run -d --name "${CONTAINER_NAME}" \
        --env NEO4J_AUTH="${NEO4J_USER}/${NEO4J_PASSWORD}" \
        --env SCHEMA_MAX_RETRIES=1 --env SCHEMA_RETRY_SLEEP=1 \
        --volume "${VOLUME_NAME}:/data" \
        "${NEO4J_IMAGE}" >/dev/null

    run timeout 60 docker wait "${CONTAINER_NAME}"
    [ "${status}" -eq 0 ]

    run docker logs "${CONTAINER_NAME}"
    assert_contains "${output}" "Schema was not applied"
}
