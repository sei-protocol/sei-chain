#!/bin/sh
#
# Invoke Mockery v2 to update generated mocks for the given type.
#

set -e

GOTOOLCHAIN="$("$(dirname "$0")/../../scripts/go-toolchain.sh")" go run github.com/vektra/mockery/v2@v2.53.7 --disable-version-string --case underscore --name "$@"
