#!/usr/bin/env python3
"""Emit the GitHub Actions matrix for independent EEST chain partitions."""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

SUITE_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SUITE_ROOT / "plugin"))

from eest_families import ISOLATED_FAMILIES  # noqa: E402


def build_matrix(shard_count: int) -> list[dict[str, object]]:
    entries: list[dict[str, object]] = []

    for index in range(shard_count):
        entries.append(
            {
                "name": f"shard {index}",
                "family": "",
                "shard": index,
                "shard_count": shard_count,
                "artifact": f"shard-{index}",
                "report": f"reports/junit-shard-{index}.xml",
                "test_paths": "",
            }
        )

    for name in sorted(ISOLATED_FAMILIES):
        family = ISOLATED_FAMILIES[name]
        for index in range(family.chains):
            entries.append(
                {
                    "name": f"{name} {index}",
                    "family": name,
                    "shard": index,
                    "shard_count": family.chains,
                    "artifact": f"{name}-{index}",
                    "report": f"reports/junit-{name}-{index}.xml",
                    "test_paths": "",
                }
            )

    # Keep the converted legacy state suite visible as a single informational
    # result, matching its established ~18 minute runtime.
    entries.append(
        {
            "name": "ported state tests",
            "family": "",
            "shard": 0,
            "shard_count": 1,
            "artifact": "ported-state",
            "report": "reports/junit-ported-state.xml",
            "test_paths": (
                "integration_test/exec_specs/config/ported-static-paths.list"
            ),
        }
    )

    return entries


def main() -> int:
    raw_count = os.environ.get("EEST_SHARD_COUNT", "8")
    try:
        shard_count = int(raw_count)
    except ValueError:
        print(
            f"EEST_SHARD_COUNT must be an integer, got {raw_count!r}.", file=sys.stderr
        )
        return 2
    if shard_count < 1:
        print("EEST_SHARD_COUNT must be positive.", file=sys.stderr)
        return 2

    print(f"matrix={json.dumps({'include': build_matrix(shard_count)})}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
