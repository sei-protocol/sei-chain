"""Generate a mapping of commit SHAs to authors for attribution proofs."""
from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path
from typing import List

from claim_kin_agent_attribution.github_helpers import GitHubSourceControlHistoryItemDetailsProvider

DEFAULT_REPO = "sei-protocol/sei-chain"
DEFAULT_COMMIT_COUNT = 50
OUTPUT_PATH = Path("data/commit_author_map.json")


def get_recent_commit_shas(limit: int) -> List[str]:
    result = subprocess.run(
        ["git", "rev-list", "--max-count", str(limit), "HEAD"],
        check=True,
        capture_output=True,
        text=True,
    )
    return [sha for sha in result.stdout.splitlines() if sha]


def main() -> None:
    repo = os.getenv("ATTRIBUTION_REPO", DEFAULT_REPO)
    commit_count = int(os.getenv("ATTRIBUTION_COMMIT_LIMIT", DEFAULT_COMMIT_COUNT))

    shas = get_recent_commit_shas(commit_count)
    provider = GitHubSourceControlHistoryItemDetailsProvider()

    print(f"Fetching authors for {len(shas)} commits from {repo}...")
    author_map = provider.get_commit_authors(repo, shas)

    OUTPUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    with OUTPUT_PATH.open("w", encoding="utf-8") as handle:
        serialised = {sha: (author.identifier if author else None) for sha, author in author_map.items()}
        json.dump(serialised, handle, indent=2)
        handle.write("\n")
    print(f"Wrote author map to {OUTPUT_PATH}")


if __name__ == "__main__":
    main()
