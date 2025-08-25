// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract SeiVault {
    struct EncryptedState {
        bytes32 moodHash;
        bytes cipherData;
        uint256 timestamp;
    }

    mapping(address => EncryptedState) public vaults;

    event VaultSealed(address indexed user, bytes32 moodHash, uint256 timestamp);

    function sealVault(bytes32 moodHash, bytes memory cipherData) external {
        vaults[msg.sender] = EncryptedState({
            moodHash: moodHash,
            cipherData: cipherData,
            timestamp: block.timestamp
        });

        emit VaultSealed(msg.sender, moodHash, block.timestamp);
    }

    function getVault(address user) external view returns (EncryptedState memory) {
        return vaults[user];
    }
}
