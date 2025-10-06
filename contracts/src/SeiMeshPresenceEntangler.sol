// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.19;

import {ISoulMoodOracle} from "./interfaces/ISoulMoodOracle.sol";

/// @title SeiMeshPresenceEntangler
/// @notice 1-of-1 presence attestation contract bound to a specific mesh SSID and mood oracle.
contract SeiMeshPresenceEntangler {
    /// @notice Thrown when a zero address is supplied where it is not allowed.
    error ZeroAddress();

    /// @notice Thrown when a caller without validator privileges attempts a restricted action.
    error Unauthorized();

    /// @notice Thrown when entanglement is requested while one is already active.
    error AlreadyEntangled();

    /// @notice Thrown when no entanglement is active but an action requires one.
    error NoActiveEntanglement();

    /// @notice Thrown when a proof was already generated and stored.
    error ProofAlreadyCommitted();

    /// @notice Address with permission to entangle and release souls.
    address public immutable validator;

    /// @notice Hash of the mesh SSID this contract is bound to.
    bytes32 public immutable meshSSIDHash;

    /// @notice External oracle providing the current mood for a given soul.
    ISoulMoodOracle public immutable moodOracle;

    /// @notice Emitted when presence is entangled for a user.
    event PresenceEntangled(address indexed user, bytes32 wifiHash, bytes32 moodHash, bytes32 proofId);

    struct PresenceProof {
        bytes32 wifiHash;
        bytes32 moodHash;
        uint64 timestamp;
        bytes32 proofId;
    }

    mapping(address => bool) public hasMinted;
    mapping(address => PresenceProof) public proofs;

    modifier onlyValidator() {
        if (msg.sender != validator) revert Unauthorized();
        _;
    }

    constructor(address _soulOracle, address _validator, string memory ssid) {
        if (_soulOracle == address(0) || _validator == address(0)) revert ZeroAddress();
        moodOracle = ISoulMoodOracle(_soulOracle);
        validator = _validator;
        meshSSIDHash = keccak256(abi.encodePacked(ssid));
    }

    /// @notice Entangles a user’s WiFi presence and current mood into a verifiable proof.
    function entanglePresence(address user, string calldata ssid, uint64 nonce) external onlyValidator {
        if (hasMinted[user]) revert ProofAlreadyCommitted();

        bytes32 wifiHash = keccak256(abi.encodePacked(ssid));
        require(wifiHash == meshSSIDHash, "SSID mismatch");

        bytes32 moodHash = moodOracle.getLatestMoodHash(user);
        bytes32 proofId = keccak256(abi.encodePacked(user, wifiHash, moodHash, nonce));

        proofs[user] = PresenceProof({
            wifiHash: wifiHash,
            moodHash: moodHash,
            timestamp: uint64(block.timestamp),
            proofId: proofId
        });

        hasMinted[user] = true;
        emit PresenceEntangled(user, wifiHash, moodHash, proofId);
    }

    /// @notice View a user’s stored proof.
    function viewProof(address user) external view returns (PresenceProof memory) {
        return proofs[user];
    }

    /// @notice Verify whether a given proof matches a user’s stored data.
    function verifyProof(bytes32 ssidHash, bytes32 moodHash, address user) external view returns (bool) {
        PresenceProof memory p = proofs[user];
        return (p.wifiHash == ssidHash && p.moodHash == moodHash);
    }
}
