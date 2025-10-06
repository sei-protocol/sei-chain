"""Client that sends signed beacon proofs to the validator endpoint."""

from __future__ import annotations

import hashlib
import time
from typing import Any, Dict

import requests


def generate_wifi_hash(ssid: str, mac: str) -> str:
    """Create a deterministic hash for a WiFi beacon observation."""
    raw = f"{ssid}-{mac}-{int(time.time())}"
    return hashlib.sha256(raw.encode()).hexdigest()


def send_proof(endpoint: str, ssid: str, mac: str, user: str) -> requests.Response:
    """Send a presence proof payload to a validator endpoint."""
    data: Dict[str, Any] = {
        "user": user,
        "wifi_hash": generate_wifi_hash(ssid, mac),
        "signed_ping": f"signed({user})",  # placeholder signature
        "nonce": int(time.time()),
    }
    response = requests.post(endpoint, json=data, timeout=10)
    print("Server response:", response.text)
    return response


if __name__ == "__main__":
    send_proof(
        "http://127.0.0.1:7545",
        "SeiMesh_Local",
        "B8:27:EB:XX:YY:ZZ",
        "0xUserAddr",
    )
