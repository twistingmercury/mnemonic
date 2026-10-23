#!/usr/bin/env bats

load 'test_helper/bats-support/load'
load 'test_helper/bats-assert/load'

SOURCE_DIR="${BATS_TEST_DIRNAME}/../../build"

# The six checks need Docker and pinned images, so docker is a stub that
# records every call and the boundary check is a stub that prints a marker.
# These tests cover the order, the fail-fast behavior and what each check is
# pointed at; the real run is build/static-checks.sh itself.
setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    export REPO_DIR="${TEST_DIR}/repo"
    export STUB_BIN="${TEST_DIR}/bin"
    export DOCKER_LOG="${TEST_DIR}/docker.log"
    export GIT_CONFIG_GLOBAL="${TEST_DIR}/gitconfig"
    export GIT_CONFIG_SYSTEM=/dev/null
    export GIT_AUTHOR_NAME="Test"
    export GIT_AUTHOR_EMAIL="test@example.com"
    export GIT_COMMITTER_NAME="Test"
    export GIT_COMMITTER_EMAIL="test@example.com"
    SCRIPT_PATH="${REPO_DIR}/build/static-checks.sh"

    mkdir -p "${REPO_DIR}/build" "${STUB_BIN}"
    cp "${SOURCE_DIR}/static-checks.sh" "${SCRIPT_PATH}"
    install_docker_stub
    install_boundary_stub
    git -C "${REPO_DIR}" init --quiet
    printf "# doc\n" > "${REPO_DIR}/README.md"
    printf "#!/usr/bin/env bash\n" > "${REPO_DIR}/tool.sh"
    git -C "${REPO_DIR}" add -A
    git -C "${REPO_DIR}" commit --quiet -m "base"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

# Logs each call; fails when the call contains STUB_FAIL_ON.
install_docker_stub() {
    cat > "${STUB_BIN}/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${DOCKER_LOG}"
case "$*" in
    *"${STUB_FAIL_ON:-@@never@@}"*) exit 1 ;;
esac
exit 0
STUB
    chmod +x "${STUB_BIN}/docker"
    export PATH="${STUB_BIN}:${PATH}"
}

install_boundary_stub() {
    cat > "${REPO_DIR}/build/check-e2e-boundary.sh" <<'STUB'
#!/usr/bin/env bash
printf 'boundary check ran\n'
exit "${STUB_BOUNDARY_STATUS:-0}"
STUB
    chmod +x "${REPO_DIR}/build/check-e2e-boundary.sh"
}

docker_log_line() {
    sed -n "${1}p" "${DOCKER_LOG}"
}

@test "static-checks all pass - runs the six checks in order" {
    run "${SCRIPT_PATH}"

    assert_success
    assert_equal "$(wc -l < "${DOCKER_LOG}" | tr -d ' ')" 7
    assert_line --index 0 "==> shellcheck"
    assert_line --index 1 "==> hadolint"
    assert_line --index 2 "==> markdownlint"
    assert_line --index 3 "==> helm lint"
    assert_line --index 4 "==> bats"
    assert_line --index 5 "==> e2e boundary"
    assert_line --index 6 "boundary check ran"
}

@test "static-checks all pass - each check runs its pinned image" {
    run "${SCRIPT_PATH}"

    assert_success
    assert_regex "$(docker_log_line 1)" 'koalaman/shellcheck:v[0-9.]+@sha256:[0-9a-f]{64}'
    assert_regex "$(docker_log_line 2)" 'hadolint/hadolint:v[0-9.]+@sha256:[0-9a-f]{64} hadolint --failure-threshold warning'
    assert_regex "$(docker_log_line 3)" 'markdownlint-cli:v[0-9.]+@sha256:[0-9a-f]{64}'
    assert_regex "$(docker_log_line 4)" 'alpine/helm:[0-9.]+@sha256:[0-9a-f]{64} lint --strict deploy/helm/mnemonic'
    assert_regex "$(docker_log_line 5)" '^pull bats/bats:[0-9.]+@sha256:[0-9a-f]{64}$'
    assert_regex "$(docker_log_line 6)" '^pull ghcr.io/jqlang/jq:[0-9.]+@sha256:[0-9a-f]{64}$'
    assert_regex "$(docker_log_line 7)" '--mount type=image,source=ghcr.io/jqlang/jq:[0-9.]+@sha256:[0-9a-f]{64},target=/jq-bin,readonly'
    assert_regex "$(docker_log_line 7)" '--mount type=image,source=bats/bats:[0-9.]+@sha256:[0-9a-f]{64},target=/bats,readonly'
}

@test "static-checks lint checks - receive the tracked files" {
    run "${SCRIPT_PATH}"

    assert_success
    assert_regex "$(docker_log_line 1)" 'tool\.sh$'
    assert_regex "$(docker_log_line 3)" '-c \.markdownlint\.json README\.md$'
}

@test "static-checks bats check - runs the build and db test files" {
    run "${SCRIPT_PATH}"

    assert_success
    assert_regex "$(docker_log_line 7)" 'golang:[0-9.]+-bookworm@sha256:[0-9a-f]{64} tests/bats db/tests/bats/build-scripts.bats db/tests/bats/build-version.bats db/tests/bats/schema-entrypoint.bats$'
}

@test "static-checks from another directory - mounts the repo root" {
    cd "${TEST_DIR}"

    run "${SCRIPT_PATH}"

    assert_success
    assert_regex "$(docker_log_line 1)" "-v ${REPO_DIR}:/repo:ro"
}

@test "static-checks failing hadolint - stops before the later checks" {
    export STUB_FAIL_ON="hadolint/hadolint"

    run "${SCRIPT_PATH}"

    assert_failure
    assert_equal "$(wc -l < "${DOCKER_LOG}" | tr -d ' ')" 2
    assert_output --partial "==> hadolint"
    refute_output --partial "==> markdownlint"
    refute_output --partial "boundary check ran"
}

@test "static-checks failing shellcheck - stops after the first check" {
    export STUB_FAIL_ON="koalaman/shellcheck"

    run "${SCRIPT_PATH}"

    assert_failure
    assert_equal "$(wc -l < "${DOCKER_LOG}" | tr -d ' ')" 1
    refute_output --partial "==> hadolint"
}

@test "static-checks failing bats image pull - does not run the tests" {
    export STUB_FAIL_ON="pull bats/bats"

    run "${SCRIPT_PATH}"

    assert_failure
    assert_equal "$(wc -l < "${DOCKER_LOG}" | tr -d ' ')" 5
    refute_output --partial "boundary check ran"
}

@test "static-checks failing boundary check - fails the script" {
    export STUB_BOUNDARY_STATUS=1

    run "${SCRIPT_PATH}"

    assert_failure
    assert_output --partial "boundary check ran"
}

@test "static-checks failing jq image pull - does not run the tests" {
    export STUB_FAIL_ON="pull ghcr.io/jqlang/jq"

    run "${SCRIPT_PATH}"

    assert_failure
    assert_equal "$(wc -l < "${DOCKER_LOG}" | tr -d ' ')" 6
    refute_output --partial "boundary check ran"
}
