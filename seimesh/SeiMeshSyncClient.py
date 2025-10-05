"""Client helper to send WiFi presence proofs to the SeiMesh verifier."""
from __future__ import annotations

import hashlib
import time
from typing import Dict

import requests

API_ENDPOINT = "http://localhost:5000/ping"


def send_presence_ping(user: str, ssid: str) -> Dict[str, str]:
    """Send a presence ping for ``user`` associated with ``ssid``."""

    wifi_hash = hashlib.sha256(ssid.encode("utf-8")).hexdigest()
    payload = {
        "user": user,
        "ssid_hash": wifi_hash,
        "signature": sign(user, wifi_hash),
        "nonce": int(time.time()),
    }
    response = requests.post(API_ENDPOINT, json=payload, timeout=5)
    print(f"[SYNC] Response: {response.status_code} - {response.text}")
    return response.json()


def sign(user: str, message: str) -> str:
    """Return a mock signature for demonstration purposes."""

    _ = (user, message)
    return "mock_signature"


__all__ = ["send_presence_ping", "sign"]
