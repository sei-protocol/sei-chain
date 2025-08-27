// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract SoulSyncProof {
    struct Proof {
        bytes32 moodHash;
        bytes32 bioHash;
        uint256 timestamp;
    }

    mapping(address => Proof) public proofs;

    event ProofSubmitted(address indexed user, bytes32 moodHash, bytes32 bioHash, uint256 timestamp);

    function submitProof(bytes32 moodHash, bytes32 bioHash) external {
        proofs[msg.sender] = Proof(moodHash, bioHash, block.timestamp);
        emit ProofSubmitted(msg.sender, moodHash, bioHash, block.timestamp);
    }

    function getProof(address user) external view returns (Proof memory) {
        return proofs[user];
    }
}
