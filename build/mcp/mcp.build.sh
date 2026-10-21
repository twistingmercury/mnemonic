#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

COMPONENT="mcp"
MNEMONIC_IMAGE_NAME="${MNEMONIC_IMAGE_NAME:-ghcr.io/twistingmercury/mnemonic-mcp}"
MNEMONIC_BUILD_VER="${MNEMONIC_BUILD_VER:-$("${PROJ_ROOT}/build/version.sh" "${COMPONENT}")}"
MNEMONIC_BUILD_DATE="${MNEMONIC_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
MNEMONIC_BUILD_COMMIT="${MNEMONIC_BUILD_COMMIT:-$(git -C "${PROJ_ROOT}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"
MNEMONIC_IMAGE_TAG="${MNEMONIC_IMAGE_TAG:-v${MNEMONIC_BUILD_VER}}"

MNEMONIC_E2E_COMPOSE_FILE="${MNEMONIC_E2E_COMPOSE_FILE:-${PROJ_ROOT}/tests/integration/docker-compose.yaml}"

source "${PROJ_ROOT}/build/build-lib.sh"

main() {
    if ! build_lib::build_image; then
        return 1
    fi

    if ! build_lib::e2e_tests; then
        return 1
    fi

    return 0
}

main "$@"
