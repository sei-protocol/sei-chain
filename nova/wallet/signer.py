"""Local signer using a mocked Sei keyring."""
from __future__ import annotations

from typing import Iterable


class LocalSigner:
    """Mock signer for development and dry-run flows."""

    def __init__(self, address: str) -> None:
        self._address = address
        self._balance = 2_000_000

    def withdraw_rewards(self, validators: Iterable[str], dry_run: bool = False) -> int:
        return 120000 if not dry_run else 0

    def get_balance(self) -> int:
        return self._balance

    def delegate(self, validator: str, amount: int) -> str:
        if amount > self._balance:
            raise ValueError("Insufficient balance")
        self._balance -= amount
        return f"MOCKTX-{validator[-6:]}-{amount}"
