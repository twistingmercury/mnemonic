#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

COMPONENT="enricher"
MNEMONIC_IMAGE_NAME="${MNEMONIC_IMAGE_NAME:-ghcr.io/twistingmercury/mnemonic-enricher}"
MNEMONIC_BUILD_VER="${MNEMONIC_BUILD_VER:-$("${PROJ_ROOT}/build/version.sh" "${COMPONENT}")}"
MNEMONIC_BUILD_DATE="${MNEMONIC_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
MNEMONIC_BUILD_COMMIT="${MNEMONIC_BUILD_COMMIT:-$(git -C "${PROJ_ROOT}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"
MNEMONIC_IMAGE_TAG="${MNEMONIC_IMAGE_TAG:-v${MNEMONIC_BUILD_VER}}"

# The e2e suite is still parked under tests/legacy until it is re-homed.
MNEMONIC_E2E_COMPOSE_FILE="${MNEMONIC_E2E_COMPOSE_FILE:-${PROJ_ROOT}/tests/legacy/enricher/docker-compose.yaml}"
E2E_INFRA_SERVICES=(e2e_postgres e2e_neo4j e2e_rabbitmq e2e_openai_stub)
E2E_UP_SERVICES=(e2e_enricher e2e_tests)
E2E_EXIT_CODE_FROM="e2e_tests"
E2E_PULL_SERVICES=(e2e_postgres e2e_neo4j e2e_rabbitmq)

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
