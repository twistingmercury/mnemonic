#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MNEMONIC_PROJ_ROOT="${MNEMONIC_PROJ_ROOT:-$(cd "${SCRIPT_DIR}/.." && pwd)}"

# Printed when a component has no release tag yet.
readonly FALLBACK_VERSION="0.0.0-dev"
readonly KNOWN_COMPONENTS="api mcp enricher db helm"

is_known_component() {
    local component="${1}"

    case " ${KNOWN_COMPONENTS} " in
        *" ${component} "*) return 0 ;;
    esac

    return 1
}

validate_args() {
    if [ -z "${1:-}" ]; then
        printf "ERROR: component is required (one of: %s)\n" "${KNOWN_COMPONENTS}" >&2
        return 1
    fi

    if ! is_known_component "${1}"; then
        printf "ERROR: unknown component '%s' (one of: %s)\n" "${1}" "${KNOWN_COMPONENTS}" >&2
        return 1
    fi

    return 0
}

# Prints the newest tag reachable from HEAD for the component, prefix removed.
component_version() {
    local component="${1}"
    local tag

    if ! tag=$(git -C "${MNEMONIC_PROJ_ROOT}" describe --tags --abbrev=0 --match "${component}/v*" 2>/dev/null); then
        printf "%s" "${FALLBACK_VERSION}"
        return 0
    fi

    printf "%s" "${tag#"${component}"/v}"

    return 0
}

main() {
    if ! validate_args "${@}"; then
        return 1
    fi

    component_version "${1}"

    return 0
}

main "$@"
