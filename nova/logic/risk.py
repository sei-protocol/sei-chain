"""Risk constraints for Nova."""
from __future__ import annotations

from collections.abc import Iterable
from typing import List, Tuple

from nova.utils.logger import get_logger

logger = get_logger(__name__)


class RiskEngine:
    def __init__(self, buffer: int, max_delegate: int | None = None, validator_cap: int | None = None) -> None:
        self._buffer = buffer
        self._max_delegate = max_delegate
        self._validator_cap = validator_cap

    def within_limits(self, balance: int) -> bool:
        if balance <= self._buffer:
            logger.info("risk.buffer", balance=balance, buffer=self._buffer)
            return False
        if self._max_delegate and balance > self._max_delegate:
            logger.warning("risk.max_delegate", balance=balance, max_delegate=self._max_delegate)
        return True

    def filter_validators(self, validators: Iterable[str]) -> List[str]:
        validators = list(validators)
        if self._validator_cap:
            return validators[: self._validator_cap]
        return validators

    def split_allocation(self, amount: int, validators: Iterable[str]) -> List[Tuple[str, int]]:
        validators = list(validators)
        if not validators:
            return []
        per_val = amount // len(validators)
        remainder = amount % len(validators)
        allocation: List[Tuple[str, int]] = []
        for idx, val in enumerate(validators):
            share = per_val + (1 if idx < remainder else 0)
            if share > 0:
                allocation.append((val, share))
        return allocation
