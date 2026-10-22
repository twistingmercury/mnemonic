#!/usr/bin/env bats
# BATS tests for the version the db build scripts print and tag with.
#
# Each script runs from a copy inside a throwaway git repo that carries the
# tags under test, next to a copy of build/version.sh. docker is a stand-in
# placed in TEST_DIR, so no image is built.

load '../../../tests/bats/test_helper/bats-support/load'
load '../../../tests/bats/test_helper/bats-assert/load'

POSTGRES_SOURCE="${BATS_TEST_DIRNAME}/../../postgres/build.sh"
NEO4J_SOURCE="${BATS_TEST_DIRNAME}/../../neo4j/build.sh"
VERSION_SOURCE="${BATS_TEST_DIRNAME}/../../../build/version.sh"

setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    export REPO_DIR="${TEST_DIR}/repo"
    export DOCKER_LOG="${TEST_DIR}/docker.log"
    export GIT_CONFIG_GLOBAL="${TEST_DIR}/gitconfig"
    export GIT_CONFIG_SYSTEM=/dev/null
    export GIT_AUTHOR_NAME="Test"
    export GIT_AUTHOR_EMAIL="test@example.com"
    export GIT_COMMITTER_NAME="Test"
    export GIT_COMMITTER_EMAIL="test@example.com"

    mkdir -p "${TEST_DIR}/bin" "${REPO_DIR}/build" "${REPO_DIR}/db/postgres" "${REPO_DIR}/db/neo4j"
    cp "${VERSION_SOURCE}" "${REPO_DIR}/build/version.sh"
    cp "${POSTGRES_SOURCE}" "${REPO_DIR}/db/postgres/build.sh"
    cp "${NEO4J_SOURCE}" "${REPO_DIR}/db/neo4j/build.sh"
    git -C "${REPO_DIR}" init --quiet
    git -C "${REPO_DIR}" add .
    git -C "${REPO_DIR}" commit --quiet -m "first"

    write_docker_stub
    chmod 0755 "${TEST_DIR}/bin/docker"
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

@test "postgres build with mcp and db tags - prints the db version" {
    git -C "${REPO_DIR}" tag mcp/v0.6.0
    git -C "${REPO_DIR}" tag db/v1.0.0

    run bash "${REPO_DIR}/db/postgres/build.sh"

    assert_success
    assert_output --partial "Version : v1.0.0"
    refute_output --partial "mcp/v0.6.0"
    grep -qF 'ghcr.io/twistingmercury/mnemonic-postgres:v1.0.0' "${DOCKER_LOG}"
}

@test "neo4j build with mcp and db tags - prints the db version" {
    git -C "${REPO_DIR}" tag mcp/v0.6.0
    git -C "${REPO_DIR}" tag db/v1.0.0

    run bash "${REPO_DIR}/db/neo4j/build.sh"

    assert_success
    assert_output --partial "Version : v1.0.0"
    refute_output --partial "mcp/v0.6.0"
    grep -qF 'ghcr.io/twistingmercury/mnemonic-neo4j:v1.0.0' "${DOCKER_LOG}"
}

@test "postgres build with only an mcp tag - prints v0.0.0-dev" {
    git -C "${REPO_DIR}" tag mcp/v0.6.0

    run bash "${REPO_DIR}/db/postgres/build.sh"

    assert_success
    assert_output --partial "Version : v0.0.0-dev"
}

@test "neo4j build with only an mcp tag - prints v0.0.0-dev" {
    git -C "${REPO_DIR}" tag mcp/v0.6.0

    run bash "${REPO_DIR}/db/neo4j/build.sh"

    assert_success
    assert_output --partial "Version : v0.0.0-dev"
}

@test "postgres build with MNEMONIC_VERSION and a db tag - the override wins" {
    git -C "${REPO_DIR}" tag db/v1.0.0
    export MNEMONIC_VERSION="v9.9.9-test"

    run bash "${REPO_DIR}/db/postgres/build.sh"

    assert_success
    assert_output --partial "Version : v9.9.9-test"
}

@test "neo4j build with MNEMONIC_VERSION and a db tag - the override wins" {
    git -C "${REPO_DIR}" tag db/v1.0.0
    export MNEMONIC_VERSION="v9.9.9-test"

    run bash "${REPO_DIR}/db/neo4j/build.sh"

    assert_success
    assert_output --partial "Version : v9.9.9-test"
}
