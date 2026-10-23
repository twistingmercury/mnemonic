#!/usr/bin/env bash
# build.sh — Build the mnemonic-neo4j Docker image.
#
# The image applies its Cypher schema itself on every container start (see
# schema-entrypoint.sh), so the build is a plain `docker build` plus tagging.
# Seeding a temporary container and committing it does not work: /data is a
# VOLUME in the neo4j base image and `docker commit` drops volume contents.
#
# Usage: bash db/neo4j/build.sh
#
# The image is tagged with both the db component version (from build/version.sh)
# and 'latest'. Set the MNEMONIC_VERSION environment variable to override that
# version.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
log_info() {
    printf 'INFO: %s\n' "$*"
}

log_success() {
    printf 'SUCCESS: %s\n' "$*"
}

# ---------------------------------------------------------------------------
# Version and image names
# ---------------------------------------------------------------------------
MNEMONIC_VERSION="${MNEMONIC_VERSION:-v$("${PROJ_ROOT}/build/version.sh" db)}"

IMAGE_BASE="ghcr.io/twistingmercury/mnemonic-neo4j"
IMAGE_VERSIONED="${IMAGE_BASE}:${MNEMONIC_VERSION}"
IMAGE_LATEST="${IMAGE_BASE}:latest"

# ---------------------------------------------------------------------------
# Build and tag
# ---------------------------------------------------------------------------
log_info "Building Neo4j image"
log_info "Version : ${MNEMONIC_VERSION}"
log_info "Image   : ${IMAGE_VERSIONED}"

docker build \
    --no-cache --pull \
    --progress=plain \
    -t "${IMAGE_VERSIONED}" \
    "${SCRIPT_DIR}"

docker tag "${IMAGE_VERSIONED}" "${IMAGE_LATEST}"

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------
IMAGE_SIZE="$(docker image inspect "${IMAGE_VERSIONED}" --format '{{.Size}}' | awk '{printf "%.1f MB", $1/1024/1024}')"
log_success "Image built: ${IMAGE_VERSIONED}"
log_success "Image size : ${IMAGE_SIZE}"
