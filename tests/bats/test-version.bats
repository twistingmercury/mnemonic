#!/usr/bin/env bats

load 'test_helper/bats-support/load'
load 'test_helper/bats-assert/load'

SOURCE_SCRIPT="${BATS_TEST_DIRNAME}/../../build/version.sh"

setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    export REPO_DIR="${TEST_DIR}/repo"
    export OUTSIDE_DIR="${TEST_DIR}/outside"
    export GIT_CONFIG_GLOBAL="${TEST_DIR}/gitconfig"
    export GIT_CONFIG_SYSTEM=/dev/null
    export GIT_AUTHOR_NAME="Test"
    export GIT_AUTHOR_EMAIL="test@example.com"
    export GIT_COMMITTER_NAME="Test"
    export GIT_COMMITTER_EMAIL="test@example.com"
    SCRIPT_PATH="${REPO_DIR}/build/version.sh"

    mkdir -p "${REPO_DIR}/build" "${OUTSIDE_DIR}"
    cp "${SOURCE_SCRIPT}" "${SCRIPT_PATH}"
    git -C "${REPO_DIR}" init --quiet
    commit_change "first"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

commit_change() {
    printf "%s\n" "${1}" >> "${REPO_DIR}/history.txt"
    git -C "${REPO_DIR}" add history.txt
    git -C "${REPO_DIR}" commit --quiet -m "${1}"
}

@test "version api with tag api/v0.5.1 - prints 0.5.1" {
    git -C "${REPO_DIR}" tag api/v0.5.1

    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.5.1"
}

@test "version mcp with tag mcp/v0.6.1 - prints 0.6.1" {
    git -C "${REPO_DIR}" tag mcp/v0.6.1

    run "${SCRIPT_PATH}" mcp

    assert_success
    assert_output "0.6.1"
}

@test "version api run from outside the repo - still reads the repo tags" {
    git -C "${REPO_DIR}" tag api/v0.5.1

    cd "${OUTSIDE_DIR}"
    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.5.1"
}

@test "version api run from a subdirectory - still reads the repo tags" {
    git -C "${REPO_DIR}" tag api/v0.5.1

    cd "${REPO_DIR}/build"
    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.5.1"
}

@test "version api with no tags - prints 0.0.0-dev and exits 0" {
    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.0.0-dev"
}

@test "version api with only an mcp tag - prints 0.0.0-dev" {
    git -C "${REPO_DIR}" tag mcp/v0.6.1

    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.0.0-dev"
}

@test "version api with a repo-wide v0.9.0 tag - ignores it" {
    git -C "${REPO_DIR}" tag v0.9.0

    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.0.0-dev"
}

@test "version api with two api tags - prints the nearest one" {
    git -C "${REPO_DIR}" tag api/v0.5.0
    commit_change "second"
    git -C "${REPO_DIR}" tag api/v0.5.1
    commit_change "third"

    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.5.1"
}

@test "version api with a newer mcp tag - api keeps its own version" {
    git -C "${REPO_DIR}" tag api/v0.5.1
    commit_change "second"
    git -C "${REPO_DIR}" tag mcp/v0.7.0

    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.5.1"
}

@test "version api with a prerelease tag - keeps the prerelease suffix" {
    git -C "${REPO_DIR}" tag api/v1.0.0-rc.1

    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "1.0.0-rc.1"
}

@test "version with no component - fails naming the usage" {
    run "${SCRIPT_PATH}"

    assert_failure
    assert_output --partial "ERROR"
    assert_output --partial "component"
}

@test "version with an unknown component - fails naming the component" {
    run "${SCRIPT_PATH}" bogus

    assert_failure
    assert_output --partial "ERROR"
    assert_output --partial "bogus"
}

@test "version with a glob as component - fails instead of matching tags" {
    git -C "${REPO_DIR}" tag api/v0.5.1

    run "${SCRIPT_PATH}" '*'

    assert_failure
    assert_output --partial "ERROR"
}

@test "version api with MNEMONIC_PROJ_ROOT set - reads the tags of that repo" {
    local other_repo="${OUTSIDE_DIR}/other"
    git init --quiet "${other_repo}"
    git -C "${other_repo}" commit --quiet --allow-empty -m "other"
    git -C "${other_repo}" tag api/v7.7.7
    git -C "${REPO_DIR}" tag api/v0.5.1
    export MNEMONIC_PROJ_ROOT="${other_repo}"

    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "7.7.7"
}

@test "version api with the unprefixed PROJ_ROOT set - ignores it" {
    local other_repo="${OUTSIDE_DIR}/other"
    git init --quiet "${other_repo}"
    git -C "${other_repo}" commit --quiet --allow-empty -m "other"
    git -C "${other_repo}" tag api/v7.7.7
    git -C "${REPO_DIR}" tag api/v0.5.1
    export PROJ_ROOT="${other_repo}"

    run "${SCRIPT_PATH}" api

    assert_success
    assert_output "0.5.1"
}
