// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SimpleStorage {
    uint256 private storedData;

    event SetEvent(uint256 value);

    function set(uint256 value) public {
        storedData = value;
        emit SetEvent(value);
    }

    function get() public view returns (uint256) {
        return storedData;
    }

    function bad() public pure {
        revert();
    }
}