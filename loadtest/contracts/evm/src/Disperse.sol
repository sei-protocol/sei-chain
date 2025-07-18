// SPDX-License-Identifier: MIT
pragma solidity ^0.8.17;

// Simple contract that disperses ETH to multiple recipients in a single transaction.
contract Disperse {
    error MismatchArrays();

    constructor() {}

    /// @notice Disperse native ether to a list of recipients.
    /// @param recipients List of addresses to receive ETH.
    /// @param values Corresponding amounts (in wei) each recipient should receive.
    ///               Must be the same length as `recipients`.
    function disperseEther(address[] calldata recipients, uint256[] calldata values) external payable {
        uint256 len = recipients.length;
        if (len != values.length) revert MismatchArrays();

        uint256 remaining = msg.value;
        for (uint256 i; i < len; i++) {
            payable(recipients[i]).transfer(values[i]);
            remaining -= values[i];
        }

        // refund any dust back to caller
        if (remaining > 0) {
            payable(msg.sender).transfer(remaining);
        }
    }
} 