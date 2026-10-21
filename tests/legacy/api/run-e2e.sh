#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
COMPOSE_FILE="${PROJECT_ROOT}/docker-compose.yaml"
TARGET="${TARGET:-ALL}"

if [ ! -f "${COMPOSE_FILE}" ]; then
    printf 'ERROR: docker-compose-dev.yaml not found at %s\n' "${COMPOSE_FILE}" >&2
    exit 1
fi
API_URL="http://localhost:13000"
METRICS_URL="http://localhost:19091"
MAX_RETRIES=30
TARGET="${TARGET:-ALL}"

cleanup() {
    echo ""
    echo "Tearing down infrastructure..."
    docker compose -f "${COMPOSE_FILE}" down -v --remove-orphans 2>&1
    docker system prune -f
}

wait_for_api() {
    echo "Waiting for API to be ready..."
    for i in $(seq 1 "${MAX_RETRIES}"); do
        if curl -sf "${API_URL}/health" > /dev/null 2>&1; then
            echo "API ready (attempt ${i}/${MAX_RETRIES})"
            return 0
        fi
        if [ "$i" -eq "${MAX_RETRIES}" ]; then
            echo "ERROR: API failed to become ready after ${MAX_RETRIES} attempts"
            docker logs librarian_api 2>&1 | tail -20
            return 1
        fi
        sleep 2
    done
}

trap cleanup EXIT

echo "Starting dev infrastructure..."
docker compose -f "${COMPOSE_FILE}" up -d
wait_for_api

echo ""

cd "${SCRIPT_DIR}/e2e"
export API_URL="${API_URL}" 
export METRICS_URL="${METRICS_URL}"

go test -v ./...

echo ""
