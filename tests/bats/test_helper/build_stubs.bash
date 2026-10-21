# Shared helpers for the <name>.build.sh tests.
#
# The build scripts are run for real, but `docker` and `git` resolve to shims
# placed first on PATH, so no image is built and no repository is read. The
# shims log their argv, one invocation per line, to ${DOCKER_LOG} and ${GIT_LOG}.

REPO_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"

install_stubs() {
    export STUB_BIN="${TEST_DIR}/bin"
    export DOCKER_LOG="${TEST_DIR}/docker.log"
    export GIT_LOG="${TEST_DIR}/git.log"
    export BUILDKIT_LOG="${TEST_DIR}/buildkit.log"

    mkdir -p "${STUB_BIN}"
    : > "${DOCKER_LOG}"
    : > "${GIT_LOG}"

    cat > "${STUB_BIN}/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${DOCKER_LOG}"
printf '%s\n' "${DOCKER_BUILDKIT:-unset}" >> "${BUILDKIT_LOG}"
# Fails any invocation whose argv contains STUB_DOCKER_FAIL_ON.
if [ -n "${STUB_DOCKER_FAIL_ON:-}" ]; then
    case "$*" in
        *"${STUB_DOCKER_FAIL_ON}"*) exit 1 ;;
    esac
fi
# Plays an interrupt: for an argv containing STUB_DOCKER_SIGNAL_ON, signals the
# calling script (INT or TERM, named by STUB_DOCKER_SIGNAL) and exits the way a
# child killed by that signal would. Done from inside the shim because a
# background job cannot trap INT, and this keeps the test free of timing.
if [ -n "${STUB_DOCKER_SIGNAL_ON:-}" ]; then
    case "$*" in
        *"${STUB_DOCKER_SIGNAL_ON}"*)
            kill -s "${STUB_DOCKER_SIGNAL}" "${PPID}"
            case "${STUB_DOCKER_SIGNAL}" in
                INT) exit 130 ;;
                *) exit 143 ;;
            esac
            ;;
    esac
fi
exit 0
STUB

    cat > "${STUB_BIN}/git" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${GIT_LOG}"
for arg in "$@"; do
    if [ "${arg}" = "describe" ]; then
        if [ -z "${STUB_GIT_DESCRIBE:-}" ]; then
            exit 128
        fi
        printf '%s\n' "${STUB_GIT_DESCRIBE}"
        exit 0
    fi
    if [ "${arg}" = "rev-parse" ]; then
        if [ -z "${STUB_GIT_COMMIT:-}" ]; then
            exit 128
        fi
        printf '%s\n' "${STUB_GIT_COMMIT}"
        exit 0
    fi
done
exit 0
STUB

    chmod +x "${STUB_BIN}/docker" "${STUB_BIN}/git"
    export PATH="${STUB_BIN}:${PATH}"
}

# First line the docker shim logged: the `docker build` invocation.
build_invocation() {
    sed -n '1p' "${DOCKER_LOG}"
}

# Line number of the first docker invocation containing the text, or empty.
log_line_of() {
    grep -n -F -e "${1}" "${DOCKER_LOG}" | head -n 1 | cut -d: -f1
}

# The exact `docker build` line expected for <component>, given the version,
# date, commit and image tag. Every build carries --no-cache and --pull.
expected_build() {
    local component="${1}"
    local version="${2}"
    local build_date="${3}"
    local commit="${4}"
    local image_tag="${5}"
    local image="ghcr.io/twistingmercury/mnemonic-${component}"

    printf "build --rm --no-cache --pull --progress=plain --file %s/build/%s/%s.Dockerfile --build-arg BUILD_VER=%s --build-arg BUILD_DATE=%s --build-arg BUILD_COMMIT=%s --target final --tag %s:%s --tag %s:latest %s" \
        "${REPO_ROOT}" "${component}" "${component}" \
        "${version}" "${build_date}" "${commit}" \
        "${image}" "${image_tag}" "${image}" "${REPO_ROOT}"
}

# Fails, printing the text, when it names the placeholder "librarian".
refute_regex_placeholder() {
    local text="${1}"

    if printf "%s" "${text}" | grep -q -i -e 'librarian'; then
        printf "placeholder name found in:\n%s\n" "${text}" >&2
        return 1
    fi

    return 0
}

# Every `docker compose` line the shim logged.
compose_calls() {
    grep -e '^compose ' "${DOCKER_LOG}"
}

# The distinct project names (the argument after -p) across all compose lines.
compose_project_names() {
    compose_calls | awk '{print $3}' | sort -u
}

# Compose lines that do not start with `compose -p mnemonic-e2e-<component>-<pid> `.
compose_calls_without_project() {
    local component="${1}"

    compose_calls | grep -v -E -e "^compose -p mnemonic-e2e-${component}-[0-9]+ " || true
}

# The distinct project names on the `down` lines only.
down_project_names() {
    grep -e ' down ' "${DOCKER_LOG}" | awk '{print $3}' | sort -u
}
