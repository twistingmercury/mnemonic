#!/usr/bin/env bats

load 'test_helper/bats-support/load'
load 'test_helper/bats-assert/load'

SOURCE_SCRIPT="${BATS_TEST_DIRNAME}/../../build/changed-components.sh"

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
    SCRIPT_PATH="${REPO_DIR}/build/changed-components.sh"

    mkdir -p "${REPO_DIR}/build" "${STUB_BIN}"
    cp "${SOURCE_SCRIPT}" "${SCRIPT_PATH}"
    install_docker_stub
    git -C "${REPO_DIR}" init --quiet
    add_file "README.md" "base"
    add_file "src/internal/api/x.go" "base"
    commit_all "base"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

# Stands in for the Go tooling image: every service lists the same shared
# package, except enricher, which lists none.
install_docker_stub() {
    cat > "${STUB_BIN}/docker" <<'STUB'
#!/usr/bin/env bash
case "$*" in
    *"./cmd/enricher"*) exit 0 ;;
esac
printf '%s\n' "/repo/src/internal/shared" "/go/pkg/mod/example.com/lib@v1"
STUB
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

# Commits one edit to ${1} and runs the script over that single commit.
run_after_change() {
    add_file "${1}" "change"
    commit_all "change"
    run "${SCRIPT_PATH}" HEAD~1 HEAD
}

@test "changed-components docs only change - prints nothing" {
    run_after_change "README.md"

    assert_success
    assert_output ""
}

@test "changed-components service folder - marks only that service" {
    run_after_change "src/internal/api/x.go"

    assert_success
    assert_output "api"
}

@test "changed-components cmd folder - marks that service" {
    run_after_change "src/cmd/mcp/main.go"

    assert_success
    assert_output "mcp"
}

@test "changed-components service Dockerfile - marks only that service" {
    run_after_change "build/enricher/Dockerfile"

    assert_success
    assert_output "enricher"
}

@test "changed-components build script only - prints nothing" {
    run_after_change "build/enricher/enricher.build.sh"

    assert_success
    assert_output ""
}

@test "changed-components markdown in a component folder - prints nothing" {
    run_after_change "db/README.md"

    assert_success
    assert_output ""
}

@test "changed-components markdown in a service package - marks nothing" {
    run_after_change "src/internal/shared/README.md"

    assert_success
    assert_output ""
}

@test "changed-components markdown beside source - marks only for the source" {
    add_file "build/api/README.md" "change"
    add_file "src/internal/api/x.go" "change"
    commit_all "change"

    run "${SCRIPT_PATH}" HEAD~1 HEAD

    assert_success
    assert_output "api"
}

@test "changed-components shared package - marks its importers" {
    run_after_change "src/internal/shared/x.go"

    assert_success
    assert_output "$(printf 'api\nmcp')"
}

@test "changed-components go.mod - marks all three services" {
    run_after_change "src/go.mod"

    assert_success
    assert_output "$(printf 'api\nenricher\nmcp')"
}

@test "changed-components build lib - marks all three services" {
    run_after_change "build/build-lib.sh"

    assert_success
    assert_output "$(printf 'api\nenricher\nmcp')"
}

@test "changed-components tests integration - marks all three services" {
    run_after_change "tests/integration/test_x.py"

    assert_success
    assert_output "$(printf 'api\nenricher\nmcp')"
}

@test "changed-components github folder - marks all three services" {
    run_after_change ".github/workflows/ci.yml"

    assert_success
    assert_output "$(printf 'api\nenricher\nmcp')"
}

@test "changed-components tests openai-stub - marks all three services" {
    run_after_change "tests/openai-stub/main.go"

    assert_success
    assert_output "$(printf 'api\nenricher\nmcp')"
}

@test "changed-components docker failure - fails instead of skipping the service" {
    printf '#!/usr/bin/env bash\nexit 1\n' > "${STUB_BIN}/docker"

    run_after_change "src/internal/shared/x.go"

    assert_failure
}

@test "changed-components db - maps to db" {
    run_after_change "db/schema.sql"

    assert_success
    assert_output "db"
}

@test "changed-components gateway - maps to gateway" {
    run_after_change "gateway/envoy.yaml"

    assert_success
    assert_output "gateway"
}

@test "changed-components helm chart - maps to helm" {
    run_after_change "deploy/helm/Chart.yaml"

    assert_success
    assert_output "helm"
}

@test "changed-components zero base - prints all six" {
    run "${SCRIPT_PATH}" 0000000000000000000000000000000000000000 HEAD

    assert_success
    assert_output "$(printf 'api\ndb\nenricher\ngateway\nhelm\nmcp')"
}

@test "changed-components moved file - marks the old and the new directory" {
    mkdir -p "${REPO_DIR}/src/internal/mcp"
    git -C "${REPO_DIR}" mv src/internal/api/x.go src/internal/mcp/x.go
    git -C "${REPO_DIR}" commit --quiet -m "move"

    run "${SCRIPT_PATH}" HEAD~1 HEAD

    assert_success
    assert_output "$(printf 'api\nmcp')"
}

@test "changed-components unknown commit - fails" {
    run "${SCRIPT_PATH}" does-not-exist HEAD

    assert_failure
}

@test "changed-components missing arguments - fails" {
    run "${SCRIPT_PATH}"

    assert_failure
}
