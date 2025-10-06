# Server-side presence validator for WiFi hash beacon proofs

import sys
import json
from datetime import datetime


def validate_proof(data):
    """Validate the basic structure of a presence proof payload.

    Args:
        data: Parsed JSON payload received from the client.

    Returns:
        str: "ACK" when the payload passes basic validation checks,
            otherwise "REJECT".
    """
    # Basic structure validation
    required_keys = {"wifi_hash", "user", "signed_ping", "nonce"}
    if not required_keys.issubset(data):
        return "REJECT"

    # TODO: Add real signature + hash validation here
    return "ACK"


def main():
    """Read JSON payload from stdin and log the validation outcome."""
    try:
        payload = json.load(sys.stdin)
        verdict = validate_proof(payload)
        print(verdict)
        with open("presence_log.txt", "a", encoding="utf-8") as log:
            log.write(f"{datetime.now()} - {verdict} - {payload}\n")
    except Exception:  # pragma: no cover - defensive catch-all
        print("REJECT")


if __name__ == "__main__":
    main()
