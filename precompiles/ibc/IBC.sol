// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant IBC_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001009;

IBC constant IBC_CONTRACT = IBC(
    IBC_PRECOMPILE_ADDRESS
);

interface IBC {
    // Transactions
    function transfer(
        address fromAddress,
        address toAddress,
        string memory port,
        string memory channel,
        string memory denom,
        uint256 amount
    ) external returns (bool success);
}
