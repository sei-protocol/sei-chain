"""Wallet manager that coordinates signers and Sei RPC interactions."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Iterable, Protocol

from nova.config.loader import NovaConfig
from nova.utils.logger import get_logger

logger = get_logger(__name__)


class Signer(Protocol):
    def withdraw_rewards(self, validators: Iterable[str], dry_run: bool = False) -> int:
        ...

    def get_balance(self) -> int:
        ...

    def delegate(self, validator: str, amount: int) -> str:
        ...


@dataclass
class WalletManager:
    config: NovaConfig
    signer: Signer

    def withdraw_rewards(self, validators: Iterable[str], dry_run: bool = False) -> int:
        logger.info("wallet.withdraw_rewards", validators=list(validators), dry_run=dry_run)
        return self.signer.withdraw_rewards(validators, dry_run=dry_run)

    def get_spendable_balance(self) -> int:
        balance = self.signer.get_balance()
        logger.info("wallet.balance", balance=balance)
        return balance

    def delegate(self, validator: str, amount: int) -> str:
        logger.info("wallet.delegate", validator=validator, amount=amount)
        return self.signer.delegate(validator, amount)
