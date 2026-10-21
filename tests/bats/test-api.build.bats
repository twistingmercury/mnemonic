#!/usr/bin/env bats

load 'test_helper/bats-support/load'
load 'test_helper/bats-assert/load'
load 'test_helper/build_stubs'

COMPONENT="api"
SCRIPT_PATH="${BATS_TEST_DIRNAME}/../../build/${COMPONENT}/${COMPONENT}.build.sh"

setup() {
    export TEST_DIR="${BATS_TEST_TMPDIR}/test-$$"
    mkdir -p "${TEST_DIR}"
    install_stubs
    export STUB_GIT_DESCRIBE="${COMPONENT}/v0.5.1"
    export STUB_GIT_COMMIT="abc1234"
    export BUILD_DATE="2026-01-02T03:04:05Z"
}

teardown() {
    rm -rf "${TEST_DIR}"
}

@test "${COMPONENT} build - runs docker build with the exact expected arguments" {
    run "${SCRIPT_PATH}"

    assert_success
    assert_equal "$(build_invocation)" \
        "$(expected_build "${COMPONENT}" 0.5.1 "${BUILD_DATE}" abc1234 v0.5.1)"
}

@test "${COMPONENT} build - always passes --no-cache" {
    run "${SCRIPT_PATH}"

    assert_success
    run build_invocation
    assert_output --partial " --no-cache "
}

@test "${COMPONENT} build - always passes --pull" {
    run "${SCRIPT_PATH}"

    assert_success
    run build_invocation
    assert_output --partial " --pull "
}

@test "${COMPONENT} build with the retired NO_CACHE=0 and PULL=0 set - still passes both" {
    export NO_CACHE=0
    export PULL=0

    run "${SCRIPT_PATH}"

    assert_success
    run build_invocation
    assert_output --partial " --no-cache --pull "
}

@test "${COMPONENT} build - enables BuildKit for the docker call" {
    run "${SCRIPT_PATH}"

    assert_success
    run sed -n '1p' "${BUILDKIT_LOG}"
    assert_output "1"
}

@test "${COMPONENT} build - runs the build, then e2e, then tears the stack down" {
    run "${SCRIPT_PATH}"

    assert_success
    build_line="$(log_line_of 'build --rm')"
    e2e_line="$(log_line_of '--abort-on-container-exit')"
    down_line="$(log_line_of 'down -v --remove-orphans')"
    assert [ "${build_line}" = "1" ]
    assert [ "${e2e_line}" -gt "${build_line}" ]
    assert [ "${down_line}" -gt "${e2e_line}" ]
}

@test "${COMPONENT} build with a failing e2e - exits non-zero" {
    export STUB_DOCKER_FAIL_ON="--abort-on-container-exit"

    run "${SCRIPT_PATH}"

    assert_failure
    assert_output --partial "end-to-end tests failed"
}

@test "${COMPONENT} build - tears the stack down even when e2e fails" {
    export STUB_DOCKER_FAIL_ON="--abort-on-container-exit"

    run "${SCRIPT_PATH}"

    assert_failure
    assert [ -n "$(log_line_of 'down -v --remove-orphans')" ]
}

@test "${COMPONENT} build - uses the legacy compose file unless E2E_COMPOSE_FILE is set" {
    run "${SCRIPT_PATH}"

    assert_success
    assert [ -n "$(log_line_of "-f ${REPO_ROOT}/tests/legacy/${COMPONENT}/docker-compose.yaml up")" ]
}

@test "${COMPONENT} build with E2E_COMPOSE_FILE set - runs e2e from that file" {
    export E2E_COMPOSE_FILE="${TEST_DIR}/other-compose.yaml"

    run "${SCRIPT_PATH}"

    assert_success
    assert [ -n "$(log_line_of "-f ${E2E_COMPOSE_FILE} up")" ]
}

@test "${COMPONENT} build - takes BUILD_VER from build/version.sh with the prefix stripped" {
    export STUB_GIT_DESCRIBE="${COMPONENT}/v1.2.3"

    run "${SCRIPT_PATH}"

    assert_success
    assert_equal "$(build_invocation)" \
        "$(expected_build "${COMPONENT}" 1.2.3 "${BUILD_DATE}" abc1234 v1.2.3)"
}

@test "${COMPONENT} build with no component tag - versions the image 0.0.0-dev" {
    unset STUB_GIT_DESCRIBE

    run "${SCRIPT_PATH}"

    assert_success
    assert_equal "$(build_invocation)" \
        "$(expected_build "${COMPONENT}" 0.0.0-dev "${BUILD_DATE}" abc1234 v0.0.0-dev)"
}

@test "${COMPONENT} build with BUILD_VER and BUILD_COMMIT set - uses them" {
    export BUILD_VER="9.9.9"
    export BUILD_COMMIT="feedbee"

    run "${SCRIPT_PATH}"

    assert_success
    assert_equal "$(build_invocation)" \
        "$(expected_build "${COMPONENT}" 9.9.9 "${BUILD_DATE}" feedbee v9.9.9)"
}

@test "${COMPONENT} build with IMAGE_NAME and IMAGE_TAG set - tags with them" {
    export IMAGE_NAME="registry.example.com/team/${COMPONENT}"
    export IMAGE_TAG="latest-dev"

    run "${SCRIPT_PATH}"

    assert_success
    assert [ -n "$(log_line_of "--tag registry.example.com/team/${COMPONENT}:latest-dev --tag registry.example.com/team/${COMPONENT}:latest ")" ]
}

@test "${COMPONENT} build with git unable to name a commit - passes unknown" {
    unset STUB_GIT_COMMIT
    unset BUILD_COMMIT

    run "${SCRIPT_PATH}"

    assert_success
    assert_equal "$(build_invocation)" \
        "$(expected_build "${COMPONENT}" 0.5.1 "${BUILD_DATE}" unknown v0.5.1)"
}

@test "${COMPONENT} build without BUILD_DATE - passes a UTC timestamp" {
    unset BUILD_DATE

    run "${SCRIPT_PATH}"

    assert_success
    run grep -E -c -e 'BUILD_DATE=[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z ' "${DOCKER_LOG}"
    assert_output "1"
}

@test "${COMPONENT} build run from another directory - uses the repo root as context" {
    cd "${TEST_DIR}"

    run "${SCRIPT_PATH}"

    assert_success
    assert_equal "$(build_invocation)" \
        "$(expected_build "${COMPONENT}" 0.5.1 "${BUILD_DATE}" abc1234 v0.5.1)"
}

@test "${COMPONENT} build when docker build fails - exits non-zero and skips e2e" {
    export STUB_DOCKER_FAIL_ON="build --rm"

    run "${SCRIPT_PATH}"

    assert_failure
    assert_output --partial "docker build failed"
    run grep -c -e 'compose' "${DOCKER_LOG}"
    assert_output "0"
}

@test "${COMPONENT} build - never pushes an image" {
    run "${SCRIPT_PATH}"

    assert_success
    run grep -c -e '^push' "${DOCKER_LOG}"
    assert_output "0"
}

@test "${COMPONENT} build - tags the default image ghcr.io/twistingmercury/mnemonic-${COMPONENT} and latest" {
    run "${SCRIPT_PATH}"

    assert_success
    run build_invocation
    assert_output --partial "--tag ghcr.io/twistingmercury/mnemonic-${COMPONENT}:v0.5.1 --tag ghcr.io/twistingmercury/mnemonic-${COMPONENT}:latest "
}

@test "${COMPONENT} build - passes no docker build argument naming the placeholder librarian" {
    run "${SCRIPT_PATH}"

    assert_success
    # Only the docker build line is inspected: the compose lines are out of
    # scope because the parked services keep their old names until the compose
    # files move. The checkout directory may itself carry the placeholder name,
    # so the repo root is stripped first.
    local logged
    logged="$(build_invocation | sed "s#${REPO_ROOT}##g")"
    refute_regex_placeholder "${logged}"
}

@test "${COMPONENT} Dockerfile - stamps the version through the mnemonic module path" {
    local dockerfile="${REPO_ROOT}/build/${COMPONENT}/${COMPONENT}.Dockerfile"
    local module_path="github.com/twistingmercury/mnemonic/internal/${COMPONENT}/version"

    run grep -F -e "-X '${module_path}.version=\${BUILD_VER}'" \
        -e "-X '${module_path}.buildDate=\${BUILD_DATE}'" \
        -e "-X '${module_path}.commit=\${BUILD_COMMIT}'" "${dockerfile}"

    assert_success
    assert_equal "${#lines[@]}" "3"
}

@test "${COMPONENT} Dockerfile - labels the source as the mnemonic repository" {
    local dockerfile="${REPO_ROOT}/build/${COMPONENT}/${COMPONENT}.Dockerfile"

    run grep -F -e 'org.opencontainers.image.source="https://github.com/twistingmercury/mnemonic"' "${dockerfile}"

    assert_success
}

@test "${COMPONENT} e2e - every docker compose call carries the same mnemonic-e2e project name" {
    run "${SCRIPT_PATH}"

    assert_success
    run compose_calls_without_project "${COMPONENT}"
    assert_output ""
    run compose_project_names
    assert_output --regexp "^mnemonic-e2e-${COMPONENT}-[0-9]+\$"
}

@test "${COMPONENT} e2e - tears the project down with volumes, orphans and local images" {
    run "${SCRIPT_PATH}"

    assert_success
    run compose_calls
    assert_line --regexp "^compose -p mnemonic-e2e-${COMPONENT}-[0-9]+ -f .* down -v --remove-orphans --rmi local\$"
}

@test "${COMPONENT} e2e - tears down only after the tests finish" {
    run "${SCRIPT_PATH}"

    assert_success
    local last_line total
    total="$(wc -l < "${DOCKER_LOG}" | tr -d ' ')"
    last_line="$(log_line_of 'down -v --remove-orphans --rmi local')"
    assert [ "${last_line}" = "${total}" ]
}

@test "${COMPONENT} e2e - reports the cleaned project after a pass" {
    run "${SCRIPT_PATH}"

    assert_success
    assert_output --regexp "cleaned up mnemonic-e2e-${COMPONENT}-[0-9]+"
}

@test "${COMPONENT} e2e - tears down and fails when bringing up the infrastructure fails" {
    export STUB_DOCKER_FAIL_ON=" up -d "

    run "${SCRIPT_PATH}"

    assert_failure
    assert_output --partial "end-to-end tests failed"
    assert [ -n "$(log_line_of 'down -v --remove-orphans --rmi local')" ]
}

@test "${COMPONENT} e2e - tears down when interrupted with INT" {
    export STUB_DOCKER_SIGNAL_ON="--abort-on-container-exit"
    export STUB_DOCKER_SIGNAL="INT"

    run "${SCRIPT_PATH}"

    assert_failure 130
    assert [ -n "$(log_line_of 'down -v --remove-orphans --rmi local')" ]
    assert_output --regexp "cleaned up mnemonic-e2e-${COMPONENT}-[0-9]+"
}

@test "${COMPONENT} e2e - tears down when terminated with TERM" {
    export STUB_DOCKER_SIGNAL_ON="--abort-on-container-exit"
    export STUB_DOCKER_SIGNAL="TERM"

    run "${SCRIPT_PATH}"

    assert_failure 143
    assert [ -n "$(log_line_of 'down -v --remove-orphans --rmi local')" ]
}

@test "${COMPONENT} e2e - tears down once when interrupted" {
    export STUB_DOCKER_SIGNAL_ON="--abort-on-container-exit"
    export STUB_DOCKER_SIGNAL="INT"

    run "${SCRIPT_PATH}"

    run grep -c -e ' down ' "${DOCKER_LOG}"
    assert_output "1"
}

@test "${COMPONENT} e2e - keeps the failing exit status after teardown" {
    export STUB_DOCKER_FAIL_ON="--abort-on-container-exit"

    run "${SCRIPT_PATH}"

    assert_failure 1
}

@test "${COMPONENT} e2e - never prunes anything" {
    export STUB_DOCKER_FAIL_ON="--abort-on-container-exit"
    "${SCRIPT_PATH}" || true

    run grep -c -e 'prune' "${DOCKER_LOG}"
    assert_output "0"
}

@test "${COMPONENT} e2e - never removes the image it built" {
    run "${SCRIPT_PATH}"

    assert_success
    run grep -c -E -e '^(rmi|image rm|image remove|rm) ' "${DOCKER_LOG}"
    assert_output "0"
    run grep -c -e "ghcr.io/twistingmercury/mnemonic-${COMPONENT}" <(grep -e ' down ' "${DOCKER_LOG}")
    assert_output "0"
}

@test "${COMPONENT} e2e - a repeated run gets a project name of its own" {
    run "${SCRIPT_PATH}"
    assert_success
    run "${SCRIPT_PATH}"
    assert_success

    run down_project_names
    assert_equal "${#lines[@]}" "2"
}
