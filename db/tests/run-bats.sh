#!/usr/bin/env bash
#
# Runs BATS files from pinned images, so a clean machine needs only Docker.
# The tests need bats, psql (the migration tests) and the docker CLI (the
# image tests), so bats and the docker CLI are mounted from their images into
# the pgvector image, which has bash and psql. The host network and the Docker
# socket let the tests reach the containers the runners started.
#
# Usage: run-bats.sh <file.bats>...

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

BATS_IMAGE="bats/bats:1.14.0@sha256:5322b877351fda0cc435de8c6116de7d0a2ec79d7c680132a0ef329a633bc66f"
DOCKER_CLI_IMAGE="docker:29-cli@sha256:5e6baf67325a79f64ed3bed6863954d5e05d0f3d4e57cbdb14809346814bbfdb"
RUN_IMAGE="pgvector/pgvector:pg16@sha256:7b822b0aac60967beb1ea5e576b8602c94c300a157d187f385ae3e0da199b90a"

# --mount does not pull its source image, so pull them first.
for image in "${BATS_IMAGE}" "${DOCKER_CLI_IMAGE}" "${RUN_IMAGE}"; do
    docker pull --quiet "${image}" >/dev/null
done

# The repository is mounted at its host path: the tests hand paths to docker.
# A variable named without a value is passed only when it is set.
docker run --rm --network host \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "${REPO_ROOT}:${REPO_ROOT}:ro" -w "${REPO_ROOT}" \
    --mount "type=image,source=${BATS_IMAGE},target=/bats,readonly" \
    --mount "type=image,source=${DOCKER_CLI_IMAGE},target=/docker-cli,readonly" \
    -e PATH=/docker-cli/usr/local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    -e MNEMONIC_NEO4J_CONTAINER -e MNEMONIC_NEO4J_IMAGE \
    -e MNEMONIC_NEO4J_USER -e MNEMONIC_NEO4J_PASSWORD \
    -e MNEMONIC_READY_TIMEOUT -e MNEMONIC_STOP_TIMEOUT \
    -e PGHOST -e PGPORT -e PGUSER -e PGDATABASE -e PGPASSWORD \
    --entrypoint /bats/opt/bats/bin/bats \
    "${RUN_IMAGE}" "$@"
