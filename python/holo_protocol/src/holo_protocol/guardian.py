"""Guardian risk checks."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Iterable, List


@dataclass(slots=True)
class GuardianDecision:
    verdict: str
    score: float
    reasons: List[str]


@dataclass(slots=True)
class Transaction:
    amount: float
    denom: str
    sender: str
    recipient: str
    location: str
    memo: str
    risk_tags: List[str]


class GuardianEngine:
    """Simple rule engine that imitates the Guardian microservice."""

    def __init__(self, suspicious_countries: Iterable[str] | None = None) -> None:
        self._suspicious = {c.lower() for c in (suspicious_countries or {"north_korea", "iran"})}

    def evaluate(self, tx: Transaction) -> GuardianDecision:
        reasons: List[str] = []
        score = 0.0

        if tx.amount >= 10_000:
            score += 0.5
            reasons.append("high_amount")
        elif tx.amount >= 1_000:
            score += 0.2
            reasons.append("medium_amount")

        if tx.memo and len(tx.memo) > 120:
            score += 0.1
            reasons.append("long_memo")

        if tx.location.lower() in self._suspicious:
            score += 0.4
            reasons.append("suspicious_location")

        if "new_recipient" in tx.risk_tags:
            score += 0.15
            reasons.append("new_recipient")

        if "velocity" in tx.risk_tags:
            score += 0.1
            reasons.append("velocity")

        verdict = "deny" if score >= 0.6 else "review" if score >= 0.3 else "allow"
        return GuardianDecision(verdict=verdict, score=round(score, 2), reasons=reasons)


__all__ = ["GuardianDecision", "Transaction", "GuardianEngine"]
