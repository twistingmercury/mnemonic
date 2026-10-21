#!/usr/bin/env bats
# BATS tests for db/postgres/build.sh and db/neo4j/build.sh.
#
# docker and git are stand-ins placed in TEST_DIR that record their arguments,
# so no image is built or retagged. The image names and the version knob are
# the contract under test.

POSTGRES_BUILD="${BATS_TEST_DIRNAME}/../../postgres/build.sh"
NEO4J_BUILD="${BATS_TEST_DIRNAME}/../../neo4j/build.sh"

setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    export DOCKER_LOG="${TEST_DIR}/docker.log"
    mkdir -p "${TEST_DIR}/bin"

    write_docker_stub
    printf '#!/usr/bin/env bash\nexit 1\n' > "${TEST_DIR}/bin/git"
    chmod 0755 "${TEST_DIR}/bin/docker" "${TEST_DIR}/bin/git"
    export PATH="${TEST_DIR}/bin:${PATH}"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

# write_docker_stub: records its arguments; answers `image inspect` with a size.
write_docker_stub() {
    cat > "${TEST_DIR}/bin/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${DOCKER_LOG}"
if [ "${1:-}" = "image" ]; then
    printf '1048576\n'
fi
STUB
}

@test "postgres build with no version - tags mnemonic-postgres dev and latest" {
    run bash "${POSTGRES_BUILD}"

    [ "${status}" -eq 0 ]
    grep -qF 'ghcr.io/twistingmercury/mnemonic-postgres:dev' "${DOCKER_LOG}"
    grep -qF 'ghcr.io/twistingmercury/mnemonic-postgres:latest' "${DOCKER_LOG}"
}

@test "postgres build with MNEMONIC_VERSION - tags that version" {
    export MNEMONIC_VERSION="v9.9.9-test"

    run bash "${POSTGRES_BUILD}"

    [ "${status}" -eq 0 ]
    grep -qF 'ghcr.io/twistingmercury/mnemonic-postgres:v9.9.9-test' "${DOCKER_LOG}"
}

@test "postgres build with the old VERSION name - ignores it" {
    export VERSION="v9.9.9-old"

    run bash "${POSTGRES_BUILD}"

    [ "${status}" -eq 0 ]
    run grep -cF 'v9.9.9-old' "${DOCKER_LOG}"
    [ "${output}" = "0" ]
}

@test "neo4j build with no version - tags mnemonic-neo4j dev and latest" {
    run bash "${NEO4J_BUILD}"

    [ "${status}" -eq 0 ]
    grep -qF 'ghcr.io/twistingmercury/mnemonic-neo4j:dev' "${DOCKER_LOG}"
    grep -qF 'ghcr.io/twistingmercury/mnemonic-neo4j:latest' "${DOCKER_LOG}"
}

@test "neo4j build with MNEMONIC_VERSION - tags that version" {
    export MNEMONIC_VERSION="v9.9.9-test"

    run bash "${NEO4J_BUILD}"

    [ "${status}" -eq 0 ]
    grep -qF 'ghcr.io/twistingmercury/mnemonic-neo4j:v9.9.9-test' "${DOCKER_LOG}"
}

@test "neo4j build with the old VERSION name - ignores it" {
    export VERSION="v9.9.9-old"

    run bash "${NEO4J_BUILD}"

    [ "${status}" -eq 0 ]
    run grep -cF 'v9.9.9-old' "${DOCKER_LOG}"
    [ "${output}" = "0" ]
}
