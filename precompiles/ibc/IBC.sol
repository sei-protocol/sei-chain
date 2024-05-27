// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant IBC_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001009;

IBC constant IBC_CONTRACT = IBC(
    IBC_PRECOMPILE_ADDRESS
);

interface IBC {
    // Transactions
    function transfer(
        string toAddress,
        string memory port,
        string memory channel,
        string memory denom,
        uint256 amount,
        uint64 revisionNumber,
        uint64 revisionHeight,
        uint64 timeoutTimestamp,
        string memo
    ) external returns (bool success);

    function transferWithDefaultTimeout(
        string toAddress,
        string memory port,
        string memory channel,
        string memory denom,
        uint256 amount,
        string memo
    ) external returns (bool success);
}
