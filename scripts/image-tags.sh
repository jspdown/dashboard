#!/usr/bin/env bash
# Derive the list of image tags for a git ref, one per line:
#
#   refs/tags/v1.2.3      -> 1.2.3, 1.2, sha-<short>, latest
#   refs/tags/v1.2.3-rc1  -> 1.2.3-rc1, sha-<short>        (pre-release)
#   refs/heads/main (etc) -> sha-<short>                   (non-tag ref)
#
# Usage: image-tags.sh <git-ref> <short-sha>
set -euo pipefail

ref="${1:?usage: image-tags.sh <git-ref> <short-sha>}"
short="${2:?usage: image-tags.sh <git-ref> <short-sha>}"

tags=()
case "$ref" in
    refs/tags/v*)
        version="${ref#refs/tags/v}"
        tags+=("$version")
        case "$version" in
            *-*)
                # Pre-release (e.g. 1.2.3-rc1): only the exact version, no
                # moving major.minor alias and no `latest`.
                ;;
            *)
                major="${version%%.*}"
                rest="${version#*.}"
                minor="${rest%%.*}"
                tags+=("$major.$minor")
                ;;
        esac
        ;;
esac

tags+=("sha-$short")

case "$ref" in
    refs/tags/v*)
        case "${ref#refs/tags/v}" in
            *-*) ;; # pre-release: no latest
            *) tags+=("latest") ;;
        esac
        ;;
esac

printf '%s\n' "${tags[@]}"
