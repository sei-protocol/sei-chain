#!/usr/bin/env python3
"""Verify SeiMesh presence pings.

The daemon expects JSON payloads with the following structure:
{
  "user": "sei1...",
  "wifi_hash": "<64 hex chars>",
  "signature": "<hex sha256(user+wifi_hash)>"
}
"""

import hashlib
import json
import sys
from typing import Any, Dict


def respond(payload: Dict[str, Any]) -> None:
    sys.stdout.write(json.dumps(payload) + "\n")
    sys.stdout.flush()


def is_valid_wifi_hash(value: str) -> bool:
    return len(value) == 64 and all(c in "0123456789abcdefABCDEF" for c in value)


def main() -> None:
    raw = sys.stdin.read().strip()
    if not raw:
        respond({"ok": False, "error": "empty_payload"})
        return

    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        respond({"ok": False, "error": "invalid_json"})
        return

    user = data.get("user")
    wifi_hash = data.get("wifi_hash")
    signature = data.get("signature", "")

    if not isinstance(user, str) or not user:
        respond({"ok": False, "error": "invalid_user"})
        return

    if not isinstance(wifi_hash, str) or not is_valid_wifi_hash(wifi_hash):
        respond({"ok": False, "error": "invalid_wifi_hash"})
        return

    if not isinstance(signature, str) or len(signature) != 64:
        respond({"ok": False, "error": "invalid_signature"})
        return

    expected = hashlib.sha256((user + wifi_hash).encode()).hexdigest()
    if signature.lower() != expected:
        respond({"ok": False, "error": "signature_mismatch"})
        return

    respond({"ok": True, "user": user, "wifi_hash": wifi_hash})


if __name__ == "__main__":
    main()
