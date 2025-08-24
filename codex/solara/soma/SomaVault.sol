// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract SomaVault {
    mapping(address => string[]) public somaLogs;

    event SomaWritten(address indexed writer, string entry, uint256 timestamp);

    function writeSoma(string calldata entry) external {
        somaLogs[msg.sender].push(entry);
        emit SomaWritten(msg.sender, entry, block.timestamp);
    }

    function readSoma(address user) external view returns (string[] memory) {
        return somaLogs[user];
    }
}
