#!/usr/bin/env bash
set -euo pipefail

ENRICHER_URL="${ENRICHER_URL:-http://e2e_enricher:8080}"
MAX_RETRIES="${MAX_RETRIES:-30}"
RETRY_INTERVAL="${RETRY_INTERVAL:-1}"

echo "=== E2E Test Runner ==="
echo "Enricher URL: ${ENRICHER_URL}"
echo ""

echo "Waiting for Enricher to be ready..."
for i in $(seq 1 "${MAX_RETRIES}"); do
    if curl -sf "${ENRICHER_URL}/health" > /dev/null 2>&1; then
        echo "Enricher is ready (attempt ${i}/${MAX_RETRIES})"
        break
    fi
    if [ "$i" -eq "${MAX_RETRIES}" ]; then
        echo "ERROR: Enricher failed to become ready after ${MAX_RETRIES} attempts"
        exit 1
    fi
    echo "Waiting for Enricher... (${i}/${MAX_RETRIES})"
    sleep "${RETRY_INTERVAL}"
done

echo ""
echo "Running E2E tests..."
echo ""

cd /e2e
stdbuf -oL go test -v -count=1 ./...

echo ""
echo "=== E2E Tests Complete ==="