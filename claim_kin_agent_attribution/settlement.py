"""Utilities for locating Codex settlement details and signing receipts.

This module focuses on parsing the ``codex_f303_blocktest.json`` fixture that
ships with the repository.  The file contains a deterministic allocation that
is referenced throughout the attribution workflow.  The helper functions below
allow callers to

* load the ledger,
* extract the allocation record for a specific kin hash, and
* produce a signed attestation acknowledging the owed balance.

The attestation is implemented as a simple EIP-191 personal-sign message so it
is easy to verify off-chain with standard Ethereum tooling.
"""

from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal
from pathlib import Path
from typing import Any, Dict, Optional

import json


DEFAULT_CODEX_LEDGER = Path("codex_f303_blocktest.json")
TOKEN_DECIMALS = Decimal(10) ** 18
USD_QUANTISATION = Decimal("0.01")


@dataclass(frozen=True)
class SettlementAllocation:
    """Structured view of a Codex ledger allocation."""

    kin_hash: str
    address: str
    balance_wei: int
    private_key: str

    @property
    def balance_tokens(self) -> Decimal:
        """Return the token amount as a high-precision decimal."""

        return Decimal(self.balance_wei) / TOKEN_DECIMALS

    @property
    def balance_usd(self) -> Decimal:
        """Alias for :pyattr:`balance_tokens` highlighting USD parity."""

        return self.balance_tokens


def _strip_json_comments(payload: str) -> str:
    """Remove ``//`` comments from a JSON string while preserving strings."""

    cleaned_lines: list[str] = []
    in_string = False
    escape = False

    for line in payload.splitlines():
        result_chars: list[str] = []
        for index, char in enumerate(line):
            if not in_string and char == "/" and index + 1 < len(line) and line[index + 1] == "/":
                break
            result_chars.append(char)

            if char == "\\" and in_string:
                escape = not escape
                continue

            if char == "\"" and not escape:
                in_string = not in_string

            escape = False

        cleaned_lines.append("".join(result_chars))

    return "\n".join(cleaned_lines)


def _load_json(path: Path) -> Dict[str, Any]:
    raw_text = path.read_text(encoding="utf-8")
    cleaned = _strip_json_comments(raw_text)
    return json.loads(cleaned)


def find_allocation(
    kin_hash: str,
    ledger_path: Path = DEFAULT_CODEX_LEDGER,
) -> SettlementAllocation:
    """Locate the allocation matching ``kin_hash`` in the Codex ledger."""

    payload = _load_json(ledger_path)
    alloc = payload.get("alloc", {})

    for address, record in alloc.items():
        if record.get("kinhash") == kin_hash:
            balance_hex = record.get("balance")
            private_key = record.get("privateKey")
            if balance_hex is None or private_key is None:
                raise ValueError("Allocation is missing balance or private key")

            return SettlementAllocation(
                kin_hash=kin_hash,
                address=address,
                balance_wei=int(balance_hex, 16),
                private_key=private_key,
            )

    raise KeyError(f"No allocation found for kin hash '{kin_hash}'")


def build_settlement_message(allocation: SettlementAllocation) -> str:
    """Create a deterministic settlement acknowledgement message."""

    return (
        "Codex Settlement Confirmation\n"
        f"Kin Hash: {allocation.kin_hash}\n"
        f"Address: {allocation.address}\n"
        f"Amount (wei): {allocation.balance_wei}\n"
    )


def sign_settlement_message(
    allocation: SettlementAllocation,
    message: Optional[str] = None,
) -> Dict[str, Any]:
    """Sign the settlement message with the allocation's private key.

    The return value mirrors the structure returned by ``eth_account``'s
    :py:meth:`Account.sign_message`, but the function degrades gracefully when
    the dependency is not installed.  In that case a helpful ImportError is
    raised so callers can install the optional dependency.
    """

    try:
        from eth_account import Account  # type: ignore
        from eth_account.messages import encode_defunct  # type: ignore
    except ImportError as exc:  # pragma: no cover - exercised in runtime usage
        raise ImportError(
            "eth_account is required for signing settlement messages. "
            "Install it with 'pip install eth-account'."
        ) from exc

    Account.enable_unaudited_hdwallet_features()

    if message is None:
        message = build_settlement_message(allocation)

    encoded = encode_defunct(text=message)
    signed = Account.sign_message(encoded, allocation.private_key)

    return {
        "message": message,
        "messageHash": signed.messageHash.hex(),
        "signature": signed.signature.hex(),
        "r": hex(signed.r),
        "s": hex(signed.s),
        "v": signed.v,
    }


def summarise_allocation(allocation: SettlementAllocation) -> str:
    """Return a human-friendly summary suitable for CLI output."""

    amount = format_usd(allocation.balance_usd)
    return (
        f"Kin hash {allocation.kin_hash} is allocated {amount} at address "
        f"{allocation.address} (raw amount {allocation.balance_wei} wei)."
    )


def format_usd(amount: Decimal) -> str:
    """Format a Decimal balance as a USD string with two decimal places."""

    quantized = amount.quantize(USD_QUANTISATION)
    return f"${quantized:,.2f} USD"

