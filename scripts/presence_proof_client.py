#!/usr/bin/env python3
"""Submit SeiMesh presence proofs to a validator beacon.

This utility mirrors the reference snippet from the SeiWiFi Fastlane Layer
specification. It collects local WiFi entropy, pings a validator beacon to
measure latency, and prints a JSON payload that could be forwarded to a Sei
chain endpoint.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import socket
import subprocess
import time
import uuid
from typing import Any, Dict, Optional


def get_wifi_entropy_hash() -> str:
    """Combine MAC, SSID, and a timestamp into a SHA3-256 hash string."""

    mac = get_mac_address()
    ssid = get_current_ssid()
    timestamp = str(int(time.time()))
    entropy = f"{mac}{ssid}{timestamp}"
    return hashlib.sha3_256(entropy.encode()).hexdigest()


def get_mac_address() -> str:
    """Return the device MAC address in colon-separated hex format."""

    node = uuid.getnode()
    octets = [(node >> ele) & 0xFF for ele in range(0, 8 * 6, 8)]
    return ":".join(f"{octet:02x}" for octet in reversed(octets))


def get_current_ssid() -> str:
    """Attempt to read the SSID of the current WiFi connection."""

    override = os.environ.get("SEIMESH_CURRENT_SSID")
    if override:
        return override

    commands = (
        ("iwgetid -r", "iwgetid"),
        ("nmcli -t -f active,ssid dev wifi", "nmcli"),
        (
            "/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport -I",
            "airport",
        ),
    )

    for command, name in commands:
        try:
            output = subprocess.check_output(command, shell=True, stderr=subprocess.DEVNULL)
        except (subprocess.CalledProcessError, FileNotFoundError):
            continue

        ssid = parse_ssid_output(name, output.decode().strip())
        if ssid:
            return ssid

    return "UNKNOWN_SSID"


def parse_ssid_output(command: str, output: str) -> Optional[str]:
    """Extract an SSID string from command output."""

    if not output:
        return None

    if command == "iwgetid":
        return output

    if command == "nmcli":
        for line in output.splitlines():
            if line.startswith("yes:"):
                _, ssid = line.split(":", 1)
                return ssid
        return None

    if command == "airport":
        for line in output.splitlines():
            if " SSID: " in line:
                return line.split("SSID: ", 1)[1].strip()
        return None

    return None


def ping_validator_beacon(host: str, port: int = 8080, timeout: float = 2.0) -> Dict[str, Any]:
    """Establish a TCP connection to the validator beacon and measure latency."""

    start = time.time()
    try:
        with socket.create_connection((host, port), timeout=timeout):
            pass
    except OSError as exc:
        return {"error": str(exc)}

    latency = (time.time() - start) * 1000
    return {
        "latency_ms": round(latency),
        "timestamp": int(time.time()),
        "validator": host,
    }


def submit_presence_proof(validator: str, port: int = 8080) -> Optional[Dict[str, Any]]:
    """Generate and print a presence proof payload for the validator."""

    wifi_hash = get_wifi_entropy_hash()
    beacon = ping_validator_beacon(validator, port=port)

    if "error" in beacon:
        print(f"⚠️  Could not reach validator beacon: {beacon['error']}")
        return None

    proof_payload: Dict[str, Any] = {
        "wifiHash": wifi_hash,
        "timestamp": beacon["timestamp"],
        "latencyMs": beacon["latency_ms"],
        "validator": beacon["validator"],
    }

    print("\n📡 Submitting Presence Proof:")
    print(json.dumps(proof_payload, indent=2))
    return proof_payload


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate SeiMesh presence proofs")
    parser.add_argument("validator", help="Validator beacon host or IP")
    parser.add_argument("--port", type=int, default=8080, help="Validator beacon port (default: 8080)")
    parser.add_argument("--json", action="store_true", help="Output raw JSON on success")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    proof = submit_presence_proof(args.validator, port=args.port)
    if proof is None:
        return 1

    if args.json:
        print(json.dumps(proof))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
