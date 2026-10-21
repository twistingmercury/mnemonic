# Library: build-lib.sh
# Description: Image build and e2e steps shared by api.build.sh, mcp.build.sh
#              and enricher.build.sh, which differ only in names and services.
# Usage: source it from build/<name>/<name>.build.sh after setting PROJ_ROOT,
#        COMPONENT, IMAGE_NAME, IMAGE_TAG, BUILD_VER, BUILD_DATE, BUILD_COMMIT,
#        E2E_COMPOSE_FILE, E2E_INFRA_SERVICES, E2E_UP_SERVICES and
#        E2E_EXIT_CODE_FROM (arrays for the *_SERVICES). Optional:
#        E2E_PULL_SERVICES (array; pulled first with PULL_POLICY),
#        NO_CACHE=1 (--no-cache), PULL=1 (--pull).
#
# Both steps print one progress line and show docker output only on failure.

# Runs a command with its output held back; prints the output if it fails.
build_lib::run_quiet() {
    local log
    local rc=0

    log="$(mktemp 2>/dev/null || mktemp -t 'librarian-build')"
    "$@" >"${log}" 2>&1 || rc=$?
    if [ "${rc}" -ne 0 ]; then
        cat "${log}"
    fi

    rm -f "${log}"

    return "${rc}"
}

# Builds the final image from the repo root. BuildKit stays on so the cache
# mounts in the Dockerfile work; the layer cache is skipped only on request.
build_lib::build_image() {
    local rc=0
    local flags=()

    if [ "${NO_CACHE:-0}" = "1" ]; then
        flags+=(--no-cache)
    fi

    if [ "${PULL:-0}" = "1" ]; then
        flags+=(--pull)
    fi

    export DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}"

    printf "building %s..." "${COMPONENT}"
    build_lib::run_quiet docker build --rm ${flags[@]+"${flags[@]}"} --progress=plain \
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

build_lib::e2e_pull() {
    if [ "${#E2E_PULL_SERVICES[@]}" -eq 0 ]; then
        return 0
    fi

    build_lib::run_quiet docker compose -f "${E2E_COMPOSE_FILE}" pull \
        --policy "${PULL_POLICY:-always}" "${E2E_PULL_SERVICES[@]}"
}

build_lib::e2e_run() {
    if ! build_lib::e2e_pull; then
        return 1
    fi

    if ! build_lib::run_quiet docker compose -f "${E2E_COMPOSE_FILE}" up -d "${E2E_INFRA_SERVICES[@]}"; then
        return 1
    fi

    build_lib::run_quiet docker compose -f "${E2E_COMPOSE_FILE}" up \
        --build --abort-on-container-exit \
        --exit-code-from "${E2E_EXIT_CODE_FROM}" "${E2E_UP_SERVICES[@]}"
}

# Runs the compose-based e2e suite, then always tears the stack down.
build_lib::e2e_tests() {
    local rc=0

    printf "starting end-to-end tests..."
    build_lib::e2e_run || rc=$?
    docker compose -f "${E2E_COMPOSE_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true
    if [ "${rc}" -ne 0 ]; then
        printf "failed\n"
        printf "ERROR: end-to-end tests failed for %s (exit %s)\n" "${COMPONENT}" "${rc}" >&2
        return "${rc}"
    fi

    printf "done\n"

    return 0
}
