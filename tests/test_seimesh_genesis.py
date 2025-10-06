import hashlib
from decimal import Decimal

from scripts.seimesh_genesis import (
    MockSigner,
    SHELL_SCRIPT_SNIPPET,
    SeiWiFiProofState,
    create_streaming_vault,
    is_valid_wifi_hash,
    tap_and_pay,
    verify_ping_signature,
)


def test_verify_ping_signature_round_trip():
    user = "sei1example"
    wifi_hash = hashlib.sha256(b"ssid").hexdigest()
    signature = hashlib.sha256(f"{user}{wifi_hash}".encode()).hexdigest()
    assert verify_ping_signature(user, signature, wifi_hash)


def test_submit_proof_updates_presence_and_nonce():
    state = SeiWiFiProofState()
    wifi_hash = hashlib.sha256(b"presence").hexdigest()
    signature = hashlib.sha256(f"user{wifi_hash}".encode()).hexdigest()

    result = state.submit_proof("user", wifi_hash, signature, nonce=1)
    assert result == "Presence confirmed: user"
    assert state.user_presence["user"] == wifi_hash
    assert state.nonces["user"] == 1

    # Reusing the same nonce should fail.
    result_retry = state.submit_proof("user", wifi_hash, signature, nonce=1)
    assert result_retry == "Error: Invalid nonce"


def test_update_validator_beacon_requires_owner():
    state = SeiWiFiProofState(owner="admin")
    wifi_hash = hashlib.sha256(b"beacon").hexdigest()

    unauthorized = state.update_validator_beacon("user", "validator", wifi_hash)
    assert unauthorized == "Error: Unauthorized"

    authorized = state.update_validator_beacon("admin", "validator", wifi_hash)
    assert authorized == "Beacon updated: validator"
    assert state.validator_beacons["validator"] == wifi_hash


def test_tap_and_pay_creates_vault_and_transaction():
    signer = MockSigner("sei1owner")
    tx_hash = tap_and_pay("AA:BB:CC", "SeiMesh", Decimal("1.5"), signer)
    assert signer.sent_transactions, "Transaction should be recorded"
    assert signer.sent_transactions[0]["to"].startswith("vault_sei1owner_")
    assert tx_hash == signer.sent_transactions[0]["hash"]


def test_create_streaming_vault_prefix():
    vault = create_streaming_vault("user", "deadbeef" * 4, Decimal("0"))
    assert vault == "vault_user_deadbeef"


def test_shell_script_snippet_contains_expected_commands():
    assert "nmcli dev wifi hotspot" in SHELL_SCRIPT_SNIPPET
    assert "socat TCP-LISTEN" in SHELL_SCRIPT_SNIPPET


def test_is_valid_wifi_hash():
    valid = "a" * 64
    invalid = "g" * 64
    assert is_valid_wifi_hash(valid)
    assert not is_valid_wifi_hash(invalid)


