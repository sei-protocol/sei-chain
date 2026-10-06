#!/usr/bin/env bash
# Prints the GOTOOLCHAIN name declared by the root go.mod, e.g. "go1.27.1".
# A patch-less `go 1.28` directive is a valid go.mod version but not a valid
# toolchain name, so it is normalized to "go1.28.0".
set -euo pipefail

version="$(awk '$1 == "go" { print $2; exit }' "$(dirname "$0")/../go.mod")"
[[ -n "$version" ]] || { echo "go.mod does not declare a Go version" >&2; exit 1; }
[[ "$version" =~ ^[0-9]+\.[0-9]+$ ]] && version="$version.0"
printf 'go%s\n' "$version"
