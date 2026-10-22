#!/usr/bin/env bats

load 'test_helper/bats-support/load'
load 'test_helper/bats-assert/load'

SOURCE_DIR="${BATS_TEST_DIRNAME}/../../build"
ZERO_SHA="0000000000000000000000000000000000000000"

setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    export REPO_DIR="${TEST_DIR}/repo"
    export STUB_BIN="${TEST_DIR}/bin"
    export GIT_CONFIG_GLOBAL="${TEST_DIR}/gitconfig"
    export GIT_CONFIG_SYSTEM=/dev/null
    export GIT_AUTHOR_NAME="Test"
    export GIT_AUTHOR_EMAIL="test@example.com"
    export GIT_COMMITTER_NAME="Test"
    export GIT_COMMITTER_EMAIL="test@example.com"
    SCRIPT_PATH="${REPO_DIR}/build/detect-components.sh"

    mkdir -p "${REPO_DIR}/build" "${STUB_BIN}"
    cp "${SOURCE_DIR}/detect-components.sh" "${SOURCE_DIR}/changed-components.sh" "${REPO_DIR}/build/"
    install_docker_stub
    git -C "${REPO_DIR}" init --quiet
    add_file "README.md" "base"
    commit_all "base"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

# Stands in for the Go tooling image: no service imports a shared package.
install_docker_stub() {
    printf '#!/usr/bin/env bash\nexit 0\n' > "${STUB_BIN}/docker"
    chmod +x "${STUB_BIN}/docker"
    export PATH="${STUB_BIN}:${PATH}"
}

add_file() {
    mkdir -p "${REPO_DIR}/$(dirname "${1}")"
    printf "%s\n" "${2}" >> "${REPO_DIR}/${1}"
}

commit_all() {
    git -C "${REPO_DIR}" add -A
    git -C "${REPO_DIR}" commit --quiet -m "${1}"
}

# Commits one edit to ${1}; the component input is ${2:-}.
run_after_change() {
    add_file "${1}" "change"
    commit_all "change"
    run "${SCRIPT_PATH}" HEAD~1 HEAD "${2:-}"
}

@test "detect-components service override - lists that service in both outputs" {
    run "${SCRIPT_PATH}" "${ZERO_SHA}" HEAD api

    assert_success
    assert_line --index 0 'components=["api"]'
    assert_line --index 1 'services=["api"]'
}

@test "detect-components override - ignores what changed" {
    run_after_change "src/cmd/mcp/main.go" enricher

    assert_success
    assert_line --index 0 'components=["enricher"]'
}

@test "detect-components db override - services is empty" {
    run "${SCRIPT_PATH}" "${ZERO_SHA}" HEAD db

    assert_success
    assert_line --index 0 'components=["db"]'
    assert_line --index 1 'services=[]'
}

@test "detect-components gateway override - services is empty" {
    run "${SCRIPT_PATH}" "${ZERO_SHA}" HEAD gateway

    assert_success
    assert_line --index 1 'services=[]'
}

@test "detect-components helm override - services is empty" {
    run "${SCRIPT_PATH}" "${ZERO_SHA}" HEAD helm

    assert_success
    assert_line --index 1 'services=[]'
}

@test "detect-components all input - falls through to the change detection" {
    run_after_change "src/cmd/mcp/main.go" all

    assert_success
    assert_line --index 0 'components=["mcp"]'
    assert_line --index 1 'services=["mcp"]'
}

@test "detect-components empty input - falls through to the change detection" {
    run_after_change "src/cmd/api/main.go" ""

    assert_success
    assert_line --index 0 'components=["api"]'
}

@test "detect-components zero base - lists all six, services filtered to three" {
    run "${SCRIPT_PATH}" "${ZERO_SHA}" HEAD ""

    assert_success
    assert_line --index 0 'components=["api","db","enricher","gateway","helm","mcp"]'
    assert_line --index 1 'services=["api","enricher","mcp"]'
}

@test "detect-components mixed change - services drops db and helm" {
    add_file "db/schema.sql" "change"
    add_file "deploy/helm/Chart.yaml" "change"
    add_file "src/cmd/api/main.go" "change"
    commit_all "change"

    run "${SCRIPT_PATH}" HEAD~1 HEAD ""

    assert_success
    assert_line --index 0 'components=["api","db","helm"]'
    assert_line --index 1 'services=["api"]'
}

@test "detect-components docs only change - both outputs are empty lists" {
    run_after_change "README.md" ""

    assert_success
    assert_line --index 0 'components=[]'
    assert_line --index 1 'services=[]'
}

@test "detect-components unknown base - fails and prints no output lines" {
    run "${SCRIPT_PATH}" does-not-exist HEAD ""

    assert_failure
    refute_output --partial 'components='
}

@test "detect-components missing arguments - fails with a usage error" {
    run "${SCRIPT_PATH}" HEAD~1 HEAD

    assert_failure
    assert_output --partial "Usage:"
}
