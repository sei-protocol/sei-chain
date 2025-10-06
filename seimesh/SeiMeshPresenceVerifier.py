"""SeiMesh presence verifier service.

This lightweight Flask application receives WiFi presence proofs from
SeiMeshSyncClient instances and records the latest valid nonce per user. The
hash of the SSID must be whitelisted in ``SoulBeaconRegistry.json`` to prevent
rogue beacons from registering presence events.
"""
from __future__ import annotations

import json
import time
from pathlib import Path
from typing import Any, Dict

from flask import Flask, jsonify, request

app = Flask(__name__)

presence_registry: Dict[str, Dict[str, Any]] = {}
beacon_whitelist: Dict[str, Dict[str, Any]] = {}


@app.route("/ping", methods=["POST"])
def receive_ping():
    """Receive a presence ping and record it if valid."""
    data = request.json or {}
    user = data.get("user")
    ssid_hash = data.get("ssid_hash")
    sig = data.get("signature")
    nonce = data.get("nonce")

    print(f"[PING] user={user}, ssid_hash={ssid_hash}, nonce={nonce}")

    if not user or not ssid_hash or nonce is None:
        return jsonify({"error": "Missing required fields"}), 400

    if not verify_signature(user, sig):
        return jsonify({"error": "Invalid signature"}), 400
    if not is_whitelisted(ssid_hash):
        return jsonify({"error": "SSID not recognized"}), 403

    last_nonce = presence_registry.get(user, {}).get("nonce", -1)
    if nonce <= last_nonce:
        return jsonify({"error": "Replay detected"}), 409

    presence_registry[user] = {
        "ssid_hash": ssid_hash,
        "nonce": nonce,
        "timestamp": time_now(),
    }
    return jsonify({"status": "Presence confirmed", "user": user})


def verify_signature(user: str, sig: str | None) -> bool:
    """Placeholder signature verification hook.

    Real deployments should replace this with signature validation that binds
    the ``user`` identifier to the presence payload. The current stub only
    checks that a signature is provided.
    """

    return bool(sig)


def is_whitelisted(ssid_hash: str) -> bool:
    """Return True if the SSID hash is registered in the beacon whitelist."""

    return ssid_hash in beacon_whitelist


def time_now() -> int:
    """Return the current Unix timestamp."""

    return int(time.time())


def load_beacons(registry_path: Path | None = None) -> None:
    """Populate :data:`beacon_whitelist` from ``SoulBeaconRegistry.json``."""

    global beacon_whitelist
    if registry_path is None:
        registry_path = Path(__file__).resolve().parent / "SoulBeaconRegistry.json"

    try:
        with registry_path.open("r", encoding="utf-8") as fp:
            payload = json.load(fp)
    except FileNotFoundError:
        print(f"[WARN] Beacon registry not found at {registry_path}")
        beacon_whitelist = {}
        return

    beacons = payload.get("beacons", [])
    beacon_whitelist = {item["ssid_hash"]: item for item in beacons if "ssid_hash" in item}
    print(f"[INFO] Loaded {len(beacon_whitelist)} whitelisted beacons")


if __name__ == "__main__":
    load_beacons()
    app.run(host="0.0.0.0", port=5000, debug=True)
