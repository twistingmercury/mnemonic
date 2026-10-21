# Library: build-lib.sh
# Description: Image build and e2e steps shared by api.build.sh, mcp.build.sh
#              and enricher.build.sh, which differ only in names and services.
# Usage: source it from build/<name>/<name>.build.sh after setting PROJ_ROOT,
#        COMPONENT, IMAGE_NAME, IMAGE_TAG, BUILD_VER, BUILD_DATE, BUILD_COMMIT,
#        E2E_COMPOSE_FILE, E2E_INFRA_SERVICES, E2E_UP_SERVICES and
#        E2E_EXIT_CODE_FROM (arrays for the *_SERVICES). Optional:
#        E2E_PULL_SERVICES (array; pulled first with PULL_POLICY).
#
# Both steps print one progress line and show docker output only on failure.
# The image build always runs with --no-cache and --pull so the quality gates
# and the base image are fresh on every run.

# Runs a command with its output held back; prints the output if it fails.
build_lib::run_quiet() {
    local log
    local rc=0

    log="$(mktemp 2>/dev/null || mktemp -t 'mnemonic-build')"
    "$@" >"${log}" 2>&1 || rc=$?
    if [ "${rc}" -ne 0 ]; then
        cat "${log}"
    fi

    rm -f "${log}"

    return "${rc}"
}

# Builds the final image from the repo root. BuildKit stays on so the cache
# mounts in the Dockerfile still work; --no-cache only skips the layer cache.
build_lib::build_image() {
    local rc=0

    export DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}"

    printf "building %s..." "${COMPONENT}"
    build_lib::run_quiet docker build --rm --no-cache --pull --progress=plain \
        --file "${PROJ_ROOT}/build/${COMPONENT}/${COMPONENT}.Dockerfile" \
        --build-arg BUILD_VER="${BUILD_VER}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg BUILD_COMMIT="${BUILD_COMMIT}" \
        --target final \
        --tag "${IMAGE_NAME}:${IMAGE_TAG}" \
        --tag "${IMAGE_NAME}:latest" \
        "${PROJ_ROOT}" || rc=$?
    if [ "${rc}" -ne 0 ]; then
        printf "failed\n"
        printf "ERROR: docker build failed for %s (exit %s)\n" "${COMPONENT}" "${rc}" >&2
        return "${rc}"
    fi

    printf "done\n"

    return 0
}

# Every compose call of a run goes through here so they all share one project
# name; that scoping is what lets teardown remove only what this run created.
build_lib::compose() {
    docker compose -p "${E2E_PROJECT}" -f "${E2E_COMPOSE_FILE}" "$@"
}

build_lib::e2e_pull() {
    if [ "${#E2E_PULL_SERVICES[@]}" -eq 0 ]; then
        return 0
    fi

    build_lib::run_quiet build_lib::compose pull \
        --policy "${PULL_POLICY:-always}" "${E2E_PULL_SERVICES[@]}"
}

build_lib::e2e_run() {
    if ! build_lib::e2e_pull; then
        return 1
    fi

    if ! build_lib::run_quiet build_lib::compose up -d "${E2E_INFRA_SERVICES[@]}"; then
        return 1
    fi

    build_lib::run_quiet build_lib::compose up \
        --build --abort-on-container-exit \
        --exit-code-from "${E2E_EXIT_CODE_FROM}" "${E2E_UP_SERVICES[@]}"
}

# Removes only what this run's compose project created: containers, networks,
# volumes, orphans and unnamed (local) images. Images with a tag of their own,
# such as the one just built, are left alone by --rmi local. Safe to call twice.
build_lib::e2e_teardown() {
    if [ "${E2E_TORN_DOWN:-0}" = "1" ]; then
        return 0
    fi

    E2E_TORN_DOWN=1
    build_lib::compose down -v --remove-orphans --rmi local >/dev/null 2>&1 || true
    printf "cleaned up %s\n" "${E2E_PROJECT}" >&8

    return 0
}

# Signal handler. A trap must end the script itself, so it exits (with the
# conventional 128+signal status); the EXIT trap then runs the teardown. A trap
# fires while run_quiet still has stdout and stderr redirected into its log, so
# the messages use the copies of the real streams saved on fds 8 and 9.
build_lib::e2e_on_signal() {
    local signal_name="${1}"
    local status="${2}"

    printf "\ninterrupted by %s\n" "${signal_name}" >&9
    exit "${status}"
}

# Runs the compose-based e2e suite under its own project name, and always tears
# that project down, also when a step fails or the run is interrupted.
build_lib::e2e_tests() {
    local rc=0

    E2E_PROJECT="mnemonic-e2e-${COMPONENT}-$$"
    E2E_TORN_DOWN=0
    exec 8>&1 9>&2
    trap build_lib::e2e_teardown EXIT
    trap 'build_lib::e2e_on_signal INT 130' INT
    trap 'build_lib::e2e_on_signal TERM 143' TERM

    printf "starting end-to-end tests..."
    build_lib::e2e_run || rc=$?
    if [ "${rc}" -ne 0 ]; then
        printf "failed\n"
        printf "ERROR: end-to-end tests failed for %s (exit %s)\n" "${COMPONENT}" "${rc}" >&2
    fi

    if [ "${rc}" -eq 0 ]; then
        printf "done\n"
    fi

    build_lib::e2e_teardown
    trap - EXIT INT TERM
    exec 8>&- 9>&-

    return "${rc}"
}
