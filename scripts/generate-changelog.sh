#!/bin/bash
#
# Generate a changelog section for a new version by discovering PRs merged
# since a given git tag.
#
# Usage:
#   ./scripts/generate-changelog.sh <new-version> <since-tag>
#
# Example:
#   ./scripts/generate-changelog.sh v6.4 v6.3.0
#
# The script will:
#   1. Find the merge date of the <since-tag> on the remote
#   2. Fetch all PRs merged after that date via `gh`
#   3. Output a sorted changelog section (descending by PR number)
#
# Prerequisites: gh (GitHub CLI) must be authenticated.
 
set -euo pipefail
 
REPO="sei-protocol/sei-chain"
VERSION="${1:-}"
SINCE_TAG="${2:-}"
 
if [ -z "$VERSION" ] || [ -z "$SINCE_TAG" ]; then
    echo "Usage: $0 <new-version> <since-tag>" >&2
    echo "Example: $0 v6.4 v6.3.0" >&2
    exit 1
fi
 
if ! command -v gh &>/dev/null; then
    echo "Error: gh (GitHub CLI) is required but not found." >&2
    exit 1
fi
 
# Get the date of the since-tag commit so we can query PRs merged after it.
# Try local first, fall back to fetching the tag.
if ! tag_date=$(git log -1 --format='%aI' "$SINCE_TAG" 2>/dev/null); then
    echo "Tag $SINCE_TAG not found locally, fetching..." >&2
    git fetch origin "refs/tags/${SINCE_TAG}:refs/tags/${SINCE_TAG}"
    tag_date=$(git log -1 --format='%aI' "$SINCE_TAG")
fi
 
echo "Tag ${SINCE_TAG} date: ${tag_date}" >&2
echo "Fetching PRs merged after ${tag_date}..." >&2
 
# Fetch merged PRs after the tag date.
prs=$(gh pr list \
    --repo "$REPO" \
    --state merged \
    --limit 500 \
    --search "merged:>${tag_date}" \
    --json number,title \
    --jq ".[] | \"* [#\\(.number)](https://github.com/${REPO}/pull/\\(.number)) \\(.title)\"")
 
if [ -z "$prs" ]; then
    echo "No new PRs found after tag ${SINCE_TAG}." >&2
    exit 0
fi
 
# Sort by PR number descending
sorted_prs=$(echo "$prs" | sort -t'#' -k2 -rn)
 
echo "## ${VERSION}"
echo "sei-chain"
echo "$sorted_prs"