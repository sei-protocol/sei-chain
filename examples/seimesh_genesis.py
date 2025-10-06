"""Example implementation of the SeiMesh Genesis WiFi Sovereignty protocol.

This module demonstrates how the pieces described in the project brief could be
stitched together in Python.  The goal is not to provide production ready code
but to give developers a compact reference implementation that mirrors the
pseudo-code that normally lives in design documents.
"""

from __future__ import annotations

import hashlib
import time
from dataclasses import dataclass, field
from decimal import Decimal
from typing import Dict


@dataclass
class SeiWiFiProofContract:
    """Minimal in-memory representation of a SeiWiFiProof contract.

    The data structure mirrors the pseudo smart contract storage that was
    described in the project brief.  A dictionary is used so that the logic can
    be exercised from unit tests or REPL sessions without having to deploy
    anything to an actual blockchain.
    """

    owner: str
    validator_beacons: Dict[str, str] = field(default_factory=dict)
    user_presence: Dict[str, str] = field(default_factory=dict)
    nonces: Dict[str, int] = field(default_factory=dict)

    def submit_proof(self, user: str, wifi_hash: str, signed_ping: str, nonce: int) -> str:
        """Handle a proof submission from a user.

        The nonce check prevents replay attacks, while the verification helper
        stubs stand in for the cryptographic verification that a production
        contract would perform.
        """

        current_nonce = self.nonces.get(user, 0)
        if nonce <= current_nonce:
            return "Error: Invalid nonce"
        if not verify_ping_signature(user, signed_ping):
            return "Error: Invalid signature"
        if not is_valid_wifi_hash(wifi_hash):
            return "Error: Invalid wifi hash"
        self.user_presence[user] = wifi_hash
        self.nonces[user] = nonce
        return f"Presence confirmed: {user}"

    def update_validator_beacon(self, sender: str, validator: str, new_hash: str) -> str:
        """Allow the contract owner to update the beacon hash for a validator."""

        if sender != self.owner:
            return "Error: Unauthorized"
        self.validator_beacons[validator] = new_hash
        return f"Beacon updated: {validator}"


# Dummy signature and WiFi hash verification helpers.  The prints from the
# original pseudo-code are intentionally removed to keep the module concise.
def verify_ping_signature(user: str, sig: str) -> bool:
    """Placeholder signature verification stub."""

    return bool(user and sig)


def is_valid_wifi_hash(hash_: str) -> bool:
    """Placeholder WiFi hash validation stub."""

    return bool(hash_)


# Embedded shell script for broadcasting validator presence and listening for
# proof submissions.  This mirrors the string provided in the project brief and
# can be written to disk if a developer wants to experiment with it.
SHELL_SCRIPT = """#!/bin/sh
SSID="SeiMesh_`hostname`"
PORT=7545
IFACE=""

# Select first wireless interface starting with 'wl'
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

# Exit if no WiFi interface is found
if [ "x$IFACE" = "x" ]; then
  echo "[-] Error: No wireless interface found."
  exit 1
fi

# Compute hash from SSID to use as beacon identifier
BEACON_HASH=`printf "%s" "$SSID" | openssl dgst -sha256 | awk '{print $2}'`

# Start broadcasting validator beacon
start_beacon() {
  echo "[+] Starting SeiMesh Beacon on SSID: $SSID via interface $IFACE"
  nmcli dev wifi hotspot ifname "$IFACE" ssid "$SSID" band bg password "seiwifi123"
  echo "$BEACON_HASH" > /tmp/sei_beacon.hash
}

# Listen for incoming user presence proof requests
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


@dataclass
class MockSigner:
    """A minimal signer that mimics the wallet interface used in the design."""

    address: str

    def send_transaction(self, to: str, value: Decimal) -> Dict[str, str]:
        return {"hash": f"tx_to_{to}_value_{value}"}


def tap_and_pay(mac: str, ssid: str, amount: str, signer: MockSigner) -> str:
    """Compute entropy from WiFi information and trigger a payment.

    The function mirrors the KinTap workflow described in the project brief: it
    derives deterministic entropy from the WiFi environment, uses it to create a
    pseudo vault name and finally sends a transaction using the provided signer.
    """

    entropy_input = f"{mac}-{ssid}-{int(time.time())}"
    entropy = hashlib.sha256(entropy_input.encode()).hexdigest()
    vault = create_streaming_vault(signer.address, entropy, amount)
    tx = signer.send_transaction(to=vault, value=Decimal(amount))
    return tx["hash"]


def create_streaming_vault(user: str, entropy: str, amount: str) -> str:
    """Create a deterministic pseudo vault name based on the entropy."""

    _ = amount  # Present for API compatibility with the design document.
    return f"vault_{user}_{entropy[:8]}"


__all__ = [
    "MockSigner",
    "SHELL_SCRIPT",
    "SeiWiFiProofContract",
    "create_streaming_vault",
    "is_valid_wifi_hash",
    "tap_and_pay",
    "verify_ping_signature",
]
