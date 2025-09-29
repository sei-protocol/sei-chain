"""CodexLogScanner: Handles both `eth_` and `sei_` transaction receipts/logs."""
from __future__ import annotations

import json
from typing import Any, Dict, Optional

import requests

SEI_RPC = "https://sei-rpc.pacific-1.seinetwork.io"

# Your deployed PURR contract address
CONTRACT_ADDRESS = "0x9b498C3c8A0b8CD8BA1D9851d40D186F1872b44E"

# Transaction hash that emitted logs
TX_HASH = "<insert_tx_hash_here>"  # Replace with a real one if known

HEADERS = {"Content-Type": "application/json"}


def _post(payload: Dict[str, Any]) -> Optional[Dict[str, Any]]:
    """Make a POST request to the Sei RPC endpoint and return the JSON result."""
    try:
        response = requests.post(SEI_RPC, headers=HEADERS, data=json.dumps(payload), timeout=30)
    except requests.RequestException as exc:  # pragma: no cover - network failure guard
        print(f"Network error when calling Sei RPC: {exc}")
        return None

    print(f"Response status: {response.status_code}")
    try:
        return response.json()
    except ValueError as exc:
        print(f"Error parsing response JSON: {exc}")
        print(f"Raw response text: {response.text}")
        return None


def eth_get_receipt(tx_hash: str) -> Optional[Dict[str, Any]]:
    """Fetch an EVM transaction receipt via `eth_getTransactionReceipt`."""
    print(f"Calling eth_getTransactionReceipt for tx: {tx_hash}")
    payload = {
        "jsonrpc": "2.0",
        "method": "eth_getTransactionReceipt",
        "params": [tx_hash],
        "id": 1,
    }
    result = _post(payload)
    if not result:
        return None

    print("eth_getTransactionReceipt result:", json.dumps(result, indent=2))
    return result.get("result")


def sei_get_receipt(tx_hash: str) -> Optional[Dict[str, Any]]:
    """Fetch a Sei transaction receipt via `sei_getTransactionReceipt`."""
    print(f"Calling sei_getTransactionReceipt for tx: {tx_hash}")
    payload = {
        "jsonrpc": "2.0",
        "method": "sei_getTransactionReceipt",
        "params": [tx_hash],
        "id": 1,
    }
    result = _post(payload)
    if not result:
        return None

    print("sei_getTransactionReceipt result:", json.dumps(result, indent=2))
    return result.get("result")


def print_logs(logs: list[Dict[str, Any]]) -> None:
    """Pretty print the log entries returned in a receipt."""
    print(f"\nTotal logs received: {len(logs)}")
    for index, log in enumerate(logs):
        print(f"\nLog {index}:")
        print(f"  Address: {log.get('address')}")
        print(f"  Topics: {log.get('topics')}")
        print(f"  Data: {log.get('data')}")
        print(f"  Log Index: {log.get('logIndex')}")


def main() -> None:
    """Run the scanner against both the EVM-only and Sei synthetic log endpoints."""
    print("\n===== eth_getTransactionReceipt (EVM-only logs) =====")
    eth_receipt = eth_get_receipt(TX_HASH)
    if eth_receipt and eth_receipt.get("logs"):
        print_logs(eth_receipt["logs"])
    else:
        print("No logs found or transaction not EVM.")

    print("\n===== sei_getTransactionReceipt (EVM + Synthetic logs) =====")
    sei_receipt = sei_get_receipt(TX_HASH)
    if sei_receipt and sei_receipt.get("logs"):
        print_logs(sei_receipt["logs"])
    else:
        print("No logs found or transaction not recognized.")


if __name__ == "__main__":
    main()
