#!/bin/bash
#
# Generate a changelog between two release/* branches.
#
# Usage:
#   ./scripts/generate-changelog.sh <base-branch> <head-branch>
#
# Example:
#   ./scripts/generate-changelog.sh release/v6.3 release/v6.4
#
# The script extracts PR numbers and titles directly from squash-merge
# commit subjects (which follow the pattern "Title (#NNN)"), so it
# requires no GitHub API calls and runs in under a second.

set -euo pipefail

REPO="sei-protocol/sei-chain"
BASE_BRANCH="${1:-}"
HEAD_BRANCH="${2:-}"

if [ -z "$BASE_BRANCH" ] || [ -z "$HEAD_BRANCH" ]; then
    echo "Usage: $0 <base-branch> <head-branch>" >&2
    echo "Example: $0 release/v6.3 release/v6.4" >&2
    exit 1
fi

# Ensure both branches are available locally.
for branch in "$BASE_BRANCH" "$HEAD_BRANCH"; do
    if ! git rev-parse --verify "$branch" &>/dev/null; then
        echo "Branch $branch not found locally, fetching..." >&2
        git fetch origin "$branch"
    fi
done

# Parse squash-merge subjects: "Some title (#1234)"
# Extract the PR number and everything before it as the title.
changelog=$(git log --format='%s' "origin/${BASE_BRANCH}..origin/${HEAD_BRANCH}" \
    | sed -n 's/\(.*\) (#\([0-9]*\))$/\2 \1/p' \
    | sort -t' ' -k1 -rn \
    | awk -v repo="$REPO" '!seen[$1]++ { printf "* [#%s](https://github.com/%s/pull/%s) %s\n", $1, repo, $1, substr($0, index($0," ")+1) }')

if [ -z "$changelog" ]; then
    echo "No PRs found between ${BASE_BRANCH} and ${HEAD_BRANCH}." >&2
    exit 0
fi

total=$(echo "$changelog" | wc -l | tr -d ' ')
echo "Found ${total} PRs." >&2

head_label="${HEAD_BRANCH#release/}"
echo "## ${head_label}"
echo "sei-chain"
echo "$changelog"