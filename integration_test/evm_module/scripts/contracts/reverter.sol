// SPDX-License-Identifier: MIT
// Contract that reverts with Error("user error") on any call. Used by eth_call revert .iox tests.
// To rebuild bytecode: from this directory: solc --bin reverter.sol | tail -1 > reverter_contract.hex

pragma solidity ^0.8.0;

contract Reverter {
    fallback() external payable {
        revert("user error");
    }
}
