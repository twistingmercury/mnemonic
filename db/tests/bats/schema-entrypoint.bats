#!/usr/bin/env bats
# BATS tests for db/neo4j/schema-entrypoint.sh.
#
# cypher-shell and the base entrypoint are stand-ins placed in TEST_DIR, so the
# script runs without a database. Settings arrive through MNEMONIC_* variables.

SCRIPT_PATH="${BATS_TEST_DIRNAME}/../../neo4j/schema-entrypoint.sh"

setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    export MNEMONIC_SCHEMA_DIR="${TEST_DIR}/schema"
    export MNEMONIC_SCHEMA_READY_FILE="${TEST_DIR}/schema_ready"
    export MNEMONIC_SCHEMA_RETRY_SLEEP=0
    export MNEMONIC_BASE_ENTRYPOINT="${TEST_DIR}/base-entrypoint.sh"
    export CYPHER_LOG="${TEST_DIR}/cypher.log"
    mkdir -p "${MNEMONIC_SCHEMA_DIR}" "${TEST_DIR}/bin"

    printf 'RETURN 1;\n' > "${MNEMONIC_SCHEMA_DIR}/001_create_constraints.cypher"
    printf 'RETURN 2;\n' > "${MNEMONIC_SCHEMA_DIR}/002_create_existence_constraints.cypher"
    printf 'RETURN 3;\n' > "${MNEMONIC_SCHEMA_DIR}/003_create_indexes.cypher"

    write_cypher_shell
    write_base_entrypoint
    export PATH="${TEST_DIR}/bin:${PATH}"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

# write_cypher_shell: a cypher-shell that logs its arguments and exits with
# CYPHER_EXIT (default 0).
write_cypher_shell() {
    cat > "${TEST_DIR}/bin/cypher-shell" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${CYPHER_LOG}"
exit "${CYPHER_EXIT:-0}"
STUB
    chmod 0755 "${TEST_DIR}/bin/cypher-shell"
}

# write_base_entrypoint: waits (at most 5s) for the schema job to finish or
# for the script to be told to stop, like the real long-running server.
write_base_entrypoint() {
    cat > "${MNEMONIC_BASE_ENTRYPOINT}" <<'STUB'
#!/usr/bin/env bash
for _ in $(seq 1 50); do
    if [ -f "${MNEMONIC_SCHEMA_READY_FILE}" ]; then
        exit 0
    fi
    sleep 0.1
done
exit 0
STUB
    chmod 0755 "${MNEMONIC_BASE_ENTRYPOINT}"
}

@test "schema entrypoint with neo4j command - writes the marker named by MNEMONIC_SCHEMA_READY_FILE" {
    run "${SCRIPT_PATH}" neo4j

    [ "${status}" -eq 0 ]
    [ -f "${MNEMONIC_SCHEMA_READY_FILE}" ]
}

@test "schema entrypoint with neo4j command - applies the files in MNEMONIC_SCHEMA_DIR and skips the enterprise-only one" {
    run "${SCRIPT_PATH}" neo4j

    [ "${status}" -eq 0 ]
    grep -qF '001_create_constraints.cypher' "${CYPHER_LOG}"
    grep -qF '003_create_indexes.cypher' "${CYPHER_LOG}"
    run grep -cF '002_create_existence_constraints.cypher' "${CYPHER_LOG}"
    [ "${output}" = "0" ]
}

@test "schema entrypoint with a non-server command - leaves no marker and applies nothing" {
    run "${SCRIPT_PATH}" cypher-shell

    [ "${status}" -eq 0 ]
    [ ! -f "${MNEMONIC_SCHEMA_READY_FILE}" ]
    [ ! -f "${CYPHER_LOG}" ]
}

@test "schema entrypoint with a stale marker - removes it before the base entrypoint runs" {
    printf 'stale\n' > "${MNEMONIC_SCHEMA_READY_FILE}"
    export CYPHER_EXIT=1
    export MNEMONIC_SCHEMA_MAX_RETRIES=1

    run "${SCRIPT_PATH}" cypher-shell

    [ ! -f "${MNEMONIC_SCHEMA_READY_FILE}" ]
}

@test "schema entrypoint with MNEMONIC_SCHEMA_MAX_RETRIES - gives up after that many attempts" {
    export CYPHER_EXIT=1
    export MNEMONIC_SCHEMA_MAX_RETRIES=2

    run "${SCRIPT_PATH}" neo4j

    [[ "${output}" == *"Attempt 2/2"* ]]
    [ ! -f "${MNEMONIC_SCHEMA_READY_FILE}" ]
}

@test "schema entrypoint with the old unprefixed names - ignores them" {
    export SCHEMA_READY_FILE="${TEST_DIR}/old_ready"
    export SCHEMA_DIR="${TEST_DIR}/old_schema"
    export BASE_ENTRYPOINT="${TEST_DIR}/missing-entrypoint.sh"

    run "${SCRIPT_PATH}" neo4j

    [ "${status}" -eq 0 ]
    [ -f "${MNEMONIC_SCHEMA_READY_FILE}" ]
    [ ! -f "${SCHEMA_READY_FILE}" ]
}
