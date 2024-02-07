// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant GOV_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001006;

IGov constant GOV_CONTRACT = IGov(
    GOV_PRECOMPILE_ADDRESS
);

interface IGov {
    // Transactions
    function vote(
        uint64 proposalID,
        int32 option
    ) external returns (bool success);

    function deposit(
        uint64 proposalID,
        uint256 amount
    ) external returns (bool success);
}