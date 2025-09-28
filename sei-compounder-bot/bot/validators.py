from __future__ import annotations

import json
import random
from pathlib import Path
from typing import Any, Dict, List, Sequence, Tuple

STATE_FILE = Path(__file__).resolve().parent / "validator_state.json"


ValidatorEntry = Tuple[str, float]


def _load_state() -> int:
    if not STATE_FILE.exists():
        return -1
    try:
        data = json.loads(STATE_FILE.read_text())
    except (json.JSONDecodeError, OSError):
        return -1
    return int(data.get("index", -1))


def _save_state(index: int) -> None:
    try:
        STATE_FILE.write_text(json.dumps({"index": index}))
    except OSError:
        pass


def _normalize(validators: Sequence[ValidatorEntry]) -> List[ValidatorEntry]:
    normalized: List[ValidatorEntry] = []
    for address, weight in validators:
        if not address:
            continue
        normalized.append((address, max(float(weight), 0.0)))
    if not normalized:
        raise ValueError("No validators configured")
    return normalized


def _parse_validators(config: Dict[str, Any]) -> List[ValidatorEntry]:
    validators_cfg = config.get("validators", [])
    validators: List[ValidatorEntry] = []
    for entry in validators_cfg:
        if isinstance(entry, str):
            validators.append((entry, 1.0))
        elif isinstance(entry, dict):
            address = entry.get("address")
            weight = entry.get("weight", 1.0)
            validators.append((address, float(weight)))
    return _normalize(validators)


def select_validator(config: Dict[str, Any]) -> str:
    validators = _parse_validators(config)
    strategy = str(config.get("validator_strategy", "weighted")).lower()

    if strategy == "round_robin":
        last_index = _load_state()
        next_index = (last_index + 1) % len(validators)
        _save_state(next_index)
        return validators[next_index][0]

    weights = [weight for _, weight in validators]
    addresses = [address for address, _ in validators]

    if strategy == "weighted":
        total = sum(weights)
        if total <= 0:
            return random.choice(addresses)
        normalized = [weight / total for weight in weights]
        return random.choices(addresses, weights=normalized, k=1)[0]

    # default random
    return random.choice(addresses)
