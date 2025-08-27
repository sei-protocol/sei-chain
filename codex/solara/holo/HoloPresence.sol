// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract HoloPresence {
    mapping(address => uint256) public lastSeen;
    mapping(address => string) public currentPresence;

    event PresenceUpdated(address indexed user, string presence, uint256 timestamp);

    function updatePresence(string calldata presence) external {
        currentPresence[msg.sender] = presence;
        lastSeen[msg.sender] = block.timestamp;
        emit PresenceUpdated(msg.sender, presence, block.timestamp);
    }

    function getPresence(address user) external view returns (string memory, uint256) {
        return (currentPresence[user], lastSeen[user]);
    }
}
