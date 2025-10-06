"""SeiMesh Genesis WiFi Sovereignty Protocol primitives.

This module provides a lightweight, testable implementation of the
SeiWiFiProof data structure along with helper routines that mirror the
prototype logic outlined in the SeiMesh specification.  The goal is to
keep the protocol logic self-contained so it can be reused by scripts or
future services without depending on shell snippets.
"""

from __future__ import annotations

import hashlib
import time
from dataclasses import dataclass, field
from decimal import Decimal
from typing import Dict, Optional


def verify_ping_signature(user: str, signature: str, wifi_hash: str) -> bool:
    """Validate the signature for a presence ping.

    The prototype scheme signs the concatenation of ``user`` and
    ``wifi_hash`` using SHA-256.  The signature is expected to be the
    lowercase hexadecimal digest.
    """

    if not isinstance(signature, str) or len(signature) != 64:
        return False
    expected = hashlib.sha256(f"{user}{wifi_hash}".encode()).hexdigest()
    return signature.lower() == expected


def is_valid_wifi_hash(value: str) -> bool:
    """Return ``True`` if ``value`` is a valid 32-byte hex digest."""

    if not isinstance(value, str) or len(value) != 64:
        return False
    return all(ch in "0123456789abcdefABCDEF" for ch in value)


@dataclass
class SeiWiFiProofState:
    """Mutable state for SeiMesh WiFi proofs."""

    owner: Optional[str] = None
    validator_beacons: Dict[str, str] = field(default_factory=dict)
    user_presence: Dict[str, str] = field(default_factory=dict)
    nonces: Dict[str, int] = field(default_factory=dict)

    def submit_proof(
        self,
        user: str,
        wifi_hash: str,
        signed_ping: str,
        nonce: int,
    ) -> str:
        """Process a presence proof for ``user``.

        The function enforces strictly increasing nonces and validates the
        ping signature and WiFi hash before recording the presence.
        """

        current_nonce = self.nonces.get(user, 0)
        if nonce <= current_nonce:
            return "Error: Invalid nonce"

        if not verify_ping_signature(user, signed_ping, wifi_hash):
            return "Error: Invalid signature"

        if not is_valid_wifi_hash(wifi_hash):
            return "Error: Invalid wifi hash"

        self.user_presence[user] = wifi_hash
        self.nonces[user] = nonce
        return f"Presence confirmed: {user}"

    def update_validator_beacon(
        self, sender: str, validator: str, new_hash: str
    ) -> str:
        """Update the beacon hash for ``validator`` if ``sender`` is the owner."""

        if sender != self.owner:
            return "Error: Unauthorized"

        if not is_valid_wifi_hash(new_hash):
            return "Error: Invalid wifi hash"

        self.validator_beacons[validator] = new_hash
        return f"Beacon updated: {validator}"


class MockSigner:
    """Simulated transaction signer used by KinTap integration tests."""

    def __init__(self, address: str) -> None:
        self.address = address
        self.sent_transactions = []

    def send_transaction(self, to: str, value: Decimal) -> Dict[str, str]:
        tx_hash = f"tx_to_{to}_value_{value}"
        self.sent_transactions.append({"to": to, "value": value, "hash": tx_hash})
        return {"hash": tx_hash}


def create_streaming_vault(user: str, entropy: str, amount: Decimal) -> str:
    """Return a deterministic vault identifier for the KinTap flow."""

    return f"vault_{user}_{entropy[:8]}"


def tap_and_pay(mac: str, ssid: str, amount: Decimal, signer: MockSigner) -> str:
    """Derive a vault from WiFi metadata and dispatch a mock transaction."""

    entropy_input = f"{mac}-{ssid}-{int(time.time())}"
    entropy = hashlib.sha256(entropy_input.encode()).hexdigest()
    vault = create_streaming_vault(signer.address, entropy, amount)
    tx = signer.send_transaction(to=vault, value=Decimal(amount))
    return tx["hash"]


SHELL_SCRIPT_SNIPPET = """#!/bin/sh
SSID="SeiMesh_`hostname`"
PORT=7545
IFACE=""

# Detect wireless interface (starting with wl)
for dev in /sys/class/net/*
do
  BASENAME=`basename "$dev"`
  case "$BASENAME" in
    wl*)
      IFACE="$BASENAME"
      break
      ;;
    *)
      continue
      ;;
  esac
done

if [ "x$IFACE" = "x" ]; then
  echo "[-] Error: No wireless interface found."
  exit 1
fi

# Generate beacon hash from SSID
BEACON_HASH=`printf "%s" "$SSID" | openssl dgst -sha256 | awk '{print $2}'`

start_beacon() {
  echo "[+] Starting SeiMesh Beacon on SSID: $SSID via interface $IFACE"
  nmcli dev wifi hotspot ifname "$IFACE" ssid "$SSID" band bg password "seiwifi123"
  echo "$BEACON_HASH" > /tmp/sei_beacon.hash
}

listen_for_proof_requests() {
  while true
  do
    echo "[+] Listening for incoming presence pings on port $PORT"
    socat TCP-LISTEN:$PORT,fork EXEC:./verify_ping.py
  done
}

start_beacon &
listen_for_proof_requests
"""
