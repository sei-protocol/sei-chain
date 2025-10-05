// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

contract KinPresenceToken {
    address public verifier;
    mapping(address => bool) public hasClaimed;

    event PresenceNFTMinted(address indexed user, bytes32 wifiHash);

    constructor(address _verifier) {
        verifier = _verifier;
    }

    function claim(bytes32 wifiHash) external {
        require(msg.sender == verifier, "Only verifier allowed");
        require(!hasClaimed[tx.origin], "Already claimed");
        hasClaimed[tx.origin] = true;

        emit PresenceNFTMinted(tx.origin, wifiHash);
    }
}
