// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant WASMD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001002;

IWasmd constant WASMD_CONTRACT = IWasmd(
    WASMD_PRECOMPILE_ADDRESS
);

interface IWasmd {
    // Transactions
    function execute(
        string memory contractAddress,
        string memory sender,
        bytes memory msg,
        bytes memory coins
    ) external returns (bytes memory response);

    // Queries
}
