#!/usr/bin/env bash
#
# Prints the registry tags to publish for a component, one per line.
# Usage: publish-tags.sh <branch> <component> <sha>
# develop: latest-dev and sha-<first 7 chars of sha>, never a version tag.
# main: latest and v<version> from build/version.sh, the same v-prefixed tag the
# build scripts give the image. postgres and neo4j share the db version.
# Any other branch fails.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

develop_tags() {
    local sha="${1}"

    printf "latest-dev\nsha-%s\n" "${sha:0:7}"

    return 0
}

# The db images are released together, so they carry the db component's version.
version_component() {
    local component="${1}"

    case "${component}" in
        postgres | neo4j) printf "db" ;;
        *) printf "%s" "${component}" ;;
    esac

    return 0
}

main_tags() {
    local component="${1}"
    local version

    if ! version=$("${SCRIPT_DIR}/version.sh" "$(version_component "${component}")"); then
        return 1
    fi

    printf "latest\nv%s\n" "${version}"

    return 0
}

main() {
    local branch="${1}"
    local component="${2}"
    local sha="${3}"

    case "${branch}" in
        develop)
            develop_tags "${sha}"
            ;;
        main)
            main_tags "${component}"
            ;;
        *)
            printf "ERROR: branch '%s' does not publish (only develop and main)\n" "${branch}" >&2
            return 1
            ;;
    esac

    return 0
}

main "$@"
