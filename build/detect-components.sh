#!/usr/bin/env bash
#
# Prints the two CI output lines for the components a run builds:
#   components=<json array of every affected component>
#   services=<json array of the affected api, mcp and enricher>
# Usage: detect-components.sh <base-sha> <head-sha> <component-input>
# A component input that is neither empty nor "all" is the one component built;
# otherwise changed-components.sh decides. The workflow appends stdout to
# $GITHUB_OUTPUT.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

validate_args() {
    if [ "${#}" -ne 3 ]; then
        printf "Usage: detect-components.sh <base-sha> <head-sha> <component-input>\n" >&2
        return 1
    fi

    return 0
}

is_override() {
    local component_input="${1}"

    [ -n "${component_input}" ] && [ "${component_input}" != "all" ]
}

# Prints the component names, one per line.
component_names() {
    local base_sha="${1}"
    local head_sha="${2}"
    local component_input="${3}"

    if is_override "${component_input}"; then
        printf "%s\n" "${component_input}"
        return 0
    fi

    "${SCRIPT_DIR}/changed-components.sh" "${base_sha}" "${head_sha}"
}

main() {
    local names
    local components
    local services

    if ! validate_args "$@"; then
        return 1
    fi

    if ! names=$(component_names "${1}" "${2}" "${3}"); then
        return 1
    fi

    components=$(jq -R -s -c 'split("\n") | map(select(. != ""))' <<<"${names}")
    services=$(jq -c 'map(select(. == "api" or . == "mcp" or . == "enricher"))' <<<"${components}")

    printf 'components=%s\n' "${components}"
    printf 'services=%s\n' "${services}"

    return 0
}

main "$@"
