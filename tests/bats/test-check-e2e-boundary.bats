#!/usr/bin/env bats

load 'test_helper/bats-support/load'
load 'test_helper/bats-assert/load'

SCRIPT_PATH="${BATS_TEST_DIRNAME}/../../build/check-e2e-boundary.sh"

setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    export MNEMONIC_PROJ_ROOT="${TEST_DIR}/root"
    export SUITE_DIR="${MNEMONIC_PROJ_ROOT}/tests/integration"

    mkdir -p "${SUITE_DIR}/src" "${SUITE_DIR}/tests"
    printf "import os\nprint('hello')\n" > "${SUITE_DIR}/src/helper.py"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

@test "boundary check on a clean tree - succeeds" {
    run "${SCRIPT_PATH}"

    assert_success
}

@test "boundary check with a relative src path - fails naming file and line" {
    printf "ok\nPATH = '../src/internal'\n" > "${SUITE_DIR}/tests/test_a.py"

    run "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "tests/integration/tests/test_a.py:2"
}

@test "boundary check with a src/internal path - fails naming file and line" {
    printf "x = 'src/internal/api'\n" > "${SUITE_DIR}/src/b.py"

    run "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "tests/integration/src/b.py:1"
}

@test "boundary check with the Go module path - fails naming file and line" {
    printf "a\nb\nc\n# github.com/twistingmercury/mnemonic/x\n" > "${SUITE_DIR}/tests/c.py"

    run "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "tests/integration/tests/c.py:4"
}

@test "boundary check with references in .venv and caches - ignores them" {
    mkdir -p "${SUITE_DIR}/.venv/lib" "${SUITE_DIR}/src/__pycache__" "${SUITE_DIR}/.pytest_cache" "${SUITE_DIR}/.mypy_cache"
    printf "../src\n" > "${SUITE_DIR}/.venv/lib/x.py"
    printf "../src\n" > "${SUITE_DIR}/src/__pycache__/x.pyc"
    printf "../src\n" > "${SUITE_DIR}/.pytest_cache/x"
    printf "../src\n" > "${SUITE_DIR}/.mypy_cache/x"

    run "${SCRIPT_PATH}"

    assert_success
}

@test "boundary check with a reference outside tests/integration - ignores it" {
    mkdir -p "${MNEMONIC_PROJ_ROOT}/other"
    printf "../src\n" > "${MNEMONIC_PROJ_ROOT}/other/x.txt"

    run "${SCRIPT_PATH}"

    assert_success
}

@test "boundary check with a missing suite directory - fails naming it" {
    rm -rf "${SUITE_DIR}"

    run "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR"
    assert_output --partial "${SUITE_DIR}"
}

@test "boundary check with the Python twin guard test_blackbox.py - ignores its patterns" {
    printf "PATTERN = 'src/internal'\n" > "${SUITE_DIR}/tests/test_blackbox.py"

    run "${SCRIPT_PATH}"

    assert_success
}

@test "boundary check with a similarly named file - still fails" {
    printf "PATTERN = 'src/internal'\n" > "${SUITE_DIR}/tests/test_blackbox_extra.py"

    run "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "test_blackbox_extra.py:1"
}
