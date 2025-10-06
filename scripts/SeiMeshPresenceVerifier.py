"""SeiMesh presence verification listener.

This module reads a JSON proof payload from standard input, validates the
presence proof, prints ``ACK`` or ``REJECT`` for the caller, and appends the
result to a log file.  The implementation is intentionally lightweight so it
can be wired into existing ``socat`` pipes while still providing structured
validation that can be extended for production deployments.

Expected payload structure::

    {
        "user": "0x...",
        "wifi_hash": "<64 hex characters>",
        "signed_ping": "<signature string>",
        "nonce": 1700000000
    }

Environment variables
=====================
``SEIMESH_PRESENCE_SECRET``
    Optional signing secret shared with clients.  When present, signatures must
    be the SHA-256 digest of ``user|wifi_hash|nonce|secret``.

``SEIMESH_PRESENCE_LOG``
    Optional path for the append-only JSONL log file.  Defaults to
    ``presence_log.txt`` in the current working directory.
"""

from __future__ import annotations

import hashlib
import hmac
import json
import os
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict

ACK = "ACK"
REJECT = "REJECT"
_DEFAULT_MAX_NONCE_SKEW = 5 * 60  # five minutes


@dataclass
class ProofValidationResult:
    """Represents the outcome of a presence proof validation."""

    accepted: bool
    reason: str


def _is_hex_string(value: str, length: int) -> bool:
    return len(value) == length and all(c in "0123456789abcdefABCDEF" for c in value)


def _nonce_is_fresh(nonce: int, *, skew_seconds: int = _DEFAULT_MAX_NONCE_SKEW) -> bool:
    now = time.time()
    # Allow for some network clock skew but reject obviously stale values.
    return now - skew_seconds <= nonce <= now + skew_seconds


def _verify_signature(payload: Dict[str, Any], *, secret: str | None) -> bool:
    signature = payload.get("signed_ping")
    if not isinstance(signature, str) or not signature:
        return False

    user = payload["user"]
    wifi_hash = payload["wifi_hash"]
    nonce = payload["nonce"]

    if secret:
        message = f"{user}|{wifi_hash}|{nonce}|{secret}".encode()
        expected = hashlib.sha256(message).hexdigest()
        return hmac.compare_digest(signature.lower(), expected.lower())

    # Fallback stub signature for development clients.
    return signature == f"signed({user})"


def validate_proof(data: Dict[str, Any], *, secret: str | None = None) -> ProofValidationResult:
    """Validate the supplied presence proof payload."""

    if not isinstance(data, dict):
        return ProofValidationResult(False, "payload_not_dict")

    user = data.get("user")
    if not isinstance(user, str) or not user:
        return ProofValidationResult(False, "invalid_user")

    wifi_hash = data.get("wifi_hash")
    if not isinstance(wifi_hash, str) or not _is_hex_string(wifi_hash, 64):
        return ProofValidationResult(False, "invalid_wifi_hash")

    nonce = data.get("nonce")
    if not isinstance(nonce, int):
        return ProofValidationResult(False, "invalid_nonce")
    if not _nonce_is_fresh(nonce):
        return ProofValidationResult(False, "stale_nonce")

    if not _verify_signature(data, secret=secret):
        return ProofValidationResult(False, "invalid_signature")

    return ProofValidationResult(True, "accepted")


def _log_event(result: ProofValidationResult, payload: Dict[str, Any]) -> None:
    log_path = Path(os.getenv("SEIMESH_PRESENCE_LOG", "presence_log.txt"))
    entry = {
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "accepted": result.accepted,
        "reason": result.reason,
        "payload": payload,
    }
    with log_path.open("a", encoding="utf-8") as log_file:
        log_file.write(json.dumps(entry) + "\n")


def main(argv: list[str] | None = None) -> int:
    secret = os.getenv("SEIMESH_PRESENCE_SECRET")
    raw = sys.stdin.read().strip()
    if not raw:
        print(REJECT)
        return 1

    try:
        payload: Dict[str, Any] = json.loads(raw)
    except json.JSONDecodeError:
        print(REJECT)
        return 1

    result = validate_proof(payload, secret=secret)
    _log_event(result, payload)
    print(ACK if result.accepted else REJECT)
    return 0 if result.accepted else 1


if __name__ == "__main__":
    sys.exit(main())
