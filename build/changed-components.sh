#!/usr/bin/env bash
#
# Prints the components a change affects, one per line, sorted.
# Usage: changed-components.sh <base-sha> <head-sha>
# A base of 40 zeros (the first push of a branch) prints every component.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Newline-separated results, filled by the mark_* functions.
AFFECTED=""
# Newline-separated changed file paths and their directories.
CHANGED_PATHS=""
CHANGED_DIRS=""

mark() {
    local component="${1}"

    AFFECTED="${AFFECTED}${component}"$'\n'

    return 0
}

changed_paths() {
    local base_sha="${1}"
    local head_sha="${2}"

    # No trailing return: the pipeline status must reach the caller, because
    # errexit is off inside a command substitution.
    git -C "${PROJ_ROOT}" diff --name-only --no-renames -z "${base_sha}" "${head_sha}" | tr '\0' '\n'
}

changed_dirs() {
    local path

    while IFS= read -r path; do
        dirname "${path}"
    done <<< "${CHANGED_PATHS}"

    return 0
}

is_affected() {
    local component="${1}"

    printf "%s" "${AFFECTED}" | grep -Fxq "${component}"
}

mark_service_path() {
    local path="${1}"
    local service

    for service in api enricher mcp; do
        case "${path}" in
            "src/cmd/${service}/"* | "src/internal/${service}/"* | "build/${service}/"*)
                mark "${service}"
                return 0
                ;;
        esac
    done

    case "${path}" in
        src/go.mod | src/go.sum | build/* | tests/integration/* | tests/openai-stub/* | .github/*)
            mark api
            mark enricher
            mark mcp
            ;;
    esac

    return 0
}

mark_passthrough_path() {
    local path="${1}"

    case "${path}" in
        db/*) mark db ;;
        gateway/*) mark gateway ;;
        deploy/helm/*) mark helm ;;
    esac

    return 0
}

# Lists the repo directories of a service's non-standard-library packages. The
# repo is mounted at /repo, so only paths under it belong to the repo; module
# cache paths are dropped. -buildvcs=false because the mounted checkout is owned
# by another user, which makes git inside the container refuse it. No trailing
# return: the pipeline status (docker failing) must reach the caller.
service_package_dirs() {
    local service="${1}"

    docker run --rm -v "${PROJ_ROOT}:/repo:ro" ghcr.io/twistingmercury/golang-tooling:go1.27.3 \
        go -C /repo/src list -buildvcs=false -deps -f '{{if not .Standard}}{{.Dir}}{{end}}' "./cmd/${service}" \
        | awk 'index($0, "/repo/") == 1 { print substr($0, 7) }'
}

# A service is also affected when a package it imports lives in a changed directory.
mark_shared_package_service() {
    local service="${1}"
    local package_dirs
    local package_dir

    if is_affected "${service}"; then
        return 0
    fi

    # Captured first: a process substitution would swallow a docker failure.
    if ! package_dirs=$(service_package_dirs "${service}"); then
        printf "ERROR: could not list the packages of %s\n" "${service}" >&2
        return 1
    fi

    while IFS= read -r package_dir; do
        if printf "%s\n" "${CHANGED_DIRS}" | grep -Fxq "${package_dir}"; then
            mark "${service}"
            return 0
        fi
    done <<< "${package_dirs}"

    return 0
}

has_source_change() {
    printf "%s\n" "${CHANGED_DIRS}" | grep -Eq '^src(/|$)'
}

mark_shared_packages() {
    local service

    if ! has_source_change; then
        return 0
    fi

    for service in api enricher mcp; do
        if ! mark_shared_package_service "${service}"; then
            return 1
        fi
    done

    return 0
}

mark_everything() {
    mark api
    mark db
    mark enricher
    mark gateway
    mark helm
    mark mcp

    return 0
}

main() {
    local base_sha="${1}"
    local head_sha="${2}"
    local path

    if [ "${base_sha}" = "0000000000000000000000000000000000000000" ]; then
        mark_everything
        printf "%s" "${AFFECTED}" | sort -u

        return 0
    fi

    if ! CHANGED_PATHS=$(changed_paths "${base_sha}" "${head_sha}"); then
        return 1
    fi

    CHANGED_DIRS=$(changed_dirs)

    while IFS= read -r path; do
        mark_service_path "${path}"
        mark_passthrough_path "${path}"
    done <<< "${CHANGED_PATHS}"

    if ! mark_shared_packages; then
        return 1
    fi

    printf "%s" "${AFFECTED}" | sort -u

    return 0
}

main "$@"
