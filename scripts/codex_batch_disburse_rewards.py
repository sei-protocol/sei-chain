#!/usr/bin/env python3
"""Batch royalty/reward disbursement helper for ERC-1967 proxies.

This script submits sequential disbursement transactions to a proxy-based
reward distributor (e.g. Compound-style multi reward distributor).

Usage example:
    PRIVATE_KEY=0xabc... python codex_batch_disburse_rewards.py \
        recipients.json \
        --proxy 0x28BF6D71b6Dc837F56F5afbF1F4A46AaC0B1f31E \
        --rpc https://rpc.hyperliquid.xyz/evm

The input file can be JSON or CSV:
- JSON: an array of objects with keys "tToken", "address", "role"
        ("supplier" or "borrower") and optional "sendTokens".
- CSV: headers "tToken", "address", "role" and optional "sendTokens".
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import List, Optional, Sequence

from eth_account import Account
from eth_account.signers.local import LocalAccount
from web3 import Web3

ABI = [
    {
        "inputs": [
            {"internalType": "address", "name": "_tToken", "type": "address"},
            {"internalType": "address", "name": "_supplier", "type": "address"},
            {"internalType": "bool", "name": "_sendTokens", "type": "bool"},
        ],
        "name": "disburseSupplierRewards",
        "outputs": [],
        "stateMutability": "nonpayable",
        "type": "function",
    },
    {
        "inputs": [
            {"internalType": "address", "name": "_tToken", "type": "address"},
            {"internalType": "address", "name": "_borrower", "type": "address"},
            {"internalType": "bool", "name": "_sendTokens", "type": "bool"},
        ],
        "name": "disburseBorrowerRewards",
        "outputs": [],
        "stateMutability": "nonpayable",
        "type": "function",
    },
]


@dataclass
class Disbursement:
    ttoken: str
    recipient: str
    role: str
    send_tokens: bool

    @classmethod
    def from_mapping(cls, data: dict) -> "Disbursement":
        try:
            ttoken = data["tToken"]
            recipient = data["address"]
            role = data["role"].lower()
        except KeyError as exc:  # pragma: no cover - runtime validation
            raise ValueError(f"Missing required field: {exc.args[0]}") from exc

        if role not in {"supplier", "borrower"}:
            raise ValueError(f"Unsupported role '{role}'. Expected 'supplier' or 'borrower'.")

        send_tokens = data.get("sendTokens", data.get("send_tokens", True))
        if isinstance(send_tokens, str):
            send_tokens = send_tokens.strip().lower() in {"1", "true", "yes"}
        else:
            send_tokens = bool(send_tokens)

        return cls(ttoken=ttoken, recipient=recipient, role=role, send_tokens=send_tokens)


def load_entries(path: Path) -> List[Disbursement]:
    if not path.exists():
        raise FileNotFoundError(f"Input file not found: {path}")

    if path.suffix.lower() == ".json":
        with path.open() as fh:
            raw = json.load(fh)
        if not isinstance(raw, Sequence) or isinstance(raw, (str, bytes)):
            raise ValueError("JSON input must be a list of disbursement objects.")
        return [Disbursement.from_mapping(item) for item in raw]

    if path.suffix.lower() == ".csv":
        with path.open(newline="") as fh:
            reader = csv.DictReader(fh)
            return [Disbursement.from_mapping(row) for row in reader]

    raise ValueError("Unsupported file extension. Use .json or .csv inputs.")


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Batch disburse rewards via ERC-1967 proxy")
    parser.add_argument("input", type=Path, help="Path to JSON/CSV recipient list")
    parser.add_argument("--proxy", required=True, help="Proxy contract address")
    parser.add_argument(
        "--rpc",
        default=os.environ.get("RPC_URL", "https://rpc.hyperliquid.xyz/evm"),
        help="RPC endpoint URL (default: %(default)s)",
    )
    parser.add_argument(
        "--gas",
        type=int,
        default=750_000,
        help="Gas limit per transaction (default: %(default)s)",
    )
    parser.add_argument(
        "--max-fee",
        type=float,
        default=None,
        help="Max fee per gas in gwei (EIP-1559). If omitted uses eth_maxFeePerGas",
    )
    parser.add_argument(
        "--priority-fee",
        type=float,
        default=None,
        help="Priority fee per gas in gwei (EIP-1559). If omitted uses eth_maxPriorityFeePerGas",
    )
    parser.add_argument(
        "--legacy",
        action="store_true",
        help="Use legacy gas pricing instead of EIP-1559",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print transactions without broadcasting",
    )
    parser.add_argument(
        "--nonce",
        type=int,
        default=None,
        help="Override starting nonce (default: fetched from RPC)",
    )
    return parser


def init_account() -> LocalAccount:
    private_key = os.environ.get("PRIVATE_KEY")
    if not private_key:
        raise SystemExit("PRIVATE_KEY environment variable is required")
    return Account.from_key(private_key)


def resolve_gas_prices(w3: Web3, args: argparse.Namespace) -> tuple[Optional[int], Optional[int]]:
    if args.legacy:
        return None, None

    max_fee = args.max_fee
    priority_fee = args.priority_fee

    if max_fee is None:
        try:
            max_fee = w3.from_wei(w3.eth.max_fee_per_gas, "gwei")
        except Exception:
            max_fee = w3.from_wei(w3.eth.gas_price, "gwei")

    if priority_fee is None:
        try:
            priority_fee = w3.from_wei(w3.eth.max_priority_fee, "gwei")
        except Exception:
            priority_fee = 1.0

    return int(w3.to_wei(max_fee, "gwei")), int(w3.to_wei(priority_fee, "gwei"))


def main() -> None:
    parser = build_arg_parser()
    args = parser.parse_args()

    entries = load_entries(args.input)
    if not entries:
        raise SystemExit("No disbursement entries found.")

    account = init_account()
    w3 = Web3(Web3.HTTPProvider(args.rpc))
    contract = w3.eth.contract(address=Web3.to_checksum_address(args.proxy), abi=ABI)

    starting_nonce = args.nonce or w3.eth.get_transaction_count(account.address)

    max_fee_per_gas, max_priority_fee_per_gas = resolve_gas_prices(w3, args)

    print(f"Loaded {len(entries)} disbursements. Starting nonce: {starting_nonce}")
    nonce = starting_nonce

    for entry in entries:
        function = (
            contract.functions.disburseSupplierRewards
            if entry.role == "supplier"
            else contract.functions.disburseBorrowerRewards
        )
        tx_params = {
            "from": account.address,
            "nonce": nonce,
            "gas": args.gas,
            "chainId": w3.eth.chain_id,
        }

        if args.legacy:
            tx_params["gasPrice"] = w3.eth.gas_price
        else:
            tx_params["maxFeePerGas"] = max_fee_per_gas
            tx_params["maxPriorityFeePerGas"] = max_priority_fee_per_gas

        tx = function(entry.ttoken, entry.recipient, entry.send_tokens).build_transaction(tx_params)

        print(
            f"Prepared {entry.role} disbursement: tToken={entry.ttoken}, "
            f"recipient={entry.recipient}, sendTokens={entry.send_tokens}, nonce={nonce}"
        )

        if args.dry_run:
            nonce += 1
            continue

        signed = account.sign_transaction(tx)
        tx_hash = w3.eth.send_raw_transaction(signed.rawTransaction)
        print(f"  -> sent {tx_hash.hex()}")
        nonce += 1

    print("All disbursements processed.")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:  # pragma: no cover - CLI script guard
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)
