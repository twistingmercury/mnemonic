#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LOCAL=${LOCAL:-0}

BUILD_VER="${BUILD_VER:-$(git -C "${PROJ_ROOT}" describe --tags --abbrev=0 2>/dev/null || echo 'dev')}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
BUILD_COMMIT="${BUILD_COMMIT:-$(git -C "${PROJ_ROOT}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"

IMAGE_NAME="${IMAGE_NAME:-ghcr.io/twistingmercury/librarian-mcp}"
IMAGE_TAG="${IMAGE_TAG:-$BUILD_VER}"

E2E_COMPOSE_FILE="${PROJ_ROOT}/tests/docker-compose.yaml"

build_api(){
    printf "\n=== starting image build, version %s ===\n" "${BUILD_VER}"

    docker build --rm --no-cache \
        --file "${SCRIPT_DIR}/Dockerfile" \
        --build-arg BUILD_VER="${BUILD_VER}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg BUILD_COMMIT="${BUILD_COMMIT}" \
        --target final \
        --tag "${IMAGE_NAME}:${IMAGE_TAG}" \
        --tag "${IMAGE_NAME}:latest" \
        "$(cd "${PROJ_ROOT}" && pwd)"

    printf "\nImage: %s:%s\n" "${IMAGE_NAME}" "${IMAGE_TAG}"
    docker images "${IMAGE_NAME}:${IMAGE_TAG}" --format "Size: {{.Size}}"

    return 0
}

e2e_tests(){
    printf "\n=== starting end-to-end tests ===\n"

    cleanup() {
        docker compose -f "${E2E_COMPOSE_FILE}" down -v --remove-orphans > /dev/null 2>&1 || true

        if [ ${LOCAL} = 1 ]; then
            docker rmi tests_librarian_tests:latest -f > /dev/null 2>&1 || true
            docker system prune -f > /dev/null 2>&1 || true
        fi
    }
    trap cleanup EXIT

    printf "Starting infrastructure services...\n"
    if ! docker compose -f "${E2E_COMPOSE_FILE}" up -d postgres neo4j rabbitmq; then
        printf "ERROR: Failed to start infrastructure services\n" >&2
        return 1
    fi

    docker compose -f "${E2E_COMPOSE_FILE}" up \
        --build \
        --abort-on-container-exit \
        --exit-code-from librarian_tests \
        librarian_mcp librarian_api librarian_tests

    trap - EXIT
    cleanup

    return 0
}

main(){
    build_api
    e2e_tests

    return 0
}

main "$@"
