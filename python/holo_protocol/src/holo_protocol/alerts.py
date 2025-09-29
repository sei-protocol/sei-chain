"""Sei alert simulator."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
import itertools
import json
from pathlib import Path
import time
from typing import Iterable, Iterator, List, Optional

DATA_DIR = Path(__file__).resolve().parent / "data"
ALERTS_FILE = DATA_DIR / "sample_alerts.json"


@dataclass(slots=True)
class AlertConfig:
    address: str
    poll_interval: float = 1.5
    limit: Optional[int] = None


@dataclass(slots=True)
class SeiAlert:
    tx_hash: str
    sender: str
    recipient: str
    amount: float
    denom: str
    memo: str
    timestamp: datetime

    @classmethod
    def from_json(cls, payload: dict) -> "SeiAlert":
        return cls(
            tx_hash=payload["tx_hash"],
            sender=payload["sender"],
            recipient=payload["recipient"],
            amount=float(payload["amount"]),
            denom=payload.get("denom", "usei"),
            memo=payload.get("memo", ""),
            timestamp=datetime.fromisoformat(payload["timestamp"]),
        )


class SeiAlertStream:
    """Stream sample alerts for a requested Sei address."""

    def __init__(self, data_source: Path = ALERTS_FILE) -> None:
        self._data_source = data_source

    def _load(self) -> List[SeiAlert]:
        with self._data_source.open("r", encoding="utf-8") as handle:
            payload = json.load(handle)
        return [SeiAlert.from_json(item) for item in payload]

    def stream(self, config: AlertConfig) -> Iterator[SeiAlert]:
        alerts = [alert for alert in self._load() if config.address in {alert.sender, alert.recipient}]
        if not alerts:
            alerts = self._load()
        iterator: Iterable[SeiAlert]
        if config.limit is None:
            iterator = itertools.cycle(alerts)
        else:
            iterator = itertools.islice(itertools.cycle(alerts), config.limit)
        for alert in iterator:
            yield alert
            time.sleep(config.poll_interval)


__all__ = ["AlertConfig", "SeiAlert", "SeiAlertStream", "ALERTS_FILE"]
