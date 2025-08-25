#!/usr/bin/env bash
set -euo pipefail

if [ -d "./x/slinky" ]; then
  echo "🧪 Running Slinky tests with race detection and coverage..."
  go test ./x/slinky/... -race -covermode=atomic -coverprofile=coverage.out
  echo "✅ Tests completed. Coverage written to coverage.out."
else
  echo "⚠️ No Slinky module found. Skipping tests."
fi
