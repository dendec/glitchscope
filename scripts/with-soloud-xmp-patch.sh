#!/usr/bin/env bash
# Temporarily apply the SoLoud XMP extension while a command uses the source tree.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PATCH="$ROOT/patches/soloud-xmp.patch"
soloud_patch_applied=0

if [[ $# -eq 0 ]]; then
	echo "usage: $0 command [args...]" >&2
	exit 2
fi

if git -C "$ROOT" apply --check "$PATCH"; then
	git -C "$ROOT" apply "$PATCH"
	soloud_patch_applied=1
elif ! git -C "$ROOT" apply --reverse --check "$PATCH"; then
	echo "patches/soloud-xmp.patch does not apply" >&2
	exit 1
fi

cleanup() {
	if [[ "$soloud_patch_applied" -eq 1 ]]; then
		git -C "$ROOT" apply --reverse "$PATCH"
	fi
}
trap cleanup EXIT

"$@"
