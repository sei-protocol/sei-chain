"""Display the Codex settlement allocation and USD amount for a kin hash."""

from __future__ import annotations

import argparse
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
        nargs="?",
        default="f303",
        help="Kin hash identifier to locate in the Codex ledger (defaults to f303)",
    )
    parser.add_argument(
        "--ledger",
        type=Path,
        default=settlement.DEFAULT_CODEX_LEDGER,
        help="Path to the Codex ledger JSON file",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    allocation = settlement.find_allocation(args.kin_hash, args.ledger)
    print(settlement.summarise_allocation(allocation))
    print(f"Payout owed: {settlement.format_usd(allocation.balance_usd)}")
    print(f"Recipient address: {allocation.address}")
    return 0


if __name__ == "__main__":  # pragma: no cover - convenience entry point
    raise SystemExit(main())
