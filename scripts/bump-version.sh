#!/bin/bash

set -e

# This script bumps the minor version of seid (e.g. v6.4.0 to v6.5, or v6.5 to v6.6).
# Run from sei-chain/ directory. Usage: `./scripts/bump-version.sh v6.5 v6.4.0-rc.2`

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <new_tag> <old_git_tag>"
  echo "  new_tag:     e.g. v6.5 or v6.6"
  echo "  old_git_tag: git tag to diff against, e.g. v6.4.0-rc.2"
  exit 1
fi

# --- Config ---
NEW_TAG="$1" # e.g. v6.5
OLD_GIT_TAG="$2" # e.g. v6.4.0-rc.2
TAG_FILE="app/tags"
ROOT_DIR="precompiles"
COMMON_DIR="$ROOT_DIR/common"

# --- Read old tag and compute new one ---
if [ ! -f "$TAG_FILE" ]; then
  echo "Tag file not found: $TAG_FILE"
  exit 1
fi

VERSION_RE='^v([0-9]+)\.([0-9]+)(\.([0-9]+))?$'

if [[ ! "$NEW_TAG" =~ $VERSION_RE ]]; then
  echo "Invalid new tag format: $NEW_TAG (expected vX.Y or vX.Y.Z)"
  exit 1
fi

if ! git rev-parse --verify "$OLD_GIT_TAG" > /dev/null 2>&1; then
  echo "Invalid old git tag: '$OLD_GIT_TAG' is not a known git revision"
  exit 1
fi

OLD_TAG=$(grep -v '^[[:space:]]*$' "$TAG_FILE" | tail -n 1)
# dedupe if it's a rerun
if [ "$OLD_TAG" = "$NEW_TAG" ]; then
  grep -v '^[[:space:]]*$' "$TAG_FILE" | sed '$d' > temp.txt
  mv temp.txt "$TAG_FILE"
  OLD_TAG=$(grep -v '^[[:space:]]*$' "$TAG_FILE" | tail -n 1)
fi
if [[ ! "$OLD_TAG" =~ $VERSION_RE ]]; then
  echo "Invalid old tag format: $OLD_TAG"
  exit 1
fi

TAG_FOLDER="${NEW_TAG//./}"  # e.g. v6.2.0 → v620
OLD_TAG_FOLDER="${OLD_TAG//./}"  # e.g. v6.1.0 → v610

echo "Old tag: $OLD_TAG"
echo "New tag: $NEW_TAG"
echo "Version folder: $TAG_FOLDER"

./scripts/generate-all-precompiles-setup.sh "$NEW_TAG"

# --- Check if anything in precompiles/ changed since old tag ---
any_change=false
common_changed=false

if ! git diff --quiet "$OLD_GIT_TAG" -- "$ROOT_DIR"; then
  any_change=true
fi

if ! git diff --quiet "$OLD_GIT_TAG" -- "$COMMON_DIR"; then
  common_changed=true
fi

# --- Copy common if anything changed ---
if [ "$any_change" = true ]; then
  echo "Changes under precompiles/ detected, copying common..."
  TARGET="$COMMON_DIR/legacy/$TAG_FOLDER"
  mkdir -p "$TARGET"

  for SRC in "$COMMON_DIR/precompiles.go" "$COMMON_DIR/evm_events.go"; do
    if [ -f "$SRC" ]; then
      filename=$(basename "$SRC")
      cp "$SRC" "$TARGET/"
      # Replace package line
      sed -i '' "1s|^package .*|package $TAG_FOLDER|" "$TARGET/$filename"
    fi
  done
fi

# --- Process all subfolders ---
find "$ROOT_DIR" -mindepth 1 -maxdepth 1 -type d | while read -r dir; do
  subfolder=$(basename "$dir")
  [ "$subfolder" = "common" ] && continue
  [ "$subfolder" = "utils" ] && continue

  src_json="$dir/abi.json"
  target_dir="$dir/legacy/$TAG_FOLDER"
  version_file="$dir/versions"
  copy=false

  if [ "$common_changed" = true ]; then
    copy=true
  elif ! git diff --quiet "$OLD_GIT_TAG" -- "$dir"; then
    copy=true
  fi

  if [ "$copy" = true ]; then
    echo "Copying $subfolder → legacy/$TAG_FOLDER/"
    mkdir -p "$target_dir"

    # Copy and process all matching .go files
    find "$dir" -maxdepth 1 -type f -name "*.go" ! -name "*_test.go" ! -name "setup.go" | while read -r gofile; do
      filename=$(basename "$gofile")
      cp "$gofile" "$target_dir/$filename"

      # Replace package line
      sed -i '' "1s|^package .*|package $TAG_FOLDER|" "$target_dir/$filename"

      # Rewrite import if common was copied
      if [ "$any_change" = true ]; then
        sed -i '' "s|\"github.com/sei-protocol/sei-chain/precompiles/common\"|\"github.com/sei-protocol/sei-chain/precompiles/common/legacy/$TAG_FOLDER\"|g" "$target_dir/$filename"
      fi
    done

    if [ -f "$src_json" ]; then
      cp "$src_json" "$target_dir/"
    fi

    echo "$NEW_TAG" >> "$version_file"
  fi
done

# --- Append new tag ---
echo "$NEW_TAG" >> "$TAG_FILE"