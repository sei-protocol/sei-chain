"""Client-side broadcaster for SeiMesh presence proofs."""

from __future__ import annotations

import argparse
import hashlib
import os
import sys
import time
from typing import Any, Dict, Optional

import requests

DEFAULT_ENDPOINT = "http://127.0.0.1:7545"
DEFAULT_SSID = "SeiMesh_Local"
DEFAULT_MAC = "B8:27:EB:00:00:00"


class ProofSigner:
    """Utility to sign presence proofs with a shared secret."""

    def __init__(self, secret: str | None) -> None:
        self._secret = secret

    def sign(self, user: str, wifi_hash: str, nonce: int) -> str:
        if self._secret:
            message = f"{user}|{wifi_hash}|{nonce}|{self._secret}".encode()
            return hashlib.sha256(message).hexdigest()
        return f"signed({user})"


def generate_wifi_hash(ssid: str, mac: str, *, timestamp: Optional[int] = None) -> str:
    timestamp = timestamp or int(time.time())
    raw = f"{ssid}-{mac}-{timestamp}".encode()
    return hashlib.sha256(raw).hexdigest()


def build_payload(user: str, ssid: str, mac: str, signer: ProofSigner) -> Dict[str, Any]:
    nonce = int(time.time())
    wifi_hash = generate_wifi_hash(ssid, mac, timestamp=nonce)
    return {
        "user": user,
        "wifi_hash": wifi_hash,
        "signed_ping": signer.sign(user, wifi_hash, nonce),
        "nonce": nonce,
    }


def send_proof(endpoint: str, payload: Dict[str, Any], *, timeout: float = 5.0) -> requests.Response:
    headers = {"Content-Type": "application/json"}
    response = requests.post(endpoint, json=payload, headers=headers, timeout=timeout)
    return response


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("user", help="Address or identifier submitting the proof")
    parser.add_argument("--endpoint", default=DEFAULT_ENDPOINT, help="Presence verifier endpoint")
    parser.add_argument("--ssid", default=DEFAULT_SSID, help="WiFi SSID to hash")
    parser.add_argument("--mac", default=DEFAULT_MAC, help="AP MAC address to hash")
    parser.add_argument(
        "--secret",
        default=os.getenv("SEIMESH_CLIENT_SECRET"),
        help="Optional signing secret shared with the verifier",
    )
    parser.add_argument("--interval", type=int, default=0, help="Loop interval in seconds (0 to send once)")
    parser.add_argument("--timeout", type=float, default=5.0, help="HTTP request timeout in seconds")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    signer = ProofSigner(args.secret)

    def _broadcast_once() -> int:
        payload = build_payload(args.user, args.ssid, args.mac, signer)
        try:
            response = send_proof(args.endpoint, payload, timeout=args.timeout)
            response.raise_for_status()
        except requests.RequestException as exc:  # pragma: no cover - network errors
            print(f"Error broadcasting proof: {exc}", file=sys.stderr)
            return 1

        try:
            body = response.json()
        except ValueError:
            body = response.text.strip()

        print("Server response:", body)
        return 0

    if args.interval <= 0:
        return _broadcast_once()

    exit_code = 0
    try:
        while True:
            exit_code = _broadcast_once() or exit_code
            time.sleep(args.interval)
    except KeyboardInterrupt:
        pass
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
