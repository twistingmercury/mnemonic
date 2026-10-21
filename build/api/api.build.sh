#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

COMPONENT="api"
IMAGE_NAME="${IMAGE_NAME:-ghcr.io/twistingmercury/librarian-api}"
BUILD_VER="${BUILD_VER:-$("${PROJ_ROOT}/build/version.sh" "${COMPONENT}")}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
BUILD_COMMIT="${BUILD_COMMIT:-$(git -C "${PROJ_ROOT}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"
IMAGE_TAG="${IMAGE_TAG:-v${BUILD_VER}}"

# The e2e suite is still parked under tests/legacy until it is re-homed.
E2E_COMPOSE_FILE="${E2E_COMPOSE_FILE:-${PROJ_ROOT}/tests/legacy/api/docker-compose.yaml}"
E2E_INFRA_SERVICES=(postgres neo4j rabbitmq)
E2E_UP_SERVICES=(librarian_api librarian_tests)
E2E_EXIT_CODE_FROM="librarian_tests"
E2E_PULL_SERVICES=()

source "${PROJ_ROOT}/build/build-lib.sh"

main() {
    if ! build_lib::build_image; then
        return 1
    fi

    if ! build_lib::e2e_tests; then
        return 1
    fi

    if ! build_lib::push; then
        return 1
    fi

    return 0
}

main "$@"
