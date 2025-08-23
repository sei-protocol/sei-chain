#!/usr/bin/env bash
set -euo pipefail

if [ -d "./x/slinky" ]; then
  go test ./x/slinky/... -race -covermode=atomic -coverprofile=coverage.out
else
  echo "No Slinky module found. Skipping tests."
fi
