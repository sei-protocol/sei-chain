#!/usr/bin/env bash
#
# generate-changelog-6.4.sh
#
# Generates a v6.4 changelog section for sei-chain in the same style
# as the existing CHANGELOG.md (e.g., v6.3).
#
# Usage:
#   ./generate-changelog-6.4.sh               # prints to stdout
#   ./generate-changelog-6.4.sh >> CHANGELOG   # append to a file
#
# Requirements: git, awk, sed

set -euo pipefail

REPO="sei-protocol/sei-chain"
BASE_URL="https://github.com/${REPO}/pull"

# The highest PR number already listed under v6.3 in CHANGELOG.md.
V63_MAX_PR=2580

# The branch/ref that contains all v6.4 work.
TARGET_REF="${1:-origin/main}"

# Extract PR-numbered entries from git log, newest first.
# Each squash-merge commit message ends with "(#NNNN)".
git log --oneline --first-parent "$TARGET_REF" \
  | awk '/\(#[0-9]+\)$/' \
  | while IFS= read -r line; do
      # Extract PR number from trailing (#NNNN)
      pr_num=$(echo "$line" | sed 's/.*(#\([0-9]*\))$/\1/')

      # Strip leading hash and trailing (#NNNN) to get the message
      msg=$(echo "$line" | sed 's/^[0-9a-f]* //' | sed "s/ (#${pr_num})\$//")

      # Only include PRs newer than v6.3
      if [ "$pr_num" -gt "$V63_MAX_PR" ]; then
        echo "* [#${pr_num}](${BASE_URL}/${pr_num}) ${msg}"
      fi
    done