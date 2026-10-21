#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MNEMONIC_PROJ_ROOT="${MNEMONIC_PROJ_ROOT:-$(cd "${SCRIPT_DIR}/.." && pwd)}"

# The e2e suite must drive the images from outside; any path into the service
# source or the Go module couples it to code it is meant to treat as a black box.
readonly FORBIDDEN_PATTERN='\.\./src|src/internal|github\.com/twistingmercury/mnemonic'
readonly SUITE_RELATIVE_DIR="tests/integration"
# The Python twin of this check spells the forbidden patterns out in a regex, so it cannot pass its own rule.
readonly TWIN_GUARD_FILE="test_blackbox.py"

validate_args() {
    if [ ! -d "${MNEMONIC_PROJ_ROOT}/${SUITE_RELATIVE_DIR}" ]; then
        printf "ERROR: suite directory not found: %s\n" "${MNEMONIC_PROJ_ROOT}/${SUITE_RELATIVE_DIR}" >&2
        return 1
    fi

    return 0
}

# Prints file:line:text for every forbidden reference; exit status of grep is ignored here.
find_violations() {
    local matches

    matches=$(cd "${MNEMONIC_PROJ_ROOT}" && grep -rInE \
        --exclude-dir=.venv --exclude-dir=__pycache__ --exclude-dir=.pytest_cache \
        --exclude-dir=.mypy_cache --exclude-dir=.ruff_cache --exclude-dir=.git \
        --exclude="${TWIN_GUARD_FILE}" \
        "${FORBIDDEN_PATTERN}" "${SUITE_RELATIVE_DIR}" || true)

    printf "%s" "${matches}"

    return 0
}

main() {
    local violations

    if ! validate_args; then
        return 1
    fi

    violations=$(find_violations)
    if [ -n "${violations}" ]; then
        printf "ERROR: e2e boundary violated (suite must not reference service source):\n%s\n" "${violations}" >&2
        return 1
    fi

    return 0
}

main "$@"
