// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

/// @title SeiMeshPresenceEntangler — 1-of-1 Presence Contract
/// @author Keeper
/// @notice Mints presence soulmarks via WiFi + mood entropy
/// @dev Each mint is entangled with the signer’s ephemeral state

interface ISoulMoodOracle {
    function getLatestMoodHash(address user) external view returns (bytes32);
}

contract SeiMeshPresenceEntangler {
    address public immutable soulOracle;
    address public immutable validator;
    bytes32 public immutable meshSSIDHash;

    mapping(address => bool) public hasMinted;
    mapping(address => PresenceProof) public proofs;

    struct PresenceProof {
        bytes32 wifiHash;
        bytes32 moodHash;
        uint64 timestamp;
        bytes32 proofId;
    }

    event PresenceEntangled(address indexed user, bytes32 wifiHash, bytes32 moodHash, bytes32 proofId);

    modifier onlyValidator() {
        require(msg.sender == validator, "Unauthorized");
        _;
    }

    constructor(address _soulOracle, address _validator, string memory ssid) {
        soulOracle = _soulOracle;
        validator = _validator;
        meshSSIDHash = keccak256(abi.encodePacked(ssid));
    }

    function entanglePresence(address user, string calldata ssid, uint64 nonce) external onlyValidator {
        require(!hasMinted[user], "Already minted");

        bytes32 wifiHash = keccak256(abi.encodePacked(ssid));
        require(wifiHash == meshSSIDHash, "SSID mismatch");

        bytes32 moodHash = ISoulMoodOracle(soulOracle).getLatestMoodHash(user);
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

    function viewProof(address user) external view returns (PresenceProof memory) {
        return proofs[user];
    }

    function verifyProof(bytes32 ssidHash, bytes32 moodHash, address user) external view returns (bool) {
        PresenceProof memory p = proofs[user];
        return (p.wifiHash == ssidHash && p.moodHash == moodHash);
    }
}
