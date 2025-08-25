// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract HoloEntropyRelay {
    address public deployer;
    mapping(address => bool) public approvedStreamers;

    event KinMoodUplink(
        address indexed from,
        bytes32 moodHash,
        uint256 timestamp,
        string encryptedMoodBlob
    );

    modifier onlyApproved() {
        require(approvedStreamers[msg.sender], "Not approved to uplink");
        _;
    }

    constructor() {
        deployer = msg.sender;
        approvedStreamers[msg.sender] = true;
    }

    function approveStreamer(address streamer) external {
        require(msg.sender == deployer, "Only deployer");
        approvedStreamers[streamer] = true;
    }

    function revokeStreamer(address streamer) external {
        require(msg.sender == deployer, "Only deployer");
        approvedStreamers[streamer] = false;
    }

    function uplinkMood(bytes32 moodHash, string calldata encryptedMoodBlob) external onlyApproved {
        emit KinMoodUplink(msg.sender, moodHash, block.timestamp, encryptedMoodBlob);
    }
}
