#!/usr/bin/env python3
"""Utility to build and sign Sei Solo precompile claim transactions offline."""
from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Optional

from eth_account import Account  # type: ignore[import]
from eth_account.signers.local import LocalAccount  # type: ignore[import]
from web3 import Web3  # type: ignore[import]
from web3.contract import ContractFunction  # type: ignore[import]

SOLO_PRECOMPILE_ADDRESS = Web3.to_checksum_address(
    "0x000000000000000000000000000000000000100C"
)

SOLO_ABI = json.loads(
    """
    [
      {
        "inputs": [
          {"internalType": "bytes", "name": "payload", "type": "bytes"}
        ],
        "name": "claim",
        "outputs": [
          {"internalType": "bool", "name": "response", "type": "bool"}
        ],
        "stateMutability": "nonpayable",
        "type": "function"
      },
      {
        "inputs": [
          {"internalType": "bytes", "name": "payload", "type": "bytes"}
        ],
        "name": "claimSpecific",
        "outputs": [
          {"internalType": "bool", "name": "response", "type": "bool"}
        ],
        "stateMutability": "nonpayable",
        "type": "function"
      }
    ]
    """
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Build and sign an offline Sei Solo precompile transaction that calls "
            "either claim(bytes) or claimSpecific(bytes)."
        )
    )
    parser.add_argument(
        "--payload",
        required=True,
        help=(
            "Hex string (with or without 0x prefix) or path to a file containing the "
            "Cosmos-signed MsgClaim or MsgClaimSpecific payload."
        ),
    )
    parser.add_argument(
        "--claim-specific",
        action="store_true",
        help="Encode the call to claimSpecific(bytes) instead of claim(bytes).",
    )
    parser.add_argument(
        "--chain-id",
        type=int,
        default=int(os.getenv("SEI_EVM_CHAIN_ID", 1329)),
        help="Sei EVM chain ID. Defaults to value from SEI_EVM_CHAIN_ID env var or 1329.",
    )
    parser.add_argument(
        "--gas-limit",
        type=int,
        default=int(os.getenv("CLAIM_TX_GAS_LIMIT", 750000)),
        help="Gas limit to use for the transaction. Defaults to 750000.",
    )
    parser.add_argument(
        "--gas-price",
        type=float,
        default=None,
        help=(
            "Legacy gas price to use in gwei. Ignored if max-fee-per-gas is provided. "
            "If omitted, the script uses the RPC gas price when --rpc-url is set."
        ),
    )
    parser.add_argument(
        "--max-fee-per-gas",
        type=float,
        default=None,
        help="EIP-1559 maxFeePerGas in gwei. Requires --max-priority-fee-per-gas.",
    )
    parser.add_argument(
        "--max-priority-fee-per-gas",
        type=float,
        default=None,
        help="EIP-1559 maxPriorityFeePerGas in gwei.",
    )
    parser.add_argument(
        "--nonce",
        type=int,
        default=None,
        help=(
            "Account nonce to use. If not provided, the script fetches it from the "
            "RPC endpoint."
        ),
    )
    parser.add_argument(
        "--rpc-url",
        type=str,
        default=os.getenv("SEI_EVM_RPC_URL"),
        help="HTTP RPC endpoint used to query nonce and (optionally) gas price.",
    )
    parser.add_argument(
        "--output",
        default=os.getenv("SIGNED_TX_OUTPUT", "signed_claim.json"),
        help="File path where the signed transaction JSON should be written.",
    )
    parser.add_argument(
        "--no-stdout",
        action="store_true",
        help="Do not echo the signed transaction hex blob to stdout.",
    )
    return parser.parse_args()


def load_payload(arg: str) -> bytes:
    potential_path = Path(arg)
    if potential_path.exists():
        data = potential_path.read_bytes()
        if data.startswith(b"0x"):
            data = data.strip()
            return bytes.fromhex(data.decode()[2:])
        return data
    text = arg.strip().lower()
    if text.startswith("0x"):
        text = text[2:]
    if len(text) % 2:
        raise ValueError("Payload hex must have an even number of characters.")
    return bytes.fromhex(text)


def build_contract_call(payload: bytes, claim_specific: bool) -> ContractFunction:
    web3 = Web3()
    contract = web3.eth.contract(address=SOLO_PRECOMPILE_ADDRESS, abi=SOLO_ABI)
    if claim_specific:
        return contract.functions.claimSpecific(payload)
    return contract.functions.claim(payload)


def initialise_account() -> LocalAccount:
    private_key = os.getenv("PRIVATE_KEY")
    if not private_key:
        raise SystemExit(
            "PRIVATE_KEY environment variable is required to sign the transaction."
        )
    return Account.from_key(private_key)


def ensure_fee_fields(
    args: argparse.Namespace, rpc_web3: Optional[Web3]
) -> dict[str, int]:
    fee_fields: dict[str, int] = {}
    if args.max_fee_per_gas is not None or args.max_priority_fee_per_gas is not None:
        if args.max_fee_per_gas is None or args.max_priority_fee_per_gas is None:
            raise SystemExit(
                "Both --max-fee-per-gas and --max-priority-fee-per-gas must be provided for EIP-1559 transactions."
            )
        fee_fields["maxFeePerGas"] = Web3.to_wei(args.max_fee_per_gas, "gwei")
        fee_fields["maxPriorityFeePerGas"] = Web3.to_wei(
            args.max_priority_fee_per_gas, "gwei"
        )
        return fee_fields

    if args.gas_price is not None:
        fee_fields["gasPrice"] = Web3.to_wei(args.gas_price, "gwei")
        return fee_fields

    if rpc_web3 is None:
        raise SystemExit(
            "Provide --gas-price, EIP-1559 fee flags, or an --rpc-url to pull gas price automatically."
        )
    fee_fields["gasPrice"] = rpc_web3.eth.gas_price
    return fee_fields


def fetch_nonce(account: LocalAccount, args: argparse.Namespace, rpc_web3: Optional[Web3]) -> int:
    if args.nonce is not None:
        return args.nonce
    if rpc_web3 is None:
        raise SystemExit(
            "Nonce is required when no RPC endpoint is available. Pass --nonce or --rpc-url."
        )
    return rpc_web3.eth.get_transaction_count(account.address)


def connect_web3(args: argparse.Namespace) -> Optional[Web3]:
    if not args.rpc_url:
        return None
    provider = Web3.HTTPProvider(args.rpc_url)
    web3 = Web3(provider)
    if not web3.is_connected():
        raise SystemExit(f"Unable to connect to RPC at {args.rpc_url!r}.")
    return web3


def main() -> int:
    args = parse_args()
    account = initialise_account()
    payload = load_payload(args.payload)
    contract_function = build_contract_call(payload, args.claim_specific)

    rpc_web3 = connect_web3(args)
    nonce = fetch_nonce(account, args, rpc_web3)
    fee_fields = ensure_fee_fields(args, rpc_web3)

    tx: dict[str, int | str | bytes] = {
        "chainId": args.chain_id,
        "nonce": nonce,
        "gas": args.gas_limit,
        "to": SOLO_PRECOMPILE_ADDRESS,
        "data": contract_function.build_transaction({})["data"],
        "value": 0,
    }
    tx.update(fee_fields)

    signed = account.sign_transaction(tx)

    output_payload = {
        "raw_transaction": signed.rawTransaction.hex(),
        "transaction_hash": signed.hash.hex(),
        "from": account.address,
        "to": SOLO_PRECOMPILE_ADDRESS,
        "nonce": nonce,
        "chain_id": args.chain_id,
        "gas_limit": args.gas_limit,
        "fee_fields": fee_fields,
        "claim_specific": args.claim_specific,
    }

    output_path = Path(args.output)
    output_path.write_text(json.dumps(output_payload, indent=2))

    if not args.no_stdout:
        print(output_payload["raw_transaction"])

    return 0


if __name__ == "__main__":
    sys.exit(main())
