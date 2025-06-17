#!/bin/bash

set -e

# This script bumps the minor version of seid (e.g. v6.0.0 to v6.1.0).
# Run from sei-chain/ directory. Usage: `./scripts/bump-version.sh v6.2.0 v6.1.0-rc.2`

# --- Config ---
NEW_TAG="$1" # e.g. v6.2.0
OLD_GIT_TAG="$2" # e.g. v6.1.0-rc.2
TAG_FILE="app/tags"
ROOT_DIR="precompiles"
COMMON_DIR="$ROOT_DIR/common"

# --- Read old tag and compute new one ---
if [ ! -f "$TAG_FILE" ]; then
  echo "Tag file not found: $TAG_FILE"
  exit 1
fi

OLD_TAG=$(tail -n 1 "$TAG_FILE")
# dedupe if it's a rerun
if [ "$OLD_TAG" = "$NEW_TAG" ]; then
  lines=$(wc -l < "$TAG_FILE")
  head -n $((lines - 1)) "$TAG_FILE" > temp.txt
  echo >> temp.txt                # Add final newline
  mv temp.txt "$TAG_FILE"
fi
OLD_TAG=$(tail -n 1 "$TAG_FILE")
if [[ ! "$OLD_TAG" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "Invalid tag format: $OLD_TAG"
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

  SRC="$COMMON_DIR/precompiles.go"
  if [ -f "$SRC" ]; then
    cp "$SRC" "$TARGET/"
    # Replace package line
    sed -i '' "1s|^package .*|package $TAG_FOLDER|" "$TARGET/precompiles.go"
  fi
fi

# --- Process all subfolders ---
find "$ROOT_DIR" -mindepth 1 -maxdepth 1 -type d | while read -r dir; do
  subfolder=$(basename "$dir")
  [ "$subfolder" = "common" ] && continue
  [ "$subfolder" = "utils" ] && continue

  src_go="$dir/$subfolder.go"
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

    if [ -f "$src_go" ]; then
      cp "$src_go" "$target_dir/"
      # Replace package line
      sed -i '' "1s|^package .*|package $TAG_FOLDER|" "$target_dir/$subfolder.go"

      # Rewrite import if common was copied
      if [ "$any_change" = true ]; then
        sed -i '' "s|\"github.com/sei-protocol/sei-chain/precompiles/common\"|\"github.com/sei-protocol/sei-chain/precompiles/common/legacy/$TAG_FOLDER\"|g" "$target_dir/$subfolder.go"
      fi
    fi

    if [ -f "$src_json" ]; then
      cp "$src_json" "$target_dir/"
    fi

    echo "$NEW_TAG" >> "$version_file"
  fi
done

# --- Append new tag ---
echo "$NEW_TAG" >> "$TAG_FILE"