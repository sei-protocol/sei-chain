// SPDX-License-Identifier: MIT

pragma solidity ^0.8.20;

// NOTE: NOT A REAL IMPLEMENTATION -- DO NOT USE IN PROD
contract MintBurn {
    mapping(address => uint256) public balanceOf;

    function mint() external returns (bool) {
        balanceOf[msg.sender] += 1000000000;
        return true;
    }

    function burn() external returns (bool) {
        require(balanceOf[msg.sender] >= 10000000, "not enough to burn");
        balanceOf[msg.sender] -= 10000000;
        return true;
    }
}
