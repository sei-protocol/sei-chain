// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Echo {
    mapping(uint256 => uint) public timestamps;

    function echo(uint256 value) public pure returns (uint256) {
        return value;
    }

    function setTime(uint256 epoch) public {
        timestamps[epoch] = block.timestamp;
    }
}