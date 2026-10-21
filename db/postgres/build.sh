#!/usr/bin/env bash
# build.sh — Build the mnemonic-postgres pre-seeded Docker image.
#
# Usage: bash src/postgres/build.sh
#
# The image is tagged with both the current git version and 'latest'.
# Set the VERSION environment variable to override the git-derived version.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
log_info() {
    printf 'INFO: %s\n' "$*"
}

log_success() {
    printf 'SUCCESS: %s\n' "$*"
}

log_error() {
    printf 'ERROR: %s\n' "$*" >&2
}

# ---------------------------------------------------------------------------
# Version
# ---------------------------------------------------------------------------
VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || echo 'dev')}"

IMAGE_BASE="ghcr.io/twistingmercury/librarian-postgres"
IMAGE_VERSIONED="${IMAGE_BASE}:${VERSION}"
IMAGE_LATEST="${IMAGE_BASE}:latest"

log_info "Building PostgreSQL pre-seeded image"
log_info "Version : ${VERSION}"
log_info "Image   : ${IMAGE_VERSIONED}"
log_info "Context : ${SCRIPT_DIR}"

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
docker build \
    --progress=plain \
    -t "${IMAGE_VERSIONED}" \
    -t "${IMAGE_LATEST}" \
    "${SCRIPT_DIR}"

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------
IMAGE_SIZE="$(docker image inspect "${IMAGE_VERSIONED}" --format '{{.Size}}' | awk '{printf "%.1f MB", $1/1024/1024}')"
log_success "Image built: ${IMAGE_VERSIONED}"
log_success "Image size : ${IMAGE_SIZE}"
