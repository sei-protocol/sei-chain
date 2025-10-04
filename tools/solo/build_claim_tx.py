"""Utility for building and signing Solo precompile claim transactions."""
from __future__ import annotations

import argparse
import json
import os
from decimal import Decimal
from pathlib import Path
from typing import Any, Dict, Iterable, Optional

from eth_account import Account
from eth_account.signers.local import LocalAccount
from web3 import HTTPProvider, Web3
from web3.contract.contract import ContractFunction
from web3.middleware import geth_poa_middleware
from web3.types import TxParams

SOLO_PRECOMPILE_ADDRESS = Web3.to_checksum_address(
    "0x000000000000000000000000000000000000100C"
)
ABI_PATH = Path(__file__).resolve().parents[2] / "precompiles" / "solo" / "abi.json"


def parse_args() -> argparse.Namespace:
    """Parse command-line arguments for claim transaction builder."""
    parser = argparse.ArgumentParser(
        description=(
            "Build, sign, and optionally broadcast a Solo precompile claim transaction."
        )
    )
    parser.add_argument(
        "--payload",
        required=True,
        help=(
            "Hex-encoded payload or path to a file containing the hex-encoded payload."
        ),
    )
    parser.add_argument(
        "--claim-specific",
        action="store_true",
        help="Use the claimSpecific(bytes) function instead of claim(bytes).",
    )
    parser.add_argument(
        "--gas-limit",
        type=int,
        default=750_000,
        help="Gas limit to use for the transaction (default: 750000).",
    )
    parser.add_argument(
        "--gas-price",
        type=Decimal,
        default=None,
        help="Legacy gas price in gwei. Mutually exclusive with EIP-1559 fee flags.",
    )
    parser.add_argument(
        "--max-fee-per-gas",
        type=Decimal,
        default=None,
        help="EIP-1559 max fee per gas in gwei.",
    )
    parser.add_argument(
        "--max-priority-fee-per-gas",
        type=Decimal,
        default=None,
        help="EIP-1559 max priority fee per gas in gwei.",
    )
    parser.add_argument(
        "--nonce",
        type=int,
        default=None,
        help="Transaction nonce. If omitted it will be fetched from the RPC node.",
    )
    parser.add_argument(
        "--chain-id",
        type=int,
        required=True,
        help="Chain ID to sign the transaction for.",
    )
    parser.add_argument(
        "--rpc-url",
        default=os.environ.get("SEI_EVM_RPC_URL"),
        help="RPC endpoint for Sei EVM. Defaults to the SEI_EVM_RPC_URL environment variable.",
    )
    parser.add_argument(
        "--private-key",
        default=None,
        help="Hex-encoded private key. Defaults to the PRIVATE_KEY environment variable.",
    )
    parser.add_argument(
        "--output",
        default="signed_claim.json",
        help="Path to the JSON file where the signed transaction will be written.",
    )
    parser.add_argument(
        "--no-stdout",
        action="store_true",
        help="Do not print the signed transaction to stdout.",
    )

    args = parser.parse_args()

    if args.rpc_url is None:
        parser.error("An RPC URL must be provided via --rpc-url or SEI_EVM_RPC_URL.")

    if args.gas_price is not None:
        if args.max_fee_per_gas is not None or args.max_priority_fee_per_gas is not None:
            parser.error(
                "--gas-price cannot be used together with EIP-1559 fee options."
            )

    if (args.max_fee_per_gas is None) != (args.max_priority_fee_per_gas is None):
        parser.error(
            "Both --max-fee-per-gas and --max-priority-fee-per-gas must be provided together."
        )

    return args


def load_payload(payload: str) -> bytes:
    """Load a hex payload either directly or from a file path."""
    candidate = Path(payload)
    if candidate.exists():
        payload_hex = candidate.read_text(encoding="utf-8").strip()
    else:
        payload_hex = payload.strip()

    if payload_hex.startswith("0x"):
        payload_hex = payload_hex[2:]

    if not payload_hex:
        raise ValueError("Payload must not be empty.")

    if len(payload_hex) % 2 != 0:
        raise ValueError("Payload must contain an even number of hex characters.")

    try:
        return bytes.fromhex(payload_hex)
    except ValueError as exc:
        raise ValueError("Payload must be valid hex.") from exc


def _load_abi() -> Iterable[Dict[str, Any]]:
    if not ABI_PATH.exists():
        raise FileNotFoundError(f"Could not locate ABI at {ABI_PATH}.")
    return json.loads(ABI_PATH.read_text(encoding="utf-8"))


def build_contract_call(payload: bytes, claim_specific: bool) -> ContractFunction:
    """Prepare the contract function call for claim or claimSpecific."""
    web3 = Web3()
    abi = _load_abi()
    contract = web3.eth.contract(address=SOLO_PRECOMPILE_ADDRESS, abi=abi)
    if claim_specific:
        return contract.functions.claimSpecific(payload)
    return contract.functions.claim(payload)


def initialise_account(cli_private_key: Optional[str] = None) -> LocalAccount:
    """Initialise the signer account from CLI or environment private key."""
    private_key = cli_private_key or os.environ.get("PRIVATE_KEY")
    if private_key is None:
        raise ValueError(
            "A private key must be provided via --private-key or the PRIVATE_KEY environment variable."
        )
    private_key = private_key.strip()
    if private_key.startswith("0x"):
        private_key = private_key[2:]
    if not private_key:
        raise ValueError("Private key must not be empty.")
    return Account.from_key(private_key)


def connect_web3(args: argparse.Namespace) -> Web3:
    """Create a Web3 instance connected to the provided RPC URL."""
    provider = HTTPProvider(args.rpc_url)
    web3 = Web3(provider)
    web3.middleware_onion.inject(geth_poa_middleware, layer=0)
    if not web3.is_connected():
        raise ConnectionError(f"Failed to connect to RPC endpoint {args.rpc_url}.")
    return web3


def fetch_nonce(account: LocalAccount, args: argparse.Namespace, web3: Web3) -> int:
    """Fetch or use the provided nonce."""
    if args.nonce is not None:
        return args.nonce
    return web3.eth.get_transaction_count(account.address)


def _decimal_gwei_to_wei(value: Decimal) -> int:
    return int(Web3.to_wei(value, "gwei"))


def ensure_fee_fields(args: argparse.Namespace, web3: Web3) -> Dict[str, int]:
    """Determine the gas fee fields for the transaction."""
    if args.gas_price is not None:
        return {"gasPrice": _decimal_gwei_to_wei(args.gas_price)}

    if args.max_fee_per_gas is not None and args.max_priority_fee_per_gas is not None:
        max_fee = _decimal_gwei_to_wei(args.max_fee_per_gas)
        max_priority = _decimal_gwei_to_wei(args.max_priority_fee_per_gas)
        if max_fee < max_priority:
            raise ValueError("max fee per gas must be >= max priority fee per gas.")
        return {"maxFeePerGas": max_fee, "maxPriorityFeePerGas": max_priority}

    gas_price = web3.eth.gas_price
    if gas_price is not None:
        return {"gasPrice": gas_price}

    pending_block = web3.eth.get_block("pending")
    base_fee = pending_block.get("baseFeePerGas")
    if base_fee is None:
        raise ValueError("Pending block does not expose baseFeePerGas for EIP-1559 fees.")
    priority_fee = web3.eth.max_priority_fee
    max_fee = base_fee * 2 + priority_fee
    return {"maxFeePerGas": max_fee, "maxPriorityFeePerGas": priority_fee}


def main() -> int:
    args = parse_args()
    account = initialise_account(args.private_key)
    payload = load_payload(args.payload)
    contract_function = build_contract_call(payload, args.claim_specific)

    rpc_web3 = connect_web3(args)
    nonce = fetch_nonce(account, args, rpc_web3)
    fee_fields = ensure_fee_fields(args, rpc_web3)

    tx: TxParams = {
        "chainId": args.chain_id,
        "nonce": nonce,
        "gas": args.gas_limit,
        "to": SOLO_PRECOMPILE_ADDRESS,
        "data": contract_function._encode_transaction_data(),
        "value": 0,
    }
    tx.update(fee_fields)

    signed = account.sign_transaction(tx)

    output_payload: Dict[str, Any] = {
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
    output_path.write_text(json.dumps(output_payload, indent=2), encoding="utf-8")

    if not args.no_stdout:
        print(output_payload["raw_transaction"])

    if args.rpc_url:
        tx_hash = rpc_web3.eth.send_raw_transaction(signed.rawTransaction)
        print(f"Transaction hash: {tx_hash.hex()}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
