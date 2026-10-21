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
# date, commit and image tag, plus any extra leading flags (such as --no-cache).
expected_build() {
    local component="${1}"
    local version="${2}"
    local build_date="${3}"
    local commit="${4}"
    local image_tag="${5}"
    local extra_flags="${6:-}"
    local image="ghcr.io/twistingmercury/librarian-${component}"

    printf "build --rm %s--progress=plain --file %s/build/%s/%s.Dockerfile --build-arg BUILD_VER=%s --build-arg BUILD_DATE=%s --build-arg BUILD_COMMIT=%s --target final --tag %s:%s --tag %s:latest %s" \
        "${extra_flags}" "${REPO_ROOT}" "${component}" "${component}" \
        "${version}" "${build_date}" "${commit}" \
        "${image}" "${image_tag}" "${image}" "${REPO_ROOT}"
}
