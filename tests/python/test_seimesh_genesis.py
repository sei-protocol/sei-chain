"""Tests for the example SeiMesh Genesis module."""

from examples.seimesh_genesis import (
    MockSigner,
    SeiWiFiProofContract,
    create_streaming_vault,
    tap_and_pay,
)


def test_submit_proof_updates_presence_and_nonce():
    contract = SeiWiFiProofContract(owner="owner")
    result = contract.submit_proof(
        user="user1", wifi_hash="hash1", signed_ping="sig", nonce=1
    )
    assert result == "Presence confirmed: user1"
    assert contract.user_presence["user1"] == "hash1"
    assert contract.nonces["user1"] == 1


def test_submit_proof_rejects_replay_nonce():
    contract = SeiWiFiProofContract(owner="owner")
    contract.submit_proof("user1", "hash1", "sig", 1)
    assert contract.submit_proof("user1", "hash2", "sig", 1) == "Error: Invalid nonce"


def test_update_validator_beacon_requires_owner():
    contract = SeiWiFiProofContract(owner="owner")
    assert (
        contract.update_validator_beacon("other", "validator", "hash")
        == "Error: Unauthorized"
    )
    assert (
        contract.update_validator_beacon("owner", "validator", "hash")
        == "Beacon updated: validator"
    )
    assert contract.validator_beacons["validator"] == "hash"


def test_tap_and_pay_generates_vault_and_transaction_hash():
    signer = MockSigner(address="addr1")
    tx_hash = tap_and_pay("aa:bb", "SeiMesh", "10", signer)
    assert tx_hash.startswith("tx_to_vault_addr1_")


def test_create_streaming_vault_is_deterministic():
    entropy = "f" * 64
    vault = create_streaming_vault("user", entropy, "10")
    assert vault == "vault_user_ffffffff"
    assert create_streaming_vault("user", entropy, "20") == vault
