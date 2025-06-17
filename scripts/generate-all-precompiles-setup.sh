#!/bin/bash

set -euo pipefail

BASE_PATH="./precompiles"
GEN_SCRIPT="../../scripts/generate-precompile-setup.sh"

EXCLUDE=("common" "utils")

# Check prerequisites
[[ ! -d "$BASE_PATH" ]] && { echo "Error: base path $BASE_PATH does not exist."; exit 1; }

echo "Scanning $BASE_PATH for modules..."

# Iterate through subdirectories
for module_dir in "$BASE_PATH"/*/; do
  MODULE_NAME=$(basename "$module_dir")

  # Skip excluded folders
  if [[ " ${EXCLUDE[*]} " == *" $MODULE_NAME "* ]]; then
    echo "Skipping excluded module: $MODULE_NAME"
    continue
  fi

  echo "Generating setup.go for module: $MODULE_NAME"
  (cd "$module_dir" && "$GEN_SCRIPT" "$MODULE_NAME")
done

echo "✅ All eligible modules processed."