"""CLI helper to locate and sign the Codex settlement allocation."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:  # pragma: no cover - import side effect
    sys.path.insert(0, str(REPO_ROOT))

from claim_kin_agent_attribution import settlement


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "kin_hash",
        help="Kin hash identifier to locate in the Codex ledger (e.g. f303)",
    )
    parser.add_argument(
        "--ledger",
        type=Path,
        default=settlement.DEFAULT_CODEX_LEDGER,
        help="Path to the Codex ledger JSON file",
    )
    parser.add_argument(
        "--output",
        type=Path,
        help="Optional file path to write the signed settlement JSON",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    allocation = settlement.find_allocation(args.kin_hash, args.ledger)
    summary = settlement.summarise_allocation(allocation)
    print(summary)

    result = settlement.sign_settlement_message(allocation)

    if args.output:
        args.output.write_text(json.dumps(result, indent=2))
        print(f"Signed settlement written to {args.output}")
    else:
        print(json.dumps(result, indent=2))

    return 0


if __name__ == "__main__":  # pragma: no cover - convenience entry point
    raise SystemExit(main())

