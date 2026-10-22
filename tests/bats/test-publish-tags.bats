#!/usr/bin/env bats

load 'test_helper/bats-support/load'
load 'test_helper/bats-assert/load'

SOURCE_DIR="${BATS_TEST_DIRNAME}/../../build"
FULL_SHA="0123456789abcdef0123456789abcdef01234567"

setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    export REPO_DIR="${TEST_DIR}/repo"
    export GIT_CONFIG_GLOBAL="${TEST_DIR}/gitconfig"
    export GIT_CONFIG_SYSTEM=/dev/null
    export GIT_AUTHOR_NAME="Test"
    export GIT_AUTHOR_EMAIL="test@example.com"
    export GIT_COMMITTER_NAME="Test"
    export GIT_COMMITTER_EMAIL="test@example.com"
    SCRIPT_PATH="${REPO_DIR}/build/publish-tags.sh"

    mkdir -p "${REPO_DIR}/build"
    cp "${SOURCE_DIR}/publish-tags.sh" "${SOURCE_DIR}/version.sh" "${REPO_DIR}/build/"
    git -C "${REPO_DIR}" init --quiet
    git -C "${REPO_DIR}" commit --quiet --allow-empty -m "first"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

@test "publish-tags develop api - prints latest-dev and the short sha" {
    git -C "${REPO_DIR}" tag api/v0.5.1

    run "${SCRIPT_PATH}" develop api "${FULL_SHA}"

    assert_success
    assert_line --index 0 "latest-dev"
    assert_line --index 1 "sha-0123456"
    assert_equal "${#lines[@]}" 2
}

@test "publish-tags develop postgres - never prints a version tag" {
    git -C "${REPO_DIR}" tag db/v1.0.0

    run "${SCRIPT_PATH}" develop postgres "${FULL_SHA}"

    assert_success
    refute_output --partial "1.0.0"
}

@test "publish-tags main api - prints latest and the v-prefixed version" {
    git -C "${REPO_DIR}" tag api/v0.5.1

    run "${SCRIPT_PATH}" main api "${FULL_SHA}"

    assert_success
    assert_line --index 0 "latest"
    assert_line --index 1 "v0.5.1"
    assert_equal "${#lines[@]}" 2
}

@test "publish-tags main postgres - uses the db version" {
    git -C "${REPO_DIR}" tag db/v1.0.0

    run "${SCRIPT_PATH}" main postgres "${FULL_SHA}"

    assert_success
    assert_line --index 1 "v1.0.0"
}

@test "publish-tags main neo4j - uses the db version" {
    git -C "${REPO_DIR}" tag db/v1.0.0

    run "${SCRIPT_PATH}" main neo4j "${FULL_SHA}"

    assert_success
    assert_line --index 1 "v1.0.0"
}

@test "publish-tags main api with a newer mcp tag - keeps the api version" {
    git -C "${REPO_DIR}" tag api/v0.5.1
    git -C "${REPO_DIR}" commit --quiet --allow-empty -m "second"
    git -C "${REPO_DIR}" tag mcp/v0.7.0

    run "${SCRIPT_PATH}" main api "${FULL_SHA}"

    assert_success
    assert_line --index 1 "v0.5.1"
}

@test "publish-tags feature branch - fails naming the branch" {
    run "${SCRIPT_PATH}" feature/x api "${FULL_SHA}"

    assert_failure 1
    assert_output --partial "ERROR"
    assert_output --partial "feature/x"
}

@test "publish-tags with an empty branch - fails" {
    run "${SCRIPT_PATH}" "" api "${FULL_SHA}"

    assert_failure 1
    assert_output --partial "ERROR"
}

@test "publish-tags main with an unknown component - fails" {
    run "${SCRIPT_PATH}" main bogus "${FULL_SHA}"

    assert_failure
    assert_output --partial "bogus"
}
