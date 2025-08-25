// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract KinKey {
    struct KeyMeta {
        address key;
        bytes32 moodProof;
        uint256 epochId;
        uint256 timestamp;
    }

    mapping(address => KeyMeta) public activeKeys;
    mapping(address => bool) public revoked;

    event KeyRotated(address indexed owner, address newKey, bytes32 moodProof, uint256 timestamp);
    event EpochAuthorized(address indexed owner, uint256 epochId, bytes32 authSig, uint256 timestamp);
    event KeyRevoked(address indexed owner, uint256 timestamp);

    function rotateKey(address pubkey, bytes32 moodProof) external {
        activeKeys[msg.sender] = KeyMeta({
            key: pubkey,
            moodProof: moodProof,
            epochId: 0,
            timestamp: block.timestamp
        });

        emit KeyRotated(msg.sender, pubkey, moodProof, block.timestamp);
    }

    function authorizeEpoch(uint256 epochId, bytes32 authSig) external {
        KeyMeta storage meta = activeKeys[msg.sender];
        meta.epochId = epochId;
        emit EpochAuthorized(msg.sender, epochId, authSig, block.timestamp);
    }

    function revokeKey() external {
        revoked[msg.sender] = true;
        delete activeKeys[msg.sender];
        emit KeyRevoked(msg.sender, block.timestamp);
    }

    function getCurrentKey(address user) external view returns (address) {
        return activeKeys[user].key;
    }
}
