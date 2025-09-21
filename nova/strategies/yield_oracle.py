"""Validator yield oracle placeholder."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Iterable, List


@dataclass
class ValidatorScore:
    operator_address: str
    score: float


class YieldOracle:
    """Fetches validator metrics and produces an ordered list."""

    def __init__(self, rpc_endpoint: str, rest_endpoint: str | None = None) -> None:
        self._rpc_endpoint = rpc_endpoint
        self._rest_endpoint = rest_endpoint or rpc_endpoint

    def rank_validators(self, validators: Iterable[str]) -> List[str]:
        scored = [self._score_validator(v) for v in validators]
        scored.sort(key=lambda item: item.score, reverse=True)
        return [item.operator_address for item in scored]

    def _score_validator(self, validator: str) -> ValidatorScore:
        apr = self._fetch_apr(validator)
        uptime = self._fetch_uptime(validator)
        commission = self._fetch_commission(validator)
        score = apr * uptime * (1 - commission)
        return ValidatorScore(operator_address=validator, score=score)

    def _fetch_apr(self, validator: str) -> float:
        return 0.18  # placeholder until integrated with Sei analytics

    def _fetch_uptime(self, validator: str) -> float:
        return 0.99

    def _fetch_commission(self, validator: str) -> float:
        return 0.05
