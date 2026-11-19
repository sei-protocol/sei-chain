// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @notice A set of addresses. positions holds each member's 1-based index into
/// values, 0 when absent; removal moves the last value into the gap. Read and
/// written by solstorage.Set.
struct AddressSet {
    address[] values;
    mapping(address => uint256) positions;
}

/// @notice A set of uint256 values, laid out as AddressSet.
struct UintSet {
    uint256[] values;
    mapping(uint256 => uint256) positions;
}
