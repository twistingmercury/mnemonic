#!/usr/bin/env bash
#
# Runs the repo's static checks, in order, stopping at the first failure:
# shellcheck, hadolint, markdownlint, helm lint, bats, e2e boundary.
# Usage: static-checks.sh
# Every tool runs from a pinned image, so only Docker and git are needed. CI and
# a local run are the same command and work from any directory.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

readonly SHELLCHECK_IMAGE="koalaman/shellcheck:v0.11.0@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d"
readonly HADOLINT_IMAGE="hadolint/hadolint:v2.15.1@sha256:32dac94127fd60b7b7e3fbfc65e1383b9b5e25c9bfd7b8536de7a539fe68a12d"
readonly MARKDOWNLINT_IMAGE="ghcr.io/igorshubovych/markdownlint-cli:v0.49.1@sha256:905baf9f6bd11da2ede8c394882c616509385f24b8b8d54fff6e111be88f39de"
readonly HELM_IMAGE="alpine/helm:4.3.1@sha256:e3f8d5c462d75470fbb1fce79ad5c20063657096d1f98c0fcea869e2600e2e5e"
readonly BATS_IMAGE="bats/bats:1.14.0@sha256:5322b877351fda0cc435de8c6116de7d0a2ec79d7c680132a0ef329a633bc66f"
readonly JQ_IMAGE="ghcr.io/jqlang/jq:1.8.1@sha256:4f34c6d23f4b1372ac789752cc955dc67c2ae177eb1b5860b75cdc5091ce6f91"
readonly GO_IMAGE="golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5"

check_shellcheck() {
    git ls-files -z '*.sh' '*.bash' ':!tests/bats/test_helper/bats-*' \
        | xargs -0 docker run --rm -v "${PROJ_ROOT}:/repo:ro" -w /repo "${SHELLCHECK_IMAGE}"
}

check_hadolint() {
    docker run --rm -v "${PROJ_ROOT}:/repo:ro" -w /repo "${HADOLINT_IMAGE}" \
        hadolint --failure-threshold warning build/*/*.Dockerfile db/*/Dockerfile
}

check_markdownlint() {
    git ls-files -z '*.md' \
        | xargs -0 docker run --rm -v "${PROJ_ROOT}:/repo:ro" -w /repo "${MARKDOWNLINT_IMAGE}" \
            -c .markdownlint.json
}

check_helm_lint() {
    docker run --rm -v "${PROJ_ROOT}:/repo:ro" -w /repo "${HELM_IMAGE}" \
        lint --strict deploy/helm/mnemonic
}

# The BATS image has no git and the tests need it, so bats is mounted from its
# image into a Debian Go image, which has git and GNU grep. The db files listed
# are the ones that need no database; the migration and image tests run in the
# db job. The detect-components tests need jq, which the Go image lacks, so its
# static binary is mounted too. --mount does not pull its source image, so both
# are pulled first.
check_bats() {
    # errexit is off inside a function called from an if, so a failed pull is
    # checked by hand.
    if ! docker pull "${BATS_IMAGE}"; then
        return 1
    fi

    if ! docker pull "${JQ_IMAGE}"; then
        return 1
    fi

    docker run --rm -v "${PROJ_ROOT}:/repo:ro" -w /repo \
        --mount "type=image,source=${BATS_IMAGE},target=/bats,readonly" \
        --mount "type=image,source=${JQ_IMAGE},target=/jq-bin,readonly" \
        -e PATH=/jq-bin:/usr/local/bin:/usr/bin:/bin \
        --entrypoint /bats/opt/bats/bin/bats \
        "${GO_IMAGE}" \
        tests/bats db/tests/bats/build-scripts.bats db/tests/bats/build-version.bats db/tests/bats/schema-entrypoint.bats
}

check_e2e_boundary() {
    "${SCRIPT_DIR}/check-e2e-boundary.sh"
}

# Prints a header, then runs the check, so a failure is attributable in the log.
run_check() {
    local title="${1}"
    local check_function="${2}"

    printf "==> %s\n" "${title}"
    if ! "${check_function}"; then
        printf "ERROR: static check failed: %s\n" "${title}" >&2
        return 1
    fi

    return 0
}

main() {
    if ! cd "${PROJ_ROOT}"; then
        return 1
    fi

    if ! run_check "shellcheck" check_shellcheck; then
        return 1
    fi

    if ! run_check "hadolint" check_hadolint; then
        return 1
    fi

    if ! run_check "markdownlint" check_markdownlint; then
        return 1
    fi

    if ! run_check "helm lint" check_helm_lint; then
        return 1
    fi

    if ! run_check "bats" check_bats; then
        return 1
    fi

    if ! run_check "e2e boundary" check_e2e_boundary; then
        return 1
    fi

    return 0
}

main "$@"
