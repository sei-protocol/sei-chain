from __future__ import annotations

import argparse
from pathlib import Path

from nova.config import load_config


def main() -> None:
    parser = argparse.ArgumentParser(description="Validate a Nova configuration file")
    parser.add_argument("--profile", required=True, type=Path)
    args = parser.parse_args()

    config = load_config(args.profile)
    print("Configuration valid for wallet:", config.wallet.address)


if __name__ == "__main__":
    main()
