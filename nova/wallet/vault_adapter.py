"""HashiCorp Vault signer adapter."""
from __future__ import annotations

from typing import Iterable


class VaultSigner:
    def __init__(self, base_url: str, role_id: str, secret_path: str, timeout: int = 5) -> None:
        self._base_url = base_url.rstrip("/")
        self._role_id = role_id
        self._secret_path = secret_path
        self._balance = 2_000_000

    def withdraw_rewards(self, validators: Iterable[str], dry_run: bool = False) -> int:
        # Placeholder for Vault transit / signer integration
        if dry_run:
            return 0
        return 100000

    def get_balance(self) -> int:
        return self._balance

    def delegate(self, validator: str, amount: int) -> str:
        if amount > self._balance:
            raise ValueError("Insufficient balance in vault signer")
        self._balance -= amount
        return f"VAULT-{validator[-6:]}-{amount}"
