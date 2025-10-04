#!/usr/bin/env python3
"""Simple utility to inspect USDC balances and allowances on Sei EVM."""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass
from decimal import Decimal, getcontext
from pathlib import Path
from typing import Iterable, List, Sequence, Tuple

from web3 import Web3


# Increase precision so division of large integer balances remains accurate.
getcontext().prec = 78


RPC_URL = "https://evm-rpc.sei-apis.com"
USDC_CONTRACT = "0xe15c1c6f7c19c1d7c2c1d1845a8e0bde8e42392"
WALLET = "0xb2b297eF9449aa0905bC318B3bd258c4804BAd98"


DEFAULT_SPENDERS: Sequence[Tuple[str, str]] = (
    ("Placeholder", "0x0000000000000000000000000000000000000000"),
)


@dataclass(frozen=True)
class Spender:
    label: str
    address: str


def parse_spender_argument(raw: str) -> Spender:
    if "=" in raw:
        label, address = raw.split("=", 1)
        label = label.strip() or address
    else:
        label, address = raw.strip(), raw.strip()
    return Spender(label=label, address=address)


def load_spenders_from_file(path: Path) -> List[Spender]:
    data = json.loads(path.read_text())
    spenders: List[Spender] = []
    if isinstance(data, dict):
        for label, address in data.items():
            spenders.append(Spender(label=label, address=str(address)))
    elif isinstance(data, list):
        for item in data:
            if isinstance(item, dict) and "address" in item:
                label = item.get("label") or item["address"]
                spenders.append(Spender(label=str(label), address=str(item["address"])))
            elif isinstance(item, str):
                spenders.append(parse_spender_argument(item))
            else:
                raise ValueError(f"Unsupported spender item: {item!r}")
    else:
        raise ValueError("Spender file must contain a JSON object or array")
    return spenders


def build_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rpc", default=RPC_URL, help="Sei EVM RPC endpoint")
    parser.add_argument("--wallet", default=WALLET, help="Wallet address to inspect")
    parser.add_argument("--usdc", default=USDC_CONTRACT, help="USDC token contract address")
    parser.add_argument(
        "--spender",
        dest="spenders",
        action="append",
        default=[],
        metavar="LABEL=ADDRESS",
        help="Additional spender addresses (optionally prefixed with a label)",
    )
    parser.add_argument(
        "--spender-file",
        type=Path,
        help="Path to JSON file providing spender addresses. Accepts {\"Label\": \"0x...\"} or list entries.",
    )
    parser.add_argument(
        "--decimals-override",
        type=int,
        help="Override token decimals instead of querying the contract",
    )
    return parser


def normalise_address(address: str) -> str:
    return Web3.to_checksum_address(address)


def format_amount(raw_amount: int, decimals: int) -> Decimal:
    scale = Decimal(10) ** decimals
    return Decimal(raw_amount) / scale


def iter_spenders(args: argparse.Namespace) -> Iterable[Spender]:
    seen = set()

    for label, address in DEFAULT_SPENDERS:
        checksum = normalise_address(address)
        seen.add(checksum.lower())
        yield Spender(label=label, address=checksum)

    for entry in args.spenders:
        spender = parse_spender_argument(entry)
        checksum = normalise_address(spender.address)
        if checksum.lower() in seen:
            continue
        seen.add(checksum.lower())
        yield Spender(label=spender.label, address=checksum)

    if args.spender_file:
        for spender in load_spenders_from_file(args.spender_file):
            checksum = normalise_address(spender.address)
            if checksum.lower() in seen:
                continue
            seen.add(checksum.lower())
            yield Spender(label=spender.label, address=checksum)


def build_web3(rpc_url: str) -> Web3:
    provider = Web3.HTTPProvider(rpc_url)
    w3 = Web3(provider)
    if not w3.is_connected():
        raise SystemExit(f"[ERROR] Could not connect to RPC: {rpc_url}")
    return w3


def load_contract(w3: Web3, token_address: str):
    abi = [
        {
            "constant": True,
            "inputs": [{"name": "_owner", "type": "address"}],
            "name": "balanceOf",
            "outputs": [{"name": "balance", "type": "uint256"}],
            "type": "function",
        },
        {
            "constant": True,
            "inputs": [
                {"name": "_owner", "type": "address"},
                {"name": "_spender", "type": "address"},
            ],
            "name": "allowance",
            "outputs": [{"name": "remaining", "type": "uint256"}],
            "type": "function",
        },
        {
            "constant": True,
            "inputs": [],
            "name": "decimals",
            "outputs": [{"name": "", "type": "uint8"}],
            "type": "function",
        },
        {
            "constant": True,
            "inputs": [],
            "name": "symbol",
            "outputs": [{"name": "", "type": "string"}],
            "type": "function",
        },
    ]
    return w3.eth.contract(address=normalise_address(token_address), abi=abi)


def main(argv: Sequence[str] | None = None) -> int:
    args = build_argument_parser().parse_args(argv)

    w3 = build_web3(args.rpc)
    contract = load_contract(w3, args.usdc)

    wallet = normalise_address(args.wallet)

    decimals = args.decimals_override
    if decimals is None:
        decimals = contract.functions.decimals().call()

    symbol = contract.functions.symbol().call()

    balance_raw = contract.functions.balanceOf(wallet).call()
    balance = format_amount(balance_raw, decimals)
    print(f"[INFO] Balance of {wallet} on {symbol}: {balance}")

    spenders = list(iter_spenders(args))
    if not spenders:
        print("[WARN] No spender addresses supplied; pass --spender or --spender-file to add them.")
        return 0

    for spender in spenders:
        allowance_raw = contract.functions.allowance(wallet, spender.address).call()
        allowance = format_amount(allowance_raw, decimals)
        print(
            f"[INFO] Allowance for {spender.label} ({spender.address}): {allowance} {symbol}"
        )

    return 0


if __name__ == "__main__":
    sys.exit(main())

